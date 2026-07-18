// Package koreader reads the KOReader Vocabulary Builder sqlite database and turns
// each row into a model.VocabEntry. The source DB is opened read-only (optionally via
// a temp copy) so a concurrent sync process can never be disturbed.
package koreader

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// ExpectedUserVersion is the KOReader schema version this reader was verified against.
const ExpectedUserVersion = 20240905

// DictionaryLookupTitle is the pseudo-title KOReader uses for words looked up straight
// from the dictionary, with no sentence context.
const DictionaryLookupTitle = "Dictionary lookup"

// suffixRe matches KOReader's trailing "(N)" uniqueness disambiguator on the word PK.
var suffixRe = regexp.MustCompile(`\s*\(\d+\)$`)

// Stats summarizes the source DB for startup diagnostics (stale-input detection).
type Stats struct {
	UserVersion     int
	RowCount        int
	NewestCreatedAt int64
}

// List opens the KOReader DB and returns every vocabulary row as a VocabEntry, plus
// summary Stats. When copyBeforeRead is true the file is copied to a temp location and
// the copy is read, avoiding lock contention with a syncing process.
func List(dbPath string, copyBeforeRead bool) ([]model.VocabEntry, Stats, error) {
	readPath := dbPath
	if copyBeforeRead {
		tmp, cleanup, err := copyToTemp(dbPath)
		if err != nil {
			return nil, Stats{}, err
		}
		defer cleanup()
		readPath = tmp
	}

	db, err := sql.Open("sqlite", "file:"+readPath+"?mode=ro")
	if err != nil {
		return nil, Stats{}, fmt.Errorf("opening %q: %w", readPath, err)
	}
	defer db.Close()

	stats := Stats{}
	if err := db.QueryRow("PRAGMA user_version").Scan(&stats.UserVersion); err != nil {
		return nil, Stats{}, fmt.Errorf("reading user_version: %w", err)
	}

	const q = `
SELECT v.word, v.highlight, v.prev_context, v.next_context,
       v.create_time, v.title_id, t.name AS book
FROM vocabulary v
LEFT JOIN title t ON t.id = v.title_id`
	rows, err := db.Query(q)
	if err != nil {
		return nil, stats, fmt.Errorf("querying vocabulary: %w", err)
	}
	defer rows.Close()

	var entries []model.VocabEntry
	for rows.Next() {
		var (
			word      string
			highlight sql.NullString
			prev      sql.NullString
			next      sql.NullString
			createdAt int64
			titleID   sql.NullInt64
			book      sql.NullString
		)
		if err := rows.Scan(&word, &highlight, &prev, &next, &createdAt, &titleID, &book); err != nil {
			return nil, stats, fmt.Errorf("scanning row: %w", err)
		}

		entry := buildEntry(word, highlight.String, prev.String, next.String, createdAt, titleID.Int64, book.String)
		entries = append(entries, entry)

		stats.RowCount++
		if createdAt > stats.NewestCreatedAt {
			stats.NewestCreatedAt = createdAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, stats, fmt.Errorf("iterating rows: %w", err)
	}
	return entries, stats, nil
}

// buildEntry applies the surface/lookup/context reconstruction rules (see plan §Reader).
func buildEntry(word, highlight, prev, next string, createdAt, titleID int64, book string) model.VocabEntry {
	clean := StripSuffix(word)
	surface := highlight
	if strings.TrimSpace(surface) == "" {
		surface = clean
	}
	context := reconstructContext(prev, surface, next)

	return model.VocabEntry{
		Word:        word,
		WordClean:   clean,
		Surface:     surface,
		Context:     context,
		Book:        book,
		TitleID:     titleID,
		CreatedAt:   createdAt,
		ContentHash: contentHash(word, surface, context),
	}
}

// StripSuffix removes a trailing "(N)" disambiguator from a KOReader word PK.
func StripSuffix(word string) string {
	return suffixRe.ReplaceAllString(word, "")
}

// reconstructContext stitches the sentence back together around the surface form.
// Returns "" when there is no surrounding context (the Dictionary-lookup rows).
func reconstructContext(prev, surface, next string) string {
	prev = strings.TrimSpace(prev)
	next = strings.TrimSpace(next)
	if prev == "" && next == "" {
		return ""
	}
	return strings.TrimSpace(prev + " " + surface + " " + next)
}

func contentHash(word, surface, context string) string {
	sum := sha256.Sum256([]byte(word + "|" + surface + "|" + context))
	return hex.EncodeToString(sum[:8])
}

// copyToTemp copies src to a temp file and returns its path plus a cleanup func.
func copyToTemp(src string) (string, func(), error) {
	in, err := os.Open(src)
	if err != nil {
		return "", nil, fmt.Errorf("opening source DB %q: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp("", "koanki-vocab-*.sqlite3")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp copy: %w", err)
	}
	cleanup := func() { os.Remove(tmp.Name()) }

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("copying DB to temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("closing temp copy: %w", err)
	}
	return tmp.Name(), cleanup, nil
}

// AbsPath is a small helper so callers can log the resolved input path.
func AbsPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
