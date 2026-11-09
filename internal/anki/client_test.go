package anki

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// recordedReq is one AnkiConnect request the fake server saw.
type recordedReq struct {
	Action string                     `json:"action"`
	Params map[string]json.RawMessage `json:"params"`
}

// fakeAnki returns a server that records requests and replies with per-action results.
func fakeAnki(t *testing.T, results map[string]any) (*Client, *[]recordedReq) {
	t.Helper()
	var seen []recordedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req recordedReq
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		seen = append(seen, req)
		res, ok := results[req.Action]
		if !ok {
			res = nil
		}
		json.NewEncoder(w).Encode(map[string]any{"result": res, "error": nil})
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, srv.Client()), &seen
}

func TestVersion(t *testing.T) {
	c, _ := fakeAnki(t, map[string]any{"version": 6})
	v, err := c.Version(context.Background())
	if err != nil || v != 6 {
		t.Fatalf("Version = %d, err=%v", v, err)
	}
}

func TestEnsureModelCreatesWhenAbsent(t *testing.T) {
	c, seen := fakeAnki(t, map[string]any{
		"modelNames":  []string{"Basic", "Cloze"}, // our model absent
		"createModel": map[string]any{},
	})
	rep, err := c.EnsureModel(context.Background(), "KOReader Vocab", false)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Created {
		t.Error("report should say the model was created")
	}
	// Expect modelNames then createModel.
	if len(*seen) != 2 || (*seen)[0].Action != "modelNames" || (*seen)[1].Action != "createModel" {
		t.Fatalf("unexpected call sequence: %+v", actions(*seen))
	}
	var params struct {
		ModelName     string   `json:"modelName"`
		InOrderFields []string `json:"inOrderFields"`
		CardTemplates []struct {
			Name string `json:"Name"`
		} `json:"cardTemplates"`
		IsCloze bool `json:"isCloze"`
	}
	if err := json.Unmarshal(mustParam(t, (*seen)[1], "modelName", "inOrderFields", "cardTemplates", "isCloze"), &params); err != nil {
		t.Fatal(err)
	}
	if params.ModelName != "KOReader Vocab" {
		t.Errorf("modelName = %q", params.ModelName)
	}
	if len(params.InOrderFields) != len(Fields) {
		t.Errorf("field count = %d, want %d", len(params.InOrderFields), len(Fields))
	}
	if len(params.CardTemplates) != 2 {
		t.Errorf("expected 2 card templates, got %d", len(params.CardTemplates))
	}
	if params.IsCloze {
		t.Error("isCloze should be false")
	}
}

func currentTemplates() map[string]any {
	out := map[string]any{}
	for _, t := range cardTemplates() {
		out[t.Name] = map[string]string{"Front": t.Front, "Back": t.Back}
	}
	return out
}

func v1Templates() map[string]any {
	out := map[string]any{}
	for _, t := range cardTemplatesV1() {
		out[t.Name] = map[string]string{"Front": t.Front, "Back": t.Back}
	}
	return out
}

var v1Fields = []string{"Word", "Lemma", "Translation", "Context", "ContextTranslation",
	"Book", "SourceLang", "TargetLang", "IPA", "POS", "Extra"}

func TestEnsureModelNoChangesWhenCurrent(t *testing.T) {
	c, seen := fakeAnki(t, map[string]any{
		"modelNames":      []string{"KOReader Vocab"},
		"modelFieldNames": Fields,
		"modelTemplates":  currentTemplates(),
		"modelStyling":    map[string]string{"css": cardCSS},
	})
	rep, err := c.EnsureModel(context.Background(), "KOReader Vocab", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions(*seen) {
		switch a {
		case "createModel", "modelFieldAdd", "modelFieldReposition", "updateModelTemplates", "updateModelStyling":
			t.Errorf("unexpected write %q on an up-to-date model", a)
		}
	}
	if rep.SchemaChanged() || rep.CSSUpdated || len(rep.TemplatesUpdated) > 0 {
		t.Errorf("report should be empty: %+v", rep)
	}
}

func TestEnsureModelUpgradesV1(t *testing.T) {
	c, seen := fakeAnki(t, map[string]any{
		"modelNames":      []string{"KOReader Vocab"},
		"modelFieldNames": v1Fields,
		"modelTemplates":  v1Templates(),
		"modelStyling":    map[string]string{"css": cardCSSV1},
	})
	rep, err := c.EnsureModel(context.Background(), "KOReader Vocab", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"SeenAs", "Forms", "OtherSenses", "Examples"}; !slices.Equal(rep.FieldsAdded, want) {
		t.Errorf("FieldsAdded = %v, want %v", rep.FieldsAdded, want)
	}
	if !rep.LemmaMovedFirst || !rep.SchemaChanged() {
		t.Errorf("Lemma should be moved first: %+v", rep)
	}
	if len(rep.TemplatesUpdated) != 2 || !rep.CSSUpdated {
		t.Errorf("unmodified v1 templates/CSS should be updated: %+v", rep)
	}

	// The adds use indices that end in the v2 order once Lemma moves to the front.
	got := slices.Clone(v1Fields)
	for _, r := range *seen {
		switch r.Action {
		case "modelFieldAdd":
			var p struct {
				FieldName string `json:"fieldName"`
				Index     int    `json:"index"`
			}
			json.Unmarshal(mustParam(t, r, "modelName", "fieldName", "index"), &p)
			got = slices.Insert(got, p.Index, p.FieldName)
		case "modelFieldReposition":
			var p struct {
				FieldName string `json:"fieldName"`
				Index     int    `json:"index"`
			}
			json.Unmarshal(mustParam(t, r, "modelName", "fieldName", "index"), &p)
			i := slices.Index(got, p.FieldName)
			got = slices.Insert(slices.Delete(got, i, i+1), p.Index, p.FieldName)
		}
	}
	if !slices.Equal(got, Fields) {
		t.Errorf("resulting field order = %v, want %v", got, Fields)
	}
}

func TestEnsureModelKeepsCustomizedTemplates(t *testing.T) {
	custom := v1Templates()
	custom[recognitionName] = map[string]string{"Front": "<div>{{Word}}</div> my own", "Back": "{{FrontSide}}"}
	results := map[string]any{
		"modelNames":      []string{"KOReader Vocab"},
		"modelFieldNames": Fields,
		"modelTemplates":  custom,
		"modelStyling":    map[string]string{"css": ".card { color: hotpink; }"},
	}
	c, seen := fakeAnki(t, results)
	rep, err := c.EnsureModel(context.Background(), "KOReader Vocab", false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.TemplatesSkipped, []string{recognitionName}) || !slices.Equal(rep.TemplatesUpdated, []string{productionName}) {
		t.Errorf("only the unmodified template should be updated: %+v", rep)
	}
	if !rep.CSSSkipped || rep.CSSUpdated {
		t.Errorf("customized CSS should be skipped: %+v", rep)
	}
	for _, r := range *seen {
		if r.Action == "updateModelTemplates" && strings.Contains(string(mustParam(t, r, "model")), "my own") {
			t.Error("customized template must not be sent back")
		}
	}

	// --force-templates overwrites both.
	c2, _ := fakeAnki(t, results)
	rep, err = c2.EnsureModel(context.Background(), "KOReader Vocab", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.TemplatesUpdated) != 2 || !rep.CSSUpdated {
		t.Errorf("force should update everything: %+v", rep)
	}
}

func TestNotesInfoDropsMissingNotes(t *testing.T) {
	c, _ := fakeAnki(t, map[string]any{"notesInfo": []any{
		map[string]any{"noteId": 7, "modelName": "KOReader Vocab", "fields": map[string]any{
			"Lemma": map[string]any{"value": "behold", "order": 0},
		}},
		map[string]any{}, // AnkiConnect returns {} for an unknown ID
	}})
	notes, err := c.NotesInfo(context.Background(), []int64{7, 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].NoteID != 7 || notes[0].Field("Lemma") != "behold" {
		t.Errorf("notes = %+v", notes)
	}
}

func TestSearchTermEscapes(t *testing.T) {
	if got := SearchTerm("Lemma", `a*b_c"d\`); got != `"Lemma:a\*b\_c\"d\\"` {
		t.Errorf("SearchTerm = %s", got)
	}
	if got := SearchTerm("note", "KOReader Vocab"); got != `"note:KOReader Vocab"` {
		t.Errorf("SearchTerm = %s", got)
	}
}

func TestEnsureDeckCachesWithinRun(t *testing.T) {
	c, seen := fakeAnki(t, map[string]any{"createDeck": 1})
	ctx := context.Background()
	for range 3 {
		if err := c.EnsureDeck(ctx, "English::KOReader Vocabulary"); err != nil {
			t.Fatal(err)
		}
	}
	if len(*seen) != 1 {
		t.Fatalf("createDeck should be called once (cached), got %d", len(*seen))
	}
}

func TestAddNotesReturnsIDs(t *testing.T) {
	c, seen := fakeAnki(t, map[string]any{"addNotes": []any{12345, nil}})
	ids, err := c.AddNotes(context.Background(), []Note{{}, {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 12345 || ids[1] != 0 {
		t.Errorf("ids = %v", ids)
	}
	if (*seen)[0].Action != "addNotes" {
		t.Errorf("action = %q", (*seen)[0].Action)
	}
}

func TestCanAddNotes(t *testing.T) {
	c, _ := fakeAnki(t, map[string]any{"canAddNotes": []bool{true, false}})
	got, err := c.CanAddNotes(context.Background(), []Note{{}, {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0] || got[1] {
		t.Errorf("canAddNotes = %v", got)
	}
}

func TestCallSurfacesAnkiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errMsg := "deck was not found"
		json.NewEncoder(w).Encode(map[string]any{"result": nil, "error": errMsg})
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, srv.Client())
	if _, err := c.Version(context.Background()); err == nil {
		t.Fatal("expected error to be surfaced")
	}
}

func actions(reqs []recordedReq) []string {
	out := make([]string, len(reqs))
	for i, r := range reqs {
		out[i] = r.Action
	}
	return out
}

// mustParam re-marshals the recorded params (ensuring the named keys are present).
func mustParam(t *testing.T, req recordedReq, keys ...string) []byte {
	t.Helper()
	for _, k := range keys {
		if _, ok := req.Params[k]; !ok {
			t.Errorf("param %q missing from %s", k, req.Action)
		}
	}
	b, _ := json.Marshal(req.Params)
	return b
}
