// Package state persists which KOReader words have already been turned into Anki
// notes, so subsequent runs only process the delta. It is the primary idempotency gate.
// It also keeps the lemma index: which Anki note holds which base form.
package state

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// Store wraps the local state sqlite DB.
type Store struct {
	db *sql.DB
}

// SchemaVersion is the state DB layout this code writes (PRAGMA user_version).
// 0/1: processed table only. 2: lemma index + processed.lemma_key.
const SchemaVersion = 2

const schemaV1 = `
CREATE TABLE IF NOT EXISTS processed (
  word         TEXT PRIMARY KEY,
  title_id     INTEGER,
  source_lang  TEXT,
  anki_note_id INTEGER,
  content_hash TEXT,
  processed_at INTEGER
);`

const schemaV2Lemmas = `
CREATE TABLE IF NOT EXISTS lemmas (
  lemma_key    TEXT PRIMARY KEY,
  source_lang  TEXT NOT NULL,
  lemma        TEXT NOT NULL,
  pos          TEXT NOT NULL,
  anki_note_id INTEGER NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);`

// Open opens (creating if needed) the state DB at path and migrates it to SchemaVersion.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, fmt.Errorf("opening state DB %q: %w", path, err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating state DB %q: %w", path, err)
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("state DB has schema version %d, this koanki only knows up to %d", version, SchemaVersion)
	}
	if _, err := db.Exec(schemaV1); err != nil {
		return err
	}
	if version < 2 {
		if _, err := db.Exec(schemaV2Lemmas); err != nil {
			return err
		}
		has, err := hasColumn(db, "processed", "lemma_key")
		if err != nil {
			return err
		}
		if !has {
			if _, err := db.Exec("ALTER TABLE processed ADD COLUMN lemma_key TEXT"); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion))
	return err
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close closes the underlying DB.
func (s *Store) Close() error { return s.db.Close() }

// processedWords returns the set of words already recorded.
func (s *Store) processedWords() (map[string]struct{}, error) {
	rows, err := s.db.Query("SELECT word FROM processed")
	if err != nil {
		return nil, fmt.Errorf("querying processed words: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, fmt.Errorf("scanning processed word: %w", err)
		}
		seen[w] = struct{}{}
	}
	return seen, rows.Err()
}

// Delta returns the subset of entries whose Word is not yet in the processed table,
// preserving input order. Known words are dropped before they ever reach the LLM.
func (s *Store) Delta(entries []model.VocabEntry) ([]model.VocabEntry, error) {
	seen, err := s.processedWords()
	if err != nil {
		return nil, err
	}
	out := make([]model.VocabEntry, 0, len(entries))
	for _, e := range entries {
		if _, ok := seen[e.Word]; !ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// Record marks a word as processed after it was added to or merged into an Anki note.
// lemmaKey is the key of the note it landed on ("" when unknown).
func (s *Store) Record(word string, titleID int64, sourceLang string, ankiNoteID int64, contentHash string, processedAt int64, lemmaKey string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO processed
		   (word, title_id, source_lang, anki_note_id, content_hash, processed_at, lemma_key)
		 VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		word, titleID, sourceLang, ankiNoteID, contentHash, processedAt, lemmaKey,
	)
	if err != nil {
		return fmt.Errorf("recording processed word %q: %w", word, err)
	}
	return nil
}

// Count returns the number of processed words (for diagnostics).
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM processed").Scan(&n)
	return n, err
}

// Lemma is one entry of the lemma index: the Anki note that holds a base form.
type Lemma struct {
	Key        string // lemma.Key(...), e.g. "en|behold|verb"
	SourceLang string
	Lemma      string // display form
	POS        string
	NoteID     int64
}

// LookupLemma returns the index entry for key, if any.
func (s *Store) LookupLemma(key string) (Lemma, bool, error) {
	l := Lemma{Key: key}
	err := s.db.QueryRow(
		"SELECT source_lang, lemma, pos, anki_note_id FROM lemmas WHERE lemma_key = ?", key,
	).Scan(&l.SourceLang, &l.Lemma, &l.POS, &l.NoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return Lemma{}, false, nil
	}
	if err != nil {
		return Lemma{}, false, fmt.Errorf("looking up lemma %q: %w", key, err)
	}
	return l, true, nil
}

// UpsertLemma stores (or repoints) the index entry for l.Key.
func (s *Store) UpsertLemma(l Lemma, now int64) error {
	_, err := s.db.Exec(
		`INSERT INTO lemmas (lemma_key, source_lang, lemma, pos, anki_note_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(lemma_key) DO UPDATE SET
		   source_lang = excluded.source_lang, lemma = excluded.lemma, pos = excluded.pos,
		   anki_note_id = excluded.anki_note_id, updated_at = excluded.updated_at`,
		l.Key, l.SourceLang, l.Lemma, l.POS, l.NoteID, now, now,
	)
	if err != nil {
		return fmt.Errorf("storing lemma %q: %w", l.Key, err)
	}
	return nil
}

// DeleteLemma removes a stale index entry (its note no longer exists in Anki).
func (s *Store) DeleteLemma(key string) error {
	if _, err := s.db.Exec("DELETE FROM lemmas WHERE lemma_key = ?", key); err != nil {
		return fmt.Errorf("deleting lemma %q: %w", key, err)
	}
	return nil
}
