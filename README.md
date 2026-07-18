# KoReader-Vocabulary-To-Anki (`koanki`)

Turn your [KOReader](https://koreader.rocks/) **Vocabulary Builder** lookups into
[Anki](https://apps.ankiweb.net/) flashcards — fully offline, using a **local** LLM via
[Ollama](https://ollama.com/) and the [AnkiConnect](https://ankiweb.net/shared/info/2055492159)
add-on.

For each *new* word it infers the source language, lemmatizes, and — using the sentence
the word appeared in to disambiguate — produces an in-context entry, then pushes a
**bidirectional** note (recognition + production cards) into Anki. It's safe to re-run:
only words not seen before are processed.

- **Default:** English → English (monolingual definitions + synonyms).
- **Bilingual:** e.g. `--target de` → English → German, with article/gender/plural filled.
  Everything (language, model, decks, tags) is a config/flag parameter — nothing is hard-coded.

## How it works

```
vocabulary_builder.sqlite3 → [read, read-only] → [delta vs state.db]
  → [Ollama: one JSON object per new word] → [Anki: ensure deck + note type, addNotes]
  → [record processed words]
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

## Usage

```sh
koanki list   [--all]                              # dump the delta (or --all rows)
koanki enrich --word W --context "..." [--book B]  # run ONE word through the LLM, print JSON
koanki init-anki                                   # create the deck(s) + note type only
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

- **Recognition (word → meaning):** front shows the word + sentence; back shows the
  definition/translation, lemma, POS, synonyms.
- **Production (meaning → word):** front shows the meaning; back recalls the word + sentence.

Notes are routed into a per-source-language deck (see `[anki.decks]` in the config) and
tagged `koreader` + `lang:<code>`. Low-confidence enrichments also get a `review` tag.

## Idempotency

A local `koanki-state.db` records every processed word, so the LLM never re-sees a known
word and Anki never gets a duplicate (AnkiConnect's `allowDuplicate=false` is a second
safety net). Re-running is always safe and only processes new lookups.

## Development

```sh
go test ./...        # unit tests (reader, state, ollama httptest, anki httptest, prompt)
go vet ./...
```

The reader test runs against a small committed fixture,
`internal/koreader/testdata/vocab_fixture.sqlite3` (regenerate with `fixture.sql`). Your
real DB in `.inbox/` is gitignored and never committed.
