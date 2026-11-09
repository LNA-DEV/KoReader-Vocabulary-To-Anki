package forms

import (
	"slices"
	"testing"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

func f(slot, form string, irregular bool) model.LemmaForm {
	return model.LemmaForm{Slot: slot, Form: form, Irregular: irregular}
}

func str(s string) *string { return &s }

func TestDowngradeEnglish(t *testing.T) {
	cases := []struct {
		lemma, slot, form string
		wantIrregular     bool
	}{
		{"walk", "past", "walked", false},           // the classic hallucination
		{"stop", "past", "stopped", false},          // consonant doubling
		{"visit", "past", "visited", false},         // no doubling
		{"bake", "past_participle", "baked", false}, // e-drop
		{"cry", "past", "cried", false},             // y → i
		{"look up", "past", "looked up", false},     // phrasal verb
		{"box", "plural", "boxes", false},
		{"city", "plural", "cities", false},
		{"big", "comparative", "bigger", false},
		{"beautiful", "superlative", "most beautiful", false},
		{"go", "past", "went", true},
		{"go", "past_participle", "gone", true},
		{"behold", "past", "beheld", true},
		{"lay", "past", "laid", true}, // not "layed"
		{"pay", "past", "paid", true}, // not "payed"
		{"cut", "past", "cut", true},
		{"give up", "past", "gave up", true},
		{"mouse", "plural", "mice", true},
		{"child", "plural", "children", true},
		{"bad", "comparative", "worse", true},
		{"dream", "past", "dreamed / dreamt", true}, // one irregular variant keeps the flag
	}
	for _, c := range cases {
		out, _ := DowngradeEnglish(c.lemma, []model.LemmaForm{f(c.slot, c.form, true)})
		if out[0].Irregular != c.wantIrregular {
			t.Errorf("%s %s %q: irregular = %v, want %v", c.lemma, c.slot, c.form, out[0].Irregular, c.wantIrregular)
		}
	}
}

func TestDowngradeEnglishNeverSetsFlag(t *testing.T) {
	in := []model.LemmaForm{f("past", "went", false)}
	out, downgraded := DowngradeEnglish("go", in)
	if out[0].Irregular || len(downgraded) != 0 {
		t.Errorf("must not add an irregular flag: %+v %v", out, downgraded)
	}
	if out, d := DowngradeEnglish("walk", []model.LemmaForm{f("past", "walked", true)}); !slices.Equal(d, []string{"walked"}) || out[0].Irregular {
		t.Errorf("downgraded = %v", d)
	}
}

func TestNormalize(t *testing.T) {
	in := []model.LemmaForm{
		f("past_participle", "  ist   gegangen ", true),
		f("past", "ging", true),
		f("past", "gingen", true), // second "past" dropped
		f("plural", "", false),    // empty dropped
		f("Mood", "ginge", true),  // unknown slot → other with label
		{Slot: "other", Label: str("imperative"), Form: "geh", Irregular: false},
		f("present_3sg", "geht", false),
	}
	got := Normalize(in)
	want := []model.LemmaForm{
		f("present_3sg", "geht", false),
		f("past", "ging", true),
		f("past_participle", "ist gegangen", true),
		{Slot: "other", Label: str("mood"), Form: "ginge", Irregular: true},
		{Slot: "other", Label: str("imperative"), Form: "geh", Irregular: false},
	}
	if len(got) != len(want) {
		t.Fatalf("Normalize = %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Slot != w.Slot || g.Form != w.Form || g.Irregular != w.Irregular || (w.Label != nil && (g.Label == nil || *g.Label != *w.Label)) {
			t.Errorf("item %d = %+v (label %v), want %+v (label %v)", i, g, g.Label, w, *w.Label)
		}
	}

	many := make([]model.LemmaForm, 0, 10)
	for _, s := range model.FormSlots {
		many = append(many, f(s, "x", false))
	}
	if n := len(Normalize(many)); n != MaxForms {
		t.Errorf("Normalize kept %d forms, want cap %d", n, MaxForms)
	}
}

func TestSelect(t *testing.T) {
	regularNoun := []model.LemmaForm{f("gender", "der", false), f("plural", "die Hunde", false), f("genitive", "des Hundes", false)}
	irregularVerb := []model.LemmaForm{f("past", "went", true), f("past_participle", "gone", true)}
	regularVerb := []model.LemmaForm{f("past", "gouged", false), f("past_participle", "gouged", false)}
	always := []string{"gender", "plural"}

	if got := Select(PolicyIrregular, nil, irregularVerb); len(got) != 2 {
		t.Errorf("irregular verb should show all forms, got %+v", got)
	}
	if got := Select(PolicyIrregular, nil, regularVerb); len(got) != 0 {
		t.Errorf("regular verb should show nothing, got %+v", got)
	}
	if got := Select(PolicyIrregular, always, regularNoun); len(got) != 2 || got[0].Slot != "gender" || got[1].Slot != "plural" {
		t.Errorf("forms_always slots should be shown for a regular noun, got %+v", got)
	}
	if got := Select(PolicyAlways, nil, regularVerb); len(got) != 2 {
		t.Errorf("always should show regular forms, got %+v", got)
	}
	if got := Select(PolicyOff, always, irregularVerb); got != nil {
		t.Errorf("off should show nothing, got %+v", got)
	}
}

func TestRender(t *testing.T) {
	got := Render([]model.LemmaForm{f("past", "beheld", true), f("past_participle", "beheld", true)}, "beheld")
	want := `<span class="fl">past</span> <span class="fv irr"><b>beheld</b></span> · ` +
		`<span class="fl">past participle</span> <span class="fv irr"><b>beheld</b></span>`
	if got != want {
		t.Errorf("Render =\n%s\nwant\n%s", got, want)
	}

	got = Render([]model.LemmaForm{f("gender", "der", false), f("plural", "die Hunde", false)}, "Hunde")
	want = `<span class="fv">der</span> · <span class="fl">plural</span> <span class="fv"><b>die Hunde</b></span>`
	if got != want {
		t.Errorf("Render =\n%s\nwant\n%s", got, want)
	}

	got = Render([]model.LemmaForm{{Slot: "other", Label: str("a<b"), Form: "x & y", Irregular: false}}, "")
	if got != `<span class="fl">a&lt;b</span> <span class="fv">x &amp; y</span>` {
		t.Errorf("Render must escape: %s", got)
	}
	if Render(nil, "x") != "" {
		t.Error("no forms should render empty")
	}
	if !isSeenForm("dreamed / dreamt", "Dreamt") || isSeenForm("went", "go") {
		t.Error("isSeenForm variant/case handling wrong")
	}
}
