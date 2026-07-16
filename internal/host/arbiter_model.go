package host

import (
	"context"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
)

// usageTrackedModel 给 Arbiter 的模型调用接上用量追踪:裁定的 token/成本必须
// 进入预算与 usage 系统,否则预算上限对裁定开销失明、UI 用量不准。
// 记录身份用 agent="arbiter"(UsageTracker 对未知角色按 Default 价目计费)。
type usageTrackedModel struct {
	inner  agentcore.ChatModel
	record func(agentName, task string, msg agentcore.AgentMessage)
}

func newUsageTrackedModel(inner agentcore.ChatModel, record func(string, string, agentcore.AgentMessage)) agentcore.ChatModel {
	if record == nil {
		return inner
	}
	return &usageTrackedModel{inner: inner, record: record}
}

func (m *usageTrackedModel) Generate(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	resp, err := m.inner.Generate(ctx, msgs, tools, opts...)
	if err == nil && resp != nil {
		m.record("arbiter", "", resp.Message)
	}
	return resp, err
}

func (m *usageTrackedModel) GenerateStream(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	// Arbiter 只走 Generate;流式路径透传(若未来走流,usage 由消费端补记)。
	return m.inner.GenerateStream(ctx, msgs, tools, opts...)
}

func (m *usageTrackedModel) SupportsTools() bool { return m.inner.SupportsTools() }

// Capabilities 透传底层模型能力，确保 Arbiter 能正确判断 thinking 支持情况。
func (m *usageTrackedModel) Capabilities() llm.Capabilities {
	if cp, ok := m.inner.(llm.CapabilityProvider); ok {
		return cp.Capabilities()
	}
	return llm.Capabilities{}
}

// Info 透传底层模型元数据。
func (m *usageTrackedModel) Info() llm.ModelInfo {
	if info, ok := m.inner.(interface{ Info() llm.ModelInfo }); ok {
		return info.Info()
	}
	return llm.ModelInfo{}
}
