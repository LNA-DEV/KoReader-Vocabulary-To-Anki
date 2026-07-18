package state

import (
	"path/filepath"
	"testing"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

func entries(words ...string) []model.VocabEntry {
	out := make([]model.VocabEntry, len(words))
	for i, w := range words {
		out[i] = model.VocabEntry{Word: w, ContentHash: "h" + w}
	}
	return out
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestDeltaFiltersProcessed(t *testing.T) {
	st := openTemp(t)
	all := entries("a", "b", "c")

	// Nothing processed → full delta, order preserved.
	d, err := st.Delta(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 3 || d[0].Word != "a" || d[2].Word != "c" {
		t.Fatalf("initial delta = %+v", d)
	}

	// Record "b"; delta should drop it.
	if err := st.Record("b", 1, "en", 42, "hb", 100); err != nil {
		t.Fatal(err)
	}
	d, err = st.Delta(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 2 || d[0].Word != "a" || d[1].Word != "c" {
		t.Fatalf("delta after recording b = %+v", d)
	}
}

func TestIdempotentSecondRunEmpty(t *testing.T) {
	st := openTemp(t)
	all := entries("x", "y")
	for _, e := range all {
		if err := st.Record(e.Word, 0, "en", 1, e.ContentHash, 1); err != nil {
			t.Fatal(err)
		}
	}
	d, err := st.Delta(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 0 {
		t.Fatalf("second-run delta should be empty, got %+v", d)
	}

	// Re-recording the same word must not error or duplicate (INSERT OR REPLACE).
	if err := st.Record("x", 0, "en", 2, "hx", 2); err != nil {
		t.Fatal(err)
	}
	n, err := st.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("Count = %d, want 2", n)
	}
}
