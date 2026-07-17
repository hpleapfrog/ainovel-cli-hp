package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestDetectUnreportedCharacters(t *testing.T) {
	chars := []domain.Character{
		{Name: "林砚", Aliases: []string{"小林"}},
		{Name: "苏"}, // 单字名：子串误伤率太高，不参与匹配
	}
	cast := []domain.CastEntry{
		{Name: "老周", BriefRole: "客栈老板"},
	}
	content := "林砚走进客栈。林砚点了酒。老周笑着迎上来，老周擦着桌子。苏离去。"

	t.Run("未申报的正文出场角色被标出", func(t *testing.T) {
		got := detectUnreportedCharacters(content, nil, chars, cast)
		if len(got) != 2 {
			t.Fatalf("want 2 unreported, got %+v", got)
		}
		byName := map[string]domain.UnreportedCharacter{}
		for _, u := range got {
			byName[u.Name] = u
		}
		if byName["林砚"].Mentions != 2 || byName["林砚"].Severity != domain.SeverityInfo {
			t.Errorf("林砚: want mentions=2 severity=info, got %+v", byName["林砚"])
		}
		if byName["老周"].Mentions != 2 {
			t.Errorf("老周: want mentions=2, got %+v", byName["老周"])
		}
	})

	t.Run("已申报正式名则不报", func(t *testing.T) {
		got := detectUnreportedCharacters(content, []string{"林砚", "老周"}, chars, cast)
		if len(got) != 0 {
			t.Fatalf("want none, got %+v", got)
		}
	})

	t.Run("别名出场计入正式名", func(t *testing.T) {
		got := detectUnreportedCharacters("小林喝了三杯，小林又添了一碟菜。", nil, chars, nil)
		if len(got) != 1 || got[0].Name != "林砚" || got[0].Mentions != 2 {
			t.Fatalf("alias mentions should count toward canonical name, got %+v", got)
		}
	})

	t.Run("单次提及与单字名不报", func(t *testing.T) {
		got := detectUnreportedCharacters("老周在柜台后打算盘。苏苏苏苏。", nil, chars, cast)
		if len(got) != 0 {
			t.Fatalf("single mention and single-char name must be skipped, got %+v", got)
		}
	})
}

func TestDetectStateRegressionDeadRevival(t *testing.T) {
	history := []domain.StateChange{
		{Entity: "老周", Field: "status", NewValue: "死亡", Chapter: 10},
	}
	incoming := []domain.StateChange{
		{Entity: "老周", Field: "status", NewValue: "重伤痊愈", Chapter: 20},
	}
	got := detectStateRegression(history, incoming)
	if len(got) != 1 || got[0].Severity != domain.SeverityError {
		t.Fatalf("dead-then-active must be an error regression, got %+v", got)
	}
}

func TestDetectRelationshipJumpLevels(t *testing.T) {
	history := []domain.RelationshipEntry{
		{CharacterA: "林砚", CharacterB: "赵鸿", Relation: "仇人", Chapter: 18},
	}
	incoming := []domain.RelationshipEntry{
		{CharacterA: "林砚", CharacterB: "赵鸿", Relation: "恋人", Chapter: 19},
	}
	got := detectRelationshipJump(history, incoming)
	if len(got) != 1 || got[0].Severity != domain.SeverityError {
		t.Fatalf("仇人→恋人 in 1 chapter must be an error jump, got %+v", got)
	}
}

// ── commit 全路径回归：历史基线必须不含本章增量 ──

func commitForContinuity(t *testing.T, st *store.Store, args map[string]any) *domain.ContinuityIssues {
	t.Helper()
	tool := NewCommitChapterTool(st)
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	raw, err := tool.Execute(context.Background(), rawArgs)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	var out struct {
		ContinuityIssues *domain.ContinuityIssues `json:"continuity_issues"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return out.ContinuityIssues
}

func newCommittedBook(t *testing.T) *store.Store {
	t.Helper()
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init("test", 20); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	for ch := 1; ch <= 2; ch++ {
		if err := st.Progress.MarkChapterComplete(ch, 3000, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete(%d): %v", ch, err)
		}
	}
	if err := st.Drafts.SaveDraft(3, "林砚与赵鸿在城外相见，长谈至深夜。"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	return st
}

// 死亡复生必须报 error：历史（ch2 死亡）不含本章增量时，新状态（ch3 痊愈）才比得出矛盾。
// 旧接线把增量先落账再检测，currState 就是 incoming 自己——这条曾经漏检。
func TestCommitDetectsDeadRevival(t *testing.T) {
	st := newCommittedBook(t)
	if err := st.World.AppendStateChanges([]domain.StateChange{
		{Entity: "老周", Field: "status", NewValue: "死亡", Chapter: 2},
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	if err := st.World.SaveRelationships([]domain.RelationshipEntry{
		{CharacterA: "林砚", CharacterB: "赵鸿", Relation: "仇人", Chapter: 2},
	}); err != nil {
		t.Fatalf("seed relations: %v", err)
	}

	issues := commitForContinuity(t, st, map[string]any{
		"chapter":    3,
		"summary":    "城外夜谈",
		"characters": []string{"林砚", "赵鸿"},
		"key_events": []string{"相见"},
		"state_changes": []map[string]any{
			{"entity": "老周", "field": "status", "new_value": "重伤痊愈"},
		},
		"relationship_changes": []map[string]any{
			{"character_a": "林砚", "character_b": "赵鸿", "relation": "恋人"},
		},
	})

	if issues == nil {
		t.Fatal("expected continuity issues (dead revival + relationship jump)")
	}
	var reg *domain.StateRegression
	for i := range issues.StateRegressions {
		if issues.StateRegressions[i].Entity == "老周" {
			reg = &issues.StateRegressions[i]
		}
	}
	if reg == nil || reg.Severity != domain.SeverityError || reg.Curr != "死亡" || reg.Next != "重伤痊愈" {
		t.Fatalf("dead revival must be an error regression, got %+v", issues.StateRegressions)
	}
	var jump *domain.RelationshipJump
	for i := range issues.RelationshipJumps {
		if issues.RelationshipJumps[i].A == "林砚" {
			jump = &issues.RelationshipJumps[i]
		}
	}
	if jump == nil || jump.Severity != domain.SeverityError || jump.Prev != "仇人" || jump.Next != "恋人" {
		t.Fatalf("仇人→恋人 must be an error jump, got %+v", issues.RelationshipJumps)
	}

	// 落盘同步（editor 消费侧）
	persisted := st.World.LoadContinuityIssues(3)
	if persisted == nil || len(persisted.StateRegressions) != 1 {
		t.Fatalf("issues should persist for editor, got %+v", persisted)
	}
}

// 合法死亡不得误报：旧接线把 incoming 当历史，新值是死亡就报"复活"（Curr=死亡 Next=死亡 的笑话）。
func TestCommitDoesNotFlagLegitimateDeath(t *testing.T) {
	st := newCommittedBook(t)
	if err := st.World.AppendStateChanges([]domain.StateChange{
		{Entity: "老周", Field: "status", NewValue: "重伤", Chapter: 2},
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	issues := commitForContinuity(t, st, map[string]any{
		"chapter":    3,
		"summary":    "老周之死",
		"characters": []string{"林砚", "赵鸿"},
		"key_events": []string{"老周身亡"},
		"state_changes": []map[string]any{
			{"entity": "老周", "field": "status", "new_value": "死亡"},
		},
	})
	if issues != nil && len(issues.StateRegressions) > 0 {
		t.Fatalf("legitimate death must not be flagged, got %+v", issues.StateRegressions)
	}
}

// ── detectUnreportedCharacters 边界 ──

func TestDetectUnreportedCharacters_AliasCountsAsReported(t *testing.T) {
	chars := []domain.Character{{Name: "周伯", Aliases: []string{"老周"}}}
	got := detectUnreportedCharacters("老周笑着迎上来。老周擦着桌子。", []string{"老周"}, chars, nil)
	if len(got) != 0 {
		t.Fatalf("writer 用别名申报不应误报, got %+v", got)
	}
}

func TestDetectUnreportedCharacters_DedupesCanonicalAcrossSources(t *testing.T) {
	// characters.json 与名册同名（配角升级为档案角色的过渡期）：只报一条
	chars := []domain.Character{{Name: "老周"}}
	cast := []domain.CastEntry{{Name: "老周"}}
	got := detectUnreportedCharacters("老周笑着。老周擦桌。", nil, chars, cast)
	if len(got) != 1 {
		t.Fatalf("同名应只报一条, got %+v", got)
	}
}
