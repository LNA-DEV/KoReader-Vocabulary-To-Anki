package ollama

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/lemma"
	"github.com/LNA-DEV/KoReader-Vocabulary-To-Anki/internal/model"
)

func TestBuildMessagesWithContext(t *testing.T) {
	e := model.VocabEntry{
		Surface:   "gouging",
		WordClean: "gouge",
		Context:   "a wall of white bone, gouging deep.",
		Book:      "A Court of Wings and Ruin",
	}
	msgs := BuildMessages(e, "English")
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("unexpected message shape: %+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "English") {
		t.Error("system prompt should mention the target language")
	}
	u := msgs[1].Content
	for _, want := range []string{"gouging", "gouge", "a wall of white bone, gouging deep.", "A Court of Wings and Ruin"} {
		if !strings.Contains(u, want) {
			t.Errorf("user prompt missing %q\n%s", want, u)
		}
	}
}

func TestBuildMessagesNoContext(t *testing.T) {
	e := model.VocabEntry{Surface: "jilt", WordClean: "jilt", Context: "", Book: "Dictionary lookup"}
	u := BuildMessages(e, "German")[1].Content
	if !strings.Contains(u, "(no context available)") {
		t.Errorf("empty context should render placeholder, got:\n%s", u)
	}
}

func TestSystemPromptTargetLanguage(t *testing.T) {
	if !strings.Contains(systemPrompt("German"), "German") {
		t.Error("system prompt not parameterized by target language")
	}
}

func TestSystemPromptLemmaRules(t *testing.T) {
	p := systemPrompt("English")
	for _, want := range []string{"citation form", "give up", "anfangen", "sich erinnern", "scissors", "worse"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing lemma rule example %q", want)
		}
	}
}

func TestBuildSenseMessages(t *testing.T) {
	msgs := BuildSenseMessages(SenseRequest{
		SourceLanguage: "en", Lemma: "bank", PartOfSpeech: "noun",
		KnownSenses: []string{"an institution that keeps money", "a row of similar things"},
		Surface:     "bank", Sentence: "They sat on the bank of the river.",
	}, "German")
	if len(msgs) != 2 || !strings.Contains(msgs[0].Content, "German") {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	u := msgs[1].Content
	for _, want := range []string{"bank (noun, language: en)", "1. an institution that keeps money", "2. a row of similar things", "They sat on the bank of the river."} {
		if !strings.Contains(u, want) {
			t.Errorf("sense prompt missing %q\n%s", want, u)
		}
	}
}

func TestSchemaRestrictsPartOfSpeech(t *testing.T) {
	var s struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaJSON, &s); err != nil {
		t.Fatal(err)
	}
	enum := s.Properties["part_of_speech"].Enum
	if !slices.Equal(enum, lemma.POSClasses) {
		t.Errorf("part_of_speech enum = %v, want %v", enum, lemma.POSClasses)
	}
}

func TestSystemPromptFormsInstructions(t *testing.T) {
	p := systemPrompt("German")
	for _, want := range []string{"lemma_forms", "SOURCE language", "past_participle", "dreamed / dreamt", "seen_as", "in a few German words"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	for _, slot := range model.FormSlots {
		if !strings.Contains(p, `"`+slot+`"`) {
			t.Errorf("system prompt does not explain slot %q", slot)
		}
	}
}

func TestSchemaRestrictsFormSlots(t *testing.T) {
	var s struct {
		Properties struct {
			LemmaForms struct {
				Items struct {
					Properties struct {
						Slot struct {
							Enum []string `json:"enum"`
						} `json:"slot"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"lemma_forms"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaJSON, &s); err != nil {
		t.Fatal(err)
	}
	if got := s.Properties.LemmaForms.Items.Properties.Slot.Enum; !slices.Equal(got, model.FormSlots) {
		t.Errorf("lemma_forms slot enum = %v, want %v", got, model.FormSlots)
	}
}
