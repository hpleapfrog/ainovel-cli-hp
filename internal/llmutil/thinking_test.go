package llmutil

import (
	"context"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
)

type fakeModel struct {
	infoFn func() llm.ModelInfo
	caps   llm.Capabilities
}

func (f *fakeModel) Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	return nil, nil
}

func (f *fakeModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	return nil, nil
}

func (f *fakeModel) SupportsTools() bool { return true }

func (f *fakeModel) Capabilities() llm.Capabilities { return f.caps }

func (f *fakeModel) Info() llm.ModelInfo {
	if f.infoFn != nil {
		return f.infoFn()
	}
	return llm.ModelInfo{}
}

func TestSafeThinkingLevel(t *testing.T) {
	cases := []struct {
		name string
		caps llm.ThinkingCapabilities
		want agentcore.ThinkingLevel
	}{
		{
			name: "OpenAI 非 reasoning 模型:Supported=No,Disable=Yes,应降级为空",
			caps: llm.ThinkingCapabilities{Supported: llm.SupportNo, Disable: llm.SupportYes, Efforts: nil},
			want: "",
		},
		{
			name: "OpenAI reasoning 模型:Supported=Partial,Disable=Yes,Off 保持 Off",
			caps: llm.ThinkingCapabilities{Supported: llm.SupportPartial, Disable: llm.SupportYes, Efforts: []agentcore.ThinkingLevel{agentcore.ThinkingLow}},
			want: agentcore.ThinkingOff,
		},
		{
			name: "Anthropic 等支持 thinking 模型:Supported=Yes,Off 保持 Off",
			caps: llm.ThinkingCapabilities{Supported: llm.SupportYes, Disable: llm.SupportYes, Efforts: []agentcore.ThinkingLevel{agentcore.ThinkingLow}},
			want: agentcore.ThinkingOff,
		},
		{
			name: "能力未知模型:保持 Off（不降级，让 provider 决定）",
			caps: llm.ThinkingCapabilities{Supported: llm.SupportUnknown, Disable: llm.SupportUnknown, Efforts: nil},
			want: agentcore.ThinkingOff,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &fakeModel{caps: llm.Capabilities{Thinking: c.caps}}
			got := SafeThinkingLevel(m, agentcore.ThinkingOff)
			if got != c.want {
				t.Fatalf("SafeThinkingLevel = %q, want %q", got, c.want)
			}
		})
	}
}
