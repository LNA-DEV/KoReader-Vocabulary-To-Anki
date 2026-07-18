package anki

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	if err := c.EnsureModel(context.Background(), "KOReader Vocab"); err != nil {
		t.Fatal(err)
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

func TestEnsureModelSkipsWhenPresent(t *testing.T) {
	c, seen := fakeAnki(t, map[string]any{"modelNames": []string{"KOReader Vocab"}})
	if err := c.EnsureModel(context.Background(), "KOReader Vocab"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("should not call createModel when present: %+v", actions(*seen))
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
