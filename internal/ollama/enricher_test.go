package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

// chatServer builds an httptest server whose /api/chat returns the given content strings
// in sequence (one per call), and whose /api/tags advertises "test-model".
func chatServer(t *testing.T, contents ...string) (*httptest.Server, *int) {
	t.Helper()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "test-model"}},
			})
		case "/api/chat":
			idx := calls
			calls++
			if idx >= len(contents) {
				idx = len(contents) - 1
			}
			json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]string{"role": "assistant", "content": contents[idx]},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testClient(url string) *Client {
	return New(Options{BaseURL: url, Model: "test-model", MaxRetries: 3, Temperature: 0.2, Timeout: 5 * time.Second})
}

var validJSON = `{"source_language":"en","surface_form":"gouging","lemma":"gouge",` +
	`"part_of_speech":"verb","translation":"to dig or scoop out forcibly",` +
	`"translation_alternatives":["dig out","scoop"],"confidence":0.9}`

func TestEnrichHappyPath(t *testing.T) {
	srv, calls := chatServer(t, validJSON)
	c := testClient(srv.URL)

	enr, err := c.Enrich(context.Background(), model.VocabEntry{Word: "gouge", Surface: "gouging"}, "English")
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if enr.Lemma != "gouge" || enr.Translation == "" {
		t.Errorf("unexpected enrichment: %+v", enr)
	}
	if *calls != 1 {
		t.Errorf("expected 1 chat call, got %d", *calls)
	}
}

func TestEnrichRetriesOnMalformedJSON(t *testing.T) {
	// First reply is not JSON, second is valid → should succeed on the 2nd call.
	srv, calls := chatServer(t, "not json at all", validJSON)
	c := testClient(srv.URL)

	enr, err := c.Enrich(context.Background(), model.VocabEntry{Word: "gouge"}, "English")
	if err != nil {
		t.Fatalf("Enrich should have recovered: %v", err)
	}
	if enr.Lemma != "gouge" {
		t.Errorf("lemma = %q", enr.Lemma)
	}
	if *calls != 2 {
		t.Errorf("expected 2 chat calls (retry), got %d", *calls)
	}
}

func TestEnrichFailsAfterMaxRetries(t *testing.T) {
	srv, calls := chatServer(t, "garbage") // always invalid
	c := testClient(srv.URL)

	_, err := c.Enrich(context.Background(), model.VocabEntry{Word: "gouge"}, "English")
	if err == nil {
		t.Fatal("expected failure after retries")
	}
	if *calls != 3 {
		t.Errorf("expected 3 attempts, got %d", *calls)
	}
}

func TestEnrichRejectsIncompleteFields(t *testing.T) {
	// Valid JSON but missing lemma/translation → validation fails, retries, then errors.
	srv, _ := chatServer(t, `{"source_language":"en","confidence":0.5}`)
	c := testClient(srv.URL)

	_, err := c.Enrich(context.Background(), model.VocabEntry{Word: "x"}, "English")
	if err == nil {
		t.Fatal("expected validation failure")
	}
}

func TestModelAvailable(t *testing.T) {
	srv, _ := chatServer(t, validJSON)
	c := testClient(srv.URL)
	ok, names, err := c.ModelAvailable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("expected model to be available, names=%v", names)
	}
}

func senseReq() SenseRequest {
	return SenseRequest{
		SourceLanguage: "en", Lemma: "bank", PartOfSpeech: "noun",
		KnownSenses: []string{"an institution that keeps money"},
		Surface:     "bank", Sentence: "They sat on the bank of the river.",
	}
}

func TestCheckSenseNewSense(t *testing.T) {
	srv, calls := chatServer(t, `{"same_sense":false,"sense":"the land along a river","confidence":0.9}`)
	res, err := testClient(srv.URL).CheckSense(context.Background(), senseReq(), "English")
	if err != nil {
		t.Fatalf("CheckSense: %v", err)
	}
	if res.SameSense || res.NewSense() != "the land along a river" || res.Confidence != 0.9 {
		t.Errorf("unexpected result: %+v", res)
	}
	if *calls != 1 {
		t.Errorf("expected 1 call, got %d", *calls)
	}
}

func TestCheckSenseSameSense(t *testing.T) {
	srv, _ := chatServer(t, `{"same_sense":true,"sense":null,"confidence":0.95}`)
	res, err := testClient(srv.URL).CheckSense(context.Background(), senseReq(), "English")
	if err != nil {
		t.Fatal(err)
	}
	if !res.SameSense || res.NewSense() != "" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestCheckSenseRetriesWhenSenseMissing(t *testing.T) {
	// "different sense" without a sense is invalid → retried.
	srv, calls := chatServer(t,
		`{"same_sense":false,"sense":null,"confidence":0.8}`,
		`{"same_sense":false,"sense":"the land along a river","confidence":0.8}`)
	res, err := testClient(srv.URL).CheckSense(context.Background(), senseReq(), "English")
	if err != nil {
		t.Fatal(err)
	}
	if res.NewSense() == "" || *calls != 2 {
		t.Errorf("result %+v after %d calls", res, *calls)
	}
}

func TestEnrichDecodesForms(t *testing.T) {
	srv, _ := chatServer(t, `{"source_language":"en","surface_form":"beheld","seen_as":"past tense","lemma":"behold",`+
		`"part_of_speech":"verb","translation":"to see","confidence":0.9,"lemma_forms":[`+
		`{"slot":"past","form":"beheld","irregular":true},{"slot":"past_participle","form":"beheld","irregular":true}]}`)
	enr, err := testClient(srv.URL).Enrich(context.Background(), model.VocabEntry{Word: "beheld", Surface: "beheld"}, "English")
	if err != nil {
		t.Fatal(err)
	}
	if enr.SeenAs == nil || *enr.SeenAs != "past tense" {
		t.Errorf("SeenAs = %v", enr.SeenAs)
	}
	if len(enr.LemmaForms) != 2 || enr.LemmaForms[0].Slot != "past" || !enr.LemmaForms[1].Irregular {
		t.Errorf("LemmaForms = %+v", enr.LemmaForms)
	}
}
