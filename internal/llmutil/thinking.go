// Package llmutil 提供与 agentcore/litellm 交互相关的辅助函数，
// 用于在不暴露底层细节的情况下做模型能力感知的安全决策。
package llmutil

import (
	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
)

// SafeThinkingLevel 根据模型实际能力把期望的 thinking 级别转成一个安全的调用级别。
//
// 某些 provider（如 OpenAI 兼容接口）对非 reasoning 模型仍会在 Capabilities 中声明
// Disable=Yes，导致 ThinkingPolicy.Resolve(ThinkingOff) 返回 ThinkingOff；但请求构建器
// 只要看到非 Unspecified 的 thinking 模式就会拒绝（"thinking is only supported for
// reasoning chat models"）。本函数在模型明确报告不支持 thinking 时，降级为空字符串，
// 即不覆盖 provider 默认行为，避免触发该校验错误。
func SafeThinkingLevel(model agentcore.ChatModel, want agentcore.ThinkingLevel) agentcore.ThinkingLevel {
	policy := llm.ThinkingPolicyFor(model)
	resolved, ok := policy.Resolve(want)
	if !ok {
		return ""
	}
	cp, ok := model.(llm.CapabilityProvider)
	if !ok {
		return resolved
	}
	caps := cp.Capabilities()
	if caps.Thinking.Supported == llm.SupportNo {
		return ""
	}
	return resolved
}
