package store

import (
	"strings"
	"testing"
)

// ExtractDialogue 应同时识别 ASCII 双引号与中文弯引号对白。
func TestExtractDialogueQuotes(t *testing.T) {
	s := newTestStore(t)
	content := "林墨压低声音道：“你先走，这里交给我。”\n" +
		"老周摇头：\"I will not leave you behind.\"\n" +
		"旁白没有对白。"
	if err := s.Drafts.SaveFinalChapter(1, content); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}

	samples := s.Drafts.ExtractDialogue("林墨", nil, 5, 1)
	if len(samples) != 1 || !strings.Contains(samples[0], "“你先走，这里交给我。”") {
		t.Fatalf("应提取弯引号对白, got %+v", samples)
	}

	samples = s.Drafts.ExtractDialogue("老周", nil, 5, 1)
	if len(samples) != 1 || !strings.Contains(samples[0], `"I will not leave you behind."`) {
		t.Fatalf("应提取 ASCII 引号对白, got %+v", samples)
	}
}
