package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/anki"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/lemma"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/ollama"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/state"
)

// Actions a sync can take for one KOReader row.
const (
	actionAdd   = "add"   // a new note was created
	actionMerge = "merge" // the row was merged into the note that already holds its lemma
	actionSkip  = "skip"  // the lemma has a note and on_known_lemma = "skip"
)

// placement is the outcome of placing one row in Anki.
type placement struct {
	action       string
	noteID       int64
	exampleAdded bool
	senseAdded   string // new meaning appended to OtherSenses, "" if none
	review       bool   // the note got the "review" tag during the merge
}

// syncer holds the clients for one sync run.
type syncer struct {
	r   *Runner
	ank *anki.Client
	oll *ollama.Client
	st  *state.Store
}

// place adds the note, or merges the row into the note that already holds its lemma.
func (s *syncer) place(ctx context.Context, entry model.VocabEntry, enr model.Enrichment, note anki.Note, key string) (placement, error) {
	existing, allowDuplicate, err := s.findExisting(ctx, enr, note, key)
	if err != nil {
		return placement{}, err
	}
	if existing == nil {
		id, err := s.add(ctx, note, allowDuplicate)
		if err != nil {
			return placement{}, err
		}
		if err := s.index(key, enr, id); err != nil {
			return placement{}, err
		}
		return placement{action: actionAdd, noteID: id}, nil
	}

	switch s.r.Cfg.Features.OnKnownLemma {
	case config.OnKnownLemmaSkip:
		return placement{action: actionSkip, noteID: existing.NoteID}, nil
	case config.OnKnownLemmaAdd:
		// A separate note on purpose; the index keeps pointing at the first one.
		id, err := s.add(ctx, note, true)
		if err != nil {
			return placement{}, err
		}
		return placement{action: actionAdd, noteID: id}, nil
	}
	return s.merge(ctx, *existing, entry, enr, note)
}

// findExisting returns the note that already holds key, if any. It trusts the lemma
// index first (dropping stale entries), then searches Anki, which also adopts notes
// created before the index existed. When nothing holds key but Anki would still reject
// the note as a first-field duplicate (a homograph with another part of speech, or the
// same spelling in another language), allowDuplicate is true.
func (s *syncer) findExisting(ctx context.Context, enr model.Enrichment, note anki.Note, key string) (*anki.NoteInfo, bool, error) {
	if l, ok, err := s.st.LookupLemma(key); err != nil {
		return nil, false, err
	} else if ok {
		notes, err := s.ank.NotesInfo(ctx, []int64{l.NoteID})
		if err != nil {
			return nil, false, err
		}
		if len(notes) == 1 && notes[0].ModelName == s.r.Cfg.Anki.NoteType {
			return &notes[0], false, nil
		}
		s.r.Logger.Info("lemma index entry is stale (note no longer in Anki); dropping it", "lemma_key", key, "note_id", l.NoteID)
		if err := s.st.DeleteLemma(key); err != nil {
			return nil, false, err
		}
	}

	query := strings.Join([]string{
		anki.SearchTerm("note", s.r.Cfg.Anki.NoteType),
		anki.SearchTerm("Lemma", note.Fields["Lemma"]),
		anki.SearchTerm("SourceLang", note.Fields["SourceLang"]),
	}, " ")
	ids, err := s.ank.FindNotes(ctx, query)
	if err != nil {
		return nil, false, err
	}
	if len(ids) > 0 {
		notes, err := s.ank.NotesInfo(ctx, ids)
		if err != nil {
			return nil, false, err
		}
		for i := range notes {
			n := notes[i]
			nKey := lemma.Key(anki.PlainText(n.Field("SourceLang")), anki.PlainText(n.Field("Lemma")), anki.PlainText(n.Field("POS")))
			if nKey == key {
				s.r.Logger.Info("adopting existing note for lemma", "lemma_key", key, "note_id", n.NoteID)
				if err := s.index(key, enr, n.NoteID); err != nil {
					return nil, false, err
				}
				return &n, false, nil
			}
		}
	}

	can, err := s.ank.CanAddNotes(ctx, []anki.Note{note})
	if err != nil {
		return nil, false, err
	}
	if len(can) == 1 && !can[0] {
		s.r.Logger.Info("Anki has a note with the same first field but a different lemma key; adding a separate note",
			"lemma_key", key, "lemma", note.Fields["Lemma"])
		return nil, true, nil
	}
	return nil, false, nil
}

// add creates the note and returns its ID.
func (s *syncer) add(ctx context.Context, note anki.Note, allowDuplicate bool) (int64, error) {
	if allowDuplicate {
		note.Options.AllowDuplicate = true
	}
	ids, err := s.ank.AddNotes(ctx, []anki.Note{note})
	if err != nil {
		return 0, err
	}
	if len(ids) != 1 || ids[0] == 0 {
		return 0, fmt.Errorf("anki did not add the note for %q", note.Fields["Lemma"])
	}
	return ids[0], nil
}

// index points the lemma index entry for key at noteID.
func (s *syncer) index(key string, enr model.Enrichment, noteID int64) error {
	return s.st.UpsertLemma(state.Lemma{
		Key:        key,
		SourceLang: enr.SourceLanguage,
		Lemma:      enr.Lemma,
		POS:        enr.PartOfSpeech,
		NoteID:     noteID,
	}, s.r.Now())
}

// fillIfEmpty lists fields that describe the lemma itself (not the first sentence), so a
// later sighting may fill them when the note has none yet.
var fillIfEmpty = []string{"IPA", "Forms"}

// merge adds the row's sentence to an existing note and, when the sentence is new, runs
// the sense check. Review scheduling is untouched.
func (s *syncer) merge(ctx context.Context, n anki.NoteInfo, entry model.VocabEntry, enr model.Enrichment, note anki.Note) (placement, error) {
	p := placement{action: actionMerge, noteID: n.NoteID}
	fields := map[string]string{}

	if ex, added := anki.AppendExample(n.Field("Examples"), n.Field("Context"), entry); added {
		fields["Examples"] = ex
		p.exampleAdded = true
	}
	for _, f := range fillIfEmpty {
		if strings.TrimSpace(n.Field(f)) == "" && note.Fields[f] != "" {
			fields[f] = note.Fields[f]
		}
	}

	// A sentence the note already has was judged before; only new ones are checked.
	if p.exampleAdded && s.r.Cfg.Features.SenseCheck {
		s.checkSense(ctx, n, entry, &p, fields)
	}

	if len(fields) > 0 {
		if err := s.ank.UpdateNoteFields(ctx, n.NoteID, fields); err != nil {
			return placement{}, err
		}
	}
	if p.review {
		if err := s.ank.AddTags(ctx, []int64{n.NoteID}, []string{"review"}); err != nil {
			return placement{}, err
		}
	}
	return p, nil
}

// checkSense asks the model whether the new sentence uses a new meaning and records the
// outcome in p and fields. A failed check is logged and leaves the merge as it is.
func (s *syncer) checkSense(ctx context.Context, n anki.NoteInfo, entry model.VocabEntry, p *placement, fields map[string]string) {
	f := s.r.Cfg.Features
	known := append([]string{anki.PlainText(n.Field("Translation"))}, anki.SenseItems(n.Field("OtherSenses"))...)
	res, err := s.oll.CheckSense(ctx, ollama.SenseRequest{
		SourceLanguage: anki.PlainText(n.Field("SourceLang")),
		Lemma:          anki.PlainText(n.Field("Lemma")),
		PartOfSpeech:   anki.PlainText(n.Field("POS")),
		KnownSenses:    known,
		Surface:        entry.Surface,
		Sentence:       entry.Context,
	}, s.r.Cfg.Target.Name)
	if err != nil {
		s.r.Logger.Warn("sense check failed; merging the example only", "word", entry.Word, "note_id", n.NoteID, "err", err)
		return
	}
	sense := res.NewSense()
	if sense == "" {
		return
	}
	if res.Confidence < f.SenseCheckMinConfidence {
		s.r.Logger.Info("possible new meaning below confidence threshold; tagging note for review",
			"word", entry.Word, "note_id", n.NoteID, "sense", sense, "confidence", res.Confidence)
		p.review = true
		return
	}
	value, outcome := anki.AppendSense(n.Field("OtherSenses"), n.Field("Translation"), sense, f.MaxOtherSenses)
	switch outcome {
	case anki.SenseAdded:
		fields["OtherSenses"] = value
		p.senseAdded = sense
	case anki.SenseListFull:
		s.r.Logger.Info("new meaning found but OtherSenses is full; tagging note for review",
			"word", entry.Word, "note_id", n.NoteID, "sense", sense, "max_other_senses", f.MaxOtherSenses)
		p.review = true
	}
}
