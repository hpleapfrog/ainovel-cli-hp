package headless

import (
	"context"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/tools"
)

func TestTerminalAskUserSingleSelect(t *testing.T) {
	handler := newTerminalAskUser(strings.NewReader("2\n"), &strings.Builder{})
	resp, err := handler.handle(context.Background(), []tools.Question{
		{
			Question: "你想要什么风格？",
			Header:   "风格",
			Options: []tools.Option{
				{Label: "热血", Description: "偏升级"},
				{Label: "悬疑", Description: "偏谜团"},
			},
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := resp.Answers["你想要什么风格？"]; got != "悬疑" {
		t.Fatalf("unexpected answer: %q", got)
	}
}

func TestTerminalAskUserCustomInput(t *testing.T) {
	handler := newTerminalAskUser(strings.NewReader("0\n不要感情线\n"), &strings.Builder{})
	resp, err := handler.handle(context.Background(), []tools.Question{
		{
			Question: "还有什么限制？",
			Header:   "限制",
			Options: []tools.Option{
				{Label: "黑暗", Description: "整体压抑"},
				{Label: "轻松", Description: "基调明快"},
			},
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := resp.Answers["还有什么限制？"]; got != "自定义" {
		t.Fatalf("unexpected answer: %q", got)
	}
	if got := resp.Notes["还有什么限制？"]; got != "不要感情线" {
		t.Fatalf("unexpected note: %q", got)
	}
}

func TestParseSelections(t *testing.T) {
	options := []tools.Option{
		{Label: "热血", Description: "偏升级"},
		{Label: "悬疑", Description: "偏谜团"},
		{Label: "黑暗", Description: "整体压抑"},
	}

	labels, err := parseSelections("1, 3", options, true)
	if err != nil {
		t.Fatalf("parseSelections: %v", err)
	}
	if got := strings.Join(labels, "、"); got != "热血、黑暗" {
		t.Fatalf("unexpected labels: %q", got)
	}

	// 严格解析：编号必须是完整整数，"2abc" 不能像 Sscanf 那样被宽容截断成 2。
	for _, line := range []string{"2abc", "1x", "1.5", "abc"} {
		if _, err := parseSelections(line, options, true); err == nil {
			t.Fatalf("parseSelections(%q) 应报错", line)
		}
	}

	if _, err := parseSelections("4", options, true); err == nil {
		t.Fatalf("parseSelections 超出范围应报错")
	}
	if _, err := parseSelections("1,2", options, false); err == nil {
		t.Fatalf("parseSelections 单选传入多个编号应报错")
	}
}
