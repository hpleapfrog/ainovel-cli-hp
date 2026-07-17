package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/userrules"
)

// povFakeModel 是最小 fake ChatModel：固定回吐含 pov_person 的归一化 JSON，
// 记录收到的 messages 供断言自然语言规则原文确实进了归一化。
type povFakeModel struct {
	lastMsgs []agentcore.Message
}

func (m *povFakeModel) Generate(_ context.Context, messages []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	m.lastMsgs = messages
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role: agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(
			`{"structured":{"pov_person":"third"},"preferences":"文风克制","uncertain":[]}`)},
	}}, nil
}

func (m *povFakeModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	return nil, nil
}

func (m *povFakeModel) SupportsTools() bool { return false }

// 全链路：./.ainovel/rules/*.md 自然语言 → 归一化 → 快照落盘 → commit 机械检查
// → rule_violations 透出（commit 返回值 + 落盘供 editor 消费）。
func TestPOVRuleEndToEnd(t *testing.T) {
	// 1. 项目规则目录放一条自然语言规则
	projectDir := t.TempDir()
	rulesDir := filepath.Join(projectDir, ".ainovel", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const ruleText = "全程第三人称，禁止第一人称叙述"
	if err := os.WriteFile(filepath.Join(rulesDir, "pov.md"), []byte(ruleText), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 2. 归一化 → 快照（假模型模拟 LLM 对该 md 的抽取）
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	model := &povFakeModel{}
	svc := userrules.NewService(st, model, rules.LoadOptions{ProjectRulesDir: rulesDir})
	snap, err := svc.Build(t.Context(), "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if snap.Structured.POVPerson != "third" {
		t.Fatalf("快照应含 pov_person=third，got %+v", snap.Structured)
	}
	// 自然语言原文确实进了归一化（不是凭空产出）
	var sawRule bool
	for _, msg := range model.lastMsgs {
		if strings.Contains(msg.TextContent(), ruleText) {
			sawRule = true
		}
	}
	if !sawRule {
		t.Fatal("归一化器应收到规则 md 原文")
	}
	// 快照已落盘
	if cur, _ := st.UserRules.Load(); cur == nil || cur.Structured.POVPerson != "third" {
		t.Fatalf("快照应落盘且含 pov_person，got %+v", cur)
	}

	// 3. 起书并写入一章违反第三人称约束的正文（叙述段 4 处第一人称）
	if err := st.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	const chapterText = "他走进客栈。我想起了一件事。他坐下。我问自己。他喝酒。我觉得不对劲。我决定了。"
	if err := st.Drafts.SaveDraft(1, chapterText); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	// 4. commit → rule_violations 透出（返回值 + 落盘）
	tool := NewCommitChapterTool(st)
	args, err := json.Marshal(map[string]any{
		"chapter":    1,
		"summary":    "客栈夜谈",
		"characters": []string{"林砚"},
		"key_events": []string{"入住客栈"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("commit 不应被机械规则阻断: %v", err)
	}
	var out struct {
		RuleViolations []rules.Violation `json:"rule_violations"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !hasPOVViolation(out.RuleViolations) {
		t.Fatalf("commit 返回值应透出 pov_person 违规，got %+v", out.RuleViolations)
	}
	persisted := st.World.LoadRuleViolations(1)
	if !hasPOVViolation(persisted) {
		t.Fatalf("落盘的 rule_violations 应含 pov_person（供 editor 消费），got %+v", persisted)
	}
}

func hasPOVViolation(vs []rules.Violation) bool {
	for _, v := range vs {
		if v.Rule == "pov_person" && v.Severity == rules.SeverityWarning {
			return true
		}
	}
	return false
}
