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

func TestValidateOnKnownLemma(t *testing.T) {
	c := Defaults()
	if err := c.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	c.Features.OnKnownLemma = "replace"
	if err := c.Validate(); err == nil {
		t.Error("expected error for unknown on_known_lemma")
	}
}

func TestExampleConfigLoads(t *testing.T) {
	c, err := Load("../../config.example.toml")
	if err != nil {
		t.Fatalf("config.example.toml does not parse: %v", err)
	}
	if err := c.Finalize(); err != nil {
		t.Fatalf("config.example.toml does not validate: %v", err)
	}
	d := Defaults()
	if c.Features.OnKnownLemma != d.Features.OnKnownLemma || c.Features.SenseCheck != d.Features.SenseCheck ||
		c.Features.SenseCheckMinConfidence != d.Features.SenseCheckMinConfidence ||
		c.Features.MaxOtherSenses != d.Features.MaxOtherSenses {
		t.Errorf("example features %+v differ from defaults %+v", c.Features, d.Features)
	}
}
