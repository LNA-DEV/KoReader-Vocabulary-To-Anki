// Package ollama enriches a KOReader word into a structured card via a local Ollama
// model. It uses Ollama's structured-output `format` field (a JSON schema) so responses
// are strict JSON, validates every response, and retries with a corrective nudge.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// Client talks to an Ollama server's /api/chat endpoint.
type Client struct {
	baseURL     string
	model       string
	keepAlive   string
	temperature float64
	maxRetries  int
	think       *bool // when non-nil, sets request "think" (e.g. false to disable qwen3 reasoning)
	httpClient  *http.Client
	logger      *slog.Logger
}

// Options configures a Client.
type Options struct {
	BaseURL     string
	Model       string
	KeepAlive   string
	Temperature float64
	MaxRetries  int
	Timeout     time.Duration
	Think       *bool
	Logger      *slog.Logger
}

// New builds a Client.
func New(o Options) *Client {
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if o.MaxRetries < 1 {
		o.MaxRetries = 1
	}
	return &Client{
		baseURL:     strings.TrimRight(o.BaseURL, "/"),
		model:       o.Model,
		keepAlive:   o.KeepAlive,
		temperature: o.Temperature,
		maxRetries:  o.MaxRetries,
		think:       o.Think,
		httpClient:  &http.Client{Timeout: o.Timeout},
		logger:      logger,
	}
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`
}

type chatRequest struct {
	Model     string          `json:"model"`
	Messages  []Message       `json:"messages"`
	Stream    bool            `json:"stream"`
	Format    json.RawMessage `json:"format,omitempty"`
	KeepAlive string          `json:"keep_alive,omitempty"`
	Think     *bool           `json:"think,omitempty"`
	Options   chatOptions     `json:"options"`
}

type chatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

// ModelAvailable reports whether the configured model is present on the server
// (via /api/tags). Used for the startup preflight check.
func (c *Client) ModelAvailable(ctx context.Context) (bool, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return false, nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("reaching Ollama at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, nil, fmt.Errorf("decoding /api/tags: %w", err)
	}
	names := make([]string, 0, len(body.Models))
	found := false
	for _, m := range body.Models {
		names = append(names, m.Name)
		if m.Name == c.model {
			found = true
		}
	}
	return found, names, nil
}

// Enrich runs one word through the model, retrying on invalid/incomplete JSON, and
// returns the validated Enrichment. targetName is the human-readable target language.
func (c *Client) Enrich(ctx context.Context, entry model.VocabEntry, targetName string) (model.Enrichment, error) {
	messages := BuildMessages(entry, targetName)
	var lastErr error

	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		if attempt > 1 {
			// Corrective nudge appended as a fresh user turn.
			messages = append(messages, Message{
				Role:    "user",
				Content: fmt.Sprintf("Your previous reply was not valid: %v. Reply with ONLY a single JSON object matching the schema.", lastErr),
			})
		}

		enr, err := c.once(ctx, messages)
		if err != nil {
			lastErr = err
			c.logger.Warn("enrich attempt failed", "word", entry.Word, "attempt", attempt, "err", err)
			continue
		}
		if err := enr.Validate(); err != nil {
			lastErr = err
			c.logger.Warn("enrich validation failed", "word", entry.Word, "attempt", attempt, "err", err)
			continue
		}
		return enr, nil
	}
	return model.Enrichment{}, fmt.Errorf("enriching %q failed after %d attempts: %w", entry.Word, c.maxRetries, lastErr)
}

// once performs a single /api/chat call and parses the JSON content.
func (c *Client) once(ctx context.Context, messages []Message) (model.Enrichment, error) {
	reqBody := chatRequest{
		Model:     c.model,
		Messages:  messages,
		Stream:    false,
		Format:    schemaJSON,
		KeepAlive: c.keepAlive,
		Think:     c.think,
		Options:   chatOptions{Temperature: c.temperature},
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return model.Enrichment{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return model.Enrichment{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return model.Enrichment{}, fmt.Errorf("calling Ollama: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.Enrichment{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return model.Enrichment{}, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return model.Enrichment{}, fmt.Errorf("decoding chat response: %w", err)
	}
	if cr.Error != "" {
		return model.Enrichment{}, fmt.Errorf("ollama error: %s", cr.Error)
	}

	content := strings.TrimSpace(cr.Message.Content)
	if content == "" {
		return model.Enrichment{}, fmt.Errorf("empty model content")
	}

	var enr model.Enrichment
	if err := json.Unmarshal([]byte(content), &enr); err != nil {
		return model.Enrichment{}, fmt.Errorf("model did not return valid JSON: %w", err)
	}
	return enr, nil
}
