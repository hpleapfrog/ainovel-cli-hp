package diag

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestContinuityAlerts_Triggers(t *testing.T) {
	snap := &Snapshot{
		Progress: &domain.Progress{CompletedChapters: []int{18, 19, 20, 21, 22}},
		ContinuityIssues: map[int]*domain.ContinuityIssues{
			20: {
				StateRegressions: []domain.StateRegression{
					{Entity: "老周", Field: "status", Curr: "死亡", Next: "重伤痊愈", Severity: domain.SeverityError},
				},
			},
			21: {
				RelationshipJumps: []domain.RelationshipJump{
					{A: "林砚", B: "赵鸿", Prev: "仇人", Next: "恋人", Gap: 1, Severity: domain.SeverityError},
				},
			},
			18: {UnreportedCharacters: []domain.UnreportedCharacter{{Name: "老周", Mentions: 3, Severity: domain.SeverityWarning}}},
			19: {UnreportedCharacters: []domain.UnreportedCharacter{{Name: "老周", Mentions: 2, Severity: domain.SeverityInfo}}},
			22: {UnreportedCharacters: []domain.UnreportedCharacter{{Name: "老周", Mentions: 4, Severity: domain.SeverityWarning}}},
		},
	}

	findings := ContinuityAlerts(snap)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d: %+v", len(findings), findings)
	}
	var crit, jumpWarn, chronicWarn *Finding
	for i := range findings {
		f := &findings[i]
		switch {
		case f.Severity == SevCritical:
			crit = f
		case strings.Contains(f.Title, "关系越级跳变"):
			jumpWarn = f
		case strings.Contains(f.Title, "出场漏报"):
			chronicWarn = f
		}
	}
	if crit == nil {
		t.Fatal("expected critical finding for error state regression")
	}
	if !strings.Contains(crit.Evidence, "ch20: 老周 status 死亡→重伤痊愈") {
		t.Fatalf("critical evidence should carry store facts, got %q", crit.Evidence)
	}
	if jumpWarn == nil || !strings.Contains(jumpWarn.Evidence, "ch21: 林砚-赵鸿 仇人→恋人") {
		t.Fatalf("jump finding/evidence mismatch: %+v", jumpWarn)
	}
	if chronicWarn == nil || !strings.Contains(chronicWarn.Evidence, "老周(18, 19, 22)") {
		t.Fatalf("chronic unreported should list chapters, got %+v", chronicWarn)
	}
}

func TestContinuityAlerts_NotTriggered(t *testing.T) {
	// 无记录
	if f := ContinuityAlerts(&Snapshot{Progress: &domain.Progress{CompletedChapters: []int{1}}}); len(f) != 0 {
		t.Fatalf("no records should not trigger, got %+v", f)
	}

	snap := &Snapshot{
		Progress: &domain.Progress{CompletedChapters: []int{18, 19, 20}},
		ContinuityIssues: map[int]*domain.ContinuityIssues{
			// 仅 warning 级回退与跳变：不达到告警级别
			18: {
				StateRegressions: []domain.StateRegression{
					{Entity: "林砚", Field: "realm", Curr: "金丹", Next: "筑基", Severity: domain.SeverityWarning},
				},
			},
			// 漏报只有 2 章：未慢性化
			19: {UnreportedCharacters: []domain.UnreportedCharacter{{Name: "老周", Mentions: 2}}},
			20: {UnreportedCharacters: []domain.UnreportedCharacter{{Name: "老周", Mentions: 3}}},
		},
	}
	if f := ContinuityAlerts(snap); len(f) != 0 {
		t.Fatalf("warning-only and sub-chronic records should not trigger, got %+v", f)
	}

	// error 记录所在章节未完成（提交中断残留）：不报
	snap2 := &Snapshot{
		Progress: &domain.Progress{CompletedChapters: []int{1, 2}},
		ContinuityIssues: map[int]*domain.ContinuityIssues{
			9: {
				StateRegressions: []domain.StateRegression{
					{Entity: "老周", Field: "status", Curr: "死亡", Next: "痊愈", Severity: domain.SeverityError},
				},
			},
		},
	}
	if f := ContinuityAlerts(snap2); len(f) != 0 {
		t.Fatalf("records of incomplete chapters should not trigger, got %+v", f)
	}
}
