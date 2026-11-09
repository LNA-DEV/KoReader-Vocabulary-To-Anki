// Package forms turns the principal parts the LLM returns for a lemma (model.LemmaForm)
// into what a card shows: it cleans them up, double-checks English "irregular" flags,
// applies the display policy and renders the Forms field.
package forms

import (
	"html"
	"slices"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// Display policies (features.forms).
const (
	PolicyOff       = "off"       // never show forms
	PolicyIrregular = "irregular" // show all forms when one is irregular, plus "always" slots
	PolicyAlways    = "always"    // show forms for every word that has them
)

// MaxForms caps how many forms one card shows.
const MaxForms = 6

// labels are the card labels per slot (English for every target language). gender has
// no label: the article alone ("der") is clear.
var labels = map[string]string{
	"gender":          "",
	"plural":          "plural",
	"genitive":        "genitive",
	"present_3sg":     "3rd person sg.",
	"past":            "past",
	"past_participle": "past participle",
	"supine":          "supine",
	"comparative":     "comparative",
	"superlative":     "superlative",
}

// Normalize trims forms, drops empty ones, keeps the first item per slot (per label for
// "other"), maps unknown slots to "other", sorts by model.FormSlots and caps at MaxForms.
func Normalize(in []model.LemmaForm) []model.LemmaForm {
	seen := make(map[string]bool)
	var out []model.LemmaForm
	for _, f := range in {
		f.Form = strings.Join(strings.Fields(f.Form), " ")
		if f.Form == "" {
			continue
		}
		f.Slot = strings.ToLower(strings.TrimSpace(f.Slot))
		if !slices.Contains(model.FormSlots, f.Slot) {
			if f.Label == nil && f.Slot != "" {
				label := f.Slot
				f.Label = &label
			}
			f.Slot = "other"
		}
		if f.Label != nil {
			label := strings.Join(strings.Fields(*f.Label), " ")
			f.Label = &label
		}
		id := f.Slot
		if f.Slot == "other" && f.Label != nil {
			id += "|" + strings.ToLower(*f.Label)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, f)
	}
	slices.SortStableFunc(out, func(a, b model.LemmaForm) int {
		return slices.Index(model.FormSlots, a.Slot) - slices.Index(model.FormSlots, b.Slot)
	})
	if len(out) > MaxForms {
		out = out[:MaxForms]
	}
	return out
}

// Select applies the display policy. alwaysSlots are the slots shown even when regular
// (features.forms_always for the source language).
func Select(policy string, alwaysSlots []string, in []model.LemmaForm) []model.LemmaForm {
	switch policy {
	case PolicyOff:
		return nil
	case PolicyAlways:
		return in
	}
	if slices.ContainsFunc(in, func(f model.LemmaForm) bool { return f.Irregular }) {
		return in
	}
	var out []model.LemmaForm
	for _, f := range in {
		if slices.Contains(alwaysSlots, f.Slot) {
			out = append(out, f)
		}
	}
	return out
}

// Render builds the Forms field HTML: "label value · label value". Irregular values get
// the class "irr"; the value matching the form seen in the book is bold. "" for none.
func Render(in []model.LemmaForm, surface string) string {
	parts := make([]string, 0, len(in))
	for _, f := range in {
		label := labels[f.Slot]
		if f.Slot == "other" && f.Label != nil {
			label = *f.Label
		}
		value := html.EscapeString(f.Form)
		if isSeenForm(f.Form, surface) {
			value = "<b>" + value + "</b>"
		}
		class := "fv"
		if f.Irregular {
			class += " irr"
		}
		part := `<span class="` + class + `">` + value + `</span>`
		if label != "" {
			part = `<span class="fl">` + html.EscapeString(label) + `</span> ` + part
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " · ")
}

// isSeenForm reports whether surface is the form, a variant of it ("dreamed / dreamt") or
// one of its words ("gegangen" in "ist gegangen").
func isSeenForm(form, surface string) bool {
	s := strings.ToLower(strings.TrimSpace(surface))
	if s == "" {
		return false
	}
	for _, variant := range strings.Split(strings.ToLower(form), "/") {
		if strings.TrimSpace(variant) == s || slices.Contains(strings.Fields(variant), s) {
			return true
		}
	}
	return false
}
