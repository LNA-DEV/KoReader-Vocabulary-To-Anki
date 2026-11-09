package state

import (
	"database/sql"
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
	if err := st.Record("b", 1, "en", 42, "hb", 100, "en|b|noun"); err != nil {
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
		if err := st.Record(e.Word, 0, "en", 1, e.ContentHash, 1, ""); err != nil {
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
	if err := st.Record("x", 0, "en", 2, "hx", 2, ""); err != nil {
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

func TestLemmaIndex(t *testing.T) {
	st := openTemp(t)
	if _, ok, err := st.LookupLemma("en|behold|verb"); err != nil || ok {
		t.Fatalf("empty index: ok=%v err=%v", ok, err)
	}
	l := Lemma{Key: "en|behold|verb", SourceLang: "en", Lemma: "behold", POS: "verb", NoteID: 11}
	if err := st.UpsertLemma(l, 100); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.LookupLemma(l.Key)
	if err != nil || !ok || got != l {
		t.Fatalf("lookup = %+v ok=%v err=%v", got, ok, err)
	}

	// Repointing (e.g. the note was recreated) replaces the note ID.
	l.NoteID = 12
	if err := st.UpsertLemma(l, 200); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.LookupLemma(l.Key); got.NoteID != 12 {
		t.Errorf("note ID after upsert = %d, want 12", got.NoteID)
	}

	if err := st.DeleteLemma(l.Key); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.LookupLemma(l.Key); ok {
		t.Error("entry should be gone after DeleteLemma")
	}
}

func TestMigratesV1StateDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO processed (word, title_id, source_lang, anki_note_id, content_hash, processed_at)
		VALUES ('beheld', 1, 'en', 5, 'h', 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open v1 DB: %v", err)
	}
	defer st.Close()

	var version int
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		t.Errorf("user_version = %d (err %v), want %d", version, err, SchemaVersion)
	}
	if n, _ := st.Count(); n != 1 {
		t.Errorf("existing rows must survive the migration, count = %d", n)
	}
	d, err := st.Delta(entries("beheld", "new"))
	if err != nil || len(d) != 1 || d[0].Word != "new" {
		t.Errorf("delta after migration = %+v, err %v", d, err)
	}
	if err := st.Record("new", 0, "en", 6, "h2", 2, "en|new|adjective"); err != nil {
		t.Fatalf("Record after migration: %v", err)
	}
	if err := st.UpsertLemma(Lemma{Key: "en|new|adjective", SourceLang: "en", Lemma: "new", POS: "adjective", NoteID: 6}, 2); err != nil {
		t.Fatalf("lemma table missing after migration: %v", err)
	}

	// Opening again is a no-op.
	st.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.Close()
}
