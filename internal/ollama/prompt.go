package ollama

import (
	"fmt"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// Message is one chat message sent to Ollama.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// systemPrompt returns the lexicographer instructions with the target language woven in.
// The "already in the target language" branch is what makes the monolingual (source ==
// target) mode produce a definition + synonyms instead of a translation.
func systemPrompt(targetName string) string {
	return fmt.Sprintf(`You are a lexicographer building %[1]s flashcards for a language learner. `+
		`You receive a word in two forms — the surface form as it appeared in a sentence and the reader's `+
		`lookup/headword form — plus the sentence itself. The source word may be in any language: infer the `+
		`source language from the sentence, not the word alone. Always produce the entry in %[1]s. Use the `+
		`sentence to pick the correct sense. Return the dictionary base form as the lemma. `+
		`If the word is already in %[1]s, put a concise in-context definition in "translation" and near-synonyms `+
		`in "translation_alternatives"; otherwise put the context-disambiguated %[1]s translation in "translation". `+
		`If %[1]s marks grammatical gender/articles and the translation is a noun, include the definite article in `+
		`"translation" and fill "article", "gender", and "plural"; if %[1]s has no such concept (e.g. English), leave `+
		`those null. Respond with a single JSON object matching the provided schema and nothing else.`, targetName)
}

// userPrompt renders the per-word user message.
func userPrompt(e model.VocabEntry) string {
	sentence := strings.TrimSpace(e.Context)
	if sentence == "" {
		sentence = "(no context available)"
	}
	book := e.Book
	if strings.TrimSpace(book) == "" {
		book = "(unknown)"
	}
	return fmt.Sprintf("Surface form (as it appeared): %s\nLookup form (user's headword): %s\nSentence: %s\nBook: %s",
		e.Surface, e.WordClean, sentence, book)
}

// BuildMessages assembles the system+user messages for one entry. Exposed for golden tests.
func BuildMessages(e model.VocabEntry, targetName string) []Message {
	return []Message{
		{Role: "system", Content: systemPrompt(targetName)},
		{Role: "user", Content: userPrompt(e)},
	}
}
