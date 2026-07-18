package ollama

import (
	"strings"
	"testing"

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
