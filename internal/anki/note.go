package anki

import (
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// Fields is the ordered field list of the KOReader Vocab note type. Field names are
// target-language-neutral (Translation holds a definition in monolingual mode, a
// translation otherwise; TargetLang records which language that is).
var Fields = []string{
	"Word", "Lemma", "Translation", "Context", "ContextTranslation",
	"Book", "SourceLang", "TargetLang", "IPA", "POS", "Extra",
}

// Note is one AnkiConnect note (yields two cards via the reversible template).
type Note struct {
	DeckName  string            `json:"deckName"`
	ModelName string            `json:"modelName"`
	Fields    map[string]string `json:"fields"`
	Tags      []string          `json:"tags"`
	Options   NoteOptions       `json:"options"`
}

// NoteOptions controls duplicate handling.
type NoteOptions struct {
	AllowDuplicate bool   `json:"allowDuplicate"`
	DuplicateScope string `json:"duplicateScope"`
}

// BuildNote maps a VocabEntry + Enrichment into an Anki note using the given config.
// lowConfidence controls whether a "review" tag is added.
func BuildNote(cfg config.Config, entry model.VocabEntry, enr model.Enrichment, lowConfidence bool) Note {
	extra := ""
	if cfg.Features.Alternatives && len(enr.TranslationAlternatives) > 0 {
		extra = "Synonyms: " + strings.Join(enr.TranslationAlternatives, ", ")
	}

	fields := map[string]string{
		"Word":               entry.Surface,
		"Lemma":              enr.Lemma,
		"Translation":        enr.Translation,
		"Context":            entry.Context,
		"ContextTranslation": deref(enr.ContextTranslation, cfg.Features.ContextTranslation),
		"Book":               entry.Book,
		"SourceLang":         enr.SourceLanguage,
		"TargetLang":         cfg.Target.Language,
		"IPA":                deref(enr.IPA, cfg.Features.IPA),
		"POS":                enr.PartOfSpeech,
		"Extra":              extra,
	}

	tags := append([]string{}, cfg.Anki.Tags...)
	if code := strings.ToLower(strings.TrimSpace(enr.SourceLanguage)); code != "" {
		tags = append(tags, "lang:"+code)
	}
	if lowConfidence {
		tags = append(tags, "review")
	}

	return Note{
		DeckName:  ResolveDeck(cfg.Anki.Decks, enr.SourceLanguage),
		ModelName: cfg.Anki.NoteType,
		Fields:    fields,
		Tags:      tags,
		Options: NoteOptions{
			AllowDuplicate: cfg.Anki.AllowDuplicate,
			DuplicateScope: "deck",
		},
	}
}

// deref returns the pointed-to string when enabled and non-nil, else "".
func deref(p *string, enabled bool) string {
	if !enabled || p == nil {
		return ""
	}
	return *p
}
