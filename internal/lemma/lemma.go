// Package lemma identifies Anki notes by their base form. A note is keyed by
// (source language, folded lemma, part-of-speech class), so every inflection of a word
// ("ran", "running") maps to the same note while true homographs with different parts
// of speech ("lead" the verb vs. "lead" the metal) stay apart.
package lemma

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// POSClasses is the closed set of part-of-speech classes used in keys and in the LLM
// schema enum.
var POSClasses = []string{
	"noun", "verb", "adjective", "adverb", "pronoun", "determiner",
	"preposition", "conjunction", "interjection", "numeral", "phrase", "other",
}

var posSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(POSClasses))
	for _, p := range POSClasses {
		m[p] = struct{}{}
	}
	return m
}()

// posContains maps free-form POS labels to a class by substring. Order matters:
// "adverb" and "pronoun" must be checked before "verb" and "noun".
var posContains = []struct{ needle, class string }{
	{"adverb", "adverb"},
	{"adjective", "adjective"},
	{"pronoun", "pronoun"},
	{"preposition", "preposition"},
	{"conjunction", "conjunction"},
	{"interjection", "interjection"},
	{"numeral", "numeral"},
	{"number", "numeral"},
	{"determiner", "determiner"},
	{"article", "determiner"},
	{"participle", "verb"},
	{"verb", "verb"},
	{"noun", "noun"},
	{"phrase", "phrase"},
	{"idiom", "phrase"},
	{"expression", "phrase"},
}

var posAbbrev = map[string]string{
	"n": "noun", "v": "verb", "adj": "adjective", "adv": "adverb",
	"prep": "preposition", "conj": "conjunction", "pron": "pronoun",
	"det": "determiner", "num": "numeral", "interj": "interjection",
}

// NormalizePOS maps a part-of-speech label to one of POSClasses. The LLM schema already
// restricts the value to the enum; this keeps keys stable for anything else (older
// notes, tests, a model that ignores the enum). Unknown labels become "other".
func NormalizePOS(pos string) string {
	v := strings.ToLower(strings.TrimSpace(pos))
	if _, ok := posSet[v]; ok {
		return v
	}
	if c, ok := posAbbrev[strings.TrimSuffix(v, ".")]; ok {
		return c
	}
	for _, pc := range posContains {
		if strings.Contains(v, pc.needle) {
			return pc.class
		}
	}
	return "other"
}

// Clean tidies a lemma for display without changing its case: Unicode NFC, trimmed,
// inner whitespace collapsed, surrounding punctuation removed ("«Hund»" → "Hund").
func Clean(s string) string {
	s = norm.NFC.String(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsSpace(r)
	})
}

// Fold is Clean plus lower-casing. It is used only for comparisons and keys; notes keep
// the display form (German nouns stay capitalized).
func Fold(s string) string {
	return strings.ToLower(Clean(s))
}

// Key returns the identity of a note: "lang|folded lemma|pos class",
// e.g. "en|behold|verb".
func Key(sourceLang, lemma, pos string) string {
	return strings.ToLower(strings.TrimSpace(sourceLang)) + "|" + Fold(lemma) + "|" + NormalizePOS(pos)
}
