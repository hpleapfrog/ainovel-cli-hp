package diag

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/rules"
)

// CrossChapterFatigue:用户显式指定的疲劳词,单章都合规但跨章高覆盖时提醒(P8-3)。
func TestCrossChapterFatigue(t *testing.T) {
	texts := map[int]string{
		1: "他不禁皱眉。他不禁叹了口气。",
		2: "她不禁苦笑。他不禁抬头。",
		3: "他不禁握拳。他不禁低声问。她不自觉地替他辩护。",
		4: "他不禁停下脚步。他不禁回头。",
	}

	t.Run("跨章高覆盖→warning", func(t *testing.T) {
		snap := &Snapshot{
			UserRules:    &rules.Snapshot{Structured: rules.Structured{FatigueWords: map[string]int{"不禁": 2}}},
			ChapterTexts: texts,
		}
		findings := CrossChapterFatigue(snap)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %+v", findings)
		}
		f := findings[0]
		if f.Rule != "CrossChapterFatigue" || f.Severity != SevWarning || f.Confidence != ConfMedium || f.AutoLevel != AutoNone {
			t.Fatalf("meta wrong: %+v", f)
		}
		if !strings.Contains(f.Title, "4/4 章") {
			t.Fatalf("title should state coverage: %q", f.Title)
		}
	})

	t.Run("覆盖不足不报", func(t *testing.T) {
		snap := &Snapshot{
			UserRules: &rules.Snapshot{Structured: rules.Structured{FatigueWords: map[string]int{"不禁": 2}}},
			ChapterTexts: map[int]string{
				1: "他不禁皱眉。",
				2: "他神色如常。",
				3: "他转身离去。",
				4: "他不再言语。",
			},
		}
		if got := CrossChapterFatigue(snap); len(got) != 0 {
			t.Fatalf("low coverage must not flag, got %+v", got)
		}
	})

	t.Run("无用户规则/正文不报", func(t *testing.T) {
		if got := CrossChapterFatigue(&Snapshot{}); len(got) != 0 {
			t.Fatalf("empty snapshot must not flag, got %+v", got)
		}
		snap := &Snapshot{
			UserRules:    &rules.Snapshot{Structured: rules.Structured{FatigueWords: map[string]int{"不禁": 2}}},
			ChapterTexts: map[int]string{1: "a", 2: "b"}, // 少于 3 章
		}
		if got := CrossChapterFatigue(snap); len(got) != 0 {
			t.Fatalf("few chapters must not flag, got %+v", got)
		}
	})
}

// FactionDrift：势力档案快照与状态历史轨迹漂移检测（W1）。
func TestFactionDrift(t *testing.T) {
	t.Run("档案滞后于历史轨迹→warning", func(t *testing.T) {
		snap := &Snapshot{
			Factions: []domain.Faction{{Name: "青云宗", Status: "鼎盛"}},
			StateChanges: []domain.StateChange{
				{Entity: "青云宗", Field: "status", NewValue: "被灭", Chapter: 50},
			},
		}
		findings := FactionDrift(snap)
		if len(findings) != 1 || findings[0].Rule != "FactionDrift" || findings[0].Severity != SevWarning {
			t.Fatalf("expected drift finding, got %+v", findings)
		}
		if findings[0].Confidence != ConfHigh {
			t.Fatalf("mechanical comparison must be high confidence: %+v", findings[0])
		}
	})

	t.Run("档案与轨迹一致不报", func(t *testing.T) {
		snap := &Snapshot{
			Factions: []domain.Faction{{Name: "青云宗", Status: "被灭"}},
			StateChanges: []domain.StateChange{
				{Entity: "青云宗", Field: "status", NewValue: "被灭", Chapter: 50},
			},
		}
		if got := FactionDrift(snap); len(got) != 0 {
			t.Fatalf("consistent facts must not flag: %+v", got)
		}
	})

	t.Run("无档案/无轨迹不报", func(t *testing.T) {
		if got := FactionDrift(&Snapshot{}); len(got) != 0 {
			t.Fatalf("empty snapshot must not flag: %+v", got)
		}
	})
}
