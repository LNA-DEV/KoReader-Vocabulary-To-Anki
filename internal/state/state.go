// Package state persists which KOReader words have already been turned into Anki
// notes, so subsequent runs only process the delta. It is the primary idempotency gate.
package state

import (
	"database/sql"
	"fmt"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// Store wraps the local state sqlite DB.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS processed (
  word         TEXT PRIMARY KEY,
  title_id     INTEGER,
  source_lang  TEXT,
  anki_note_id INTEGER,
  content_hash TEXT,
  processed_at INTEGER
);`

// Open opens (creating if needed) the state DB at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, fmt.Errorf("opening state DB %q: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing state schema: %w", err)
	}
	return &Store{db: db}, nil
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

// Record marks a word as processed after a successful Anki insert.
func (s *Store) Record(word string, titleID int64, sourceLang string, ankiNoteID int64, contentHash string, processedAt int64) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO processed
		   (word, title_id, source_lang, anki_note_id, content_hash, processed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		word, titleID, sourceLang, ankiNoteID, contentHash, processedAt,
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
