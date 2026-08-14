package host

import (
	"context"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
)

// 共创的 LLM 消耗必须进 UsageTracker（身份 "cocreate"）：流结束时的最终消息
// 携带 Usage，据此记账，否则用量面板与预算对共创路径失明。
func TestCoCreateStream_RecordsUsage(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	inner := &stubChatModel{stream: []agentcore.StreamEvent{
		{Type: agentcore.StreamEventTextDelta, Delta: "<reply>好</reply>"},
		{Type: agentcore.StreamEventDone, Message: assistantUsageMsg("<reply>好</reply>", agentcore.Usage{Input: 42, Output: 7})},
	}}
	ms := &bootstrap.ModelSet{Default: bootstrap.NewSwappableModel("test", "stub-model", inner)}

	reply, err := coCreateStream(context.Background(), ms, nil, tk.Record, "sys",
		[]CoCreateMessage{{Role: "user", Content: "想法"}}, nil)
	if err != nil {
		t.Fatalf("coCreateStream: %v", err)
	}
	if reply.Message != "好" {
		t.Fatalf("reply = %q, want 好", reply.Message)
	}
	_, input, output, _, _ := tk.Totals()
	if input != 42 || output != 7 {
		t.Fatalf("totals = input:%d output:%d, want 42/7", input, output)
	}
	var sawCoCreate bool
	for _, a := range tk.PerAgent() {
		if a.Role == "cocreate" {
			sawCoCreate = true
		}
	}
	if !sawCoCreate {
		t.Fatal("per-agent totals should include role cocreate")
	}
}

// 流式路径同样遵守"无 Usage 不记账"口径：最终消息不带 Usage 时总量为零。
func TestCoCreateStream_SkipsRecordWithoutUsage(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	done := agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock("<reply>好</reply>")},
	}
	inner := &stubChatModel{stream: []agentcore.StreamEvent{
		{Type: agentcore.StreamEventDone, Message: done},
	}}
	ms := &bootstrap.ModelSet{Default: bootstrap.NewSwappableModel("test", "stub-model", inner)}

	if _, err := coCreateStream(context.Background(), ms, nil, tk.Record, "sys",
		[]CoCreateMessage{{Role: "user", Content: "想法"}}, nil); err != nil {
		t.Fatalf("coCreateStream: %v", err)
	}
	_, input, output, _, _ := tk.Totals()
	if input != 0 || output != 0 {
		t.Fatalf("no-usage response should not accumulate, got input:%d output:%d", input, output)
	}
}

// ── 结构化 JSON 协议（P4-10）──

func TestParseCoCreateResponse_StructuredJSON(t *testing.T) {
	raw := "```json\n{\"reply\": \"好，我们开始。\", \"draft\": \"## 主题\\n- 悬疑\\n- 都市\", \"ready\": true, \"suggestions\": [\"我想写仙侠\", \"换个方向\"]}\n```"
	got, err := parseCoCreateResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Message != "好，我们开始。" {
		t.Fatalf("reply = %q", got.Message)
	}
	if !strings.Contains(got.Prompt, "## 主题") || !strings.Contains(got.Prompt, "\n- 悬疑") {
		t.Fatalf("draft escapes not decoded: %q", got.Prompt)
	}
	if !got.Ready {
		t.Fatal("ready should be true")
	}
	if len(got.Suggestions) != 2 || got.Suggestions[0] != "我想写仙侠" {
		t.Fatalf("suggestions = %v", got.Suggestions)
	}
}

func TestParseCoCreateResponse_XMLFallbackStillWorks(t *testing.T) {
	raw := "<reply>好</reply>\n<draft>## 主题\n- 悬疑</draft>\n<ready>false</ready>\n<suggestions>\n- 我想写仙侠\n</suggestions>"
	got, err := parseCoCreateResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Message != "好" || !strings.Contains(got.Prompt, "## 主题") || got.Ready {
		t.Fatalf("xml fallback wrong: %+v", got)
	}
	if len(got.Suggestions) != 1 || got.Suggestions[0] != "我想写仙侠" {
		t.Fatalf("suggestions = %v", got.Suggestions)
	}
}

func TestParseCoCreateResponse_BrokenJSONFallsBackToText(t *testing.T) {
	raw := `{"reply": "好", "draft": "未转义
换行会破坏 JSON"}`
	got, err := parseCoCreateResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 非法 JSON → XML 也解析不出 → 整段作为 reply,不丢显示。
	if got.Message == "" {
		t.Fatal("reply must not be empty after fallback")
	}
}

func TestExtractReplyPreview_JSONAndXML(t *testing.T) {
	if got := extractReplyPreview(`{"reply": "好，我们开`); got != "好，我们开" {
		t.Fatalf("partial JSON preview = %q", got)
	}
	if got := extractReplyPreview(`{"reply": "好\n吗", "draft": "x"}`); got != "好\n吗" {
		t.Fatalf("closed JSON preview = %q", got)
	}
	if got := extractReplyPreview("<reply>好</reply><draft>x</draft>"); got != "好" {
		t.Fatalf("xml preview = %q", got)
	}
}
