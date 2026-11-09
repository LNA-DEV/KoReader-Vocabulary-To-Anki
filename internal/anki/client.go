// Package anki talks to the AnkiConnect add-on: it ensures a reversible note type and
// per-language decks exist, then adds notes (deduplicated). All calls are POSTs of the
// shape {action, version:6, params}.
package anki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/config"
)

const apiVersion = 6

// Client is an AnkiConnect client.
type Client struct {
	url          string
	httpClient   *http.Client
	ensuredDecks map[string]struct{} // decks created/verified this run
}

// New builds a Client bound to an AnkiConnect URL.
func New(url string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		url:          strings.TrimRight(url, "/"),
		httpClient:   httpClient,
		ensuredDecks: make(map[string]struct{}),
	}
}

type request struct {
	Action  string `json:"action"`
	Version int    `json:"version"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error"`
}

// call performs one AnkiConnect action and unmarshals result into out (if non-nil).
func (c *Client) call(ctx context.Context, action string, params any, out any) error {
	buf, err := json.Marshal(request{Action: action, Version: apiVersion, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reaching AnkiConnect at %s (is Anki running?): %w", c.url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ankiconnect HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decoding %s response: %w", action, err)
	}
	if r.Error != nil {
		return fmt.Errorf("ankiconnect %s: %s", action, *r.Error)
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("decoding %s result: %w", action, err)
		}
	}
	return nil
}

// Version returns the AnkiConnect API version (health check).
func (c *Client) Version(ctx context.Context) (int, error) {
	var v int
	err := c.call(ctx, "version", nil, &v)
	return v, err
}

// EnsureDeck creates the deck (full "Parent::Child" path) if not already ensured this
// run. AnkiConnect createDeck is idempotent and creates missing parents without
// disturbing an existing parent deck.
func (c *Client) EnsureDeck(ctx context.Context, deck string) error {
	if _, ok := c.ensuredDecks[deck]; ok {
		return nil
	}
	if err := c.call(ctx, "createDeck", map[string]any{"deck": deck}, nil); err != nil {
		return err
	}
	c.ensuredDecks[deck] = struct{}{}
	return nil
}

// modelExists reports whether a note type with the given name exists.
func (c *Client) modelExists(ctx context.Context, name string) (bool, error) {
	var names []string
	if err := c.call(ctx, "modelNames", nil, &names); err != nil {
		return false, err
	}
	return slices.Contains(names, name), nil
}

// ModelReport describes what EnsureModel did.
type ModelReport struct {
	Created          bool
	FieldsAdded      []string
	LemmaMovedFirst  bool
	TemplatesUpdated []string
	TemplatesSkipped []string // customized (or missing) templates left alone
	CSSUpdated       bool
	CSSSkipped       bool // customized stylesheet left alone
}

// SchemaChanged reports whether fields were added or reordered. Anki treats that as a
// schema change and asks for a full upload at the next AnkiWeb sync.
func (r ModelReport) SchemaChanged() bool {
	return len(r.FieldsAdded) > 0 || r.LemmaMovedFirst
}

// EnsureModel creates the reversible note type if it does not exist, and otherwise
// upgrades it to the current layout: missing fields are added, Lemma is moved to the
// first position, and unmodified templates/CSS are replaced by the current version.
// Customized templates/CSS are only replaced when force is true.
func (c *Client) EnsureModel(ctx context.Context, modelName string, force bool) (ModelReport, error) {
	var rep ModelReport
	exists, err := c.modelExists(ctx, modelName)
	if err != nil {
		return rep, err
	}
	if !exists {
		params := map[string]any{
			"modelName":     modelName,
			"inOrderFields": Fields,
			"css":           cardCSS,
			"isCloze":       false,
			"cardTemplates": cardTemplates(),
		}
		rep.Created = true
		return rep, c.call(ctx, "createModel", params, nil)
	}
	if err := c.upgradeFields(ctx, modelName, &rep); err != nil {
		return rep, err
	}
	if err := c.upgradeTemplates(ctx, modelName, force, &rep); err != nil {
		return rep, err
	}
	if err := c.upgradeCSS(ctx, modelName, force, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

func (c *Client) upgradeFields(ctx context.Context, modelName string, rep *ModelReport) error {
	var current []string
	if err := c.call(ctx, "modelFieldNames", map[string]any{"modelName": modelName}, &current); err != nil {
		return err
	}
	for i, f := range Fields {
		if slices.Contains(current, f) {
			continue
		}
		idx := min(i, len(current))
		if err := c.call(ctx, "modelFieldAdd", map[string]any{"modelName": modelName, "fieldName": f, "index": idx}, nil); err != nil {
			return err
		}
		current = slices.Insert(current, idx, f)
		rep.FieldsAdded = append(rep.FieldsAdded, f)
	}
	if current[0] != Fields[0] {
		if err := c.call(ctx, "modelFieldReposition", map[string]any{"modelName": modelName, "fieldName": Fields[0], "index": 0}, nil); err != nil {
			return err
		}
		rep.LemmaMovedFirst = true
	}
	return nil
}

type templateSides struct {
	Front string `json:"Front"`
	Back  string `json:"Back"`
}

func (c *Client) upgradeTemplates(ctx context.Context, modelName string, force bool, rep *ModelReport) error {
	var current map[string]templateSides
	if err := c.call(ctx, "modelTemplates", map[string]any{"modelName": modelName}, &current); err != nil {
		return err
	}
	v1 := make(map[string]cardTemplate)
	for _, t := range cardTemplatesV1() {
		v1[t.Name] = t
	}
	updates := make(map[string]templateSides)
	for _, want := range cardTemplates() {
		cur, ok := current[want.Name]
		switch {
		case !ok:
			// Adding a card type would create cards for every note; leave it to the user.
			rep.TemplatesSkipped = append(rep.TemplatesSkipped, want.Name)
		case sameText(cur.Front, want.Front) && sameText(cur.Back, want.Back):
			// already current
		case force || isUnmodified(cur, v1[want.Name]):
			updates[want.Name] = templateSides{Front: want.Front, Back: want.Back}
			rep.TemplatesUpdated = append(rep.TemplatesUpdated, want.Name)
		default:
			rep.TemplatesSkipped = append(rep.TemplatesSkipped, want.Name)
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return c.call(ctx, "updateModelTemplates", map[string]any{
		"model": map[string]any{"name": modelName, "templates": updates},
	}, nil)
}

// isUnmodified reports whether a stored template is a shipped version: an older
// koanki-marked template or the exact v1 text.
func isUnmodified(cur templateSides, v1 cardTemplate) bool {
	if strings.Contains(cur.Front, "<!-- koanki:templates ") && strings.Contains(cur.Back, "<!-- koanki:templates ") {
		return true
	}
	return sameText(cur.Front, v1.Front) && sameText(cur.Back, v1.Back)
}

func (c *Client) upgradeCSS(ctx context.Context, modelName string, force bool, rep *ModelReport) error {
	var current struct {
		CSS string `json:"css"`
	}
	if err := c.call(ctx, "modelStyling", map[string]any{"modelName": modelName}, &current); err != nil {
		return err
	}
	switch {
	case sameText(current.CSS, cardCSS):
		return nil
	case force || sameText(current.CSS, cardCSSV1) || strings.HasPrefix(strings.TrimSpace(current.CSS), "/* koanki:css "):
		rep.CSSUpdated = true
		return c.call(ctx, "updateModelStyling", map[string]any{
			"model": map[string]any{"name": modelName, "css": cardCSS},
		}, nil)
	default:
		rep.CSSSkipped = true
		return nil
	}
}

// NoteInfo is one entry of AnkiConnect's notesInfo result.
type NoteInfo struct {
	NoteID    int64    `json:"noteId"`
	ModelName string   `json:"modelName"`
	Tags      []string `json:"tags"`
	Fields    map[string]struct {
		Value string `json:"value"`
		Order int    `json:"order"`
	} `json:"fields"`
}

// Field returns a field's raw (HTML) value, "" when absent.
func (n NoteInfo) Field(name string) string {
	return n.Fields[name].Value
}

// FindNotes returns the IDs of notes matching an Anki search query.
func (c *Client) FindNotes(ctx context.Context, query string) ([]int64, error) {
	var ids []int64
	err := c.call(ctx, "findNotes", map[string]any{"query": query}, &ids)
	return ids, err
}

// NotesInfo returns the notes for the given IDs. IDs that no longer exist are omitted.
func (c *Client) NotesInfo(ctx context.Context, ids []int64) ([]NoteInfo, error) {
	var raw []NoteInfo
	if err := c.call(ctx, "notesInfo", map[string]any{"notes": ids}, &raw); err != nil {
		return nil, err
	}
	out := raw[:0]
	for _, n := range raw {
		if n.NoteID != 0 {
			out = append(out, n)
		}
	}
	return out, nil
}

// UpdateNoteFields overwrites the given fields of a note. Review scheduling is not
// affected. AnkiConnect may drop the update if the note is open in Anki's browser.
func (c *Client) UpdateNoteFields(ctx context.Context, id int64, fields map[string]string) error {
	return c.call(ctx, "updateNoteFields", map[string]any{
		"note": map[string]any{"id": id, "fields": fields},
	}, nil)
}

// AddTags adds tags to notes.
func (c *Client) AddTags(ctx context.Context, ids []int64, tags []string) error {
	return c.call(ctx, "addTags", map[string]any{"notes": ids, "tags": strings.Join(tags, " ")}, nil)
}

// SearchTerm quotes one "field:value" term for an Anki search, escaping the characters
// Anki treats specially inside a quoted term.
func SearchTerm(field, value string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `*`, `\*`, `_`, `\_`)
	return `"` + r.Replace(field) + `:` + r.Replace(value) + `"`
}

// CanAddNotes returns, per note, whether Anki would accept it (false = duplicate/invalid).
func (c *Client) CanAddNotes(ctx context.Context, notes []Note) ([]bool, error) {
	var out []bool
	err := c.call(ctx, "canAddNotes", map[string]any{"notes": notes}, &out)
	return out, err
}

// AddNotes adds notes and returns their new note IDs (null entries → 0 for skipped).
func (c *Client) AddNotes(ctx context.Context, notes []Note) ([]int64, error) {
	var raw []*int64
	if err := c.call(ctx, "addNotes", map[string]any{"notes": notes}, &raw); err != nil {
		return nil, err
	}
	ids := make([]int64, len(raw))
	for i, p := range raw {
		if p != nil {
			ids[i] = *p
		}
	}
	return ids, nil
}

// ResolveDeck picks the deck path for a note given its source language and the config.
func ResolveDeck(d config.DecksConfig, sourceLang string) string {
	if !d.RouteByLanguage {
		return d.Default
	}
	code := strings.ToLower(strings.TrimSpace(sourceLang))
	if code == "" {
		return d.Fallback
	}
	if deck, ok := d.Map[code]; ok {
		return deck
	}
	if d.Template != "" {
		deck := strings.ReplaceAll(d.Template, "{language_code}", code)
		deck = strings.ReplaceAll(deck, "{language_name}", config.LanguageName(code))
		return deck
	}
	return d.Fallback
}
