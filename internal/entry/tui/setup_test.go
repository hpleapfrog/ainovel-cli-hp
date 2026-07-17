package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// /key 直入（escTargetClose）：Esc 应直接关闭面板，不再经 provider 列表/菜单绕三层。
func TestSetupAddStepEsc_DirectEntryCloses(t *testing.T) {
	m := &Model{textarea: textarea.New()}
	s := newSetupState()
	s.focus = setupFocusAddStep
	s.addStep = setupAddStepKey
	s.escTarget = escTargetClose
	m.setup = s

	got, _ := s.handleAddStepKey(tea.KeyMsg{Type: tea.KeyEsc}, m)
	gm := got.(*Model)
	if gm.setup != nil {
		t.Fatal("escTargetClose: Esc 应直接关闭设置面板")
	}
}

// 列表进入：Esc 回 provider 列表而非菜单或关闭。
func TestSetupAddStepEsc_FromListReturnsToList(t *testing.T) {
	m := &Model{textarea: textarea.New()}
	s := newSetupState()
	s.focus = setupFocusAddStep
	s.addStep = setupAddStepKey
	s.escTarget = setupFocusAddProvider
	m.setup = s

	got, _ := s.handleAddStepKey(tea.KeyMsg{Type: tea.KeyEsc}, m)
	gm := got.(*Model)
	if gm.setup == nil || gm.setup.focus != setupFocusAddProvider {
		t.Fatalf("Esc 应回 provider 列表, got %+v", gm.setup)
	}
}

// 标题只看模式：新增（含 my-provider 预填）显示"添加"，编辑显示"编辑 <name>"。
func TestRenderAddStepModal_TitleByMode(t *testing.T) {
	add := newSetupState()
	add.addName = "my-provider" // 无 provider 时的预填：历史上曾被误标为"编辑"
	add.addInput = "my-provider"
	if got := renderAddStepModal(66, add); !strings.Contains(got, "添加 Provider") || strings.Contains(got, "编辑") {
		t.Fatalf("新增预填应显示添加, got:\n%s", got)
	}

	edit := newSetupState()
	edit.editing = true
	edit.addName = "kimi"
	if got := renderAddStepModal(66, edit); !strings.Contains(got, "编辑 kimi") {
		t.Fatalf("编辑应显示名称, got:\n%s", got)
	}
}

// 协议类型步忽略自由键入（只允许 ←→ 切换）。
func TestSetupAddStepType_IgnoresRunes(t *testing.T) {
	m := &Model{}
	s := newSetupState()
	s.focus = setupFocusAddStep
	s.addStep = setupAddStepType
	s.addInput = "openai"
	m.setup = s

	got, _ := s.handleAddStepKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, m)
	if got.(*Model).setup.addInput != "openai" {
		t.Fatalf("type 步自由键入应被忽略, got %q", got.(*Model).setup.addInput)
	}
}

// 粘贴控制字符被过滤（防写进 config.json 后启动校验拒不开机）。
func TestAppendSafeInput_FiltersControlRunes(t *testing.T) {
	got := appendSafeInput("sk-", []rune("ab\n\r\tc\x00d"))
	if got != "sk-abcd" {
		t.Fatalf("控制字符应被过滤, got %q", got)
	}
}
