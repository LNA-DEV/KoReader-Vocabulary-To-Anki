package ollama

import "encoding/json"

// schemaJSON is the JSON Schema passed to Ollama's structured-output `format` field so
// the model returns strict JSON matching model.Enrichment. Nullable fields accept both
// their type and null.
var schemaJSON = json.RawMessage(`{
  "type": "object",
  "properties": {
    "source_language":          { "type": "string" },
    "surface_form":             { "type": "string" },
    "lemma":                    { "type": "string" },
    "part_of_speech":           { "type": "string" },
    "translation":              { "type": "string" },
    "translation_alternatives": { "type": "array", "items": { "type": "string" } },
    "article":                  { "type": ["string", "null"] },
    "gender":                   { "type": ["string", "null"] },
    "plural":                   { "type": ["string", "null"] },
    "context_translation":      { "type": ["string", "null"] },
    "ipa":                      { "type": ["string", "null"] },
    "confidence":               { "type": "number" }
  },
  "required": ["source_language", "lemma", "part_of_speech", "translation", "confidence"]
}`)
