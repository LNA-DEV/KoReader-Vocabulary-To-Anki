// Command koanki turns KOReader vocabulary lookups into Anki flashcards via a local
// Ollama model. See `koanki -h` for subcommands.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/koreader"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/pipeline"
)

const usage = `koanki — KOReader → Anki vocabulary pipeline

Usage:
  koanki sync      [flags]                     Read new words, enrich, push Anki notes
  koanki list      [flags] [--all]             Print the delta (or all) vocabulary rows
  koanki enrich    --word W [--context "..."]  Run one word through the LLM, print JSON
  koanki init-anki [flags]                     Create the deck(s) + note type only

Common flags (override config.toml):
  --config PATH     Config file (optional; built-in defaults otherwise)
  --db PATH         KOReader sqlite path
  --model NAME      Ollama model
  --target CODE     Target language ISO 639-1 code (e.g. en, de)
  --ollama-url URL  Ollama base URL
  --anki-url URL    AnkiConnect URL
  --deck NAME       Force every note into one deck (disables language routing)

Sync flags:
  --dry-run         Enrich and print notes, write nothing to Anki
  --limit N         Process at most N new words
`

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "sync":
		err = runSync(ctx, args, logger)
	case "list":
		err = runList(args, logger)
	case "enrich":
		err = runEnrich(ctx, args, logger)
	case "init-anki":
		err = runInitAnki(ctx, args, logger)
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		logger.Error("command failed", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

// overrideFlags holds the common config-override flags shared by subcommands.
type overrideFlags struct {
	configPath string
	db         string
	model      string
	target     string
	ollamaURL  string
	ankiURL    string
	deck       string
}

func registerOverrides(fs *flag.FlagSet) *overrideFlags {
	o := &overrideFlags{}
	fs.StringVar(&o.configPath, "config", "", "path to config.toml")
	fs.StringVar(&o.db, "db", "", "KOReader sqlite path")
	fs.StringVar(&o.model, "model", "", "Ollama model")
	fs.StringVar(&o.target, "target", "", "target language ISO 639-1 code")
	fs.StringVar(&o.ollamaURL, "ollama-url", "", "Ollama base URL")
	fs.StringVar(&o.ankiURL, "anki-url", "", "AnkiConnect URL")
	fs.StringVar(&o.deck, "deck", "", "force a single deck (disables language routing)")
	return o
}

// resolve loads config and applies the override flags (empty flag = keep config value).
// When --config is omitted it auto-discovers a config file from the default locations
// (see config.Discover); if none is found it falls back to built-in defaults.
func (o *overrideFlags) resolve(logger *slog.Logger) (config.Config, error) {
	path := o.configPath
	if path == "" {
		path = config.Discover()
	}
	if path != "" {
		logger.Info("using config file", "path", path)
	} else {
		logger.Info("no config file found; using built-in defaults", "searched", config.DefaultConfigPaths())
	}
	cfg, err := config.Load(path)
	if err != nil {
		return cfg, err
	}
	if o.db != "" {
		cfg.Input.DBPath = o.db
	}
	if o.model != "" {
		cfg.Ollama.Model = o.model
	}
	if o.target != "" {
		cfg.Target.Language = o.target
		cfg.Target.Name = "" // force re-derive from the code
	}
	if o.ollamaURL != "" {
		cfg.Ollama.BaseURL = o.ollamaURL
	}
	if o.ankiURL != "" {
		cfg.Anki.URL = o.ankiURL
	}
	if o.deck != "" {
		cfg.Anki.Decks.RouteByLanguage = false
		cfg.Anki.Decks.Default = o.deck
	}
	if err := cfg.Finalize(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func runSync(ctx context.Context, args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	o := registerOverrides(fs)
	dryRun := fs.Bool("dry-run", false, "enrich and print, write nothing to Anki")
	limit := fs.Int("limit", 0, "process at most N new words (0 = no limit)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := o.resolve(logger)
	if err != nil {
		return err
	}
	return pipeline.New(cfg, logger).Sync(ctx, pipeline.SyncOptions{DryRun: *dryRun, Limit: *limit})
}

func runList(args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	o := registerOverrides(fs)
	all := fs.Bool("all", false, "list all rows, not just the unprocessed delta")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := o.resolve(logger)
	if err != nil {
		return err
	}
	entries, err := pipeline.New(cfg, logger).List(*all)
	if err != nil {
		return err
	}
	for _, e := range entries {
		ctxStr := oneLine(e.Context)
		if ctxStr == "" {
			ctxStr = "(no context)"
		}
		fmt.Printf("%-24s  surface=%-18s  book=%-28s  %s\n", e.Word, e.Surface, truncate(e.Book, 28), truncate(ctxStr, 70))
	}
	fmt.Fprintf(os.Stderr, "%d entries\n", len(entries))
	return nil
}

func runEnrich(ctx context.Context, args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("enrich", flag.ExitOnError)
	o := registerOverrides(fs)
	word := fs.String("word", "", "the word to enrich (required)")
	contextStr := fs.String("context", "", "the sentence the word appeared in")
	book := fs.String("book", "", "book title")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*word) == "" {
		return fmt.Errorf("--word is required")
	}
	cfg, err := o.resolve(logger)
	if err != nil {
		return err
	}
	entry := model.VocabEntry{
		Word:      *word,
		WordClean: koreader.StripSuffix(*word),
		Surface:   *word,
		Context:   *contextStr,
		Book:      *book,
	}
	enr, err := pipeline.New(cfg, logger).EnrichOne(ctx, entry)
	if err != nil {
		return err
	}
	out, _ := json.MarshalIndent(enr, "", "  ")
	fmt.Println(string(out))
	return nil
}

func runInitAnki(ctx context.Context, args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("init-anki", flag.ExitOnError)
	o := registerOverrides(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := o.resolve(logger)
	if err != nil {
		return err
	}
	return pipeline.New(cfg, logger).InitAnki(ctx)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// oneLine collapses embedded whitespace/newlines to single spaces for tidy list output.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
