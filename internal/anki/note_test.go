package anki

import (
	"slices"
	"testing"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

func decksConfig() config.DecksConfig {
	return config.DecksConfig{
		RouteByLanguage: true,
		Default:         "KOReader",
		Template:        "{language_name}::KOReader Vocabulary",
		Fallback:        "KOReader::Uncategorized",
		Map:             map[string]string{"en": "English::KOReader Vocabulary"},
	}
}

func TestResolveDeck(t *testing.T) {
	d := decksConfig()
	cases := []struct{ lang, want string }{
		{"en", "English::KOReader Vocabulary"}, // exact map hit, verbatim
		{"sv", "Swedish::KOReader Vocabulary"}, // template with known ISO name
		{"xx", "XX::KOReader Vocabulary"},      // template, unknown code echoes upper
		{"", "KOReader::Uncategorized"},        // empty/unknown → fallback
	}
	for _, c := range cases {
		if got := ResolveDeck(d, c.lang); got != c.want {
			t.Errorf("ResolveDeck(%q) = %q, want %q", c.lang, got, c.want)
		}
	}
}

func TestResolveDeckRoutingDisabled(t *testing.T) {
	d := decksConfig()
	d.RouteByLanguage = false
	if got := ResolveDeck(d, "en"); got != "KOReader" {
		t.Errorf("routing off should use default, got %q", got)
	}
}

func baseConfig() config.Config {
	cfg := config.Defaults()
	cfg.Anki.Decks = decksConfig()
	cfg.Anki.Tags = []string{"koreader"}
	cfg.Target = config.TargetConfig{Language: "en", Name: "English"}
	return cfg
}

func TestBuildNoteFieldsAndTags(t *testing.T) {
	cfg := baseConfig()
	cfg.Features.Alternatives = true
	entry := model.VocabEntry{Surface: "gouging", Context: "a wall of white bone, gouging deep.", Book: "ACOWAR"}
	enr := model.Enrichment{
		SourceLanguage: "en", Lemma: "gouge", PartOfSpeech: "verb",
		Translation: "to dig out forcibly", TranslationAlternatives: []string{"dig out", "scoop"},
		Confidence: 0.9,
	}
	n := BuildNote(cfg, entry, enr, false)

	if n.DeckName != "English::KOReader Vocabulary" {
		t.Errorf("deck = %q", n.DeckName)
	}
	if n.Fields["Word"] != "gouging" || n.Fields["Lemma"] != "gouge" || n.Fields["Translation"] != "to dig out forcibly" {
		t.Errorf("core fields wrong: %+v", n.Fields)
	}
	if n.Fields["Extra"] != "Synonyms: dig out, scoop" {
		t.Errorf("Extra = %q", n.Fields["Extra"])
	}
	if n.Fields["TargetLang"] != "en" || n.Fields["SourceLang"] != "en" {
		t.Errorf("lang fields wrong: %+v", n.Fields)
	}
	if !hasTag(n.Tags, "koreader") || !hasTag(n.Tags, "lang:en") {
		t.Errorf("tags = %v", n.Tags)
	}
	if hasTag(n.Tags, "review") {
		t.Errorf("should not have review tag when not low confidence")
	}
	if n.Options.DuplicateScope != "deck" {
		t.Errorf("duplicateScope = %q", n.Options.DuplicateScope)
	}
}

func TestBuildNoteLowConfidenceReviewTag(t *testing.T) {
	cfg := baseConfig()
	enr := model.Enrichment{SourceLanguage: "sv", Lemma: "springa", Translation: "to run", Confidence: 0.3}
	n := BuildNote(cfg, model.VocabEntry{Surface: "sprang"}, enr, true)
	if !hasTag(n.Tags, "review") {
		t.Errorf("expected review tag, got %v", n.Tags)
	}
	if !hasTag(n.Tags, "lang:sv") {
		t.Errorf("expected lang:sv tag, got %v", n.Tags)
	}
}

func TestBuildNoteOptionalFieldsGatedOff(t *testing.T) {
	cfg := baseConfig()
	cfg.Features.IPA = false
	cfg.Features.ContextTranslation = false
	ipa := "ˈɡaʊdʒ"
	ct := "eine Wand aus weißem Knochen"
	enr := model.Enrichment{
		SourceLanguage: "en", Lemma: "gouge", Translation: "to dig out",
		IPA: &ipa, ContextTranslation: &ct, Confidence: 0.9,
	}
	n := BuildNote(cfg, model.VocabEntry{Surface: "gouging"}, enr, false)
	if n.Fields["IPA"] != "" {
		t.Errorf("IPA should be gated off, got %q", n.Fields["IPA"])
	}
	if n.Fields["ContextTranslation"] != "" {
		t.Errorf("ContextTranslation should be gated off, got %q", n.Fields["ContextTranslation"])
	}
}

func hasTag(tags []string, want string) bool {
	return slices.Contains(tags, want)
}
