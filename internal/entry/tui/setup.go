package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type setupFocus int

const (
	setupFocusMenu setupFocus = iota
	setupFocusAddProvider
	setupFocusAddStep
)

type setupAddStep int

const (
	setupAddStepName setupAddStep = iota
	setupAddStepType
	setupAddStepKey
	setupAddStepURL
)

// escTargetClose 是 escTarget 的特殊值：addStep 流程 Esc 直接关闭设置面板。
const escTargetClose setupFocus = -1

type setupState struct {
	focus     setupFocus
	cursor    int
	addStep   setupAddStep
	addInput  string
	addName   string
	addType   string
	addKey    string
	addURL    string
	editing   bool       // true=编辑已有 provider（走 UpdateProvider）；false=新增（走 AddProvider）
	escTarget setupFocus // addStep 流程 Esc 的落点：menu / provider 列表 / escTargetClose
	message   string
}

func newSetupState() *setupState {
	return &setupState{cursor: -1, escTarget: setupFocusAddProvider}
}

var setupMenuItems = []string{
	"角色模型分配",
	"管理 Provider",
}

func (s *setupState) handleKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	switch s.focus {
	case setupFocusAddProvider:
		return s.handleProviderKey(msg, m)
	case setupFocusAddStep:
		return s.handleAddStepKey(msg, m)
	default:
		return s.handleMenuKey(msg, m)
	}
}

func (s *setupState) handleMenuKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.setup = nil
		return m, m.textarea.Focus()
	case tea.KeyUp:
		if s.cursor > 0 {
			s.cursor--
		}
		return m, nil
	case tea.KeyDown:
		if s.cursor < len(setupMenuItems)-1 {
			s.cursor++
		}
		return m, nil
	case tea.KeyEnter:
		switch s.cursor {
		case 0: // 角色模型分配
			m.modelSwitch = newModelSwitchState(m.runtime, "")
			m.setup = nil
			return m, nil
		case 1: // 管理 Provider
			s.focus = setupFocusAddProvider
			s.cursor = 0
			return m, nil
		}
	case tea.KeyRunes:
		if len(msg.Runes) == 0 || msg.Paste {
			return m, nil
		}
		switch msg.Runes[0] {
		case 'a':
			names := m.runtime.ConfiguredProviders()
			if len(names) == 0 {
				n := "my-provider"
				s.startAddProvider(n)
				s.addName = n
			} else {
				s.startAddProvider("")
			}
			s.escTarget = setupFocusMenu
			return m, nil
		}
	}
	return m, nil
}

func (s *setupState) startAddProvider(name string) {
	s.focus = setupFocusAddStep
	s.addStep = setupAddStepName
	s.addInput = name
	s.addType = ""
	s.addKey = ""
	s.addURL = ""
	s.editing = false
}

func (s *setupState) handleAddStepKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		// Esc 落点随入口走：/key 直入的直接关闭，菜单来的回菜单，列表来的回列表
		if s.escTarget == escTargetClose {
			m.setup = nil
			return m, m.textarea.Focus()
		}
		s.focus = s.escTarget
		s.cursor = 0
		s.message = ""
		return m, nil
	}

	switch s.addStep {
	case setupAddStepType:
		// 协议类型只允许 ←→ 循环切换，自由键入一律忽略（防止 "openaix" 落盘）
		switch msg.Type {
		case tea.KeyLeft, tea.KeyRight:
			types := []string{"", "openai", "anthropic", "gemini"}
			cur := s.addInput
			idx := -1
			for i, t := range types {
				if t == cur {
					idx = i
					break
				}
			}
			delta := 1
			if msg.Type == tea.KeyLeft {
				delta = -1
			}
			idx = (idx + delta + len(types)) % len(types)
			s.addInput = types[idx]
			return m, nil
		case tea.KeyEnter:
			return s.handleAddStepEnter(m)
		default:
			return m, nil
		}
	case setupAddStepName:
		// 编辑模式下名称锁定（改名=新建，见 handleAddStepEnter 的说明）
		if s.editing {
			switch msg.Type {
			case tea.KeyEnter:
				return s.handleAddStepEnter(m)
			default:
				return m, nil
			}
		}
	}

	switch msg.Type {
	case tea.KeyEnter:
		return s.handleAddStepEnter(m)
	case tea.KeyBackspace:
		if len(s.addInput) > 0 {
			r := []rune(s.addInput)
			s.addInput = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeySpace:
		s.addInput += " "
		return m, nil
	case tea.KeyRunes:
		// 逐 rune 过滤控制字符：bracketed paste 会把 \n\r\t 一并投递，
		// 写进 config.json 后下次启动 ValidateBase 直接拒绝开机
		s.addInput = appendSafeInput(s.addInput, msg.Runes)
		return m, nil
	}
	return m, nil
}

func (s *setupState) handleAddStepEnter(m *Model) (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(s.addInput)
	switch s.addStep {
	case setupAddStepName:
		if val == "" {
			s.message = "Provider 名称不能为空"
			return m, nil
		}
		s.addName = val
		s.addStep = setupAddStepType
		s.addInput = s.addType
		return m, nil
	case setupAddStepType:
		s.addType = val
		s.addStep = setupAddStepKey
		s.addInput = s.addKey
		return m, nil
	case setupAddStepKey:
		s.addKey = val
		s.addStep = setupAddStepURL
		s.addInput = s.addURL
		return m, nil
	case setupAddStepURL:
		s.addURL = val
		// 编辑走 UpdateProvider（空字段保留原值），新增走 AddProvider。
		// 名称在编辑流中锁定：改名=新建 provider，不支持 rename。
		if s.editing {
			if err := m.runtime.UpdateProvider(s.addName, s.addType, s.addKey, s.addURL); err != nil {
				s.message = err.Error()
				s.focus = setupFocusAddProvider
				s.cursor = 0
				return m, nil
			}
			s.message = fmt.Sprintf("Provider %q 已更新", s.addName)
			if s.addKey == "" || s.addURL == "" {
				s.message += "（留空字段保留原值）"
			}
			s.focus = setupFocusAddProvider
			s.cursor = 0
			return m, fetchSnapshot(m.runtime)
		}
		if err := m.runtime.AddProvider(s.addName, s.addType, s.addKey, s.addURL); err != nil {
			s.message = err.Error()
			s.focus = setupFocusAddProvider
			s.cursor = 0
			return m, nil
		}
		s.message = fmt.Sprintf("Provider %q 已添加", s.addName)
		s.focus = setupFocusAddProvider
		s.cursor = 0
		return m, fetchSnapshot(m.runtime)
	}
	return m, nil
}

func (s *setupState) handleProviderKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	providers := m.runtime.ConfiguredProviders()
	hasCursor := s.cursor >= 0 && s.cursor < len(providers)

	switch msg.Type {
	case tea.KeyEsc:
		s.focus = setupFocusMenu
		s.cursor = 0
		return m, nil
	case tea.KeyUp:
		if len(providers) > 0 && s.cursor > 0 {
			s.cursor--
		}
		return m, nil
	case tea.KeyDown:
		if s.cursor < len(providers)-1 {
			s.cursor++
		}
		return m, nil
	case tea.KeyRunes:
		if len(msg.Runes) == 0 || msg.Paste {
			return m, nil
		}
		switch msg.Runes[0] {
		case 'a':
			s.startAddProvider("")
			s.escTarget = setupFocusAddProvider
			return m, nil
		case 'd':
			if !hasCursor {
				s.message = "没有可删除的 Provider"
				return m, nil
			}
			name := providers[s.cursor]
			if err := m.runtime.RemoveProvider(name); err != nil {
				s.message = err.Error()
			} else {
				s.message = fmt.Sprintf("Provider %q 已删除", name)
				if s.cursor >= len(m.runtime.ConfiguredProviders()) {
					s.cursor = len(m.runtime.ConfiguredProviders()) - 1
				}
			}
			return m, fetchSnapshot(m.runtime)
		case 'e':
			if !hasCursor {
				s.message = "没有可编辑的 Provider"
				return m, nil
			}
			name := providers[s.cursor]
			t, k, u, ok := m.runtime.GetProviderConfig(name)
			if !ok {
				s.message = "无法读取 Provider 配置"
				return m, nil
			}
			s.addName = name
			s.addType = t
			s.addKey = k
			s.addURL = u
			s.addStep = setupAddStepName
			s.addInput = name
			s.editing = true
			s.escTarget = setupFocusAddProvider
			s.focus = setupFocusAddStep
			return m, nil
		case 'k':
			if !hasCursor {
				return m, nil
			}
			name := providers[s.cursor]
			t, _, u, ok := m.runtime.GetProviderConfig(name)
			if !ok {
				return m, nil
			}
			s.addName = name
			s.addType = t
			s.addURL = u
			s.addStep = setupAddStepKey
			s.addInput = ""
			s.addKey = ""
			s.editing = true
			s.escTarget = setupFocusAddProvider
			s.focus = setupFocusAddStep
			return m, nil
		case 'u':
			if !hasCursor {
				return m, nil
			}
			name := providers[s.cursor]
			t, k, _, ok := m.runtime.GetProviderConfig(name)
			if !ok {
				return m, nil
			}
			s.addName = name
			s.addType = t
			s.addKey = k
			s.addStep = setupAddStepURL
			s.addInput = ""
			s.addURL = ""
			s.editing = true
			s.escTarget = setupFocusAddProvider
			s.focus = setupFocusAddStep
			return m, nil
		}
	}
	return m, nil
}

func renderSetup(width, height int, state *setupState, rt modelRuntime) string {
	if state == nil || width <= 0 {
		return ""
	}
	boxW := min(width-4, 66)
	_ = height

	switch state.focus {
	case setupFocusAddStep:
		return renderAddStepModal(boxW, state)
	case setupFocusAddProvider:
		return renderProviderListModal(boxW, state, rt)
	default:
		return renderSetupMenu(boxW, state, rt)
	}
}

func renderSetupMenu(w int, state *setupState, rt modelRuntime) string {
	title := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(" 设置 ")
	dim := lipgloss.NewStyle().Foreground(colorDim)

	var lines []string
	lines = append(lines, "")
	for i, item := range setupMenuItems {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(bodyTextColor)
		if i == state.cursor {
			cursor = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			style = style.Foreground(colorAccent).Bold(true)
		}
		lines = append(lines, cursor+style.Render(item))
	}
	lines = append(lines, "")

	lines = append(lines, dim.Render("  ↑↓ 选择  Enter 进入  a 添加 Provider  Esc 退出"))
	lines = append(lines, "")

	quickInfo := renderQuickRoleInfo(rt)
	if quickInfo != "" {
		lines = append(lines, dim.Render("  ── 当前分配 ──"))
		for _, line := range strings.Split(quickInfo, "\n") {
			lines = append(lines, dim.Render("  "+line))
		}
		lines = append(lines, "")
	}

	if state.message != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(bodyTextColor).Render("  "+state.message))
		lines = append(lines, "")
	}

	return renderModalBox(w, title, lines)
}

func renderQuickRoleInfo(rt modelRuntime) string {
	var lines []string
	for _, role := range []struct{ key, label string }{
		{"default", "默认"},
		{"architect", "Architect"},
		{"writer", "Writer"},
		{"editor", "Editor"},
	} {
		provider, model, _ := rt.CurrentModelSelection(role.key)
		if provider == "" {
			lines = append(lines, fmt.Sprintf("%s: 未设置", role.label))
		} else {
			lines = append(lines, fmt.Sprintf("%s: %s / %s", role.label, provider, model))
		}
	}
	return strings.Join(lines, "\n")
}

func renderProviderListModal(w int, state *setupState, rt modelRuntime) string {
	title := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(" Provider 管理 ")
	dim := lipgloss.NewStyle().Foreground(colorDim)

	var lines []string
	lines = append(lines, "")

	providers := rt.ConfiguredProviders()
	if len(providers) == 0 {
		lines = append(lines, dim.Render("  暂无 Provider，按 a 添加"))
	} else {
		for i, name := range providers {
			t, k, u, _ := rt.GetProviderConfig(name)
			cursor := "  "
			style := lipgloss.NewStyle().Foreground(bodyTextColor)
			if i == state.cursor {
				cursor = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
				style = style.Foreground(colorAccent).Bold(true)
			}
			keyStatus := "已设置"
			if k == "" {
				keyStatus = dim.Render("未设置")
			}
			typeStr := t
			if typeStr == "" {
				typeStr = "auto"
			}
			urlStr := u
			if urlStr == "" {
				urlStr = "默认"
			}
			lines = append(lines, cursor+style.Render(name))
			lines = append(lines, dim.Render(fmt.Sprintf("     type=%s  key=%s  url=%s", typeStr, keyStatus, urlStr)))
		}
	}
	lines = append(lines, "")

	lines = append(lines, dim.Render("  a 添加  d 删除  e 编辑  k 改Key  u 改URL"))
	lines = append(lines, dim.Render("  Esc 返回"))
	lines = append(lines, "")

	if state.message != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(bodyTextColor).Render("  "+state.message))
		lines = append(lines, "")
	}

	return renderModalBox(w, title, lines)
}

func renderAddStepModal(w int, state *setupState) string {
	// 标题只看模式标志：编辑（e/k/u//key 进入）显示名称，新增统一"添加"——
	// 不再用输入值猜（无 provider 时 my-provider 预填曾被误标为"编辑"）
	action := "添加"
	if state.editing && state.addName != "" {
		action = "编辑 " + state.addName
	}
	title := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(" " + action + " Provider ")

	steps := []struct {
		step  setupAddStep
		label string
	}{
		{setupAddStepName, "名称"},
		{setupAddStepType, "协议类型"},
		{setupAddStepKey, "API Key"},
		{setupAddStepURL, "Base URL"},
	}

	var lines []string
	lines = append(lines, "")

	for _, st := range steps {
		label := st.label
		val := ""
		if st.step == state.addStep {
			display := state.addInput
			if st.step == setupAddStepKey && display != "" {
				display = maskAPIKey(display)
			}
			if display == "" {
				display = "▌"
			} else {
				display += "▌"
			}
			if st.step == setupAddStepType {
				display = state.addInput
				if display == "" {
					display = "自动识别"
				}
				display = "[" + display + "] ▌"
			}
			val = lipgloss.NewStyle().Foreground(colorAccent).Render(display)
		} else if st.step < state.addStep {
			v := valueForStep(state, st.step)
			if st.step == setupAddStepKey && v != "" {
				v = "已设置"
			}
			if st.step == setupAddStepType && v == "" {
				v = "自动识别"
			}
			val = lipgloss.NewStyle().Foreground(colorDim).Render(v)
		} else {
			val = lipgloss.NewStyle().Foreground(colorDim).Render("…")
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", label, val))
	}

	lines = append(lines, "")
	if state.addStep == setupAddStepType {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("  ←→ 切换协议类型  Enter 确认  Esc 取消"))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("  Enter 确认  Esc 取消"))
	}
	lines = append(lines, "")

	if state.message != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorError).Render("  "+state.message))
		lines = append(lines, "")
	}

	return renderModalBox(w, title, lines)
}

func valueForStep(s *setupState, step setupAddStep) string {
	switch step {
	case setupAddStepName:
		return s.addName
	case setupAddStepType:
		return s.addType
	case setupAddStepKey:
		return s.addKey
	case setupAddStepURL:
		return s.addURL
	}
	return ""
}

func maskAPIKey(key string) string {
	r := []rune(key)
	if len(r) <= 8 {
		return "****"
	}
	return string(r[:4]) + "****" + string(r[len(r)-4:])
}

// appendSafeInput 只追加可打印 rune（>=0x20 且非 DEL）。
// bracketed paste 会把 \n\r\t 等控制字符一并投递，原样写进 config.json
// 会让下次启动的文本校验直接拒绝开机（host.New → ValidateBase）。
func appendSafeInput(s string, runes []rune) string {
	for _, r := range runes {
		if r >= 0x20 && r != 0x7f {
			s += string(r)
		}
	}
	return s
}

func renderModalBox(w int, title string, lines []string) string {
	lineStyle := lipgloss.NewStyle().Foreground(colorDim)
	innerW := w - 2
	if innerW < 20 {
		innerW = 20
	}

	sepW := innerW - lipgloss.Width(title) - 3
	if sepW < 0 {
		sepW = 0
	}
	topBorder := lineStyle.Render("┌─ ") + title + lineStyle.Render(" "+strings.Repeat("─", sepW)+"┐")
	bottomBorder := lineStyle.Render("└" + strings.Repeat("─", innerW) + "┘")

	body := make([]string, 0, len(lines))
	for _, line := range lines {
		padding := innerW - lipgloss.Width(line)
		if padding < 0 {
			padding = 0
		}
		body = append(body, lineStyle.Render("│")+line+strings.Repeat(" ", padding)+lineStyle.Render("│"))
	}

	return strings.Join(append(append([]string{topBorder}, body...), bottomBorder), "\n")
}
