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
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/forms"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/koreader"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/lemma"
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
	r.logDowngrades(entry, postProcess(entry, &enr))
	return enr, nil
}

// postProcess normalizes model output that varies run to run: it maps the source
// language to an ISO code (the model sometimes returns "English" instead of "en"),
// tidies the lemma, maps the part of speech to its class (both feed the note key) and
// backfills the surface form the model occasionally leaves blank. It also cleans up the
// lemma's forms and, for English, clears irregular flags on regular forms; it returns
// the forms it downgraded.
func postProcess(entry model.VocabEntry, enr *model.Enrichment) []string {
	enr.SourceLanguage = config.NormalizeLanguageCode(enr.SourceLanguage)
	enr.Lemma = lemma.Clean(enr.Lemma)
	enr.PartOfSpeech = lemma.NormalizePOS(enr.PartOfSpeech)
	if strings.TrimSpace(enr.SurfaceForm) == "" {
		enr.SurfaceForm = entry.Surface
	}
	if enr.SeenAs != nil && strings.TrimSpace(*enr.SeenAs) == "" {
		enr.SeenAs = nil
	}
	enr.LemmaForms = forms.Normalize(enr.LemmaForms)
	var downgraded []string
	if enr.SourceLanguage == "en" {
		enr.LemmaForms, downgraded = forms.DowngradeEnglish(enr.Lemma, enr.LemmaForms)
	}
	return downgraded
}

// logDowngrades reports forms the English check found to be regular after all.
func (r *Runner) logDowngrades(entry model.VocabEntry, downgraded []string) {
	if len(downgraded) > 0 {
		r.Logger.Info("model marked regular English forms as irregular; corrected", "word", entry.Word, "forms", downgraded)
	}
}

// InitAnki ensures the note type (created or upgraded) and the statically-known decks
// (default, fallback, and every mapped deck) exist. forceTemplates overwrites customized
// card templates and CSS with the shipped version.
func (r *Runner) InitAnki(ctx context.Context, forceTemplates bool) error {
	client := r.newAnki()
	if _, err := client.Version(ctx); err != nil {
		return err
	}
	rep, err := client.EnsureModel(ctx, r.Cfg.Anki.NoteType, forceTemplates)
	if err != nil {
		return err
	}
	r.logModelReport(rep)
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
		rep, err := ank.EnsureModel(ctx, r.Cfg.Anki.NoteType, false)
		if err != nil {
			return err
		}
		r.logModelReport(rep)
	}

	s := &syncer{r: r, ank: ank, oll: oll, st: st}
	plannedAdds := make(map[string]bool) // dry-run: keys an earlier row of this run would add
	var enriched, added, merged, skipped int
	for i, entry := range delta {
		enr, err := oll.Enrich(ctx, entry, r.Cfg.Target.Name)
		if err != nil {
			r.Logger.Warn("skipping word (enrichment failed)", "word", entry.Word, "err", err)
			skipped++
			continue
		}
		r.logDowngrades(entry, postProcess(entry, &enr))
		enriched++
		key := lemma.Key(enr.SourceLanguage, enr.Lemma, enr.PartOfSpeech)
		lowConf := enr.Confidence < r.Cfg.Features.LowConfidenceReviewTag
		note := anki.BuildNote(r.Cfg, entry, enr, lowConf)

		if opts.DryRun {
			action, noteID, err := plannedAction(st, key, plannedAdds)
			if err != nil {
				return err
			}
			r.printDryRun(i+1, entry, enr, note, key, action, noteID)
			continue
		}

		if err := ank.EnsureDeck(ctx, note.DeckName); err != nil {
			r.Logger.Warn("skipping word (ensure deck failed)", "word", entry.Word, "err", err)
			skipped++
			continue
		}
		p, err := s.place(ctx, entry, enr, note, key)
		if err != nil {
			r.Logger.Warn("skipping word (placing note failed)", "word", entry.Word, "lemma_key", key, "err", err)
			skipped++
			continue
		}
		if err := st.Record(entry.Word, entry.TitleID, enr.SourceLanguage, p.noteID, entry.ContentHash, r.Now(), key); err != nil {
			return err
		}
		switch p.action {
		case actionAdd:
			added++
			r.Logger.Info("added note", "word", entry.Word, "lemma", enr.Lemma, "deck", note.DeckName,
				"note_id", p.noteID, "confidence", enr.Confidence, "review", lowConf)
		case actionMerge:
			merged++
			r.Logger.Info("merged into existing note", "word", entry.Word, "lemma", enr.Lemma, "note_id", p.noteID,
				"example_added", p.exampleAdded, "new_sense", p.senseAdded, "review", p.review)
		case actionSkip:
			r.Logger.Info("lemma already has a note; skipped (on_known_lemma = skip)", "word", entry.Word,
				"lemma", enr.Lemma, "note_id", p.noteID)
		}
	}

	r.Logger.Info("sync complete", "enriched", enriched, "added", added, "merged", merged, "skipped", skipped)
	return nil
}

// plannedAction predicts, without contacting Anki, whether a dry-run row would be added
// or merged. It only knows the local lemma index and earlier rows of the same run, so a
// note Anki already has but the index does not would still show as "add".
func plannedAction(st *state.Store, key string, plannedAdds map[string]bool) (string, int64, error) {
	if l, ok, err := st.LookupLemma(key); err != nil {
		return "", 0, err
	} else if ok {
		return actionMerge, l.NoteID, nil
	}
	if plannedAdds[key] {
		return actionMerge, 0, nil
	}
	plannedAdds[key] = true
	return actionAdd, 0, nil
}

// logModelReport tells the user what EnsureModel changed in their note type.
func (r *Runner) logModelReport(rep anki.ModelReport) {
	name := r.Cfg.Anki.NoteType
	if rep.Created {
		r.Logger.Info("created note type", "note_type", name)
		return
	}
	if len(rep.FieldsAdded) > 0 {
		r.Logger.Info("added fields to note type", "note_type", name, "fields", rep.FieldsAdded)
	}
	if rep.LemmaMovedFirst {
		r.Logger.Info("moved Lemma to the first field; Anki's duplicate check now uses the base form", "note_type", name)
	}
	if rep.SchemaChanged() {
		r.Logger.Warn("note type fields changed: Anki will ask for a full upload at the next AnkiWeb sync (sync your other devices first)", "note_type", name)
	}
	if len(rep.TemplatesUpdated) > 0 {
		r.Logger.Info("updated card templates", "note_type", name, "templates", rep.TemplatesUpdated)
	}
	if len(rep.TemplatesSkipped) > 0 {
		r.Logger.Warn("card templates are customized and were not updated; run `koanki init-anki --force-templates` to replace them",
			"note_type", name, "templates", rep.TemplatesSkipped)
	}
	if rep.CSSUpdated {
		r.Logger.Info("updated card styling", "note_type", name)
	}
	if rep.CSSSkipped {
		r.Logger.Warn("card styling is customized and was not updated; run `koanki init-anki --force-templates` to replace it", "note_type", name)
	}
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

func (r *Runner) printDryRun(n int, entry model.VocabEntry, enr model.Enrichment, note anki.Note, key, action string, noteID int64) {
	payload := map[string]any{
		"n":          n,
		"word":       entry.Word,
		"lemma_key":  key,
		"action":     action,
		"deck":       note.DeckName,
		"tags":       note.Tags,
		"enrichment": enr,
		"fields":     note.Fields,
	}
	if noteID != 0 {
		payload["target_note_id"] = noteID
	}
	out, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println(string(out))
}
