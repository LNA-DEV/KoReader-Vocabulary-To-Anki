# KoReader-Vocabulary-To-Anki (`koanki`)

Turn your [KOReader](https://koreader.rocks/) **Vocabulary Builder** lookups into
[Anki](https://apps.ankiweb.net/) flashcards — fully offline, using a **local** LLM via
[Ollama](https://ollama.com/) and the [AnkiConnect](https://ankiweb.net/shared/info/2055492159)
add-on.

For each *new* word it infers the source language, lemmatizes, and — using the sentence
the word appeared in to disambiguate — produces an in-context entry, then pushes a
**bidirectional** note (recognition + production cards) into Anki. There is **one note per
base form**: looking up *ran* and later *running* gives one note for *run*, and the second
sentence is added to it as an example. Irregular forms of the word (*go – went – gone*,
*mouse – mice*) are shown on the card. It's safe to re-run: only words not seen before are
processed.

- **Default:** English → English (monolingual definitions + synonyms).
- **Bilingual:** e.g. `--target de` → English → German, with article/gender/plural filled.
  Everything (language, model, decks, tags) is a config/flag parameter — nothing is hard-coded.

## How it works

```
vocabulary_builder.sqlite3 → [read, read-only] → [delta vs state.db]
  → [Ollama: one JSON object per new word, incl. lemma, part of speech and its forms]
  → [Anki: lemma already has a note? merge the sentence into it : add a new note]
  → [record processed words + lemma index]
```

## Prerequisites

1. **Ollama** running with a model pulled:
   ```sh
   ollama pull gemma3:27b        # default; or qwen3:32b, gemma3:12b, gemma3:4b, …
   ```
2. **Anki** running with the **AnkiConnect** add-on installed (code `2055492159`).
   (Only required for `sync` / `init-anki`, not for `list` / `enrich`.)
3. A synced copy of KOReader's `vocabulary_builder.sqlite3` on this machine. Getting the
   file off the e-reader (WebDAV/SFTP/USB) is out of scope — point `input.db_path` at it.
4. **Go 1.22+** to build.

## Build

```sh
go build -o koanki ./cmd/koanki
```

## Configure

Copy `config.example.toml` to `config.toml` and edit `input.db_path` (and, if you want
German cards, set `target.language = "de"`). Every key has a built-in default, so a
config file is optional — flags can override anything.

**koanki finds the config automatically** (no `--config` needed) from, in order:

1. `./config.toml` — the directory you run koanki from
2. `~/.config/koanki/config.toml` — a fixed, machine-wide location

Use `~/.config/koanki/config.toml` if you want it picked up no matter where you run from.
Pass `--config PATH` to override. **Precedence:** flags → config file → built-in defaults.

> Relative paths in the config (`db_path`, `state.db_path`) resolve against your **current
> working directory**. If you'll run koanki from different directories, use **absolute
> paths** (e.g. `db_path = "/home/lna-dev/koanki/vocabulary_builder.sqlite3"`).

## Usage

```sh
koanki list   [--all]                              # dump the delta (or --all rows)
koanki enrich --word W --context "..." [--book B]  # run ONE word through the LLM, print JSON
koanki init-anki [--force-templates]               # create/upgrade the note type, create deck(s)
koanki sync   [--dry-run] [--limit N]              # the main pipeline
```

Common flags override the config file: `--config --db --model --target --ollama-url
--anki-url --deck`. `--deck NAME` forces every note into one deck (disables language
routing).

### Typical first run

```sh
./koanki list --limit 5                      # sanity-check what will be processed
./koanki enrich --word gouge --context "gouging deep."     # eyeball one enrichment
./koanki init-anki                           # create deck + note type in Anki
./koanki sync --dry-run --limit 5            # enrich + print, write nothing
./koanki sync                                # real run
```

A one-off run in another language/model needs no config edit:

```sh
./koanki sync --target de --model qwen3:32b
```

## Anki note type

`sync`/`init-anki` create a note type **KOReader Vocab** with two card templates so each
note studies both directions:

- **Recognition (word → meaning):** front shows the base form (lemma) large and the sentence
  with the form you met in **bold**; back shows the definition/translation, other meanings,
  part of speech, what the seen form is ("beheld = past tense"), the word's forms, synonyms,
  further example sentences and the book.
- **Production (meaning → word):** front shows the meaning; back recalls the base form, its
  forms and the sentence.

The first field is `Lemma`, so Anki's own duplicate check works on the base form. Notes are
routed into a per-source-language deck (see `[anki.decks]` in the config) and tagged
`koreader` + `lang:<code>`. Low-confidence enrichments also get a `review` tag.

### One note per base form

A note is identified by source language + lemma + part of speech, so *lead* (to guide) and
*lead* (the metal) get separate notes. When a new lookup belongs to a lemma that already has
a note (`features.on_known_lemma = "merge"`, the default):

- its sentence is appended to the note's **Examples** (no cap, no duplicates); the first
  sentence on the front never changes, and review scheduling is not touched;
- a second, small LLM call (the **sense check**) decides whether the word is used in a new
  meaning there (*bank* of a river vs. *bank* for money). A new meaning goes into the
  **OtherSenses** field on the back of the recognition card; an uncertain one only tags the
  note `review`.

### Irregular forms

The model also returns the word's principal parts in the **source** language (English verbs:
past and past participle; German nouns: gender, plural, genitive; …), each marked irregular
or not. With `features.forms = "irregular"` (default) a card shows them only when at least
one form is irregular, with the irregular ones highlighted and the form you met in bold:

```
behold      past beheld · past participle beheld
mouse       plural mice
```

Some forms are worth showing even when regular, because a learner can't predict them —
German and Swedish noun gender and plural are on by default (`[features.forms_always]`):

```
Hund        der · plural die Hunde
```

For English, a form the model marks irregular although the regular rules produce it
(*walked*) is corrected before the card is built. `forms = "always"` shows the forms of every
word, `"off"` none.

### Upgrading from an earlier version

The first `sync` or `init-anki` after updating upgrades an existing **KOReader Vocab** note
type in place: it adds the new fields (`SeenAs`, `Forms`, `OtherSenses`, `Examples`) and moves
`Lemma` to the first position. Existing notes and their review history stay as they are.

- **Sync all your devices with AnkiWeb before upgrading.** Adding or moving fields is a schema
  change, so Anki asks for a full upload at the next sync.
- Card templates and styling are replaced only if you never changed them. Customized ones are
  left alone and koanki logs a warning; `koanki init-anki --force-templates` replaces them.
- Notes you already have for different forms of the same word (e.g. *ran* and *running*)
  stay separate; new lookups merge into the first matching one.

## Idempotency

A local `koanki-state.db` records every processed word, so the LLM never re-sees a known
word. It also keeps a **lemma index** (which note holds which base form). The index is only a
cache: a note deleted in Anki is recreated on the next lookup, and notes created before the
index existed are found in Anki and adopted. Re-running is always safe and only processes new
lookups.

## Development

```sh
go test ./...        # unit tests (reader, state, lemma, ollama/anki httptest, prompt,
                     #   end-to-end sync against fake Ollama + fake AnkiConnect)
go vet ./...
```

The reader test runs against a small committed fixture,
`internal/koreader/testdata/vocab_fixture.sqlite3` (regenerate with `fixture.sql`). Your
real DB in `.inbox/` is gitignored and never committed.
