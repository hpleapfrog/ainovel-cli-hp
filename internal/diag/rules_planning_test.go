package diag

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestStaleForeshadowDormancyBasis(t *testing.T) {
	completed := make([]int, 0, 30)
	for ch := 1; ch <= 30; ch++ {
		completed = append(completed, ch)
	}
	snap := &Snapshot{
		Progress: &domain.Progress{CompletedChapters: completed},
		Foreshadow: []domain.ForeshadowEntry{
			// 埋设后从未推进：休眠期从 PlantedAt 起算 → 停滞
			{ID: "f_old_planted", Description: "旧伏笔", PlantedAt: 1, Status: "planted"},
			// 埋得早但最近刚推进过：不算停滞（旧口径会误报）
			{ID: "f_recent_touch", Description: "刚推进", PlantedAt: 1, Status: "advanced", LastTouchedAt: 25},
			// 推进过一次但之后烂尾：advanced 不再豁免停滞检查（旧口径漏报）
			{ID: "f_advanced_dormant", Description: "推进后烂尾", PlantedAt: 1, Status: "advanced", LastTouchedAt: 5},
			// 已回收：不参与
			{ID: "f_resolved", Description: "已回收", PlantedAt: 1, Status: "resolved", ResolvedAt: 20},
			// 近期埋设：不停滞
			{ID: "f_new", Description: "新伏笔", PlantedAt: 28, Status: "planted"},
		},
	}

	findings := StaleForeshadow(snap)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	ev := findings[0].Evidence
	if !strings.Contains(ev, "f_old_planted") {
		t.Errorf("old planted foreshadow should be stale: %s", ev)
	}
	if !strings.Contains(ev, "f_advanced_dormant") {
		t.Errorf("advanced-but-dormant foreshadow should be stale: %s", ev)
	}
	if strings.Contains(ev, "f_recent_touch") {
		t.Errorf("recently advanced foreshadow must not be stale: %s", ev)
	}
	if strings.Contains(ev, "f_resolved") || strings.Contains(ev, "f_new") {
		t.Errorf("resolved or recent foreshadow must not be stale: %s", ev)
	}
}

// 停滞阈值下限与 writer 的 foreshadow_due 共用 domain.ForeshadowDueChapters（=5）：
// 小题量书的停滞口径收紧到 5 章（原下限 8），与线上提醒同一把尺子。
func TestStaleForeshadowSharesDueThreshold(t *testing.T) {
	completed := make([]int, 0, 12)
	for ch := 1; ch <= 12; ch++ {
		completed = append(completed, ch)
	}
	snap := &Snapshot{
		Progress: &domain.Progress{CompletedChapters: completed},
		Foreshadow: []domain.ForeshadowEntry{
			{ID: "dormant_6", PlantedAt: 6, Status: "planted"}, // 休眠 6 章 > 5 → 停滞
			{ID: "dormant_5", PlantedAt: 7, Status: "planted"}, // 恰好 5 章 → 不停滞
		},
	}

	findings := StaleForeshadow(snap)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	ev := findings[0].Evidence
	if !strings.Contains(ev, "dormant_6") {
		t.Errorf("dormant 6 chapters should be stale under shared threshold 5: %s", ev)
	}
	if strings.Contains(ev, "dormant_5") {
		t.Errorf("dormant exactly 5 chapters must not be stale: %s", ev)
	}
}
