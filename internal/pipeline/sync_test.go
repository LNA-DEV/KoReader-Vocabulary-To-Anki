package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/anki"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/state"
)

// ---- fake AnkiConnect -------------------------------------------------------------

type fakeNote struct {
	model, deck string
	fields      map[string]string
	tags        []string
}

type fakeAnki struct {
	mu        sync.Mutex
	models    map[string]bool
	templates map[string]map[string]string
	css       string
	notes     map[int64]*fakeNote
	nextID    int64
	calls     []string
}

func newFakeAnki(t *testing.T) (*fakeAnki, string) {
	f := &fakeAnki{models: map[string]bool{}, templates: map[string]map[string]string{}, notes: map[int64]*fakeNote{}, nextID: 1000}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Action string          `json:"action"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad AnkiConnect request: %v", err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, req.Action)
		res, errMsg := f.handle(t, req.Action, req.Params)
		var e any
		if errMsg != "" {
			e = errMsg
		}
		json.NewEncoder(w).Encode(map[string]any{"result": res, "error": e})
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

var termRe = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
var unescapeRe = regexp.MustCompile(`\\(.)`)

func (f *fakeAnki) duplicate(n anki.Note) bool {
	for _, x := range f.notes {
		if x.model == n.ModelName && x.deck == n.DeckName && x.fields[anki.Fields[0]] == n.Fields[anki.Fields[0]] {
			return true
		}
	}
	return false
}

func (f *fakeAnki) add(model, deck string, fields map[string]string, tags []string) int64 {
	f.nextID++
	f.notes[f.nextID] = &fakeNote{model: model, deck: deck, fields: fields, tags: tags}
	return f.nextID
}

func (f *fakeAnki) handle(t *testing.T, action string, raw json.RawMessage) (any, string) {
	switch action {
	case "version":
		return 6, ""
	case "createDeck":
		return 1, ""
	case "modelNames":
		var names []string
		for m := range f.models {
			names = append(names, m)
		}
		return names, ""
	case "createModel":
		var p struct {
			ModelName     string `json:"modelName"`
			CSS           string `json:"css"`
			CardTemplates []struct{ Name, Front, Back string }
		}
		json.Unmarshal(raw, &p)
		f.models[p.ModelName] = true
		f.css = p.CSS
		for _, ct := range p.CardTemplates {
			f.templates[ct.Name] = map[string]string{"Front": ct.Front, "Back": ct.Back}
		}
		return map[string]any{}, ""
	case "modelFieldNames":
		return anki.Fields, ""
	case "modelTemplates":
		return f.templates, ""
	case "modelStyling":
		return map[string]string{"css": f.css}, ""
	case "canAddNotes":
		var p struct{ Notes []anki.Note }
		json.Unmarshal(raw, &p)
		out := make([]bool, len(p.Notes))
		for i, n := range p.Notes {
			out[i] = !f.duplicate(n)
		}
		return out, ""
	case "addNotes":
		var p struct{ Notes []anki.Note }
		json.Unmarshal(raw, &p)
		out := make([]any, len(p.Notes))
		for i, n := range p.Notes {
			if !n.Options.AllowDuplicate && f.duplicate(n) {
				out[i] = nil
				continue
			}
			out[i] = f.add(n.ModelName, n.DeckName, n.Fields, n.Tags)
		}
		return out, ""
	case "findNotes":
		var p struct{ Query string }
		json.Unmarshal(raw, &p)
		var ids []int64
		for id, n := range f.notes {
			match := true
			for _, m := range termRe.FindAllStringSubmatch(p.Query, -1) {
				field, value, _ := strings.Cut(unescapeRe.ReplaceAllString(m[1], "$1"), ":")
				got := n.fields[field]
				if field == "note" {
					got = n.model
				}
				if !strings.EqualFold(got, value) { // Anki searches case-insensitively
					match = false
				}
			}
			if match {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		return ids, ""
	case "notesInfo":
		var p struct{ Notes []int64 }
		json.Unmarshal(raw, &p)
		out := make([]any, len(p.Notes))
		for i, id := range p.Notes {
			n, ok := f.notes[id]
			if !ok {
				out[i] = map[string]any{}
				continue
			}
			fields := map[string]any{}
			for k, v := range n.fields {
				fields[k] = map[string]any{"value": v, "order": slices.Index(anki.Fields, k)}
			}
			out[i] = map[string]any{"noteId": id, "modelName": n.model, "tags": n.tags, "fields": fields}
		}
		return out, ""
	case "updateNoteFields":
		var p struct {
			Note struct {
				ID     int64             `json:"id"`
				Fields map[string]string `json:"fields"`
			} `json:"note"`
		}
		json.Unmarshal(raw, &p)
		n, ok := f.notes[p.Note.ID]
		if !ok {
			return nil, "note was not found"
		}
		for k, v := range p.Note.Fields {
			n.fields[k] = v
		}
		return nil, ""
	case "addTags":
		var p struct {
			Notes []int64
			Tags  string
		}
		json.Unmarshal(raw, &p)
		for _, id := range p.Notes {
			if n, ok := f.notes[id]; ok {
				n.tags = append(n.tags, strings.Fields(p.Tags)...)
			}
		}
		return nil, ""
	}
	t.Errorf("fake AnkiConnect: unexpected action %q", action)
	return nil, "unsupported action"
}

// notesWithLemma returns the fake notes whose Lemma field equals lemma.
func (f *fakeAnki) notesWithLemma(lemma string) []*fakeNote {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*fakeNote
	ids := make([]int64, 0, len(f.notes))
	for id := range f.notes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if f.notes[id].fields["Lemma"] == lemma {
			out = append(out, f.notes[id])
		}
	}
	return out
}

// ---- fake Ollama ------------------------------------------------------------------

// fakeOllama answers enrichment requests via enrich(surface, sentence) and sense checks
// via sense(sentence). It counts sense checks.
func fakeOllama(t *testing.T, enrich func(surface, sentence string) string, sense func(sentence string) string) (string, *int) {
	senseCalls := 0
	surfaceRe := regexp.MustCompile(`Surface form \(as it appeared\): (.*)`)
	sentenceRe := regexp.MustCompile(`Sentence: (.*)`)
	newSentenceRe := regexp.MustCompile(`New sentence: (.*)`)
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "test-model"}}})
		case "/api/chat":
			var req struct {
				Messages []struct{ Role, Content string }
				Format   json.RawMessage
			}
			json.NewDecoder(r.Body).Decode(&req)
			user := req.Messages[1].Content
			var content string
			mu.Lock()
			if strings.Contains(string(req.Format), "same_sense") {
				senseCalls++
				content = sense(newSentenceRe.FindStringSubmatch(user)[1])
			} else {
				sm := sentenceRe.FindStringSubmatch(user)
				content = enrich(surfaceRe.FindStringSubmatch(user)[1], sm[1])
			}
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": content}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &senseCalls
}

func enr(lemma, pos, translation string) string {
	return enrWith(lemma, pos, translation, nil)
}

// enrWith builds an enrichment reply; extra overrides or adds fields.
func enrWith(lemma, pos, translation string, extra map[string]any) string {
	m := map[string]any{
		"source_language": "en", "lemma": lemma, "part_of_speech": pos,
		"translation": translation, "confidence": 0.9,
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func form(slot, value string, irregular bool) map[string]any {
	return map[string]any{"slot": slot, "form": value, "irregular": irregular}
}

func defaultEnrich(surface, sentence string) string {
	switch {
	case surface == "ran" || surface == "running" || surface == "runs":
		return enr("run", "verb", "to move fast on foot")
	case surface == "bank" && strings.Contains(sentence, "river"):
		return enr("bank", "noun", "the land along a river")
	case surface == "bank":
		return enr("bank", "noun", "a financial institution")
	case surface == "lead" && strings.Contains(sentence, "pipe"):
		return enr("lead", "noun", "a heavy metal")
	case surface == "lead":
		return enr("lead", "verb", "to guide or direct")
	case surface == "gave" && strings.Contains(sentence, "gave up"):
		return enr("give up", "verb", "to stop trying")
	case surface == "beheld":
		return enrWith("behold", "verb", "to see", map[string]any{
			"seen_as":     "past tense",
			"lemma_forms": []any{form("past", "beheld", true), form("past_participle", "beheld", true)},
		})
	case surface == "gouging":
		return enrWith("gouge", "verb", "to dig out", map[string]any{
			"seen_as":     "present participle",
			"lemma_forms": []any{form("past", "gouged", false), form("past_participle", "gouged", false)},
		})
	case surface == "walked":
		// The model wrongly flags a regular form; the English check must correct it.
		return enrWith("walk", "verb", "to move on foot", map[string]any{
			"lemma_forms": []any{form("past", "walked", true), form("past_participle", "walked", true)},
		})
	case surface == "Hunde":
		return enrWith("Hund", "noun", "dog", map[string]any{
			"source_language": "de", "seen_as": "plural",
			"lemma_forms": []any{form("gender", "der", false), form("plural", "die Hunde", false), form("genitive", "des Hundes", false)},
		})
	}
	return enr(surface, "noun", "something")
}

func defaultSense(sentence string) string {
	if strings.Contains(sentence, "river") {
		return `{"same_sense":false,"sense":"the land along a river","confidence":0.9}`
	}
	return `{"same_sense":true,"sense":null,"confidence":0.95}`
}

// ---- KOReader DB ------------------------------------------------------------------

type row struct{ word, highlight, prev, next string }

func writeVocabDB(t *testing.T, path string, rows ...row) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`PRAGMA user_version = 20240905`,
		`CREATE TABLE IF NOT EXISTS vocabulary (word TEXT NOT NULL UNIQUE, title_id INTEGER, create_time INTEGER NOT NULL,
		  review_time INTEGER, due_time INTEGER NOT NULL, review_count INTEGER NOT NULL DEFAULT 0, prev_context TEXT,
		  next_context TEXT, streak_count INTEGER NOT NULL DEFAULT 0, highlight TEXT, PRIMARY KEY(word))`,
		`CREATE TABLE IF NOT EXISTS title (id INTEGER NOT NULL UNIQUE, name TEXT UNIQUE, filter INTEGER NOT NULL DEFAULT 1, PRIMARY KEY(id))`,
		`INSERT OR IGNORE INTO title (id, name) VALUES (1, 'Test Book')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i, r := range rows {
		var hl any
		if r.highlight != "" {
			hl = r.highlight
		}
		if _, err := db.Exec(`INSERT INTO vocabulary (word, title_id, create_time, due_time, prev_context, next_context, highlight)
			VALUES (?, 1, ?, ?, ?, ?, ?)`, r.word, 1700000000+i, 1700000000+i, r.prev, r.next, hl); err != nil {
			t.Fatal(err)
		}
	}
}

// ---- harness ----------------------------------------------------------------------

type harness struct {
	runner     *Runner
	anki       *fakeAnki
	vocabPath  string
	senseCalls *int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	ank, ankiURL := newFakeAnki(t)
	ollURL, senseCalls := fakeOllama(t, defaultEnrich, defaultSense)

	cfg := config.Defaults()
	cfg.Input.DBPath = filepath.Join(dir, "vocab.sqlite3")
	cfg.Input.CopyBeforeRead = false
	cfg.State.DBPath = filepath.Join(dir, "state.db")
	cfg.Ollama.BaseURL = ollURL
	cfg.Ollama.Model = "test-model"
	cfg.Ollama.MaxRetries = 1
	cfg.Anki.URL = ankiURL
	if err := cfg.Finalize(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(cfg, logger)
	r.Now = func() int64 { return 1700000100 }
	return &harness{runner: r, anki: ank, vocabPath: cfg.Input.DBPath, senseCalls: senseCalls}
}

func (h *harness) sync(t *testing.T) {
	t.Helper()
	if err := h.runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func onlyNote(t *testing.T, h *harness, lemma string) *fakeNote {
	t.Helper()
	notes := h.anki.notesWithLemma(lemma)
	if len(notes) != 1 {
		t.Fatalf("expected exactly one note for %q, got %d", lemma, len(notes))
	}
	return notes[0]
}

// ---- tests ------------------------------------------------------------------------

func TestSyncOneNotePerLemma(t *testing.T) {
	h := newHarness(t)
	writeVocabDB(t, h.vocabPath,
		row{word: "ran", prev: "He", next: "home."},
		row{word: "running", prev: "She was", next: "late."},
		row{word: "bank", prev: "We went to the", next: "to get money."},
		row{word: "bank(1)", highlight: "bank", prev: "They sat on the", next: "of the river."},
		row{word: "lead", prev: "She will", next: "the team."},
		row{word: "lead(1)", highlight: "lead", prev: "The pipe was made of", next: "."},
		row{word: "gave", prev: "She", next: "up."},
	)
	h.sync(t)

	// ran + running → one "run" note; running's sentence is an example.
	run := onlyNote(t, h, "run")
	if run.fields["Word"] != "ran" || run.fields["Context"] != "He <b>ran</b> home." {
		t.Errorf("run note: Word=%q Context=%q", run.fields["Word"], run.fields["Context"])
	}
	if !strings.Contains(run.fields["Examples"], "She was <b>running</b> late.") {
		t.Errorf("run Examples = %q", run.fields["Examples"])
	}
	if run.fields["OtherSenses"] != "" {
		t.Errorf("same sense must not add OtherSenses: %q", run.fields["OtherSenses"])
	}

	// bank: second sighting with a new meaning → OtherSenses, Translation unchanged.
	bank := onlyNote(t, h, "bank")
	if bank.fields["Translation"] != "a financial institution" {
		t.Errorf("bank Translation = %q", bank.fields["Translation"])
	}
	if bank.fields["OtherSenses"] != `<div class="sense">the land along a river</div>` {
		t.Errorf("bank OtherSenses = %q", bank.fields["OtherSenses"])
	}

	// lead verb vs. lead noun → two notes despite the same first field.
	if leads := h.anki.notesWithLemma("lead"); len(leads) != 2 || leads[0].fields["POS"] == leads[1].fields["POS"] {
		t.Errorf("expected separate lead notes per POS, got %d", len(leads))
	}

	// Phrasal verb from a single-word highlight.
	onlyNote(t, h, "give up")

	if *h.senseCalls != 2 {
		t.Errorf("sense checks = %d, want 2 (running, bank(1))", *h.senseCalls)
	}

	// State: every row recorded, a second run does nothing.
	st, err := state.Open(h.runner.Cfg.State.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := st.Count()
	st.Close()
	if n != 7 {
		t.Errorf("processed rows = %d, want 7", n)
	}
	before := len(h.anki.calls)
	h.sync(t)
	for _, a := range h.anki.calls[before:] {
		if a == "addNotes" || a == "updateNoteFields" {
			t.Errorf("second run must not write, saw %q", a)
		}
	}
}

func TestSyncAdoptsNoteFromBeforeTheIndex(t *testing.T) {
	h := newHarness(t)
	// A v1-era note: in Anki, but not in the lemma index.
	old := map[string]string{}
	for _, f := range anki.Fields {
		old[f] = ""
	}
	old["Lemma"], old["Word"], old["POS"], old["SourceLang"] = "behold", "behold", "Verb", "en"
	old["Translation"], old["Context"] = "to see", "Lo and behold."
	h.anki.add("KOReader Vocab", "English::KOReader Vocabulary", old, []string{"koreader"})

	writeVocabDB(t, h.vocabPath, row{word: "beheld", prev: "She", next: "the ruin before her."})
	h.sync(t)

	note := onlyNote(t, h, "behold")
	if !strings.Contains(note.fields["Examples"], "She <b>beheld</b> the ruin before her.") {
		t.Errorf("row should be merged into the existing note, Examples = %q", note.fields["Examples"])
	}
	if !strings.Contains(note.fields["Forms"], "past participle") {
		t.Errorf("empty Forms of the old note should be filled on merge, Forms = %q", note.fields["Forms"])
	}
	if note.fields["SeenAs"] != "" {
		t.Errorf("SeenAs describes the first sentence and must not be filled on merge, got %q", note.fields["SeenAs"])
	}
	st, _ := state.Open(h.runner.Cfg.State.DBPath)
	defer st.Close()
	if _, ok, _ := st.LookupLemma("en|behold|verb"); !ok {
		t.Error("adopted note should be in the lemma index")
	}
}

func TestSyncRecreatesNoteWhenIndexIsStale(t *testing.T) {
	h := newHarness(t)
	writeVocabDB(t, h.vocabPath, row{word: "ran", prev: "He", next: "home."})
	h.sync(t)

	// The user deletes the note in Anki.
	h.anki.mu.Lock()
	for id := range h.anki.notes {
		delete(h.anki.notes, id)
	}
	h.anki.mu.Unlock()

	writeVocabDB(t, h.vocabPath, row{word: "runs", prev: "It", next: "."})
	h.sync(t)
	note := onlyNote(t, h, "run")
	if note.fields["Word"] != "runs" {
		t.Errorf("a fresh note should be created from the new row, Word = %q", note.fields["Word"])
	}
}

func TestSyncOnKnownLemmaSkip(t *testing.T) {
	h := newHarness(t)
	h.runner.Cfg.Features.OnKnownLemma = config.OnKnownLemmaSkip
	writeVocabDB(t, h.vocabPath,
		row{word: "ran", prev: "He", next: "home."},
		row{word: "running", prev: "She was", next: "late."},
	)
	h.sync(t)
	if note := onlyNote(t, h, "run"); note.fields["Examples"] != "" {
		t.Errorf("skip must not touch the note, Examples = %q", note.fields["Examples"])
	}
	if *h.senseCalls != 0 {
		t.Errorf("skip must not run the sense check, got %d calls", *h.senseCalls)
	}
}

func TestSyncLowConfidenceSenseTagsReview(t *testing.T) {
	h := newHarness(t)
	ollURL, _ := fakeOllama(t, defaultEnrich, func(string) string {
		return `{"same_sense":false,"sense":"the land along a river","confidence":0.4}`
	})
	h.runner.Cfg.Ollama.BaseURL = ollURL
	writeVocabDB(t, h.vocabPath,
		row{word: "bank", prev: "We went to the", next: "to get money."},
		row{word: "bank(1)", highlight: "bank", prev: "They sat on the", next: "of the river."},
	)
	h.sync(t)
	bank := onlyNote(t, h, "bank")
	if bank.fields["OtherSenses"] != "" {
		t.Errorf("low-confidence sense must not be added, OtherSenses = %q", bank.fields["OtherSenses"])
	}
	if !slices.Contains(bank.tags, "review") {
		t.Errorf("note should be tagged review, tags = %v", bank.tags)
	}
}

func TestPlannedAction(t *testing.T) {
	st, err := state.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.UpsertLemma(state.Lemma{Key: "en|run|verb", SourceLang: "en", Lemma: "run", POS: "verb", NoteID: 5}, 1)
	planned := map[string]bool{}

	if a, id, _ := plannedAction(st, "en|run|verb", planned); a != actionMerge || id != 5 {
		t.Errorf("indexed key: %s %d", a, id)
	}
	if a, _, _ := plannedAction(st, "en|bank|noun", planned); a != actionAdd {
		t.Errorf("new key: %s", a)
	}
	if a, _, _ := plannedAction(st, "en|bank|noun", planned); a != actionMerge {
		t.Errorf("second row of the same new key in one dry run should merge, got %s", a)
	}
}

func TestSyncDryRunDoesNotTouchAnki(t *testing.T) {
	h := newHarness(t)
	writeVocabDB(t, h.vocabPath,
		row{word: "ran", prev: "He", next: "home."},
		row{word: "running", prev: "She was", next: "late."},
	)
	if err := h.runner.Sync(context.Background(), SyncOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.anki.calls) != 0 {
		t.Errorf("dry run must not call AnkiConnect, saw %v", h.anki.calls)
	}
	st, _ := state.Open(h.runner.Cfg.State.DBPath)
	defer st.Close()
	if n, _ := st.Count(); n != 0 {
		t.Errorf("dry run must not record state, got %d rows", n)
	}
}

func TestSyncForms(t *testing.T) {
	h := newHarness(t)
	writeVocabDB(t, h.vocabPath,
		row{word: "beheld", prev: "She", next: "the ruin before her."},
		row{word: "gouge(1)", highlight: "gouging", prev: "a wall of white bone,", next: "deep."},
		row{word: "walked", prev: "He", next: "home."},
		row{word: "Hunde", prev: "Die", next: "bellen laut."},
	)
	h.sync(t)

	behold := onlyNote(t, h, "behold")
	if behold.fields["SeenAs"] != "past tense" {
		t.Errorf("behold SeenAs = %q", behold.fields["SeenAs"])
	}
	if !strings.Contains(behold.fields["Forms"], `<span class="fv irr"><b>beheld</b></span>`) {
		t.Errorf("irregular forms should be shown with the seen form bold, Forms = %q", behold.fields["Forms"])
	}
	if f := onlyNote(t, h, "gouge").fields["Forms"]; f != "" {
		t.Errorf("regular English verb should show no forms, Forms = %q", f)
	}
	if f := onlyNote(t, h, "walk").fields["Forms"]; f != "" {
		t.Errorf("wrongly flagged regular forms should be corrected and hidden, Forms = %q", f)
	}
	hund := onlyNote(t, h, "Hund")
	if f := hund.fields["Forms"]; !strings.Contains(f, `<span class="fv">der</span>`) || !strings.Contains(f, "<b>die Hunde</b>") || strings.Contains(f, "des Hundes") {
		t.Errorf("German noun should show gender + plural only, Forms = %q", f)
	}
	if hund.deck != "German::KOReader Vocabulary" {
		t.Errorf("German note deck = %q", hund.deck)
	}
}
