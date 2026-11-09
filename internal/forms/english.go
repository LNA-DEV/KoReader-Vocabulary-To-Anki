package forms

import (
	"slices"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// DowngradeEnglish clears the irregular flag on English forms that the simple regular
// rules produce from the lemma ("walked" for "walk"). It never sets the flag, so an
// incomplete rule set can only miss a hallucination, not create one. It returns the
// corrected forms and the forms it downgraded.
func DowngradeEnglish(lemma string, in []model.LemmaForm) ([]model.LemmaForm, []string) {
	out := slices.Clone(in)
	var downgraded []string
	for i, f := range out {
		if !f.Irregular {
			continue
		}
		allRegular := true
		for _, variant := range strings.Split(f.Form, "/") {
			if !isRegularEnglish(lemma, f.Slot, variant) {
				allRegular = false
				break
			}
		}
		if allRegular {
			out[i].Irregular = false
			downgraded = append(downgraded, f.Form)
		}
	}
	return out, downgraded
}

// isRegularEnglish reports whether form is a regular English inflection of lemma for the
// slot. For multi-word lemmas (phrasal verbs) the first word is inflected and the rest
// must be unchanged ("look up" → "looked up").
func isRegularEnglish(lemma, slot, form string) bool {
	lw := strings.Fields(strings.ToLower(lemma))
	fw := strings.Fields(strings.ToLower(form))
	if len(lw) == 0 || len(fw) == 0 {
		return false
	}
	if (slot == "comparative" || slot == "superlative") && len(fw) == len(lw)+1 && slices.Equal(fw[1:], lw) {
		return (slot == "comparative" && fw[0] == "more") || (slot == "superlative" && fw[0] == "most")
	}
	if len(fw) != len(lw) || !slices.Equal(fw[1:], lw[1:]) {
		return false
	}
	head, got := lw[0], fw[0]
	var candidates []string
	switch slot {
	case "past", "past_participle":
		candidates = suffixForms(head, "ed")
	case "plural", "present_3sg":
		candidates = sForms(head)
	case "comparative":
		candidates = suffixForms(head, "er")
	case "superlative":
		candidates = suffixForms(head, "est")
	default:
		return false
	}
	return slices.Contains(candidates, got)
}

// suffixForms applies a vowel-initial suffix (-ed, -er, -est) the regular way: e-drop
// ("bake" → "baked"), y → i after a consonant ("cry" → "cried") and optional final
// consonant doubling after a short vowel ("stop" → "stopped", but also "visit" →
// "visited").
func suffixForms(w, suffix string) []string {
	switch {
	case strings.HasSuffix(w, "e"):
		return []string{w + suffix[1:]}
	case consonantY(w):
		return []string{w[:len(w)-1] + "i" + suffix}
	}
	out := []string{w + suffix}
	if cvc(w) {
		out = append(out, w+w[len(w)-1:]+suffix)
	}
	return out
}

// sForms applies -s/-es the regular way ("dog" → "dogs", "box" → "boxes", "city" →
// "cities", "potato" → "potatoes"/"potatos").
func sForms(w string) []string {
	switch {
	case strings.HasSuffix(w, "s"), strings.HasSuffix(w, "x"), strings.HasSuffix(w, "z"),
		strings.HasSuffix(w, "ch"), strings.HasSuffix(w, "sh"):
		return []string{w + "es"}
	case consonantY(w):
		return []string{w[:len(w)-1] + "ies"}
	case strings.HasSuffix(w, "o"):
		return []string{w + "s", w + "es"}
	}
	return []string{w + "s"}
}

func isVowel(b byte) bool { return strings.IndexByte("aeiou", b) >= 0 }

func isConsonant(b byte) bool { return b >= 'a' && b <= 'z' && !isVowel(b) }

func consonantY(w string) bool {
	return len(w) >= 2 && w[len(w)-1] == 'y' && isConsonant(w[len(w)-2])
}

// cvc reports a consonant-vowel-consonant ending whose last letter may double.
func cvc(w string) bool {
	n := len(w)
	if n < 3 {
		return false
	}
	last := w[n-1]
	return isConsonant(last) && strings.IndexByte("wxy", last) < 0 && isVowel(w[n-2]) && isConsonant(w[n-3])
}
