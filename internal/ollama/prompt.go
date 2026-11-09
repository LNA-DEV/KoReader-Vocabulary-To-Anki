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
// target) mode produce a definition + synonyms instead of a translation. The lemma rules
// decide which note a lookup belongs to, so they are spelled out explicitly.
func systemPrompt(targetName string) string {
	return fmt.Sprintf(`You are a lexicographer building %[1]s flashcards for a language learner. `+
		`You receive a word in two forms — the surface form as it appeared in a sentence and the reader's `+
		`lookup/headword form — plus the sentence itself. The source word may be in any language: infer the `+
		`source language from the sentence, not the word alone. Always produce the entry in %[1]s. Use the `+
		`sentence to pick the correct sense.

The flashcard is about the lemma: the citation form that a learner's dictionary of the source language uses `+
		`as headword. Rules for "lemma":
- Verbs: the infinitive without "to". Keep an inherent reflexive pronoun (German "sich erinnern"). If the `+
		`sentence shows that the word belongs to a separable verb, re-attach the particle (German "fing ... an" → `+
		`"anfangen"). If the word belongs to a phrasal verb in the sentence, the lemma is the whole phrasal verb `+
		`(English "gave" in "She gave up." → "give up"; but "gave" in "She gave him a book." → "give").
- Nouns: the nominative singular without article. Plural-only nouns stay plural ("scissors"). Capitalize `+
		`where the language does (German nouns).
- Adjectives and adverbs: the positive degree, uninflected ("worse" → "bad").
- A participle used as an adjective: the verb, unless dictionaries list the adjective as a headword of its `+
		`own ("tired").
- A selection of several words: the citation form of the whole expression.
- Proper nouns and abbreviations: unchanged.
"part_of_speech" is the part of speech of the lemma (a phrasal verb is a "verb").

Write "translation" for the lemma, not for the inflected surface form. If the word is already in %[1]s, `+
		`put a concise in-context definition in "translation" and near-synonyms in "translation_alternatives"; `+
		`otherwise put the context-disambiguated %[1]s translation in "translation". If %[1]s marks grammatical `+
		`gender/articles and the translation is a noun, include the definite article in "translation" and fill `+
		`"article", "gender", and "plural"; if %[1]s has no such concept (e.g. English), leave those null. `+
		`Respond with a single JSON object matching the provided schema and nothing else.`, targetName)
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

// senseSystemPrompt instructs the model to compare the word's use in a new sentence with
// the meanings a flashcard already teaches.
func senseSystemPrompt(targetName string) string {
	return fmt.Sprintf(`You maintain %[1]s flashcards for a language learner. A flashcard for a word already `+
		`teaches one or more meanings. You receive a new sentence in which the learner met the word again. `+
		`Decide whether the word is used there in one of the known meanings. Different wording of the same `+
		`meaning counts as the same meaning. If it is a known meaning, set "same_sense" to true and "sense" to `+
		`null. Otherwise set "same_sense" to false and put the new meaning in "sense": in %[1]s, written like the `+
		`known meanings (a concise definition if the word is itself %[1]s, otherwise a translation). Set `+
		`"confidence" (0 to 1) to how sure you are. Respond with a single JSON object matching the provided `+
		`schema and nothing else.`, targetName)
}

// senseUserPrompt renders the sense-check request.
func senseUserPrompt(r SenseRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Word: %s (%s, language: %s)\n", r.Lemma, r.PartOfSpeech, r.SourceLanguage)
	b.WriteString("Known meanings on the flashcard:\n")
	for i, s := range r.KnownSenses {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s)
	}
	fmt.Fprintf(&b, "Word as it appears in the new sentence: %s\n", r.Surface)
	fmt.Fprintf(&b, "New sentence: %s", strings.TrimSpace(r.Sentence))
	return b.String()
}

// BuildSenseMessages assembles the system+user messages for a sense check.
func BuildSenseMessages(r SenseRequest, targetName string) []Message {
	return []Message{
		{Role: "system", Content: senseSystemPrompt(targetName)},
		{Role: "user", Content: senseUserPrompt(r)},
	}
}
