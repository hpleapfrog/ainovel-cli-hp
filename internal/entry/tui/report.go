package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/voocel/ainovel-cli/internal/diag"
)

type reportState struct {
	reqID      int
	report     *diag.Report
	exportPath string // 脱敏诊断文件路径，渲染在报告顶部供贴 issue
	loading    bool
	renderW    int
	startedAt  time.Time
	finishedAt time.Time
	repairs    []diag.RepairResult // 最近一次按 f 自动修复的结果，渲染在报告顶部
	notice     string              // 面板内一次性提示（如运行中禁止修复）
	viewport   viewport.Model
}

func newReportState(width, height int, reqID int, startedAt time.Time) *reportState {
	boxW, boxH := reportModalSize(width, height)
	contentW := paddedModalContentWidth(boxW)
	vp := viewport.New(contentW, boxH-4) // border 2 + padding 2
	state := &reportState{
		reqID:     reqID,
		loading:   true,
		startedAt: startedAt,
		viewport:  vp,
	}
	state.setContent(contentW)
	return state
}

func (s *reportState) load(report diag.Report, contentW int, exportPath string, finishedAt time.Time) {
	s.loading = false
	s.report = &report
	s.exportPath = exportPath
	s.finishedAt = finishedAt
	s.setContent(contentW)
}

func (s *reportState) setContent(contentW int) {
	s.renderW = contentW
	switch {
	case s.loading:
		s.viewport.SetContent(renderReportLoadingText(contentW, s.startedAt))
	case s.report != nil:
		s.viewport.SetContent(renderReportText(*s.report, contentW, s.exportPath, s.startedAt, s.finishedAt, s.repairs, s.notice))
	default:
		s.viewport.SetContent("诊断报告不可用")
	}
}

func reportModalSize(termW, termH int) (int, int) {
	w := termW * 80 / 100
	if w > 100 {
		w = 100
	}
	if w < 60 {
		w = termW - 4
	}
	h := termH * 85 / 100
	if h < 20 {
		h = termH - 2
	}
	return w, h
}

func renderReportText(report diag.Report, width int, exportPath string, startedAt, finishedAt time.Time, repairs []diag.RepairResult, notice string) string {
	var b strings.Builder
	st := report.Stats

	// 概览
	titleStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	mutedStyle := lipgloss.NewStyle().Foreground(colorMuted)

	// 面板内一次性提示（如运行中禁止修复）
	if notice != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(colorReview).Render(wrapText(notice, width)))
		b.WriteString("\n\n")
	}

	// 自动修复结果（按 f 后重跑诊断，修复项应已从下方发现列表消失）
	if len(repairs) > 0 {
		okStyle := lipgloss.NewStyle().Foreground(colorSuccess)
		errStyle := lipgloss.NewStyle().Foreground(colorError)
		b.WriteString(titleStyle.Render("修复结果"))
		b.WriteString("\n")
		for _, r := range repairs {
			if r.Err != nil {
				b.WriteString(errStyle.Render("✗ " + r.Rule + ": " + r.Summary + " - " + r.Err.Error()))
			} else {
				b.WriteString(okStyle.Render("✓ " + r.Rule + ": " + r.Summary))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// 脱敏诊断已导出 → 引导用户贴 issue
	if exportPath != "" {
		exportStyle := lipgloss.NewStyle().Foreground(colorAccent2)
		b.WriteString(exportStyle.Render("已导出脱敏诊断（可贴到 GitHub issue）"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(wrapText(exportPath, width)))
		b.WriteString("\n\n")
	}

	b.WriteString(titleStyle.Render("概览"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("开始 "))
	b.WriteString(formatReportTime(startedAt))
	if !finishedAt.IsZero() {
		b.WriteString(dimStyle.Render("  完成 "))
		b.WriteString(formatReportTime(finishedAt))
	}
	b.WriteString("\n\n")

	// 第一行：章节 + 字数
	b.WriteString(mutedStyle.Render("章节 "))
	b.WriteString(fmt.Sprintf("%d/%d", st.CompletedChapters, st.TotalChapters))
	b.WriteString(mutedStyle.Render("  字数 "))
	b.WriteString(fmt.Sprintf("%d", st.TotalWords))
	if st.AvgWordsPerCh > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf(" (%d/ch)", st.AvgWordsPerCh)))
	}
	b.WriteString(mutedStyle.Render("  阶段 "))
	b.WriteString(st.Phase)
	if st.Flow != "" && st.Flow != "writing" {
		b.WriteString(mutedStyle.Render("/"))
		b.WriteString(st.Flow)
	}
	b.WriteString("\n")

	// 第二行：评审 + 改写 + 均分
	b.WriteString(mutedStyle.Render("评审 "))
	b.WriteString(fmt.Sprintf("%d次", st.ReviewCount))
	if st.RewriteCount > 0 {
		b.WriteString(mutedStyle.Render("  改写 "))
		b.WriteString(fmt.Sprintf("%d次", st.RewriteCount))
	}
	if st.AvgReviewScore > 0 {
		b.WriteString(mutedStyle.Render("  均分 "))
		b.WriteString(fmt.Sprintf("%.1f", st.AvgReviewScore))
	}
	b.WriteString("\n")

	// 第三行：伏笔 + 规划
	if st.ForeshadowOpen > 0 || st.ForeshadowStale > 0 {
		b.WriteString(mutedStyle.Render("伏笔 "))
		b.WriteString(fmt.Sprintf("打开%d", st.ForeshadowOpen))
		if st.ForeshadowStale > 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(colorReview).Render(fmt.Sprintf(" 停滞%d", st.ForeshadowStale)))
		}
		b.WriteString("\n")
	}
	if st.PlanningTier != "" {
		b.WriteString(mutedStyle.Render("规划 "))
		b.WriteString(st.PlanningTier)
		b.WriteString("\n")
	}

	// 第四行：追加式台账指标（数据先行：体积/条目随章节线性增长，是归档压缩决策依据）
	if st.StateChangeCount > 0 || st.TimelineCount > 0 || st.AppendOnlyBytes > 0 {
		b.WriteString(mutedStyle.Render("台账 "))
		b.WriteString(fmt.Sprintf("状态%d条 时间线%d条", st.StateChangeCount, st.TimelineCount))
		if st.AppendOnlyBytes > 0 {
			mb := float64(st.AppendOnlyBytes) / (1024 * 1024)
			level := dimStyle
			if st.AppendOnlyBytes > 10*1024*1024 {
				level = lipgloss.NewStyle().Foreground(colorReview) // >10MB 提示考虑归档
			}
			b.WriteString(mutedStyle.Render("  体积 "))
			b.WriteString(level.Render(fmt.Sprintf("%.1fMB", mb)))
			if st.AppendOnlyBytes > 10*1024*1024 {
				b.WriteString(dimStyle.Render("（大台账建议归档压缩）"))
			}
		}
		b.WriteString("\n")
	}

	// 发现
	b.WriteString("\n")
	findings := report.Findings
	if len(findings) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Render("未发现问题"))
		b.WriteString("\n")
		return b.String()
	}

	criticals, warnings, infos := countSeverities(findings)
	b.WriteString(titleStyle.Render("发现"))
	b.WriteString(" ")
	b.WriteString(dimStyle.Render(formatSeverityCounts(criticals, warnings, infos)))
	b.WriteString("\n")

	for _, f := range findings {
		b.WriteString("\n")
		renderFinding(&b, f, width)
	}

	if len(report.Actions) > 0 {
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("可执行动作"))
		b.WriteString(" ")
		b.WriteString(dimStyle.Render(fmt.Sprintf("(%d)", len(report.Actions))))
		b.WriteString("\n")
		actionStyle := lipgloss.NewStyle().Foreground(colorSuccess)
		for _, a := range report.Actions {
			b.WriteString("\n")
			b.WriteString(actionStyle.Render("[" + string(a.Kind) + "]"))
			b.WriteString(" ")
			b.WriteString(a.Summary)
			b.WriteString("\n")
			if a.Message != "" {
				b.WriteString("  ")
				b.WriteString(mutedStyle.Render(wrapText(a.Message, width-4)))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func renderReportLoadingText(width int, startedAt time.Time) string {
	titleStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	bodyStyle := lipgloss.NewStyle().Foreground(colorMuted)
	hintStyle := lipgloss.NewStyle().Foreground(colorDim)

	var b strings.Builder
	b.WriteString(titleStyle.Render("正在生成诊断报告"))
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("开始时间 " + formatReportTime(startedAt)))
	b.WriteString("\n\n")
	b.WriteString(bodyStyle.Render(wrapText("正在读取当前小说 output 产物并分析流程、质量、规划和上下文问题。项目较大时可能需要几秒。", width)))
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("Esc 可先关闭面板，后台分析完成后下次打开会重新生成。"))
	return b.String()
}

func formatReportTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func renderFinding(b *strings.Builder, f diag.Finding, width int) {
	var sevStyle lipgloss.Style
	var marker string
	switch f.Severity {
	case diag.SevCritical:
		sevStyle = lipgloss.NewStyle().Foreground(colorError).Bold(true)
		marker = "critical"
	case diag.SevWarning:
		sevStyle = lipgloss.NewStyle().Foreground(colorReview)
		marker = "warning"
	default:
		sevStyle = lipgloss.NewStyle().Foreground(colorDim)
		marker = "info"
	}

	evidenceStyle := lipgloss.NewStyle().Foreground(colorDim)
	suggestionStyle := lipgloss.NewStyle().Foreground(colorAccent2)

	b.WriteString(sevStyle.Render(fmt.Sprintf("[%s]", marker)))
	b.WriteString(" ")
	b.WriteString(f.Title)
	if f.Confidence != "" || f.AutoLevel != "" {
		tagStyle := lipgloss.NewStyle().Foreground(colorDim)
		tags := ""
		if f.Confidence != "" {
			tags += string(f.Confidence)
		}
		if f.AutoLevel != "" && f.AutoLevel != diag.AutoNone {
			if tags != "" {
				tags += "/"
			}
			tags += string(f.AutoLevel)
		}
		if tags != "" {
			b.WriteString(" ")
			b.WriteString(tagStyle.Render("[" + tags + "]"))
		}
	}
	b.WriteString("\n")

	if f.Evidence != "" {
		b.WriteString("  ")
		b.WriteString(evidenceStyle.Render(wrapText(f.Evidence, width-4)))
		b.WriteString("\n")
	}
	if f.Suggestion != "" {
		b.WriteString("  ")
		b.WriteString(suggestionStyle.Render("-> " + wrapText(f.Suggestion, width-7)))
		b.WriteString("\n")
	}
}

func countSeverities(findings []diag.Finding) (c, w, i int) {
	for _, f := range findings {
		switch f.Severity {
		case diag.SevCritical:
			c++
		case diag.SevWarning:
			w++
		case diag.SevInfo:
			i++
		}
	}
	return
}

func formatSeverityCounts(c, w, i int) string {
	parts := make([]string, 0, 3)
	if c > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", c))
	}
	if w > 0 {
		parts = append(parts, fmt.Sprintf("%d warning", w))
	}
	if i > 0 {
		parts = append(parts, fmt.Sprintf("%d info", i))
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " / ") + ")"
}

// wrapText 对长文本做简单换行。
func wrapText(s string, maxWidth int) string {
	if maxWidth <= 0 || lipgloss.Width(s) <= maxWidth {
		return s
	}
	var b strings.Builder
	lineW := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if lineW+w > maxWidth && lineW > 0 {
			b.WriteRune('\n')
			b.WriteString("  ") // indent continuation
			lineW = 2
		}
		b.WriteRune(r)
		lineW += w
	}
	return b.String()
}

func renderReportModal(width, height int, state *reportState) string {
	if state == nil {
		return ""
	}

	boxW, boxH := reportModalSize(width, height)

	contentW := paddedModalContentWidth(boxW)

	// 如果 viewport 尺寸变化了，更新
	if state.viewport.Width != contentW {
		state.viewport.Width = contentW
		state.viewport.Height = boxH - 4
	}
	if state.viewport.Height != boxH-4 {
		state.viewport.Height = boxH - 4
	}
	if state.renderW != contentW {
		state.setContent(contentW)
	}

	footer := "  ↑↓ 滚动 · Esc 关闭"
	if state.report != nil {
		if n := len(diag.Repairable(state.report.Findings)); n > 0 {
			footer = fmt.Sprintf("  ↑↓ 滚动 · f 自动修复 %d 项 · Esc 关闭", n)
		}
	}
	modal := renderPaddedModalFrame(
		boxW,
		boxH,
		"诊断报告",
		footer,
		strings.Split(state.viewport.View(), "\n"),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func (m Model) handleReportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.report == nil {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.report = nil
		return m, m.textarea.Focus()
	case tea.KeyUp:
		m.report.viewport.ScrollUp(1)
		return m, nil
	case tea.KeyDown:
		m.report.viewport.ScrollDown(1)
		return m, nil
	case tea.KeyPgUp:
		m.report.viewport.HalfPageUp()
		return m, nil
	case tea.KeyPgDown:
		m.report.viewport.HalfPageDown()
		return m, nil
	case tea.KeyRunes:
		return m.handleReportRepairKey(msg)
	default:
		return m, nil
	}
}

// handleReportRepairKey 处理报告面板的 f 键：对可自动修复项执行修复。
// 修复直接改写 progress.json/run.json，运行中禁止（避免与引擎并发写）。
func (m Model) handleReportRepairKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(msg.Runes) == 0 || msg.Runes[0] != 'f' || m.report.report == nil {
		return m, nil
	}
	repairable := diag.Repairable(m.report.report.Findings)
	if len(repairable) == 0 {
		return m, nil
	}
	if m.snapshot.RuntimeState == "running" || m.snapshot.RuntimeState == "paused" {
		m.report.notice = "运行或暂停中不可自动修复：请先停止创作，再打开 /diag 按 f 修复。"
		m.report.setContent(m.report.renderW)
		return m, nil
	}
	m.report.notice = "正在自动修复..."
	m.report.setContent(m.report.renderW)
	return m, repairFindings(m.runtime.Dir(), m.report.reqID, m.report.report.Findings)
}
