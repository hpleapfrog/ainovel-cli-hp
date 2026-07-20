package host

import (
	"context"
	"errors"
	"testing"

	"github.com/voocel/agentcore"
)

// stubChatModel 是 ChatModel 的最小 mock：Generate 返回固定响应，GenerateStream
// 回放固定事件序列。
type stubChatModel struct {
	resp   *agentcore.LLMResponse
	err    error
	stream []agentcore.StreamEvent
}

func (m *stubChatModel) Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	return m.resp, m.err
}

func (m *stubChatModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	ch := make(chan agentcore.StreamEvent, len(m.stream))
	for _, ev := range m.stream {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (m *stubChatModel) SupportsTools() bool { return true }

func assistantUsageMsg(text string, u agentcore.Usage) agentcore.Message {
	return agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(text)},
		Usage:   &u,
	}
}

// 导入/仿写/裁定路径共用同一个包装：Generate 成功且带 Usage 时按指定身份记账，
// token 累计进 UsageTracker（预算经 onCost 自动覆盖）。
func TestUsageTrackedModelAs_RecordsUsageWithIdentity(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	inner := &stubChatModel{resp: &agentcore.LLMResponse{
		Message: assistantUsageMsg("ok", agentcore.Usage{Input: 100, Output: 20}),
	}}
	m := newUsageTrackedModelAs(inner, "import", tk.Record)
	if _, err := m.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_, input, output, _, _ := tk.Totals()
	if input != 100 || output != 20 {
		t.Fatalf("totals = input:%d output:%d, want 100/20", input, output)
	}
	var sawImport bool
	for _, a := range tk.PerAgent() {
		if a.Role == "import" {
			sawImport = true
		}
	}
	if !sawImport {
		t.Fatal("per-agent totals should include role import")
	}
}

// 响应不带 Usage 时无量可计：不累计、也不误触 missingAssistantUsage 诊断。
// Generate 出错同样不记账。
func TestUsageTrackedModelAs_SkipsRecordWithoutUsage(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	inner := &stubChatModel{resp: &agentcore.LLMResponse{
		Message: agentcore.Message{
			Role:    agentcore.RoleAssistant,
			Content: []agentcore.ContentBlock{agentcore.TextBlock("ok")},
		},
	}}
	m := newUsageTrackedModelAs(inner, "simulate", tk.Record)
	if _, err := m.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	failing := &stubChatModel{err: errors.New("boom")}
	m = newUsageTrackedModelAs(failing, "simulate", tk.Record)
	if _, err := m.Generate(context.Background(), nil, nil); err == nil {
		t.Fatal("expected generate error")
	}

	_, input, output, _, _ := tk.Totals()
	if input != 0 || output != 0 {
		t.Fatalf("no-usage/error responses should not accumulate, got input:%d output:%d", input, output)
	}
	if got := tk.MissingAssistantUsage(); got != 0 {
		t.Fatalf("missing-usage diagnostic should not fire, got %d", got)
	}
}
