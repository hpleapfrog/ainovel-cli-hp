package diag

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// analyzeAndRepair 对当前目录跑一次诊断并执行自动修复，返回修复结果。
func analyzeAndRepair(t *testing.T, s *store.Store) []RepairResult {
	t.Helper()
	rep := Analyze(s)
	results := Repair(s, rep.Findings)
	if len(results) == 0 {
		t.Fatal("expected at least one repairable finding")
	}
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("repair %s failed: %v", r.Rule, r.Err)
		}
	}
	return results
}

// findingRules 返回一次新诊断里的规则名集合。
func findingRules(s *store.Store) map[string]bool {
	out := map[string]bool{}
	for _, f := range Analyze(s).Findings {
		out[f.Rule] = true
	}
	return out
}

func TestRepairOrphanedSteer(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Progress.Init("test", 10); err != nil {
		t.Fatal(err)
	}
	if err := s.RunMeta.SetPendingSteer("写得再黑暗一点"); err != nil {
		t.Fatal(err)
	}
	if rules := findingRules(s); !rules["OrphanedSteer"] {
		t.Fatal("expected OrphanedSteer finding before repair")
	}

	analyzeAndRepair(t, s)

	if rules := findingRules(s); rules["OrphanedSteer"] {
		t.Fatal("OrphanedSteer should be gone after repair")
	}
	meta, err := s.RunMeta.Load()
	if err != nil {
		t.Fatal(err)
	}
	if meta.PendingSteer != "" {
		t.Fatalf("pending_steer should be cleared, got %q", meta.PendingSteer)
	}
}

func TestRepairPhaseFlowMismatch(t *testing.T) {
	s := store.NewStore(t.TempDir())
	// 直接落盘损坏状态：完结阶段挂着 rewriting 流程。
	if err := s.Progress.Save(&domain.Progress{
		NovelName: "test", Phase: domain.PhaseComplete,
		TotalChapters: 10, CompletedChapters: []int{1, 2},
		Flow: domain.FlowRewriting,
	}); err != nil {
		t.Fatal(err)
	}
	if rules := findingRules(s); !rules["PhaseFlowMismatch"] {
		t.Fatal("expected PhaseFlowMismatch finding before repair")
	}

	analyzeAndRepair(t, s)

	if rules := findingRules(s); rules["PhaseFlowMismatch"] {
		t.Fatal("PhaseFlowMismatch should be gone after repair")
	}
	p, _ := s.Progress.Load()
	if p.Flow != "" {
		t.Fatalf("flow should be reset to initial, got %s", p.Flow)
	}
}

func TestRepairInvalidPendingRewrites_KeepsValid(t *testing.T) {
	s := store.NewStore(t.TempDir())
	// 队列混入未完成章节 65：修复后保留合法的 2。
	if err := s.Progress.Save(&domain.Progress{
		NovelName: "test", Phase: domain.PhaseWriting,
		TotalChapters: 10, CompletedChapters: []int{1, 2},
		Flow: domain.FlowPolishing, PendingRewrites: []int{2, 65}, RewriteReason: "补强",
	}); err != nil {
		t.Fatal(err)
	}

	analyzeAndRepair(t, s)

	if rules := findingRules(s); rules["InvalidPendingRewrites"] {
		t.Fatal("InvalidPendingRewrites should be gone after repair")
	}
	p, _ := s.Progress.Load()
	if len(p.PendingRewrites) != 1 || p.PendingRewrites[0] != 2 {
		t.Fatalf("valid entries should be kept, got %v", p.PendingRewrites)
	}
}

func TestRepairInvalidPendingRewrites_DrainsQueue(t *testing.T) {
	s := store.NewStore(t.TempDir())
	// 队列里全是未完成章节：修复后队列排空，flow 复位 writing。
	if err := s.Progress.Save(&domain.Progress{
		NovelName: "test", Phase: domain.PhaseWriting,
		TotalChapters: 10, CompletedChapters: []int{1, 2},
		Flow: domain.FlowRewriting, PendingRewrites: []int{65}, RewriteReason: "补强",
	}); err != nil {
		t.Fatal(err)
	}

	analyzeAndRepair(t, s)

	p, _ := s.Progress.Load()
	if len(p.PendingRewrites) != 0 || p.RewriteReason != "" {
		t.Fatalf("queue/reason should be drained, got %v/%q", p.PendingRewrites, p.RewriteReason)
	}
	if p.Flow != domain.FlowWriting {
		t.Fatalf("flow should be writing, got %s", p.Flow)
	}
}

func TestRepairableGating(t *testing.T) {
	findings := []Finding{
		// 已登记修复 + high + safe → 可修
		{Rule: "OrphanedSteer", Confidence: ConfHigh, AutoLevel: AutoSafe},
		// 未登记修复（ChapterGaps 需人工）→ 不可修
		{Rule: "ChapterGaps", Confidence: ConfHigh, AutoLevel: AutoSafe},
		// 中置信 → 不可修
		{Rule: "OrphanedSteer", Confidence: ConfMedium, AutoLevel: AutoSafe},
		// 非 safe → 不可修
		{Rule: "OrphanedSteer", Confidence: ConfHigh, AutoLevel: AutoSuggest},
	}
	got := Repairable(findings)
	if len(got) != 1 || got[0].Rule != "OrphanedSteer" {
		t.Fatalf("expected only the registered high/safe finding, got %+v", got)
	}
}
