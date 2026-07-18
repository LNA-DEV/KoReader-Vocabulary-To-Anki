package koreader

import (
	"path/filepath"
	"testing"
)

func fixturePath() string {
	return filepath.Join("testdata", "vocab_fixture.sqlite3")
}

func TestStripSuffix(t *testing.T) {
	cases := map[string]string{
		"gouge(1)":  "gouge",
		"coo(2)":    "coo",
		"plunge(1)": "plunge",
		"beheld":    "beheld",
		"do(not)":   "do(not)", // only trailing (N) with digits is stripped
	}
	for in, want := range cases {
		if got := StripSuffix(in); got != want {
			t.Errorf("StripSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListReconstructsEntries(t *testing.T) {
	entries, stats, err := List(fixturePath(), true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if stats.UserVersion != ExpectedUserVersion {
		t.Errorf("user_version = %d, want %d", stats.UserVersion, ExpectedUserVersion)
	}
	if stats.RowCount != 4 {
		t.Errorf("RowCount = %d, want 4", stats.RowCount)
	}
	if stats.NewestCreatedAt != 1700000004 {
		t.Errorf("NewestCreatedAt = %d, want 1700000004", stats.NewestCreatedAt)
	}

	byWord := make(map[string]struct{ surface, wordClean, context, book string })
	for _, e := range entries {
		byWord[e.Word] = struct{ surface, wordClean, context, book string }{e.Surface, e.WordClean, e.Context, e.Book}
	}

	// Normal row: highlight NULL → surface == word; context stitched around it.
	if got := byWord["beheld"]; got.surface != "beheld" || got.context != "She beheld the ruin before her." {
		t.Errorf("beheld = %+v", got)
	}

	// Suffix + highlight: word "gouge(1)" → clean "gouge", surface "gouging".
	g := byWord["gouge(1)"]
	if g.wordClean != "gouge" {
		t.Errorf("gouge(1) wordClean = %q, want gouge", g.wordClean)
	}
	if g.surface != "gouging" {
		t.Errorf("gouge(1) surface = %q, want gouging", g.surface)
	}
	if g.context != "a wall of white bone, gouging deep." {
		t.Errorf("gouge(1) context = %q", g.context)
	}

	// Dictionary lookup: empty context, book present.
	j := byWord["jilt"]
	if j.context != "" {
		t.Errorf("jilt context = %q, want empty", j.context)
	}
	if j.book != "Dictionary lookup" {
		t.Errorf("jilt book = %q", j.book)
	}

	// Null title_id → empty book, context still built.
	n := byWord["nook"]
	if n.book != "" {
		t.Errorf("nook book = %q, want empty", n.book)
	}
	if n.context != "a cozy nook by the fire." {
		t.Errorf("nook context = %q", n.context)
	}
}

func TestContentHashStable(t *testing.T) {
	e1, _, err := List(fixturePath(), true)
	if err != nil {
		t.Fatal(err)
	}
	e2, _, err := List(fixturePath(), false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range e1 {
		if e1[i].ContentHash == "" {
			t.Errorf("empty content hash for %q", e1[i].Word)
		}
		if e1[i].ContentHash != e2[i].ContentHash {
			t.Errorf("content hash not stable for %q: %q vs %q", e1[i].Word, e1[i].ContentHash, e2[i].ContentHash)
		}
	}
}
