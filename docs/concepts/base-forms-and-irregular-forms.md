# Concept: Base-form notes and irregular forms

| | |
|---|---|
| **Status** | Draft for discussion |
| **Date** | 2026-11-09 |
| **Touches** | `internal/ollama`, `internal/model`, `internal/anki`, `internal/state`, `internal/pipeline`, `internal/config` |

## 1. Summary

Two related extensions to the `sync` pipeline:

- **A. Base-form notes.** Every Anki note is about the *dictionary base form* (lemma) of a
  looked-up word, not the inflected form that happened to appear in the book. Looking up
  *ran* and later *running* yields one note for *run*, not two. A later sighting of a lemma
  that already has a note adds the new sentence to that note instead of creating another one.
- **B. Irregular forms on the card.** When a lemma has forms a learner cannot derive from the
  regular rules (*go – went – gone*, *mouse – mice*, *bad – worse – worst*), the card shows
  them. The forms come from the same LLM call that already returns the lemma.

Part B builds on Part A (forms are a property of the lemma), so both are designed together
but can ship as separate steps.

## 2. How a note is built today

Example row from the test fixture: KOReader word `gouge(1)`, highlight `gouging`.

1. `koreader.List` builds `VocabEntry{Word: "gouge(1)", WordClean: "gouge", Surface: "gouging", Context: "a wall of white bone, gouging deep."}`
   ([reader.go](../../internal/koreader/reader.go)).
2. `ollama.Enrich` returns a `model.Enrichment` that already contains `lemma: "gouge"`
   ([model.go](../../internal/model/model.go), [prompt.go](../../internal/ollama/prompt.go)).
3. `anki.BuildNote` writes `Word = Surface` (`gouging`) as the **first field** and the lemma
   into a secondary `Lemma` field ([note.go](../../internal/anki/note.go)).
4. The state DB records the KOReader key `gouge(1)` as processed
   ([state.go](../../internal/state/state.go)).

Consequences:

- **One note per inflection.** Anki's duplicate check and sort field use the first field,
  i.e. the surface form. *ran*, *running* and *runs* become three notes. The state DB can't
  prevent that because it is keyed by KOReader row, not by lemma.
- **The headword is the seen form.** The recognition card shows *gouging* large; the base
  form is a small "Lemma:" line on the back.
- **Production asks for the wrong thing.** The production card's answer is
  `{{Word}} ({{Lemma}})` → "gouging (gouge)", so the learner is asked to produce an
  inflection that only made sense in one sentence.
- **Irregular forms are invisible.** `article`, `gender` and `plural` in the enrichment
  describe the *target-language translation* (e.g. German "der Hund, die Hunde" in
  English → German mode). Nothing describes the forms of the *source* word, so
  *beheld → behold* never tells the learner that the past tense is irregular.

## 3. Goals and non-goals

**Goals**

- **G1** Exactly one note per (source language, lemma, part of speech).
- **G2** The lemma is the headword on both cards; the seen form stays visible in its sentence.
- **G3** Seeing a known lemma again never creates a note and never resets review scheduling;
  it adds the sentence as an example.
- **G4** Cards show irregular (or otherwise unpredictable) forms of the lemma, configurable.
- **G5** Stay offline and language-agnostic; no extra LLM call in the default path.
- **G6** Existing users upgrade without losing review history or their template customizations.

**Non-goals**

- Full inflection tables or conjugation drills — only the principal parts.
- Automatically merging duplicate notes that already exist (report only, see §7).
- Linguistic guarantees. LLM output is checked for plausibility, not proven correct.

## 4. Part A — one note per base form

### 4.1 What the base form is

The lemma is the citation form a learner's dictionary uses as headword. Today the prompt only
says "Return the dictionary base form as the lemma"; it gets explicit rules:

| Part of speech | Rule | Examples |
|---|---|---|
| Verb | Infinitive without "to". Inherent reflexive pronoun kept. Separable particle re-attached when it is in the sentence. | *beheld → behold*, *ging → gehen*, *fing … an → anfangen*, *erinnerte mich → sich erinnern*, *sprang → springa* |
| Noun | Nominative singular. Plural-only nouns stay plural. German nouns capitalized. No article in the lemma (it belongs to the forms, Part B). | *mice → mouse*, *scissors → scissors*, *Hunde → Hund* |
| Adjective / adverb | Positive degree, uninflected. | *worse → bad*, *schönen → schön*, *större → stor* |
| Participle used as adjective | Verb lemma, unless dictionaries list the adjective as a headword of its own. | *gouging → gouge*, but *tired → tired* |
| Multi-word highlight | Citation form of the whole expression. | highlight *gave up → give up* |
| Proper noun, abbreviation | Unchanged. | |

If only a single word is highlighted but the meaning is that of a phrasal verb (*gave* in
"she gave up"), the lemma stays the single word *give* and the phrasal meaning goes into the
translation. See Q4.

### 4.2 Lemma key

Notes are identified by a normalized key:

```
lemma_key = source_lang + "|" + fold(lemma) + "|" + pos_class
            e.g. "en|behold|verb", "de|hund|noun"
```

- **`fold`**: Unicode NFC, trim, collapse inner whitespace, strip surrounding punctuation,
  lower-case. Only the key is folded; the note keeps the display form (*Hund*).
- **`pos_class`**: `part_of_speech` becomes a closed set — `noun`, `verb`, `adjective`,
  `adverb`, `pronoun`, `determiner`, `preposition`, `conjunction`, `interjection`, `numeral`,
  `phrase`, `other` — enforced as a JSON-schema `enum`. "Verb", "verb (transitive)" and
  "phrasal verb" can no longer produce different keys.

POS is part of the key so real homographs stay apart (*lead* the verb vs. *lead* the metal).
The trade-off: if the LLM misclassifies the POS, the lemma gets a second note. That is
accepted because a duplicate is visible and easy to fix, while a wrong merge silently mixes
two words. See Q1.

### 4.3 Note type v2

Field layout (new or changed fields in bold):

| # | Field | Content |
|---|---|---|
| 1 | **Lemma** | Headword. Now the first field → Anki's duplicate check (and the sort field of newly created note types). |
| 2 | Word | The form as seen in the book. Meaning unchanged; the name is kept so user templates that reference `{{Word}}` keep working. |
| 3 | **SeenAs** | Short grammatical description of the seen form, e.g. "past tense" (Part B). |
| 4 | **Forms** | Rendered key forms of the lemma, HTML (Part B). |
| 5 | Translation | unchanged |
| 6 | Context | First sentence the word was seen in; the seen form wrapped in `<b>`. |
| 7 | **Examples** | Later sightings as an HTML list, appended on merge. |
| 8… | ContextTranslation, Book, SourceLang, TargetLang, IPA, POS, Extra | unchanged |

All field values are HTML-escaped before they are sent. Today they are not, so a sentence
containing `<` or `&` renders incorrectly. The `<b>` around the seen form is added after
escaping.

Templates v2:

- **Recognition** — front: `Lemma` + `Context`. Back: `Translation`, `POS`, `SeenAs`, `Forms`,
  synonyms (`Extra`), `Examples`, `Book`.
- **Production** — front: `Translation` + `POS`. Back: `Lemma` + `Forms` + `Context`.

### 4.4 Pipeline flow

```
KOReader row not yet in `processed`
  │
  ▼
LLM enrich ──► lemma, pos, forms, translation, …   (one call, as today)
  │
  ▼
lemma_key = key(lang, lemma, pos)
  │
  ├─ key in lemma index ───────────────► merge into that note ─────────────┐
  │                                      (note no longer exists → as new)  │
  │                                                                        │
  └─ key not in index ─► canAddNotes                                        │
                          ├─ ok ──────────► addNotes, add key to index ────┤
                          └─ duplicate ───► findNotes + notesInfo          │
                                 ├─ a note has the same key ─► adopt it,   │
                                 │                             merge ──────┤
                                 └─ only other POS ─► addNotes with        │
                                                      allowDuplicate ──────┤
                                                                           ▼
                                   record row as processed (note id, lemma_key)
```

Rows are processed one by one, as today, so two rows with the same lemma in one run become one
add plus one merge.

### 4.5 Merging a sighting into an existing note

For a known lemma, koanki reads the note with `notesInfo` and writes changes with
`updateNoteFields`:

- **Examples:** append the sentence (seen form in bold, book title after it) unless the same
  sentence (folded) is already in `Context` or `Examples`. At most `max_examples` entries
  (default 3); when full, the oldest example is replaced. `Context` — the first sighting,
  shown on the front — is never changed.
- **Fill if empty:** `Forms`, `SeenAs`, `IPA` and similar fields are written only when the
  note's field is empty. Content written by the user or an earlier run is never overwritten.
- **Translation is not touched.** A different sense in the new sentence is out of scope for the
  first version (see the sense check in §11 and Q3).

Scheduling is not affected: `updateNoteFields` changes field content only. One known
AnkiConnect limitation: an update to a note that is currently open in Anki's browser editor
can get lost. Every merge is logged with its note ID, so this is easy to spot.

`sync --dry-run` prints `"action": "add"` or `"action": "merge"` with the target note ID and
writes nothing.

### 4.6 Lemma index in the state DB

New table. The state DB also gets a schema version (`PRAGMA user_version`) so that this and
later migrations are explicit:

```sql
CREATE TABLE IF NOT EXISTS lemmas (
  lemma_key    TEXT PRIMARY KEY,   -- "en|behold|verb"
  source_lang  TEXT NOT NULL,
  lemma        TEXT NOT NULL,      -- display form as written to the note
  pos          TEXT NOT NULL,
  anki_note_id INTEGER NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);
ALTER TABLE processed ADD COLUMN lemma_key TEXT;
```

The index is a cache of what is in Anki, not the source of truth:

- **Stale entry** (note deleted in Anki): `notesInfo` returns nothing → the entry is dropped and
  the word is treated as new.
- **Missing entry** (note created before v2, or on another machine): found through the
  duplicate branch in §4.4 and adopted.

So no backfill is required. An optional `koanki reindex` rebuilds the index from Anki in one go.

### 4.7 Anki's own duplicate check

With `Lemma` as first field, Anki's check (`allowDuplicate = false`, scope `deck`) becomes a
lemma check and agrees with the index in nearly all cases. The one deliberate exception is a
homograph with a different POS. koanki first confirms via `notesInfo` that no note with the
same key exists, then adds the note with `allowDuplicate = true`. Anki marks it as a duplicate
in the browser, which is harmless.

`findNotes` query: `"note:KOReader Vocab" "Lemma:<lemma>" "SourceLang:<lang>"`, with `*`, `_`,
`"` and `:` escaped. Results are checked again in code against the folded key, so Anki's search
semantics (case, wildcards) don't affect correctness.

## 5. Part B — irregular forms on the card

### 5.1 Which forms to show

"Irregular" alone doesn't fit every language. Two kinds of forms are worth showing:

1. **Irregular forms** — forms that the language's regular, productive rules don't produce from
   the lemma: en *went, gone, mice, worse*; de *ging, gegangen*; sv *sprang, sprungit*.
2. **Lexical forms** — properties every dictionary lists because they are unpredictable for a
   whole word class, regular or not: German noun gender and plural (*der Hund, die Hunde*),
   Swedish noun gender (*en/ett*) and plural.

So the LLM returns the lemma's **key forms** (principal parts) for its part of speech, each
flagged as irregular or not, and the code decides by policy what to show (§5.4).

### 5.2 Slots

A closed set of slot keys (schema `enum`) keeps labels consistent across cards. The displayed
labels come from a small table in code (English first; labels in the target language can be
added later, Q7).

| Slot | Meaning | Examples |
|---|---|---|
| `past` | simple past / preterite | en *go → went*, de *gehen → ging*, sv *gå → gick* |
| `past_participle` | past participle; German with auxiliary | en *gone*, de *ist gegangen* |
| `supine` | supine | sv *gått*, *sprungit* |
| `present_3sg` | 3rd person singular present, only when the stem changes | de *laufen → läuft* |
| `gender` | grammatical gender as article | de *der*, sv *en* |
| `plural` | plural; German with article | en *mice*, de *die Hunde*, sv *hundar* |
| `genitive` | genitive singular | de *des Hundes* |
| `comparative` | comparative | en *worse*, de *besser*, sv *bättre* |
| `superlative` | superlative | en *worst*, de *am besten*, sv *bäst* |
| `other` | anything else, with a free-text `label` | |

### 5.3 LLM contract

Schema additions in [schema.go](../../internal/ollama/schema.go):

```json
"part_of_speech": { "type": "string",
                    "enum": ["noun", "verb", "adjective", "adverb", "pronoun", "determiner",
                             "preposition", "conjunction", "interjection", "numeral",
                             "phrase", "other"] },
"seen_as":        { "type": ["string", "null"] },
"lemma_forms": {
  "type": "array",
  "items": {
    "type": "object",
    "properties": {
      "slot":      { "type": "string",
                     "enum": ["past", "past_participle", "supine", "present_3sg", "gender",
                              "plural", "genitive", "comparative", "superlative", "other"] },
      "label":     { "type": ["string", "null"] },
      "form":      { "type": "string" },
      "irregular": { "type": "boolean" }
    },
    "required": ["slot", "form", "irregular"]
  }
}
```

`lemma_forms` is optional; an empty array means the word class has no principal parts
(adverbs, prepositions, …). `model.Enrichment` gets `SeenAs *string` and
`LemmaForms []LemmaForm`.

Prompt additions (sketch), on top of the lemma rules from §4.1:

> In "lemma_forms", list the principal parts that a learner's dictionary of the **source**
> language gives for the lemma's part of speech (for example English verbs: past, past
> participle; German nouns: gender, plural, genitive; Swedish verbs: past, supine). These forms
> describe the source word, not the translation. Mark a form "irregular" only if the language's
> regular rules would not produce it from the lemma. Leave the list empty if the word class has
> no principal parts. In "seen_as", describe in a few {target} words the grammatical form of
> the surface form in the sentence, or null if the surface form is the lemma.

Ollama turns the JSON schema into a grammar, which supports `enum`. This still needs to be
checked with both `gemma3` and `qwen3` in the evaluation (§10).

### 5.4 Validation and display policy

After the LLM call, before the note is built:

- Drop items with an empty form; trim; keep only the first item per slot; at most 6 items.
- Variants go into one item: `"dreamed / dreamt"`.
- **English plausibility check (downgrade only):** if a form flagged `irregular` is exactly
  what the simple regular rules produce (`+ed`, `+s`/`+es`, `+er`/`+est`, with e-drop,
  y → i and optional consonant doubling), the flag is set to `false` and logged. This catches the most
  common hallucination ("walked" marked irregular) cheaply. It can only remove the irregular
  flag, never add it, so an imperfect rule set can't make things worse (*laid* ≠ *layed*,
  *paid* ≠ *payed* stay irregular). Other languages can get a rule set later; until then their
  flags are taken as returned.

Display policy (`features.forms`):

- `off` — never show forms.
- `irregular` (default) — show the full principal-parts row when **at least one** form is
  irregular; mark the irregular ones. Also always show the slots listed for the source
  language in `features.forms_always` (defaults: German and Swedish nouns → `gender`,
  `plural`).
- `always` — show the principal parts of every word that has them.

### 5.5 Rendering

The code renders the `Forms` field; the LLM never produces HTML. Labels are visible text, not
tooltips, because AnkiDroid/AnkiMobile have no hover:

```html
<span class="forms">
  <span class="fl">past</span> <span class="fv irr">beheld</span> ·
  <span class="fl">past participle</span> <span class="fv irr">beheld</span>
</span>
```

If the seen form is one of the forms, it is bold as well, so the card shows which form came up
in the book.

Recognition card (English, monolingual):

```
┌──────────────────────────────────────────────────┐
│                      behold                      │
│        "She **beheld** the ruin before her."     │
├──────────────────────────────────────────────────┤
│   to see or observe (something remarkable)       │
│   VERB · beheld = past tense                     │
│   past **beheld** · past participle beheld       │  bold = form seen in the book,
│                                                  │  irregular forms in color
│   Synonyms: see, observe, gaze upon              │
│   Also seen: "… he had never beheld …"           │
│   A Court of Wings and Ruin                      │
└──────────────────────────────────────────────────┘
```

Production card:

```
Front:  to see or observe (something remarkable)        VERB
Back:   behold
        past beheld · past participle beheld
        "She **beheld** the ruin before her."
```

German source word, English target (forms from `forms_always`):

```
Hund                                                    NOUN
der · plural die Hunde · genitive des Hundes
```

### 5.6 Optional third card: "Forms"

A third template drills the forms: front `Lemma` + the slot labels, back the forms. It reuses
the `Forms` field — on the front, a CSS rule hides the `.fv` values and leaves only the labels.
Anki only generates the card when the front renders content, so the front is wrapped in
`{{#Forms}}…{{/Forms}}` and words without forms get no third card.

Off by default (`features.forms_card = false`): adding a template to an existing note type
immediately creates a card for every existing note with non-empty `Forms`, which can be a lot
of new cards at once.

## 6. Configuration

New keys in `config.example.toml` and `config.FeaturesConfig`:

```toml
[features]
on_known_lemma = "merge"      # merge | skip | add
                              #   merge: append the sentence to the existing note (default)
                              #   skip:  record the row as processed, change nothing
                              #   add:   create a separate note anyway (old behavior)
max_examples   = 3            # sentences kept in the Examples field
seen_as        = true         # fill SeenAs ("beheld = past tense")
forms          = "irregular"  # off | irregular | always
forms_card     = false        # third card type that drills the forms

[features.forms_always]       # slots shown even when regular, per source language
de = ["gender", "plural"]
sv = ["gender", "plural"]
```

## 7. Migrating existing setups

**Note type.** `EnsureModel` today only creates the note type when it is missing, so existing
users would never get v2. New behavior in `init-anki` and the `sync` preflight:

1. Read `modelFieldNames`; add missing fields (`modelFieldAdd`); move `Lemma` to position 0
   (`modelFieldReposition`).
2. Shipped templates and CSS carry a version marker (`<!-- koanki:templates v2 -->`,
   `/* koanki:css v2 */`). v1 has no marker, so v1 is recognized by exact match with the
   shipped v1 text. Only unmodified templates are updated (`updateModelTemplates`,
   `updateModelStyling`). Customized templates are left alone; koanki prints a warning with the
   suggested changes, and `init-anki --force-templates` overwrites them.
3. Adding or moving fields is a schema change in Anki. At the next sync Anki asks for a full
   upload to AnkiWeb, so all devices should be synced before upgrading. The README and the
   `init-anki` output need to say this.

**Existing notes.** Every v1 note already has a `Lemma` (the field is required), so after the
reposition they are keyed by lemma automatically. Older notes keep their unbolded `Context`.
Two optional, separate commands:

- `koanki duplicates` — read-only report of notes that share a lemma key (e.g. the v1 notes for
  *ran* and *running*). Merging notes with review history stays a manual decision in Anki.
- `koanki backfill-forms [--dry-run]` — runs existing notes with empty `Forms` through the LLM
  (using `Lemma` + `Context`) and fills `Forms`/`SeenAs` only where they are empty.

**State DB.** Version bump: add the `lemmas` table and the `lemma_key` column. Existing
`processed` rows keep `lemma_key = NULL`, which is fine because they are never looked up by
lemma.

## 8. Edge cases

| Case | Handling |
|---|---|
| Dictionary-lookup row (no sentence) | Lemma from the word alone; `seen_as` null; nothing to bold; merge works the same. |
| Separable verb, only *fing* highlighted | The sentence lets the LLM re-attach the particle → *anfangen* (§4.1 rule). |
| Same surface, different lemmas (*left* → *leave* / *left*) | The sentence decides; different keys → different notes. |
| LLM lemma differs from KOReader's lookup word | The LLM wins; `WordClean` stays a hint in the prompt. |
| Variant forms (*dreamed/dreamt*) | One item `"dreamed / dreamt"`, irregular. |
| Uncountable / defective words (*information*) | Empty `lemma_forms`. |
| Word already in base form (*nook*) | `seen_as` null; regular forms → nothing shown in `irregular` mode. |
| User edited `Lemma` in Anki | Merges still go by note ID; `reindex` picks up the new lemma. |
| Note deleted in Anki | Stale index entry → new note (§4.6). |
| Note open in Anki's browser during a merge | AnkiConnect may drop the update; logged with note ID (§4.5). |
| `--deck` forces one deck for all languages | Key includes the language. A cross-language homograph triggers Anki's first-field check, gets verified via `notesInfo` and is added with `allowDuplicate` (§4.7). |

## 9. Alternatives considered

- **Stemmers (e.g. Snowball).** They produce stems, not dictionary forms (*gouging → goug*),
  so they can't be used as headwords.
- **Offline morphological data (Wiktionary extracts such as kaikki.org, Hunspell).** Very good
  for irregular forms, but each language is a large download, and lookups have no context
  (separable verbs, homographs). Possible later as an optional *verifier* behind an interface
  (`FormsProvider`), not for the first version.
- **A separate LLM call for the forms.** A smaller, focused prompt, but roughly double the
  latency per word, and the current call already has the sentence. Worth doing only if the
  evaluation shows quality drops with the bigger schema.
- **Merge by lemma only (no POS in the key).** Fewer special cases with Anki's duplicate check,
  but real homographs would be merged. Rejected (§4.2), see Q1.

## 10. Testing and evaluation

**Unit tests** (no network, same `httptest` style as today):

- Key folding (NFC, case, whitespace, punctuation) and POS mapping.
- `BuildNote` v2: `Lemma` first, escaping, bolding, `Forms` rendering for each policy.
- Merge: example append/dedupe/cap, fill-if-empty, stale index, adopt-from-duplicate branch,
  against a fake AnkiConnect (`canAddNotes`, `findNotes`, `notesInfo`, `updateNoteFields`).
- English downgrade-only check (*walked* downgraded; *went*, *laid*, *paid* untouched).
- State DB migration from a v1 file.
- Note type upgrade: v1 templates updated, customized templates left alone.
- Updated prompt golden tests.

**LLM evaluation** (opt-in, needs Ollama): a small `testdata/eval/lemmas.jsonl` with tricky
cases and expected lemma, POS and irregular forms, run via `koanki eval` (or
`go test -tags ollama`). It reports accuracy per model so `gemma3:27b`, `qwen3:32b` and smaller
models can be compared. Initial cases: *beheld → behold*, *gouging → gouge*, *mice → mouse*,
*worse → bad*, *went → go*, *children → child*, *ging → gehen*, *fing … an → anfangen*,
*Hunde → Hund*, *sprang → springa*, *större → stor*, *left* (verb vs. adjective), *dreamt*.

## 11. Rollout

1. **Phase 1 — base-form notes:** lemma rules in the prompt, POS enum, lemma key, note type v2
   (`Lemma` first, `Examples`), HTML escaping, lemma index, merge, note-type and state
   migration, dry-run output.
2. **Phase 2 — forms:** schema and prompt fields, validation, English check, display policy,
   rendering, `SeenAs`, configuration.
3. **Phase 3 — optional:** forms card, `backfill-forms`, `duplicates`, `reindex`, and a *sense
   check* for known lemmas — a second LLM call that compares the new sentence's meaning with
   the note's translation and, if it differs, adds it as another sense.

Each phase is its own pull request and can ship on its own.

## 12. Open questions

- **Q1** Homographs: separate notes per part of speech (recommended), or one note per lemma?
- **Q2** Recognition front: lemma large (recommended, matches the goal) or the seen form large
  with the lemma below?
- **Q3** Different meaning when a known lemma shows up again: ignore in the first version
  (recommended), always add as "other sense", or wait for the phase-3 sense check?
- **Q4** Phrasal verb from a single-word highlight: lemma *give* (recommended) or *give up*?
- **Q5** Examples: keep the first N stable, or rolling most recent N (recommended)?
- **Q6** German and Swedish nouns: always show gender and plural (recommended)?
- **Q7** Form labels in the target language (e.g. German labels in English → German mode), or
  English only for now (recommended)?

## Appendix: example enrichments

English source, English target, *beheld*:

```json
{
  "source_language": "en",
  "surface_form": "beheld",
  "seen_as": "past tense",
  "lemma": "behold",
  "part_of_speech": "verb",
  "translation": "to see or observe (something remarkable)",
  "translation_alternatives": ["see", "observe", "gaze upon"],
  "lemma_forms": [
    { "slot": "past",            "form": "beheld", "irregular": true },
    { "slot": "past_participle", "form": "beheld", "irregular": true }
  ],
  "confidence": 0.93
}
```

German source, English target, *fing* in "Sie fing sofort an zu lachen.":

```json
{
  "source_language": "de",
  "surface_form": "fing",
  "seen_as": "simple past, 3rd person singular (separable verb, particle 'an')",
  "lemma": "anfangen",
  "part_of_speech": "verb",
  "translation": "to begin, to start",
  "lemma_forms": [
    { "slot": "present_3sg",     "form": "fängt an",       "irregular": true },
    { "slot": "past",            "form": "fing an",        "irregular": true },
    { "slot": "past_participle", "form": "hat angefangen", "irregular": true }
  ],
  "confidence": 0.88
}
```

Swedish source, English target, *hundar*:

```json
{
  "source_language": "sv",
  "surface_form": "hundar",
  "seen_as": "indefinite plural",
  "lemma": "hund",
  "part_of_speech": "noun",
  "translation": "dog",
  "lemma_forms": [
    { "slot": "gender", "form": "en",     "irregular": false },
    { "slot": "plural", "form": "hundar", "irregular": false }
  ],
  "confidence": 0.95
}
```

With the default policy, *behold* and *anfangen* show their full principal parts because they
contain irregular forms. *hund* shows gender and plural because `forms_always` lists those
slots for Swedish nouns.
