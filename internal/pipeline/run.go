// Package pipeline wires the stages together: read KOReader → delta-filter against
// state → enrich each new word via Ollama → add Anki notes → record state.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/anki"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/koreader"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/ollama"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/state"
)

// Runner holds shared configuration and logging for the commands.
type Runner struct {
	Cfg    config.Config
	Logger *slog.Logger
	Now    func() int64 // injectable clock; defaults to time.Now().Unix()
}

// New builds a Runner with sensible defaults.
func New(cfg config.Config, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{Cfg: cfg, Logger: logger, Now: func() int64 { return time.Now().Unix() }}
}

// SyncOptions tune a sync run.
type SyncOptions struct {
	DryRun bool
	Limit  int // 0 = no limit
}

// newOllama builds an Ollama client, disabling reasoning for qwen3-family models so
// their <think> output cannot interfere with strict structured JSON.
func (r *Runner) newOllama() *ollama.Client {
	var think *bool
	if strings.Contains(strings.ToLower(r.Cfg.Ollama.Model), "qwen3") {
		f := false
		think = &f
	}
	return ollama.New(ollama.Options{
		BaseURL:     r.Cfg.Ollama.BaseURL,
		Model:       r.Cfg.Ollama.Model,
		KeepAlive:   r.Cfg.Ollama.KeepAlive,
		Temperature: r.Cfg.Ollama.Temperature,
		MaxRetries:  r.Cfg.Ollama.MaxRetries,
		Timeout:     time.Duration(r.Cfg.Ollama.TimeoutSeconds) * time.Second,
		Think:       think,
		Logger:      r.Logger,
	})
}

func (r *Runner) newAnki() *anki.Client {
	return anki.New(r.Cfg.Anki.URL, &http.Client{Timeout: 30 * time.Second})
}

// List reads all KOReader entries; when all is false it returns only the delta.
func (r *Runner) List(all bool) ([]model.VocabEntry, error) {
	entries, stats, err := koreader.List(r.Cfg.Input.DBPath, r.Cfg.Input.CopyBeforeRead)
	if err != nil {
		return nil, err
	}
	r.logInput(stats)
	if all {
		return entries, nil
	}
	st, err := state.Open(r.Cfg.State.DBPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	return st.Delta(entries)
}

// EnrichOne runs a single ad-hoc word through the model (no state, no Anki).
func (r *Runner) EnrichOne(ctx context.Context, entry model.VocabEntry) (model.Enrichment, error) {
	client := r.newOllama()
	if ok, have, err := client.ModelAvailable(ctx); err != nil {
		return model.Enrichment{}, err
	} else if !ok {
		return model.Enrichment{}, fmt.Errorf("model %q not found on Ollama; available: %s", r.Cfg.Ollama.Model, strings.Join(have, ", "))
	}
	enr, err := client.Enrich(ctx, entry, r.Cfg.Target.Name)
	if err != nil {
		return model.Enrichment{}, err
	}
	postProcess(entry, &enr)
	return enr, nil
}

// postProcess normalizes model output that varies run to run: it maps the source
// language to an ISO code (the model sometimes returns "English" instead of "en") and
// backfills the surface form the model occasionally leaves blank.
func postProcess(entry model.VocabEntry, enr *model.Enrichment) {
	enr.SourceLanguage = config.NormalizeLanguageCode(enr.SourceLanguage)
	if strings.TrimSpace(enr.SurfaceForm) == "" {
		enr.SurfaceForm = entry.Surface
	}
}

// InitAnki ensures the note type and the statically-known decks (default, fallback,
// and every mapped deck) exist.
func (r *Runner) InitAnki(ctx context.Context) error {
	client := r.newAnki()
	if _, err := client.Version(ctx); err != nil {
		return err
	}
	if err := client.EnsureModel(ctx, r.Cfg.Anki.NoteType); err != nil {
		return err
	}
	d := r.Cfg.Anki.Decks
	decks := []string{d.Default, d.Fallback}
	for _, deck := range d.Map {
		decks = append(decks, deck)
	}
	for _, deck := range decks {
		if strings.TrimSpace(deck) == "" {
			continue
		}
		if err := client.EnsureDeck(ctx, deck); err != nil {
			return err
		}
		r.Logger.Info("ensured deck", "deck", deck)
	}
	r.Logger.Info("ensured note type", "note_type", r.Cfg.Anki.NoteType)
	return nil
}

// Sync runs the full pipeline. It processes entries one at a time so progress is
// recorded incrementally (a mid-run interruption never re-enriches finished words).
func (r *Runner) Sync(ctx context.Context, opts SyncOptions) error {
	// Read + diagnostics.
	entries, stats, err := koreader.List(r.Cfg.Input.DBPath, r.Cfg.Input.CopyBeforeRead)
	if err != nil {
		return err
	}
	r.logInput(stats)

	// State + delta.
	st, err := state.Open(r.Cfg.State.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	delta, err := st.Delta(entries)
	if err != nil {
		return err
	}
	if opts.Limit > 0 && len(delta) > opts.Limit {
		delta = delta[:opts.Limit]
	}
	r.Logger.Info("delta computed", "new_words", len(delta), "total_words", len(entries), "dry_run", opts.DryRun)
	if len(delta) == 0 {
		r.Logger.Info("nothing to do")
		return nil
	}

	// Preflight (skipped in dry-run for Anki since we never write).
	oll := r.newOllama()
	if ok, have, err := oll.ModelAvailable(ctx); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("model %q not found on Ollama; run `ollama pull %s`. available: %s",
			r.Cfg.Ollama.Model, r.Cfg.Ollama.Model, strings.Join(have, ", "))
	}

	var ank *anki.Client
	if !opts.DryRun {
		ank = r.newAnki()
		if _, err := ank.Version(ctx); err != nil {
			return err
		}
		if err := ank.EnsureModel(ctx, r.Cfg.Anki.NoteType); err != nil {
			return err
		}
	}

	var enriched, added, skipped int
	for i, entry := range delta {
		enr, err := oll.Enrich(ctx, entry, r.Cfg.Target.Name)
		if err != nil {
			r.Logger.Warn("skipping word (enrichment failed)", "word", entry.Word, "err", err)
			skipped++
			continue
		}
		postProcess(entry, &enr)
		enriched++
		lowConf := enr.Confidence < r.Cfg.Features.LowConfidenceReviewTag
		note := anki.BuildNote(r.Cfg, entry, enr, lowConf)

		if opts.DryRun {
			r.printDryRun(i+1, entry, enr, note)
			continue
		}

		if err := ank.EnsureDeck(ctx, note.DeckName); err != nil {
			r.Logger.Warn("skipping word (ensure deck failed)", "word", entry.Word, "err", err)
			skipped++
			continue
		}
		noteID, err := r.addNote(ctx, ank, note)
		if err != nil {
			r.Logger.Warn("skipping word (add note failed)", "word", entry.Word, "err", err)
			skipped++
			continue
		}
		if err := st.Record(entry.Word, entry.TitleID, enr.SourceLanguage, noteID, entry.ContentHash, r.Now()); err != nil {
			return err
		}
		added++
		r.Logger.Info("added note", "word", entry.Word, "deck", note.DeckName, "note_id", noteID, "confidence", enr.Confidence, "review", lowConf)
	}

	r.Logger.Info("sync complete", "enriched", enriched, "added", added, "skipped", skipped)
	return nil
}

// addNote adds a single note, tolerating the duplicate case: if AnkiConnect refuses it
// as a duplicate we still return a 0 id so the word is recorded and never re-enriched.
func (r *Runner) addNote(ctx context.Context, ank *anki.Client, note anki.Note) (int64, error) {
	can, err := ank.CanAddNotes(ctx, []anki.Note{note})
	if err != nil {
		return 0, err
	}
	if len(can) == 1 && !can[0] {
		r.Logger.Info("note already in Anki (duplicate); recording as processed", "word", note.Fields["Word"])
		return 0, nil
	}
	ids, err := ank.AddNotes(ctx, []anki.Note{note})
	if err != nil {
		return 0, err
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return 0, nil
}

func (r *Runner) logInput(stats koreader.Stats) {
	if stats.UserVersion != koreader.ExpectedUserVersion {
		r.Logger.Warn("unexpected KOReader schema version",
			"got", stats.UserVersion, "expected", koreader.ExpectedUserVersion)
	}
	newest := "n/a"
	if stats.NewestCreatedAt > 0 {
		newest = time.Unix(stats.NewestCreatedAt, 0).Format(time.RFC3339)
	}
	r.Logger.Info("read KOReader DB",
		"path", koreader.AbsPath(r.Cfg.Input.DBPath),
		"rows", stats.RowCount,
		"newest_lookup", newest)
}

func (r *Runner) printDryRun(n int, entry model.VocabEntry, enr model.Enrichment, note anki.Note) {
	payload := map[string]any{
		"n":          n,
		"word":       entry.Word,
		"deck":       note.DeckName,
		"tags":       note.Tags,
		"enrichment": enr,
		"fields":     note.Fields,
	}
	out, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println(string(out))
}
