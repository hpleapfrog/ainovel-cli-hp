package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestForeshadowWithDormancy(t *testing.T) {
	entries := []domain.ForeshadowEntry{
		{ID: "no_touch", PlantedAt: 10, Status: "planted"},                   // 无推进记录 → 从埋设章算
		{ID: "touched", PlantedAt: 3, Status: "advanced", LastTouchedAt: 18}, // 从最近推进章算
		{ID: "future", PlantedAt: 20, Status: "planted", LastTouchedAt: 21},  // 异常未来章 → 兜底 0，不出负数
	}
	got := foreshadowWithDormancy(entries, 20)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].ChaptersSinceLastTouch != 10 {
		t.Errorf("no_touch: want 10, got %d", got[0].ChaptersSinceLastTouch)
	}
	if got[1].ChaptersSinceLastTouch != 2 {
		t.Errorf("touched: want 2, got %d", got[1].ChaptersSinceLastTouch)
	}
	if got[2].ChaptersSinceLastTouch != 0 {
		t.Errorf("future: want 0, got %d", got[2].ChaptersSinceLastTouch)
	}
	// 原始字段必须原样带出（注入视图 = 台账 + 派生字段）
	if got[1].ID != "touched" || got[1].PlantedAt != 3 || got[1].Status != "advanced" {
		t.Errorf("entry fields must pass through: %+v", got[1])
	}
}

func TestForeshadowDue(t *testing.T) {
	entries := []domain.ForeshadowEntry{
		{ID: "fresh", PlantedAt: 17, Status: "planted"},                         // 3 章：未到期
		{ID: "boundary", PlantedAt: 15, Status: "planted"},                      // 恰好 5 章：未到期（严格大于）
		{ID: "due_second", PlantedAt: 1, Status: "advanced", LastTouchedAt: 12}, // 8 章：到期，排第二
		{ID: "due_first", PlantedAt: 2, Status: "planted"},                      // 18 章：到期，排第一
	}
	got := foreshadowDue(entries, 20)
	if len(got) != 2 {
		t.Fatalf("want 2 due, got %+v", got)
	}
	if got[0].ID != "due_first" || got[0].ChaptersSinceLastTouch != 18 {
		t.Errorf("first: want due_first(18), got %+v", got[0])
	}
	if got[1].ID != "due_second" || got[1].ChaptersSinceLastTouch != 8 {
		t.Errorf("second: want due_second(8), got %+v", got[1])
	}

	// 与 diag StaleForeshadow 同阈值：常量即 domain.ForeshadowDueChapters
	if domain.ForeshadowDueChapters != 5 {
		t.Fatalf("shared threshold changed unexpectedly: %d", domain.ForeshadowDueChapters)
	}
}

// check_consistency 的 foreshadow_ledger 与 novel_context 同一视图：
// 每条带 chapters_since_last_touch 派生字段（形状不再漂移）。
func TestCheckConsistencyInjectsDormantForeshadow(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Drafts.SaveDraft(7, "正文内容"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.World.SaveForeshadowLedger([]domain.ForeshadowEntry{
		{ID: "f1", Description: "旧伏笔", PlantedAt: 2, Status: "advanced", LastTouchedAt: 5},
	}); err != nil {
		t.Fatalf("SaveForeshadowLedger: %v", err)
	}

	tool := NewCheckConsistencyTool(s)
	args, _ := json.Marshal(map[string]any{"chapter": 7})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var payload struct {
		Foreshadow []domain.ForeshadowStatus `json:"foreshadow_ledger"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(payload.Foreshadow) != 1 {
		t.Fatalf("want 1 entry, got %+v", payload.Foreshadow)
	}
	if payload.Foreshadow[0].ChaptersSinceLastTouch != 2 {
		t.Fatalf("dormancy = %d, want 2 (7-5)", payload.Foreshadow[0].ChaptersSinceLastTouch)
	}
}
