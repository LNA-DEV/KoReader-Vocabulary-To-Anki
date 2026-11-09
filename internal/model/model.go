// Package model holds the shared data types that flow through the pipeline:
// a VocabEntry read from KOReader and the Enrichment returned by the LLM.
package model

import (
	"fmt"
	"strings"
)

// VocabEntry is one row from the KOReader vocabulary DB after processing.
type VocabEntry struct {
	Word          string // raw PK from DB, may include a trailing "(N)" — the state key
	WordClean     string // Word with a trailing "(N)" stripped — the base-form hint
	Surface       string // COALESCE(NULLIF(highlight,''), WordClean) — form seen in the sentence
	Context       string // reconstructed sentence around Surface ("" for Dictionary-lookup rows)
	ContextBefore string // trimmed text before Surface in Context (lets cards bold Surface exactly)
	ContextAfter  string // trimmed text after Surface in Context
	Book          string // book title ("" when title_id is null)
	TitleID       int64  // 0 when null
	CreatedAt     int64  // unix seconds (create_time)
	ContentHash   string // hash(Word|Surface|Context) — dedup / optional edit detection
}

// HasContext reports whether the entry carries a usable sentence.
func (e VocabEntry) HasContext() bool {
	return strings.TrimSpace(e.Context) != ""
}

// Enrichment is the structured JSON contract the LLM must return. Field names are
// target-language-neutral: for a monolingual target (source == target) Translation
// holds an in-context definition and TranslationAlternatives holds synonyms; for a
// cross-lingual target it holds the translation and the grammar fields are filled
// when the target language marks them.
type Enrichment struct {
	SourceLanguage          string   `json:"source_language"`          // ISO 639-1, inferred from context
	SurfaceForm             string   `json:"surface_form"`             // echoes input surface
	Lemma                   string   `json:"lemma"`                    // dictionary/base form
	PartOfSpeech            string   `json:"part_of_speech"`           // noun, verb, ...
	Translation             string   `json:"translation"`              // definition or translation in the target language
	TranslationAlternatives []string `json:"translation_alternatives"` // synonyms / other senses (may be empty)
	Article                 *string  `json:"article"`                  // target-language definite article, else null
	Gender                  *string  `json:"gender"`                   // target-language grammatical gender, else null
	Plural                  *string  `json:"plural"`                   // target-language plural form, else null
	ContextTranslation      *string  `json:"context_translation"`      // optional whole-sentence rendering
	IPA                     *string  `json:"ipa"`                      // optional pronunciation
	Confidence              float64  `json:"confidence"`               // 0..1 self-assessed certainty
}

// Validate checks that the model returned a usable enrichment. It is intentionally
// strict about the fields a card cannot do without and lenient about the optional ones.
func (e Enrichment) Validate() error {
	if strings.TrimSpace(e.Lemma) == "" {
		return fmt.Errorf("empty lemma")
	}
	if strings.TrimSpace(e.Translation) == "" {
		return fmt.Errorf("empty translation")
	}
	if strings.TrimSpace(e.SourceLanguage) == "" {
		return fmt.Errorf("empty source_language")
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence %.3f out of range [0,1]", e.Confidence)
	}
	return nil
}
