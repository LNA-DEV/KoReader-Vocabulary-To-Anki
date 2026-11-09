package lemma

import "testing"

func TestNormalizePOS(t *testing.T) {
	cases := map[string]string{
		"verb":              "verb",
		"Verb":              "verb",
		"verb (transitive)": "verb",
		"phrasal verb":      "verb",
		"past participle":   "verb",
		"adverb":            "adverb",  // must not become "verb"
		"pronoun":           "pronoun", // must not become "noun"
		"proper noun":       "noun",
		"adj.":              "adjective",
		"definite article":  "determiner",
		"idiom":             "phrase",
		"":                  "other",
		"gibberish":         "other",
	}
	for in, want := range cases {
		if got := NormalizePOS(in); got != want {
			t.Errorf("NormalizePOS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanKeepsCase(t *testing.T) {
	cases := map[string]string{
		"  Hund ":       "Hund",
		"«Hund»":        "Hund",
		"give   up":     "give up",
		"\"behold.\"":   "behold",
		"sich erinnern": "sich erinnern",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFoldNormalizesUnicode(t *testing.T) {
	composed := "café"    // é as one code point
	decomposed := "café" // e + combining acute
	if Fold(composed) != Fold(decomposed) {
		t.Errorf("NFC folding failed: %q vs %q", Fold(composed), Fold(decomposed))
	}
	if Fold("Hund") != "hund" {
		t.Errorf("Fold(Hund) = %q", Fold("Hund"))
	}
}

func TestKey(t *testing.T) {
	if got := Key("EN ", "Behold", "Verb"); got != "en|behold|verb" {
		t.Errorf("Key = %q", got)
	}
	if Key("en", "lead", "verb") == Key("en", "lead", "noun") {
		t.Error("homographs with different POS must have different keys")
	}
	if Key("en", "kind", "adjective") == Key("de", "Kind", "noun") {
		t.Error("different languages must have different keys")
	}
	if Key("de", "Hund", "noun") != Key("de", " hund ", "Noun") {
		t.Error("case and whitespace must not change the key")
	}
}
