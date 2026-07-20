package host

import (
	"context"
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
