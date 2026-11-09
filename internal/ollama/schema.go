package ollama

import (
	"encoding/json"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/lemma"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// schemaJSON is the JSON Schema passed to Ollama's structured-output `format` field so
// the model returns strict JSON matching model.Enrichment. Nullable fields accept both
// their type and null. part_of_speech is restricted to lemma.POSClasses so note keys
// stay stable.
var schemaJSON = mustSchema(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"source_language":          map[string]any{"type": "string"},
		"surface_form":             map[string]any{"type": "string"},
		"lemma":                    map[string]any{"type": "string"},
		"part_of_speech":           map[string]any{"type": "string", "enum": lemma.POSClasses},
		"translation":              map[string]any{"type": "string"},
		"translation_alternatives": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"article":                  nullableString(),
		"gender":                   nullableString(),
		"plural":                   nullableString(),
		"context_translation":      nullableString(),
		"ipa":                      nullableString(),
		"confidence":               map[string]any{"type": "number"},
		"seen_as":                  nullableString(),
		"lemma_forms": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slot":      map[string]any{"type": "string", "enum": model.FormSlots},
					"label":     nullableString(),
					"form":      map[string]any{"type": "string"},
					"irregular": map[string]any{"type": "boolean"},
				},
				"required": []string{"slot", "form", "irregular"},
			},
		},
	},
	"required": []string{"source_language", "lemma", "part_of_speech", "translation", "confidence"},
})

// senseSchemaJSON is the schema for the sense check (see SenseResult).
var senseSchemaJSON = mustSchema(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"same_sense": map[string]any{"type": "boolean"},
		"sense":      nullableString(),
		"confidence": map[string]any{"type": "number"},
	},
	"required": []string{"same_sense", "confidence"},
})

func nullableString() map[string]any {
	return map[string]any{"type": []string{"string", "null"}}
}

func mustSchema(s map[string]any) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}
