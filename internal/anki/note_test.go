package anki

import (
	"slices"
	"strings"
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

func TestFieldsLemmaFirst(t *testing.T) {
	if Fields[0] != "Lemma" {
		t.Fatalf("first field = %q, want Lemma (Anki's duplicate check uses it)", Fields[0])
	}
	n := BuildNote(baseConfig(), model.VocabEntry{Surface: "gouging"}, model.Enrichment{Lemma: "gouge", Translation: "x"}, false)
	for _, f := range Fields {
		if _, ok := n.Fields[f]; !ok {
			t.Errorf("BuildNote does not set field %q", f)
		}
	}
	if len(n.Fields) != len(Fields) {
		t.Errorf("BuildNote sets %d fields, note type has %d", len(n.Fields), len(Fields))
	}
}

func TestBuildNoteEscapesAndHighlights(t *testing.T) {
	cfg := baseConfig()
	entry := model.VocabEntry{
		Surface: "beheld", Context: "She beheld <the> ruin & more.",
		ContextBefore: "She", ContextAfter: "<the> ruin & more.", Book: "Tom & Jerry",
	}
	enr := model.Enrichment{SourceLanguage: "en", Lemma: " behold ", PartOfSpeech: "verb", Translation: "to see <something>", Confidence: 0.9}
	n := BuildNote(cfg, entry, enr, false)
	if got := n.Fields["Context"]; got != "She <b>beheld</b> &lt;the&gt; ruin &amp; more." {
		t.Errorf("Context = %q", got)
	}
	if n.Fields["Lemma"] != "behold" {
		t.Errorf("Lemma = %q", n.Fields["Lemma"])
	}
	if n.Fields["Translation"] != "to see &lt;something&gt;" || n.Fields["Book"] != "Tom &amp; Jerry" {
		t.Errorf("fields not escaped: %+v", n.Fields)
	}
}

func TestHighlightedContextFallback(t *testing.T) {
	// No before/after split (e.g. `koanki enrich`): first whole-word match, case-insensitive.
	e := model.VocabEntry{Surface: "ran", Context: "Frank ran. Ran again."}
	if got := HighlightedContext(e); got != "Frank <b>ran</b>. Ran again." {
		t.Errorf("got %q", got)
	}
	if got := HighlightedContext(model.VocabEntry{Surface: "zzz", Context: "no match here"}); got != "no match here" {
		t.Errorf("no match should leave the sentence unbolded, got %q", got)
	}
	if got := HighlightedContext(model.VocabEntry{Surface: "jilt"}); got != "" {
		t.Errorf("no context should give empty, got %q", got)
	}
}

func TestAppendExample(t *testing.T) {
	first := model.VocabEntry{Surface: "ran", Context: "He ran home.", ContextBefore: "He", ContextAfter: "home."}
	second := model.VocabEntry{Surface: "running", Context: "She was running late.", ContextBefore: "She was", ContextAfter: "late.", Book: "B"}
	context := HighlightedContext(first)

	ex, changed := AppendExample("", context, second)
	if !changed || ex != `<div class="example"><span class="example-text">She was <b>running</b> late.</span> <span class="example-book">— B</span></div>` {
		t.Fatalf("first append: changed=%v ex=%q", changed, ex)
	}
	if _, changed := AppendExample(ex, context, second); changed {
		t.Error("the same sentence must not be added twice")
	}
	if _, changed := AppendExample(ex, context, first); changed {
		t.Error("the note's own Context must not be repeated as an example")
	}
	// v1 notes have an unbolded Context; the comparison ignores markup.
	if _, changed := AppendExample("", "He ran home.", first); changed {
		t.Error("plain v1 Context should be recognized as the same sentence")
	}
	if _, changed := AppendExample("", context, model.VocabEntry{Surface: "ran"}); changed {
		t.Error("a row without sentence adds no example")
	}
	third := model.VocabEntry{Surface: "runs", Context: "It runs.", ContextBefore: "It", ContextAfter: "."}
	ex2, changed := AppendExample(ex, context, third)
	if !changed || !strings.HasPrefix(ex2, ex) || strings.Count(ex2, `class="example"`) != 2 {
		t.Errorf("examples must be appended in order without cap: %q", ex2)
	}
}

func TestAppendSense(t *testing.T) {
	translation := "an institution that keeps money"
	s, res := AppendSense("", translation, "the land along a river", 2)
	if res != SenseAdded || s != `<div class="sense">the land along a river</div>` {
		t.Fatalf("res=%v s=%q", res, s)
	}
	if _, res := AppendSense(s, translation, "The land  along a river", 2); res != SenseDuplicate {
		t.Errorf("same sense (case/space) should be a duplicate, got %v", res)
	}
	if _, res := AppendSense(s, translation, "An institution that keeps money", 2); res != SenseDuplicate {
		t.Errorf("the main translation should be a duplicate, got %v", res)
	}
	s, _ = AppendSense(s, translation, "a row of similar things", 2)
	if got := SenseItems(s); !slices.Equal(got, []string{"the land along a river", "a row of similar things"}) {
		t.Errorf("SenseItems = %v", got)
	}
	if _, res := AppendSense(s, translation, "a tilt of an aircraft", 2); res != SenseListFull {
		t.Errorf("third sense with max 2 should be refused, got %v", res)
	}
	if got, _ := AppendSense("", translation, "x < y", 3); got != `<div class="sense">x &lt; y</div>` {
		t.Errorf("sense must be escaped, got %q", got)
	}
}

func TestBuildNoteSeenAsAndForms(t *testing.T) {
	cfg := baseConfig()
	seen := "past tense"
	entry := model.VocabEntry{Surface: "beheld", Context: "She beheld it.", ContextBefore: "She", ContextAfter: "it."}
	enr := model.Enrichment{
		SourceLanguage: "en", Lemma: "behold", PartOfSpeech: "verb", Translation: "to see", Confidence: 0.9,
		SeenAs: &seen,
		LemmaForms: []model.LemmaForm{
			{Slot: "past", Form: "beheld", Irregular: true},
			{Slot: "past_participle", Form: "beheld", Irregular: true},
		},
	}
	n := BuildNote(cfg, entry, enr, false)
	if n.Fields["SeenAs"] != "past tense" {
		t.Errorf("SeenAs = %q", n.Fields["SeenAs"])
	}
	if !strings.Contains(n.Fields["Forms"], `<span class="fv irr"><b>beheld</b></span>`) {
		t.Errorf("Forms = %q", n.Fields["Forms"])
	}

	cfg.Features.Forms = "off"
	cfg.Features.SeenAs = false
	n = BuildNote(cfg, entry, enr, false)
	if n.Fields["Forms"] != "" || n.Fields["SeenAs"] != "" {
		t.Errorf("disabled features must leave fields empty: %q / %q", n.Fields["Forms"], n.Fields["SeenAs"])
	}
}

func TestBuildNoteSeenAsEmptyForLemma(t *testing.T) {
	base := "base form"
	enr := model.Enrichment{SourceLanguage: "en", Lemma: "nook", Translation: "x", SeenAs: &base}
	if got := BuildNote(baseConfig(), model.VocabEntry{Surface: "Nook"}, enr, false).Fields["SeenAs"]; got != "" {
		t.Errorf("SeenAs should be empty when the seen form is the lemma, got %q", got)
	}
}

func TestBuildNoteFormsAlwaysPerLanguage(t *testing.T) {
	cfg := baseConfig() // defaults: de/sv nouns always show gender + plural
	enr := model.Enrichment{
		SourceLanguage: "de", Lemma: "Hund", PartOfSpeech: "noun", Translation: "dog",
		LemmaForms: []model.LemmaForm{
			{Slot: "gender", Form: "der"}, {Slot: "plural", Form: "die Hunde"}, {Slot: "genitive", Form: "des Hundes"},
		},
	}
	got := BuildNote(cfg, model.VocabEntry{Surface: "Hunde"}, enr, false).Fields["Forms"]
	if !strings.Contains(got, "der") || !strings.Contains(got, "die Hunde") || strings.Contains(got, "des Hundes") {
		t.Errorf("German noun Forms = %q", got)
	}
	enr.SourceLanguage = "en" // same regular forms, but no forms_always for English
	if got := BuildNote(cfg, model.VocabEntry{Surface: "Hunde"}, enr, false).Fields["Forms"]; got != "" {
		t.Errorf("regular forms without forms_always should be hidden, got %q", got)
	}
}
