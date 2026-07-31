package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/internal/diag"
	"github.com/voocel/ainovel-cli/internal/host"
)

func repairableReport() *diag.Report {
	return &diag.Report{Findings: []diag.Finding{
		{Rule: "OrphanedSteer", Confidence: diag.ConfHigh, AutoLevel: diag.AutoSafe, Title: "存在未消费的转向指令"},
	}}
}

func TestReportRepairBlockedWhileRunning(t *testing.T) {
	m := Model{snapshot: host.UISnapshot{RuntimeState: "running"}}
	m.report = &reportState{reqID: 1, report: repairableReport()}

	_, cmd := m.handleReportKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if cmd != nil {
		t.Fatal("repair must not run while engine is running")
	}
	if m.report.notice == "" {
		t.Fatal("expected a notice explaining the block")
	}
}

func TestReportRepairBlockedWhilePaused(t *testing.T) {
	m := Model{snapshot: host.UISnapshot{RuntimeState: "paused"}}
	m.report = &reportState{reqID: 1, report: repairableReport()}

	_, cmd := m.handleReportKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if cmd != nil {
		t.Fatal("repair must not run while engine is paused")
	}
	if m.report.notice == "" {
		t.Fatal("expected a notice explaining the block")
	}
}

func TestReportFooterShowsRepairHint(t *testing.T) {
	st := newReportState(120, 40, 1, time.Now())
	st.load(*repairableReport(), 96, "", time.Now())

	out := renderReportModal(120, 40, st)
	if !strings.Contains(out, "f 自动修复 1 项") {
		t.Fatalf("footer should advertise repair, got:\n%s", out)
	}

	plain := newReportState(120, 40, 2, time.Now())
	plain.load(diag.Report{}, 96, "", time.Now())
	out = renderReportModal(120, 40, plain)
	if strings.Contains(out, "自动修复") {
		t.Fatalf("footer should not advertise repair without repairable findings, got:\n%s", out)
	}
}

func TestReportRendersRepairResults(t *testing.T) {
	st := newReportState(120, 40, 1, time.Now())
	st.repairs = []diag.RepairResult{
		{Rule: "OrphanedSteer", Summary: "已清除未消费的转向指令"},
	}
	st.load(diag.Report{}, 96, "", time.Now())

	content := st.viewport.View()
	if !strings.Contains(content, "修复结果") || !strings.Contains(content, "OrphanedSteer") {
		t.Fatalf("repair results should render on top of report, got:\n%s", content)
	}
}
