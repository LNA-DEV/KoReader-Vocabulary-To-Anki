// Package anki talks to the AnkiConnect add-on: it ensures a reversible note type and
// per-language decks exist, then adds notes (deduplicated). All calls are POSTs of the
// shape {action, version:6, params}.
package anki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
)

const apiVersion = 6

// Client is an AnkiConnect client.
type Client struct {
	url          string
	httpClient   *http.Client
	ensuredDecks map[string]struct{} // decks created/verified this run
}

// New builds a Client bound to an AnkiConnect URL.
func New(url string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		url:          strings.TrimRight(url, "/"),
		httpClient:   httpClient,
		ensuredDecks: make(map[string]struct{}),
	}
}

type request struct {
	Action  string `json:"action"`
	Version int    `json:"version"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error"`
}

// call performs one AnkiConnect action and unmarshals result into out (if non-nil).
func (c *Client) call(ctx context.Context, action string, params any, out any) error {
	buf, err := json.Marshal(request{Action: action, Version: apiVersion, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reaching AnkiConnect at %s (is Anki running?): %w", c.url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ankiconnect HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decoding %s response: %w", action, err)
	}
	if r.Error != nil {
		return fmt.Errorf("ankiconnect %s: %s", action, *r.Error)
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("decoding %s result: %w", action, err)
		}
	}
	return nil
}

// Version returns the AnkiConnect API version (health check).
func (c *Client) Version(ctx context.Context) (int, error) {
	var v int
	err := c.call(ctx, "version", nil, &v)
	return v, err
}

// EnsureDeck creates the deck (full "Parent::Child" path) if not already ensured this
// run. AnkiConnect createDeck is idempotent and creates missing parents without
// disturbing an existing parent deck.
func (c *Client) EnsureDeck(ctx context.Context, deck string) error {
	if _, ok := c.ensuredDecks[deck]; ok {
		return nil
	}
	if err := c.call(ctx, "createDeck", map[string]any{"deck": deck}, nil); err != nil {
		return err
	}
	c.ensuredDecks[deck] = struct{}{}
	return nil
}

// modelExists reports whether a note type with the given name exists.
func (c *Client) modelExists(ctx context.Context, name string) (bool, error) {
	var names []string
	if err := c.call(ctx, "modelNames", nil, &names); err != nil {
		return false, err
	}
	return slices.Contains(names, name), nil
}

// EnsureModel creates the reversible note type if it does not already exist.
func (c *Client) EnsureModel(ctx context.Context, modelName string) error {
	exists, err := c.modelExists(ctx, modelName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	params := map[string]any{
		"modelName":     modelName,
		"inOrderFields": Fields,
		"css":           cardCSS,
		"isCloze":       false,
		"cardTemplates": cardTemplates(),
	}
	return c.call(ctx, "createModel", params, nil)
}

// CanAddNotes returns, per note, whether Anki would accept it (false = duplicate/invalid).
func (c *Client) CanAddNotes(ctx context.Context, notes []Note) ([]bool, error) {
	var out []bool
	err := c.call(ctx, "canAddNotes", map[string]any{"notes": notes}, &out)
	return out, err
}

// AddNotes adds notes and returns their new note IDs (null entries → 0 for skipped).
func (c *Client) AddNotes(ctx context.Context, notes []Note) ([]int64, error) {
	var raw []*int64
	if err := c.call(ctx, "addNotes", map[string]any{"notes": notes}, &raw); err != nil {
		return nil, err
	}
	ids := make([]int64, len(raw))
	for i, p := range raw {
		if p != nil {
			ids[i] = *p
		}
	}
	return ids, nil
}

// ResolveDeck picks the deck path for a note given its source language and the config.
func ResolveDeck(d config.DecksConfig, sourceLang string) string {
	if !d.RouteByLanguage {
		return d.Default
	}
	code := strings.ToLower(strings.TrimSpace(sourceLang))
	if code == "" {
		return d.Fallback
	}
	if deck, ok := d.Map[code]; ok {
		return deck
	}
	if d.Template != "" {
		deck := strings.ReplaceAll(d.Template, "{language_code}", code)
		deck = strings.ReplaceAll(deck, "{language_name}", config.LanguageName(code))
		return deck
	}
	return d.Fallback
}
