package anki

// cardTemplate mirrors AnkiConnect's createModel cardTemplates entry.
type cardTemplate struct {
	Name  string `json:"Name"`
	Front string `json:"Front"`
	Back  string `json:"Back"`
}

// cardTemplates returns the two directions of the reversible note type. Card 1 goes
// from the source word (+context) to its meaning; Card 2 from the meaning back to the
// word. One note therefore yields both study directions.
func cardTemplates() []cardTemplate {
	return []cardTemplate{
		{
			Name: "Recognition (word→meaning)",
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
			Name: "Production (meaning→word)",
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

// cardCSS is a small, legible stylesheet shared by both cards.
const cardCSS = `.card {
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
