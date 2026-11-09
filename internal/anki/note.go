package anki

import (
	"html"
	"regexp"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/lemma"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// Fields is the ordered field list of the KOReader Vocab note type (v2). Lemma is first,
// so Anki's duplicate check works on the base form. Field names are target-language-
// neutral (Translation holds a definition in monolingual mode, a translation otherwise;
// TargetLang records which language that is). SeenAs and Forms are filled by a later
// phase; they exist now so the note type changes only once.
var Fields = []string{
	"Lemma", "Word", "SeenAs", "Forms", "Translation", "OtherSenses", "Context", "Examples",
	"ContextTranslation", "Book", "SourceLang", "TargetLang", "IPA", "POS", "Extra",
}

// Note is one AnkiConnect note (yields two cards via the reversible template).
type Note struct {
	DeckName  string            `json:"deckName"`
	ModelName string            `json:"modelName"`
	Fields    map[string]string `json:"fields"`
	Tags      []string          `json:"tags"`
	Options   NoteOptions       `json:"options"`
}

// NoteOptions controls duplicate handling.
type NoteOptions struct {
	AllowDuplicate bool   `json:"allowDuplicate"`
	DuplicateScope string `json:"duplicateScope"`
}

// BuildNote maps a VocabEntry + Enrichment into an Anki note using the given config.
// lowConfidence controls whether a "review" tag is added. All values are HTML-escaped,
// because Anki fields are HTML.
func BuildNote(cfg config.Config, entry model.VocabEntry, enr model.Enrichment, lowConfidence bool) Note {
	esc := html.EscapeString
	extra := ""
	if cfg.Features.Alternatives && len(enr.TranslationAlternatives) > 0 {
		extra = "Synonyms: " + esc(strings.Join(enr.TranslationAlternatives, ", "))
	}

	fields := map[string]string{
		"Lemma":              esc(lemma.Clean(enr.Lemma)),
		"Word":               esc(entry.Surface),
		"SeenAs":             "",
		"Forms":              "",
		"Translation":        esc(enr.Translation),
		"OtherSenses":        "",
		"Context":            HighlightedContext(entry),
		"Examples":           "",
		"ContextTranslation": esc(deref(enr.ContextTranslation, cfg.Features.ContextTranslation)),
		"Book":               esc(entry.Book),
		"SourceLang":         esc(enr.SourceLanguage),
		"TargetLang":         esc(cfg.Target.Language),
		"IPA":                esc(deref(enr.IPA, cfg.Features.IPA)),
		"POS":                esc(enr.PartOfSpeech),
		"Extra":              extra,
	}

	tags := append([]string{}, cfg.Anki.Tags...)
	if code := strings.ToLower(strings.TrimSpace(enr.SourceLanguage)); code != "" {
		tags = append(tags, "lang:"+code)
	}
	if lowConfidence {
		tags = append(tags, "review")
	}

	return Note{
		DeckName:  ResolveDeck(cfg.Anki.Decks, enr.SourceLanguage),
		ModelName: cfg.Anki.NoteType,
		Fields:    fields,
		Tags:      tags,
		Options: NoteOptions{
			AllowDuplicate: cfg.Anki.AllowDuplicate,
			DuplicateScope: "deck",
		},
	}
}

// deref returns the pointed-to string when enabled and non-nil, else "".
func deref(p *string, enabled bool) string {
	if !enabled || p == nil {
		return ""
	}
	return *p
}

// HighlightedContext returns the entry's sentence as escaped HTML with the seen form in
// <b>. It uses the exact before/after split from the reader when available and falls
// back to the first whole-word, case-insensitive match. "" when there is no sentence.
func HighlightedContext(e model.VocabEntry) string {
	if !e.HasContext() {
		return ""
	}
	esc := html.EscapeString
	surface := strings.TrimSpace(e.Surface)
	if surface == "" {
		return esc(e.Context)
	}
	if (e.ContextBefore != "" || e.ContextAfter != "") &&
		strings.TrimSpace(e.ContextBefore+" "+surface+" "+e.ContextAfter) == e.Context {
		return strings.TrimSpace(esc(e.ContextBefore) + " <b>" + esc(surface) + "</b> " + esc(e.ContextAfter))
	}
	re, err := regexp.Compile(`(?i)(?:^|[^\pL\pN])(` + regexp.QuoteMeta(surface) + `)(?:[^\pL\pN]|$)`)
	if err != nil {
		return esc(e.Context)
	}
	m := re.FindStringSubmatchIndex(e.Context)
	if m == nil {
		return esc(e.Context)
	}
	start, end := m[2], m[3]
	return esc(e.Context[:start]) + "<b>" + esc(e.Context[start:end]) + "</b>" + esc(e.Context[end:])
}

var (
	tagRe         = regexp.MustCompile(`(?s)<[^>]*>`)
	exampleTextRe = regexp.MustCompile(`(?s)<span class="example-text">(.*?)</span>`)
	senseItemRe   = regexp.MustCompile(`(?s)<div class="sense">(.*?)</div>`)
)

// PlainText strips tags and HTML entities from a field value.
func PlainText(fieldHTML string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(fieldHTML, ""))), " ")
}

func foldPlain(fieldHTML string) string {
	return strings.ToLower(PlainText(fieldHTML))
}

// ExampleHTML renders one Examples entry: the highlighted sentence plus the book title.
func ExampleHTML(e model.VocabEntry) string {
	sentence := HighlightedContext(e)
	if sentence == "" {
		return ""
	}
	out := `<div class="example"><span class="example-text">` + sentence + `</span>`
	if b := strings.TrimSpace(e.Book); b != "" {
		out += ` <span class="example-book">— ` + html.EscapeString(b) + `</span>`
	}
	return out + `</div>`
}

// AppendExample adds the entry's sentence to an Examples field value. It reports false
// (and returns the value unchanged) when the entry has no sentence or the same sentence
// is already the note's Context or one of its examples. There is no cap.
func AppendExample(examplesHTML, contextHTML string, e model.VocabEntry) (string, bool) {
	item := ExampleHTML(e)
	if item == "" {
		return examplesHTML, false
	}
	sentence := foldPlain(HighlightedContext(e))
	if sentence == foldPlain(contextHTML) {
		return examplesHTML, false
	}
	for _, m := range exampleTextRe.FindAllStringSubmatch(examplesHTML, -1) {
		if foldPlain(m[1]) == sentence {
			return examplesHTML, false
		}
	}
	return examplesHTML + item, true
}

// SenseItems returns the plain-text entries of an OtherSenses field value.
func SenseItems(otherSensesHTML string) []string {
	var out []string
	for _, m := range senseItemRe.FindAllStringSubmatch(otherSensesHTML, -1) {
		if s := PlainText(m[1]); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SenseAppend is the outcome of AppendSense.
type SenseAppend int

const (
	SenseAdded     SenseAppend = iota // the sense was appended
	SenseDuplicate                    // already the main translation or an existing entry
	SenseListFull                     // maxSenses reached; nothing appended
)

// AppendSense adds a new meaning to an OtherSenses field value unless it repeats the
// translation or an existing entry, or the list already holds maxSenses entries.
func AppendSense(otherSensesHTML, translationHTML, sense string, maxSenses int) (string, SenseAppend) {
	folded := strings.ToLower(strings.Join(strings.Fields(sense), " "))
	if folded == foldPlain(translationHTML) {
		return otherSensesHTML, SenseDuplicate
	}
	items := SenseItems(otherSensesHTML)
	for _, it := range items {
		if strings.ToLower(it) == folded {
			return otherSensesHTML, SenseDuplicate
		}
	}
	if maxSenses > 0 && len(items) >= maxSenses {
		return otherSensesHTML, SenseListFull
	}
	return otherSensesHTML + `<div class="sense">` + html.EscapeString(strings.TrimSpace(sense)) + `</div>`, SenseAdded
}
