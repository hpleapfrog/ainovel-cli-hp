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
	setupAddStepDone
)

type setupState struct {
	focus        setupFocus
	cursor       int
	addStep      setupAddStep
	addInput     string
	addName      string
	addType      string
	addKey       string
	addURL       string
	message      string
}

func newSetupState() *setupState {
	return &setupState{cursor: -1}
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
		switch msg.Runes[0] {
		case 'a':
			names := m.runtime.ConfiguredProviders()
			if len(names) == 0 {
				n := "my-provider"
				s.startAddProvider(n)
				s.addName = n
				return m, nil
			}
			s.startAddProvider("")
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
}

func (s *setupState) handleAddStepKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		s.focus = setupFocusAddProvider
		s.cursor = 0
		s.message = ""
		return m, nil
	}

	switch s.addStep {
	case setupAddStepType:
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
	case tea.KeyRunes:
		s.addInput += string(msg.Runes)
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
		switch msg.Runes[0] {
		case 'a':
			s.startAddProvider("")
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
		lines = append(lines, lipgloss.NewStyle().Foreground(colorSuccess).Render("  "+state.message))
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
		lines = append(lines, lipgloss.NewStyle().Foreground(colorSuccess).Render("  "+state.message))
		lines = append(lines, "")
	}

	return renderModalBox(w, title, lines)
}

func renderAddStepModal(w int, state *setupState) string {
	action := "添加"
	if state.addStep != setupAddStepName || state.addInput == "" || state.addInput == state.addName {
		if state.addName != "" {
			action = "编辑 " + state.addName
		}
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
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
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

func (s *setupState) handleEditStep(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		s.focus = setupFocusAddProvider
		s.cursor = 0
		s.message = ""
		return m, nil
	}

	switch s.addStep {
	case setupAddStepType:
		if msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight {
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
		}
	}

	switch msg.Type {
	case tea.KeyEnter:
		val := strings.TrimSpace(s.addInput)
		switch s.addStep {
		case setupAddStepName:
			if val == "" {
				s.message = "名称不能为空"
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
			name := s.addName
			if err := m.runtime.UpdateProvider(name, s.addType, s.addKey, s.addURL); err != nil {
				s.message = err.Error()
				s.focus = setupFocusAddProvider
				s.cursor = 0
			} else {
				s.message = fmt.Sprintf("Provider %q 已更新", name)
				s.focus = setupFocusAddProvider
				s.cursor = 0
			}
			return m, fetchSnapshot(m.runtime)
		}
	case tea.KeyBackspace:
		if len(s.addInput) > 0 {
			r := []rune(s.addInput)
			s.addInput = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeyRunes:
		s.addInput += string(msg.Runes)
		return m, nil
	}
	return m, nil
}
