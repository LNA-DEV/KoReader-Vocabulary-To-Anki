package anki

import "strings"

// cardTemplate mirrors AnkiConnect's createModel cardTemplates entry.
type cardTemplate struct {
	Name  string `json:"Name"`
	Front string `json:"Front"`
	Back  string `json:"Back"`
}

// templateMarker and cssMarker identify the shipped v2 templates and stylesheet. A note
// type whose templates carry the marker (or match the shipped v1 text exactly) is
// considered unmodified and may be upgraded automatically; anything else is a user
// customization and is left alone unless forced.
const (
	templateMarker = "<!-- koanki:templates v2 -->"
	cssMarker      = "/* koanki:css v2 */"
)

// Template names are shared by v1 and v2, so updateModelTemplates can address them.
const (
	recognitionName = "Recognition (word→meaning)"
	productionName  = "Production (meaning→word)"
)

// cardTemplates returns the two directions of the reversible note type (v2). Card 1 goes
// from the base form (+ the sentence it was met in) to its meaning; card 2 from the
// meaning back to the base form. One note therefore yields both study directions.
// OtherSenses stays off the production front so one meaning asks for one word.
func cardTemplates() []cardTemplate {
	return []cardTemplate{
		{
			Name: recognitionName,
			Front: templateMarker + `
<div class="headword">{{Lemma}}</div>
{{#Context}}<div class="context">{{Context}}</div>{{/Context}}`,
			Back: templateMarker + `
{{FrontSide}}
<hr id=answer>
<div class="translation">{{Translation}}</div>
{{#OtherSenses}}<div class="other-senses"><div class="label">Other meanings</div>{{OtherSenses}}</div>{{/OtherSenses}}
{{#POS}}<div class="pos">{{POS}}</div>{{/POS}}
{{#SeenAs}}<div class="seen-as">{{Word}} = {{SeenAs}}</div>{{/SeenAs}}
{{#Forms}}<div class="forms">{{Forms}}</div>{{/Forms}}
{{#Extra}}<div class="extra">{{Extra}}</div>{{/Extra}}
{{#IPA}}<div class="ipa">{{IPA}}</div>{{/IPA}}
{{#ContextTranslation}}<div class="context-translation">{{ContextTranslation}}</div>{{/ContextTranslation}}
{{#Examples}}<div class="examples"><div class="label">Also seen</div>{{Examples}}</div>{{/Examples}}
{{#Book}}<div class="book">{{Book}}</div>{{/Book}}`,
		},
		{
			Name: productionName,
			Front: templateMarker + `
<div class="translation">{{Translation}}</div>
{{#POS}}<div class="pos">{{POS}}</div>{{/POS}}`,
			Back: templateMarker + `
{{FrontSide}}
<hr id=answer>
<div class="headword">{{Lemma}}</div>
{{#Forms}}<div class="forms">{{Forms}}</div>{{/Forms}}
{{#Context}}<div class="context">{{Context}}</div>{{/Context}}
{{#IPA}}<div class="ipa">{{IPA}}</div>{{/IPA}}
{{#Book}}<div class="book">{{Book}}</div>{{/Book}}`,
		},
	}
}

// cardCSS is the v2 stylesheet shared by both cards.
const cardCSS = cssMarker + `
.card {
  font-family: -apple-system, Segoe UI, Roboto, sans-serif;
  font-size: 20px;
  text-align: center;
  color: #202124;
  background: #ffffff;
}
.headword { font-size: 30px; font-weight: 600; margin-bottom: 8px; }
.translation { font-size: 26px; color: #1a73e8; margin: 8px 0; }
.context { font-style: italic; color: #5f6368; margin: 10px auto; max-width: 90%; }
.context b, .example b { font-style: normal; color: #202124; }
.context-translation { color: #5f6368; margin: 6px auto; max-width: 90%; }
.label { font-size: 13px; color: #9aa0a6; text-transform: uppercase; letter-spacing: 0.05em; margin-top: 10px; }
.other-senses .sense { font-size: 18px; color: #1a73e8; margin: 2px 0; }
.pos { font-size: 15px; color: #9aa0a6; text-transform: uppercase; letter-spacing: 0.05em; }
.seen-as { font-size: 16px; color: #5f6368; }
.forms { font-size: 18px; margin: 6px 0; }
.forms .fl { font-size: 13px; color: #9aa0a6; }
.forms .fv.irr { color: #c5221f; }
.extra { font-size: 16px; color: #5f6368; margin-top: 6px; }
.ipa { font-size: 16px; color: #9aa0a6; }
.examples .example { font-size: 16px; font-style: italic; color: #5f6368; margin: 4px auto; max-width: 90%; }
.example-book { font-style: normal; font-size: 13px; color: #bdc1c6; }
.book { font-size: 13px; color: #bdc1c6; margin-top: 12px; }
hr#answer { margin: 14px 0; }
@media (prefers-color-scheme: dark) {
  .card { color: #e8eaed; background: #202124; }
  .headword, .context b, .example b { color: #e8eaed; }
  .translation, .other-senses .sense { color: #8ab4f8; }
  .forms .fv.irr { color: #f28b82; }
}`

// cardTemplatesV1 and cardCSSV1 are the exact v1 texts, kept only to recognize an
// unmodified v1 note type during the upgrade.
func cardTemplatesV1() []cardTemplate {
	return []cardTemplate{
		{
			Name: recognitionName,
			Front: `<div class="word">{{Word}}</div>
{{#Context}}<div class="context">{{Context}}</div>{{/Context}}`,
			Back: `{{FrontSide}}
<hr id=answer>
<div class="translation">{{Translation}}</div>
{{#Lemma}}<div class="lemma">Lemma: {{Lemma}}</div>{{/Lemma}}
{{#POS}}<div class="pos">{{POS}}</div>{{/POS}}
{{#Extra}}<div class="extra">{{Extra}}</div>{{/Extra}}
{{#IPA}}<div class="ipa">{{IPA}}</div>{{/IPA}}
{{#ContextTranslation}}<div class="context-translation">{{ContextTranslation}}</div>{{/ContextTranslation}}
{{#Book}}<div class="book">{{Book}}</div>{{/Book}}`,
		},
		{
			Name: productionName,
			Front: `<div class="translation">{{Translation}}</div>
{{#POS}}<div class="pos">{{POS}}</div>{{/POS}}`,
			Back: `{{FrontSide}}
<hr id=answer>
<div class="word">{{Word}}{{#Lemma}} <span class="lemma">({{Lemma}})</span>{{/Lemma}}</div>
{{#Context}}<div class="context">{{Context}}</div>{{/Context}}
{{#IPA}}<div class="ipa">{{IPA}}</div>{{/IPA}}
{{#Book}}<div class="book">{{Book}}</div>{{/Book}}`,
		},
	}
}

const cardCSSV1 = `.card {
  font-family: -apple-system, Segoe UI, Roboto, sans-serif;
  font-size: 20px;
  text-align: center;
  color: #202124;
  background: #ffffff;
}
.word { font-size: 30px; font-weight: 600; margin-bottom: 8px; }
.translation { font-size: 26px; color: #1a73e8; margin: 8px 0; }
.context { font-style: italic; color: #5f6368; margin: 10px auto; max-width: 90%; }
.context-translation { color: #5f6368; margin: 6px auto; max-width: 90%; }
.lemma { font-size: 16px; color: #5f6368; }
.pos { font-size: 15px; color: #9aa0a6; text-transform: uppercase; letter-spacing: 0.05em; }
.extra { font-size: 16px; color: #5f6368; margin-top: 6px; }
.ipa { font-size: 16px; color: #9aa0a6; }
.book { font-size: 13px; color: #bdc1c6; margin-top: 12px; }
hr#answer { margin: 14px 0; }
@media (prefers-color-scheme: dark) {
  .card { color: #e8eaed; background: #202124; }
  .word { color: #e8eaed; }
  .translation { color: #8ab4f8; }
}`

// sameText compares template/CSS text ignoring line-ending style and surrounding space.
func sameText(a, b string) bool {
	norm := func(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")) }
	return norm(a) == norm(b)
}
