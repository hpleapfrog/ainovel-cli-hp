package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/host"
)

// newModelPanelTestHost 构造真实 Host 供 /model 面板按键测试。
// 输出目录与全局配置都落在临时目录，不碰真实 ~/.ainovel。
func newModelPanelTestHost(t *testing.T) *host.Host {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := bootstrap.Config{
		Provider:  "openrouter",
		ModelName: "m1",
		Providers: map[string]bootstrap.ProviderConfig{
			"openrouter": {Type: "openai", APIKey: "sk-x", BaseURL: "http://localhost/v1", Models: []string{"m1"}},
		},
		Style: "default",
	}
	cfg.OutputDir = t.TempDir()
	bundle := assets.Load("default", assets.DefaultLoadOptions(cfg.OutputDir))
	rt, err := host.New(cfg, bundle)
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	t.Cleanup(func() { rt.Close() })
	return rt
}

// 重名在名称步即拦截：旧行为走到最后一步才报错并关闭向导，四步输入全丢。
func TestProviderAddDuplicateNameBlockedAtNameStep(t *testing.T) {
	rt := newModelPanelTestHost(t)
	m := Model{runtime: rt, textarea: textarea.New()}
	state := newModelSwitchState(rt, "")
	m.modelSwitch = state
	state.provAct = provActionAdd
	state.provStep = provStepName
	state.provInput = "openrouter"

	next, _ := m.handleProviderInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(Model)
	if mm.modelSwitch.provStep != provStepName {
		t.Fatalf("重名应停留在名称步, got step=%v", mm.modelSwitch.provStep)
	}
	if !strings.Contains(mm.modelSwitch.message, "已存在") {
		t.Fatalf("应有重名提示, got %q", mm.modelSwitch.message)
	}
	if mm.modelSwitch.provAct == provActionNone {
		t.Fatal("向导不应被关闭（输入不应丢失）")
	}
}

// 新建 provider 保存后自动进入「添加模型」输入：旧行为面对空模型列表，
// 用户按 Enter 只会吃到"没有已配置模型"的报错。
func TestProviderAddGuidesIntoModelInput(t *testing.T) {
	rt := newModelPanelTestHost(t)
	m := Model{runtime: rt, textarea: textarea.New()}
	state := newModelSwitchState(rt, "")
	m.modelSwitch = state

	enter := func(mm Model) Model {
		next, _ := mm.handleProviderInputKey(tea.KeyMsg{Type: tea.KeyEnter})
		return next.(Model)
	}
	state.provAct = provActionAdd
	state.provStep = provStepName
	state.provInput = "newprov"
	m = enter(m) // 名称 → 协议步
	m = enter(m) // 协议留空(自动识别) → key 步
	m.modelSwitch.provInput = "sk-x"
	m = enter(m) // key → url 步
	m = enter(m) // url 留空 → 保存 → done 步
	if m.modelSwitch.provStep != provStepDone {
		t.Fatalf("保存后应到 done 步, got %v", m.modelSwitch.provStep)
	}
	m = enter(m) // done Enter → 自动进入添加模型
	if !m.modelSwitch.adding {
		t.Fatal("新建 provider 后应自动进入添加模型输入")
	}
	if m.modelSwitch.focus != modelFocusModel {
		t.Fatalf("焦点应在模型栏, got %v", m.modelSwitch.focus)
	}
	// provider 已真实落库
	if _, _, _, ok := rt.GetProviderConfig("newprov"); !ok {
		t.Fatal("newprov 应已保存")
	}
}
