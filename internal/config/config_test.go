package config

import "testing"

func TestNormalizeLanguageCode(t *testing.T) {
	cases := map[string]string{
		"en":       "en",
		"EN":       "en",
		"English":  "en",
		"english ": "en",
		"German":   "de",
		"de":       "de",
		"Swedish":  "sv",
		"klingon":  "klingon", // unknown → passthrough lowercased
		"":         "",
	}
	for in, want := range cases {
		if got := NormalizeLanguageCode(in); got != want {
			t.Errorf("NormalizeLanguageCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFinalizeDerivesTargetName(t *testing.T) {
	c := Defaults()
	c.Target.Language = "de"
	c.Target.Name = "" // blank → should derive
	if err := c.Finalize(); err != nil {
		t.Fatal(err)
	}
	if c.Target.Name != "German" {
		t.Errorf("derived name = %q, want German", c.Target.Name)
	}
}

func TestValidateRejectsBadURL(t *testing.T) {
	c := Defaults()
	c.Ollama.BaseURL = "not-a-url"
	if err := c.Validate(); err == nil {
		t.Error("expected validation error for bad ollama URL")
	}
}

func TestLanguageName(t *testing.T) {
	if LanguageName("de") != "German" {
		t.Errorf("LanguageName(de) = %q", LanguageName("de"))
	}
	if LanguageName("zz") != "ZZ" {
		t.Errorf("unknown code should upper-case, got %q", LanguageName("zz"))
	}
}
