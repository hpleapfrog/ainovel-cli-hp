package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/voocel/agentcore"
)

type modelRuntime interface {
	ConfiguredProviders() []string
	ConfiguredModels(provider string) []string
	CurrentModelSelection(role string) (string, string, bool)
	AvailableThinking(role string) []agentcore.ThinkingLevel
	CurrentThinking(role string) string
	SwitchModel(role, provider, model string) error
	SetRoleThinking(role, level string) error
	AddProviderModel(provider, model string) error
	RemoveProviderModel(provider, model string) error
	RenameProviderModel(provider, oldName, newName string) error
	AddProvider(name, apiType, apiKey, baseURL string) error
	RemoveProvider(name string) error
	UpdateProvider(name, apiType, apiKey, baseURL string) error
	GetProviderConfig(name string) (apiType, apiKey, baseURL string, ok bool)
}

type modelSwitchFocus int

const (
	modelFocusRole modelSwitchFocus = iota
	modelFocusProvider
	modelFocusModel
	modelFocusThinking
)

type modelRoleOption struct {
	Key   string
	Label string
}

var modelRoleOptions = []modelRoleOption{
	{Key: "default", Label: "默认"},

	{Key: "architect", Label: "Architect"},
	{Key: "writer", Label: "Writer"},
	{Key: "editor", Label: "Editor"},
}

type thinkingOption struct{ Key, Label string }

var allThinkingOptions = []thinkingOption{
	{"", "默认(继承)"},
	{"off", "关闭"},
	{"low", "低"},
	{"medium", "中"},
	{"high", "高"},
	{"xhigh", "极高"},
	{"max", "最高"},
}

func thinkingOptionsFor(rt modelRuntime, role string) []thinkingOption {
	levels := rt.AvailableThinking(role)
	if len(levels) == 0 {
		return []thinkingOption{allThinkingOptions[0]}
	}
	out := make([]thinkingOption, 0, len(levels))
	for _, level := range levels {
		key := string(level)
		for _, option := range allThinkingOptions {
			if option.Key == key {
				out = append(out, option)
				break
			}
		}
	}
	if len(out) == 0 {
		return []thinkingOption{allThinkingOptions[0]}
	}
	return out
}

func thinkingIndexOf(options []thinkingOption, level string) int {
	level = strings.ToLower(strings.TrimSpace(level))
	for i, o := range options {
		if o.Key == level {
			return i
		}
	}
	return 0 // 未知值 → 继承
}

type modelSwitchState struct {
	focus       modelSwitchFocus
	roleIdx     int
	providerIdx int
	modelIdx    int
	thinkingIdx int
	providers   []string
	models      []string
	thinking    []thinkingOption
	message     string
	adding      bool
	addInput    string
	editing     bool
	editingOld  string

	provAct   providerAction
	provName  string
	provInput string
	provStep  providerInputStep
	provType  string
	provKey   string
	provURL   string
}

type providerAction int

const (
	provActionNone providerAction = iota
	provActionAdd
	provActionEdit
)

type providerInputStep int

const (
	provStepName providerInputStep = iota
	provStepType
	provStepAPIKey
	provStepBaseURL
	provStepDone
)

var providerTypeOptions = []string{"", "openai", "anthropic", "gemini"}

func newModelSwitchState(rt modelRuntime, roleHint string) *modelSwitchState {
	state := &modelSwitchState{
		providers: rt.ConfiguredProviders(),
	}
	if len(state.providers) == 0 {
		state.message = "当前没有可用 provider"
	}

	roleHint = normalizeRoleKey(roleHint)
	for i, opt := range modelRoleOptions {
		if opt.Key == roleHint {
			state.roleIdx = i
			break
		}
	}
	state.syncSelection(rt)
	return state
}

func normalizeRoleKey(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "default":
		return "default"
	case "architect", "writer", "editor":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return ""
	}
}

func (s *modelSwitchState) role() string {
	return modelRoleOptions[s.roleIdx].Key
}

func (s *modelSwitchState) roleLabel() string {
	return modelRoleOptions[s.roleIdx].Label
}

func (s *modelSwitchState) provider() string {
	if len(s.providers) == 0 || s.providerIdx < 0 || s.providerIdx >= len(s.providers) {
		return ""
	}
	return s.providers[s.providerIdx]
}

func (s *modelSwitchState) model() string {
	if len(s.models) == 0 || s.modelIdx < 0 || s.modelIdx >= len(s.models) {
		return ""
	}
	return s.models[s.modelIdx]
}

func (s *modelSwitchState) thinkingKey() string {
	if s.thinkingIdx < 0 || s.thinkingIdx >= len(s.thinking) {
		return ""
	}
	return s.thinking[s.thinkingIdx].Key
}

func (s *modelSwitchState) thinkingLabel() string {
	if s.thinkingIdx < 0 || s.thinkingIdx >= len(s.thinking) {
		return allThinkingOptions[0].Label
	}
	return s.thinking[s.thinkingIdx].Label
}

func (s *modelSwitchState) moveFocus(delta int) {
	total := 4
	s.focus = modelSwitchFocus((int(s.focus) + delta + total) % total)
}

func (s *modelSwitchState) cycle(delta int, rt modelRuntime) {
	switch s.focus {
	case modelFocusRole:
		total := len(modelRoleOptions)
		s.roleIdx = (s.roleIdx + delta + total) % total
		s.syncSelection(rt)
	case modelFocusProvider:
		if len(s.providers) == 0 {
			return
		}
		total := len(s.providers)
		s.providerIdx = (s.providerIdx + delta + total) % total
		s.syncModels(rt, "")
	case modelFocusModel:
		if len(s.models) == 0 {
			return
		}
		total := len(s.models)
		s.modelIdx = (s.modelIdx + delta + total) % total
	case modelFocusThinking:
		total := len(s.thinking)
		if total == 0 {
			return
		}
		s.thinkingIdx = (s.thinkingIdx + delta + total) % total
	}
}

func (s *modelSwitchState) syncSelection(rt modelRuntime) {
	provider, model, _ := rt.CurrentModelSelection(s.role())
	if len(s.providers) > 0 {
		s.providerIdx = 0
		for i, candidate := range s.providers {
			if candidate == provider {
				s.providerIdx = i
				break
			}
		}
	}
	s.syncModels(rt, model)
	s.syncThinking(rt)
	s.message = ""
}

func (s *modelSwitchState) syncModels(rt modelRuntime, preferred string) {
	s.models = rt.ConfiguredModels(s.provider())
	s.modelIdx = 0
	if len(s.models) == 0 {
		return
	}
	preferred = strings.TrimSpace(preferred)
	for i, model := range s.models {
		if model == preferred {
			s.modelIdx = i
			return
		}
	}
}

func (s *modelSwitchState) syncThinking(rt modelRuntime) {
	s.thinking = thinkingOptionsFor(rt, s.role())
	s.thinkingIdx = thinkingIndexOf(s.thinking, rt.CurrentThinking(s.role()))
}

func (s *modelSwitchState) apply(rt modelRuntime) error {
	if len(s.providers) == 0 {
		return fmt.Errorf("当前没有可用 provider")
	}
	if len(s.models) == 0 {
		return fmt.Errorf("provider %q 没有已配置模型", s.provider())
	}
	wantThinking := s.thinkingKey()
	if err := rt.SwitchModel(s.role(), s.provider(), s.model()); err != nil {
		return err
	}
	s.syncThinking(rt)
	// 推理强度与模型正交：仅当较当前值有变化时应用，避免冗余持久化/事件。
	if wantThinking != strings.ToLower(strings.TrimSpace(rt.CurrentThinking(s.role()))) {
		if err := rt.SetRoleThinking(s.role(), wantThinking); err != nil {
			return err
		}
		s.syncThinking(rt)
	}
	return nil
}

func (m Model) handleModelSwitchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modelSwitch == nil {
		return m, nil
	}
	state := m.modelSwitch

	if state.provAct != provActionNone {
		return m.handleProviderInputKey(msg)
	}

	if state.editing {
		return m.handleModelEditInput(msg)
	}

	if state.adding {
		return m.handleModelAddInput(msg)
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.modelSwitch = nil
		return m, m.textarea.Focus()
	case tea.KeyTab, tea.KeyDown:
		state.moveFocus(1)
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		state.moveFocus(-1)
		return m, nil
	case tea.KeyLeft:
		state.cycle(-1, m.runtime)
		return m, nil
	case tea.KeyRight:
		state.cycle(1, m.runtime)
		return m, nil
	case tea.KeyEnter:
		if err := state.apply(m.runtime); err != nil {
			state.message = err.Error()
			return m, nil
		}
		m.modelSwitch = nil
		return m, tea.Batch(m.textarea.Focus(), fetchSnapshot(m.runtime))
	case tea.KeyRunes:
		return m.handleModelSwitchRune(msg)
	default:
		return m, nil
	}
}

func (m Model) handleModelEditInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.modelSwitch
	switch msg.Type {
	case tea.KeyEnter:
		newName := strings.TrimSpace(state.addInput)
		if newName == "" {
			state.message = "模型名不能为空"
			return m, nil
		}
		if newName == state.editingOld {
			state.editing = false
			state.addInput = ""
			state.message = ""
			return m, nil
		}
		// 重命名同步所有引用点（default/角色/fallback），否则旧名会被
		// CandidateModels 从引用复活、角色仍指旧模型
		if err := m.runtime.RenameProviderModel(state.provider(), state.editingOld, newName); err != nil {
			state.message = err.Error()
			return m, nil
		}
		state.editing = false
		state.addInput = ""
		state.syncModels(m.runtime, newName)
		state.message = ""
		return m, nil
	case tea.KeyEsc:
		state.editing = false
		state.addInput = ""
		state.message = ""
		return m, nil
	case tea.KeyBackspace:
		if len(state.addInput) > 0 {
			r := []rune(state.addInput)
			state.addInput = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeySpace:
		state.addInput += " "
		return m, nil
	case tea.KeyRunes:
		state.addInput = appendSafeInput(state.addInput, msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m Model) handleModelAddInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.modelSwitch
	switch msg.Type {
	case tea.KeyEnter:
		modelName := strings.TrimSpace(state.addInput)
		if modelName == "" {
			state.message = "模型名不能为空"
			return m, nil
		}
		if err := m.runtime.AddProviderModel(state.provider(), modelName); err != nil {
			state.message = err.Error()
			return m, nil
		}
		state.adding = false
		state.addInput = ""
		state.syncModels(m.runtime, modelName)
		state.message = ""
		return m, nil
	case tea.KeyEsc:
		state.adding = false
		state.addInput = ""
		state.message = ""
		return m, nil
	case tea.KeyBackspace:
		if len(state.addInput) > 0 {
			r := []rune(state.addInput)
			state.addInput = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeySpace:
		state.addInput += " "
		return m, nil
	case tea.KeyRunes:
		state.addInput = appendSafeInput(state.addInput, msg.Runes)
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) handleModelSwitchRune(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.modelSwitch
	if len(msg.Runes) == 0 || msg.Paste {
		return m, nil
	}
	r := msg.Runes[0]
	if state.focus == modelFocusProvider {
		switch r {
		case 'a':
			state.provAct = provActionAdd
			state.provStep = provStepName
			state.provInput = ""
			state.provType = ""
			state.provKey = ""
			state.provURL = ""
			state.provName = ""
			state.message = ""
			return m, nil
		case 'd':
			name := state.provider()
			if name == "" {
				state.message = "没有可删除的 Provider"
				return m, nil
			}
			if err := m.runtime.RemoveProvider(name); err != nil {
				state.message = err.Error()
				return m, nil
			}
			state.providers = m.runtime.ConfiguredProviders()
			if state.providerIdx >= len(state.providers) {
				state.providerIdx = len(state.providers) - 1
			}
			if state.providerIdx < 0 {
				state.providerIdx = 0
			}
			state.syncModels(m.runtime, "")
			state.message = ""
			return m, nil
		case 'e':
			name := state.provider()
			if name == "" {
				state.message = "没有可编辑的 Provider"
				return m, nil
			}
			if t, k, u, ok := m.runtime.GetProviderConfig(name); ok {
				state.provAct = provActionEdit
				state.provStep = provStepName
				state.provInput = name
				state.provName = name
				state.provType = t
				state.provKey = k
				state.provURL = u
			} else {
				state.message = "无法读取 Provider 配置"
			}
			return m, nil
		}
	}
	if state.focus == modelFocusModel {
		switch r {
		case 'a':
			state.adding = true
			state.addInput = ""
			state.message = ""
			return m, nil
		case 'e':
			oldName := state.model()
			if oldName == "" {
				state.message = "没有可编辑的模型"
				return m, nil
			}
			state.editing = true
			state.editingOld = oldName
			state.addInput = oldName
			state.message = ""
			return m, nil
		case 'd':
			modelName := state.model()
			if modelName == "" {
				state.message = "没有可删除的模型"
				return m, nil
			}
			if err := m.runtime.RemoveProviderModel(state.provider(), modelName); err != nil {
				state.message = err.Error()
				return m, nil
			}
			state.syncModels(m.runtime, "")
			state.message = ""
			return m, nil
		}
	}
	return m, nil
}

func (m Model) handleProviderInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.modelSwitch

	if msg.Type == tea.KeyEsc {
		state.provAct = provActionNone
		state.provInput = ""
		state.message = ""
		return m, nil
	}

	if state.provStep == provStepDone {
		if msg.Type == tea.KeyEnter {
			savedName := state.provInput
			state.provAct = provActionNone
			state.provInput = ""
			state.providers = m.runtime.ConfiguredProviders()
			for i, p := range state.providers {
				if p == savedName {
					state.providerIdx = i
					break
				}
			}
			state.syncModels(m.runtime, "")
			state.message = ""
			return m, tea.Batch(m.textarea.Focus(), fetchSnapshot(m.runtime))
		}
		return m, nil
	}

	// 编辑模式下名称锁定：改名=新建 provider，不支持 rename（与 setup 向导一致）
	if state.provAct == provActionEdit && state.provStep == provStepName && msg.Type != tea.KeyEnter {
		return m, nil
	}

	// 协议类型只允许 ←→ 循环切换；其余键（含自由键入）一律忽略，防止 "openaix" 落盘
	if state.provStep == provStepType && msg.Type != tea.KeyEnter {
		if msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight {
			types := providerTypeOptions
			cur := state.provInput
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
			state.provInput = types[idx]
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter:
		val := strings.TrimSpace(state.provInput)
		switch state.provStep {
		case provStepName:
			if val == "" {
				state.message = "Provider 名称不能为空"
				return m, nil
			}
			state.provName = val
			if state.provAct == provActionAdd {
				state.provType = ""
				state.provKey = ""
				state.provURL = ""
			}
			state.provStep = provStepType
			state.provInput = state.provType
			state.message = ""
			return m, nil
		case provStepType:
			state.provType = val
			state.provStep = provStepAPIKey
			state.provInput = state.provKey
			state.message = ""
			return m, nil
		case provStepAPIKey:
			state.provKey = val
			state.provStep = provStepBaseURL
			state.provInput = state.provURL
			state.message = ""
			return m, nil
		case provStepBaseURL:
			state.provURL = val
			name := provCfgName(state)
			if state.provAct == provActionAdd {
				if err := m.runtime.AddProvider(name, state.provType, state.provKey, state.provURL); err != nil {
					state.message = err.Error()
					state.provAct = provActionNone
					return m, nil
				}
			} else {
				if err := m.runtime.UpdateProvider(name, state.provType, state.provKey, state.provURL); err != nil {
					state.message = err.Error()
					state.provAct = provActionNone
					return m, nil
				}
			}
			state.provStep = provStepDone
			state.provInput = name
			state.message = fmt.Sprintf("Provider %q 已保存，按 Enter 继续", name)
			return m, nil
		}
		return m, nil
	case tea.KeyBackspace:
		if len(state.provInput) > 0 {
			r := []rune(state.provInput)
			state.provInput = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeySpace:
		state.provInput += " "
		return m, nil
	case tea.KeyRunes:
		state.provInput = appendSafeInput(state.provInput, msg.Runes)
		return m, nil
	default:
		return m, nil
	}
}

func provCfgName(s *modelSwitchState) string {
	if s.provAct == provActionEdit {
		return s.provider()
	}
	return strings.TrimSpace(s.provName)
}

var providerStepLabels = map[providerInputStep]string{
	provStepName:    "Provider 名称",
	provStepType:    "API 协议类型（←→ 切换，空=自动识别）",
	provStepAPIKey:  "API Key",
	provStepBaseURL: "Base URL",
}

func renderProviderInput(state *modelSwitchState) []string {
	var lines []string
	action := "添加"
	if state.provAct == provActionEdit {
		action = "编辑"
	}
	title := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).
		Render(fmt.Sprintf("  %s Provider", action))
	lines = append(lines, title)

	steps := []providerInputStep{provStepName, provStepType, provStepAPIKey, provStepBaseURL}
	for _, s := range steps {
		label := providerStepLabels[s]
		val := ""
		if s == state.provStep {
			display := state.provInput
			if s == provStepAPIKey && display != "" {
				display = maskAPIKey(display)
			}
			if display == "" {
				display = "▌"
			} else {
				display += "▌"
			}
			if s == provStepType {
				display = state.provInput
				if display == "" {
					display = "自动识别"
				}
				display = "[" + display + "] ▌"
			}
			val = lipgloss.NewStyle().Foreground(colorAccent).Render(display)
		} else if s < state.provStep {
			v := provInputForStep(state, s)
			if s == provStepAPIKey && v != "" {
				v = "已设置"
			}
			if s == provStepType {
				if v == "" {
					v = "自动识别"
				}
			}
			val = lipgloss.NewStyle().Foreground(colorDim).Render(v)
		} else {
			val = lipgloss.NewStyle().Foreground(colorDim).Render("…")
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", label, val))
	}
	return lines
}

func provInputForStep(s *modelSwitchState, step providerInputStep) string {
	switch step {
	case provStepName:
		return provCfgName(s)
	case provStepType:
		return s.provType
	case provStepAPIKey:
		return s.provKey
	case provStepBaseURL:
		return s.provURL
	}
	return ""
}

func renderModelSwitchBar(width int, state *modelSwitchState) string {
	if state == nil || width <= 0 {
		return ""
	}

	title := lipgloss.NewStyle().
		Foreground(colorMuted).
		Bold(true).
		Render("/model 切换模型")

	row1 := renderModelField("角色", state.roleLabel(), state.focus == modelFocusRole)
	row2 := renderModelField("Provider", state.provider(), state.focus == modelFocusProvider)
	row3 := renderModelField("模型", state.model(), state.focus == modelFocusModel)
	row4 := renderModelField("推理强度", state.thinkingLabel(), state.focus == modelFocusThinking)
	hint := lipgloss.NewStyle().
		Foreground(colorDim).
		Italic(true)
	lines := []string{
		row1,
		row2,
		row3,
		row4,
	}
	if state.provAct != provActionNone {
		lines = append(lines, renderProviderInput(state)...)
		lines = append(lines, hint.Render("Enter 下一步  Esc 取消"))
	} else if state.editing {
		prompt := fmt.Sprintf("编辑模型名: %s▏", state.addInput)
		lines = append(lines, lipgloss.NewStyle().Foreground(colorAccent).Render(prompt))
		lines = append(lines, hint.Render("Enter 确认  Esc 取消"))
	} else if state.adding {
		prompt := fmt.Sprintf("输入模型名: %s▏", state.addInput)
		lines = append(lines, lipgloss.NewStyle().Foreground(colorAccent).Render(prompt))
		lines = append(lines, hint.Render("Enter 确认  Esc 取消"))
	} else {
		hintText := "Tab 切字段   ←→ 切选项   Enter 应用   Esc 取消"
		if state.focus == modelFocusProvider {
			hintText = "Tab 切字段   ←→ 切选项   a 添加   d 删除   e 编辑   Enter 应用   Esc 取消"
		} else if state.focus == modelFocusModel {
			hintText = "Tab 切字段   ←→ 切选项   a 添加   e 编辑   d 删除   Enter 应用   Esc 取消"
		}
		lines = append(lines, hint.Render(hintText))
	}
	if state.message != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorError).Italic(true).Render(truncate(state.message, width-8)))
	}

	content := strings.Join(lines, "\n")
	boxW := lipgloss.Width(content) + 8
	maxW := width - 2
	if maxW > 68 {
		maxW = 68
	}
	if boxW > maxW {
		boxW = maxW
	}
	if boxW < 56 {
		boxW = 56
	}

	innerW := boxW - 2
	if innerW < 16 {
		innerW = 16
	}
	sepW := innerW - lipgloss.Width(title) - 3
	if sepW < 0 {
		sepW = 0
	}
	lineStyle := lipgloss.NewStyle().Foreground(colorDim)
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

func renderModelField(label, value string, focused bool) string {
	if strings.TrimSpace(value) == "" {
		value = "未设置"
	}
	labelText := lipgloss.NewStyle().
		Foreground(colorMuted).
		Width(12).
		Render(label + ":")
	style := lipgloss.NewStyle().Padding(0, 1).Foreground(bodyTextColor)
	if focused {
		style = style.Foreground(colorAccent).Bold(true).Underline(true)
	}
	return labelText + style.Render("["+value+"]")
}
