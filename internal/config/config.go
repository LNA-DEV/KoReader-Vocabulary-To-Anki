// Package config loads koanki configuration from a TOML file, applies flag/env
// overrides, and validates the result. Built-in defaults mean a bare run works with
// no config file; nothing about a specific language or model is hard-coded.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the fully-resolved configuration used by every command.
type Config struct {
	Input    InputConfig    `toml:"input"`
	State    StateConfig    `toml:"state"`
	Ollama   OllamaConfig   `toml:"ollama"`
	Anki     AnkiConfig     `toml:"anki"`
	Target   TargetConfig   `toml:"target"`
	Features FeaturesConfig `toml:"features"`
}

type InputConfig struct {
	DBPath         string `toml:"db_path"`
	CopyBeforeRead bool   `toml:"copy_before_read"`
}

type StateConfig struct {
	DBPath string `toml:"db_path"`
}

type OllamaConfig struct {
	BaseURL        string  `toml:"base_url"`
	Model          string  `toml:"model"`
	KeepAlive      string  `toml:"keep_alive"`
	TimeoutSeconds int     `toml:"timeout_seconds"`
	Temperature    float64 `toml:"temperature"`
	MaxRetries     int     `toml:"max_retries"`
}

type DecksConfig struct {
	RouteByLanguage bool              `toml:"route_by_language"`
	Default         string            `toml:"default"`
	Template        string            `toml:"template"`
	Fallback        string            `toml:"fallback"`
	Map             map[string]string `toml:"map"`
}

type AnkiConfig struct {
	URL            string      `toml:"url"`
	NoteType       string      `toml:"note_type"`
	Tags           []string    `toml:"tags"`
	AllowDuplicate bool        `toml:"allow_duplicate"`
	Decks          DecksConfig `toml:"decks"`
}

type TargetConfig struct {
	Language string `toml:"language"` // ISO 639-1 target for translations
	Name     string `toml:"name"`     // human-readable name injected into the prompt
}

type FeaturesConfig struct {
	IPA                    bool    `toml:"ipa"`
	ContextTranslation     bool    `toml:"context_translation"`
	Alternatives           bool    `toml:"alternatives"`
	LowConfidenceReviewTag float64 `toml:"low_confidence_review_tag"`
}

// Defaults returns the built-in configuration. The shipped default target is English
// (monolingual definitions); the default model is gemma3:27b. Both are parameters.
func Defaults() Config {
	return Config{
		Input: InputConfig{
			DBPath:         "./.inbox/vocabulary_builder.sqlite3",
			CopyBeforeRead: true,
		},
		State: StateConfig{DBPath: "./koanki-state.db"},
		Ollama: OllamaConfig{
			BaseURL:        "http://localhost:11434",
			Model:          "gemma3:27b",
			KeepAlive:      "30m",
			TimeoutSeconds: 120,
			Temperature:    0.2,
			MaxRetries:     3,
		},
		Anki: AnkiConfig{
			URL:            "http://localhost:8765",
			NoteType:       "KOReader Vocab",
			Tags:           []string{"koreader"},
			AllowDuplicate: false,
			Decks: DecksConfig{
				RouteByLanguage: true,
				Default:         "KOReader",
				Template:        "{language_name}::KOReader Vocabulary",
				Fallback:        "KOReader::Uncategorized",
				Map:             map[string]string{"en": "English::KOReader Vocabulary"},
			},
		},
		Target: TargetConfig{Language: "en", Name: "English"},
		Features: FeaturesConfig{
			IPA:                    false,
			ContextTranslation:     false,
			Alternatives:           true,
			LowConfidenceReviewTag: 0.6,
		},
	}
}

// DefaultConfigPaths lists the locations Discover searches, in priority order:
//  1. ./config.toml (the current working directory)
//  2. <user config dir>/koanki/config.toml (e.g. ~/.config/koanki/config.toml on Linux)
func DefaultConfigPaths() []string {
	paths := []string{"config.toml"}
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "koanki", "config.toml"))
	}
	return paths
}

// Discover returns the first existing default config path, or "" if none exist.
func Discover() string {
	for _, p := range DefaultConfigPaths() {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// Load starts from Defaults(), decodes the TOML at path over it (when path != ""),
// then leaves flag/env overrides to the caller via Apply* helpers. It does not
// validate — call Validate() after overrides are applied.
func Load(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("reading config %q: %w", path, err)
	}
	return cfg, nil
}

// Finalize fills any derived fields (e.g. target name from its ISO code) and validates.
func (c *Config) Finalize() error {
	if strings.TrimSpace(c.Target.Name) == "" {
		c.Target.Name = LanguageName(c.Target.Language)
	}
	return c.Validate()
}

// Validate checks required paths and URLs.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Input.DBPath) == "" {
		return fmt.Errorf("input.db_path is required")
	}
	if strings.TrimSpace(c.State.DBPath) == "" {
		return fmt.Errorf("state.db_path is required")
	}
	if err := validateURL(c.Ollama.BaseURL, "ollama.base_url"); err != nil {
		return err
	}
	if err := validateURL(c.Anki.URL, "anki.url"); err != nil {
		return err
	}
	if strings.TrimSpace(c.Ollama.Model) == "" {
		return fmt.Errorf("ollama.model is required")
	}
	if strings.TrimSpace(c.Target.Language) == "" {
		return fmt.Errorf("target.language is required")
	}
	return nil
}

func validateURL(raw, field string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute http(s) URL, got %q", field, raw)
	}
	return nil
}

// isoNames is a small ISO 639-1 → English name map so target.name and prompt text
// can be derived from the code alone. Extend as needed; unknown codes echo the code.
var isoNames = map[string]string{
	"en": "English", "de": "German", "fr": "French", "es": "Spanish",
	"it": "Italian", "sv": "Swedish", "nl": "Dutch", "pt": "Portuguese",
	"no": "Norwegian", "da": "Danish", "fi": "Finnish", "pl": "Polish",
	"ru": "Russian", "cs": "Czech", "ja": "Japanese", "zh": "Chinese",
	"ko": "Korean", "tr": "Turkish", "el": "Greek", "hu": "Hungarian",
}

// LanguageName returns the human-readable name for an ISO 639-1 code, or the code
// itself (upper-cased) when unknown.
func LanguageName(code string) string {
	if name, ok := isoNames[strings.ToLower(strings.TrimSpace(code))]; ok {
		return name
	}
	return strings.ToUpper(code)
}

// nameToCode is the reverse of isoNames (lowercased language name → ISO code).
var nameToCode = func() map[string]string {
	m := make(map[string]string, len(isoNames))
	for code, name := range isoNames {
		m[strings.ToLower(name)] = code
	}
	return m
}()

// NormalizeLanguageCode maps a model-returned language — which may be an ISO 639-1 code
// ("en") or an English language name ("English") — to a lowercase ISO code when
// recognizable, otherwise the trimmed/lowercased input. This keeps deck routing and
// tags consistent regardless of which form the model emits.
func NormalizeLanguageCode(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return ""
	}
	if _, ok := isoNames[v]; ok {
		return v
	}
	if code, ok := nameToCode[v]; ok {
		return code
	}
	return v
}
