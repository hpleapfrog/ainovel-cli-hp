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

// W1 势力兴衰与角色死亡同管道：已灭势力"复出"必须报 error。
func TestDetectStateRegressionFactionDeadRevival(t *testing.T) {
	history := []domain.StateChange{
		{Entity: "青云宗", Field: "status", NewValue: "被灭门", Chapter: 50},
	}
	incoming := []domain.StateChange{
		{Entity: "青云宗", Field: "status", NewValue: "重新崛起", Chapter: 80},
	}
	got := detectStateRegression(history, incoming)
	if len(got) != 1 || got[0].Severity != domain.SeverityError {
		t.Fatalf("destroyed faction revival must be an error regression, got %+v", got)
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

// ── checkPlanContinuity：contract_violation 单向判定 ──

func planContinuityStore(t *testing.T) *store.Store {
	t.Helper()
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init("test", 20); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	return st
}

func contractViolations(warnings []PlanWarning) []PlanWarning {
	var out []PlanWarning
	for _, w := range warnings {
		if w.Rule == "contract_violation" {
			out = append(out, w)
		}
	}
	return out
}

// 计划文本命中 forbidden_moves 条目 → 提醒；未命中 → 不报。
// 旧实现是双向包含，planText 为多字段拼接长文，反向包含几乎必中/必不中，判定无效。
func TestCheckPlanContinuityContractViolation(t *testing.T) {
	st := planContinuityStore(t)

	hit := checkPlanContinuity(st, domain.ChapterPlan{
		Chapter: 3,
		Goal:    "林砚夜探禁地，提前揭露师尊真实身份",
		Contract: domain.ChapterContract{
			ForbiddenMoves: []string{"提前揭露师尊真实身份"},
		},
	})
	if got := contractViolations(hit); len(got) != 1 {
		t.Fatalf("plan 命中禁止项应报 1 条 contract_violation, got %+v", got)
	}

	miss := checkPlanContinuity(st, domain.ChapterPlan{
		Chapter: 3,
		Goal:    "林砚夜探禁地",
		Contract: domain.ChapterContract{
			ForbiddenMoves: []string{"提前揭露师尊真实身份"},
		},
	})
	if got := contractViolations(miss); len(got) != 0 {
		t.Fatalf("plan 未命中禁止项不应报 contract_violation, got %+v", got)
	}
}

// 空 forbidden 条目是任意字符串的子串（strings.Contains 恒真），必须跳过，否则必误报。
func TestCheckPlanContinuitySkipsEmptyForbiddenMove(t *testing.T) {
	st := planContinuityStore(t)
	warnings := checkPlanContinuity(st, domain.ChapterPlan{
		Chapter:  3,
		Goal:     "林砚夜探禁地",
		Contract: domain.ChapterContract{ForbiddenMoves: []string{""}},
	})
	if got := contractViolations(warnings); len(got) != 0 {
		t.Fatalf("空 forbidden 条目不应触发 contract_violation, got %+v", got)
	}
}

// ── classifyRelation：多关键词文本分类确定 ──

// 关系文本同时含多个等级关键词时，按特异性（|等级| 高者优先）首个命中生效，
// 且结果与调用次数无关。旧实现遍历 map（迭代顺序随机），同文本多次运行分类会漂移。
func TestClassifyRelationMultiKeywordDeterministic(t *testing.T) {
	cases := []struct {
		rel  string
		want int
	}{
		{"昔日仇人今日盟友", -3}, // 仇人(-3) 与 盟友(2) 并存，高特异性的 -3 优先
		{"从敌人变成恋人", 3},   // 敌人(-2) 与 恋人(3) 并存，|3| > |-2|
		{"双方仍是陌生人", 0},   // 0 级关键词命中即返回，不落正/负兜底词
	}
	for _, c := range cases {
		for range 10 {
			if got := classifyRelation(c.rel); got != c.want {
				t.Fatalf("classifyRelation(%q) = %d, want %d（多次调用结果须稳定）", c.rel, got, c.want)
			}
		}
	}
}

// ── detectFactConflicts：数值事实未交代变更 ──

func TestDetectFactConflicts(t *testing.T) {
	history := []domain.StateChange{
		{Entity: "星辰科技", Field: "人数", NewValue: "41人", Chapter: 5},
		{Entity: "林砚", Field: "realm", NewValue: "筑基", Chapter: 5},
	}

	t.Run("数值变更未交代旧值记 warning", func(t *testing.T) {
		got := detectFactConflicts(history, []domain.StateChange{
			{Entity: "星辰科技", Field: "人数", NewValue: "300人", Chapter: 30},
		})
		if len(got) != 1 || got[0].Prev != "41人" || got[0].Next != "300人" || got[0].Severity != domain.SeverityWarning {
			t.Fatalf("41→300 未交代旧值应记 warning, got %+v", got)
		}
	})

	t.Run("old_value 与历史值吻合视为合理变更不报", func(t *testing.T) {
		got := detectFactConflicts(history, []domain.StateChange{
			{Entity: "星辰科技", Field: "人数", OldValue: "41人", NewValue: "300人", Chapter: 30},
		})
		if len(got) != 0 {
			t.Fatalf("明知旧值的剧情内变更不应记, got %+v", got)
		}
	})

	t.Run("old_value 填错也记", func(t *testing.T) {
		got := detectFactConflicts(history, []domain.StateChange{
			{Entity: "星辰科技", Field: "人数", OldValue: "50人", NewValue: "300人", Chapter: 30},
		})
		if len(got) != 1 {
			t.Fatalf("old_value 与历史不符应记, got %+v", got)
		}
	})

	t.Run("白名单字段/无数字/无历史/值一致均不报", func(t *testing.T) {
		got := detectFactConflicts(history, []domain.StateChange{
			{Entity: "林砚", Field: "realm", NewValue: "练气", Chapter: 30},  // 回退白名单字段归 detectStateRegression
			{Entity: "星辰科技", Field: "人数", NewValue: "四十一人", Chapter: 30}, // 无数字，无法机械比对
			{Entity: "新公司", Field: "人数", NewValue: "10人", Chapter: 30},   // 无历史记录
			{Entity: "星辰科技", Field: "人数", NewValue: "41人", Chapter: 30},  // 与历史一致
		})
		if len(got) != 0 {
			t.Fatalf("这些场景都不应记, got %+v", got)
		}
	})
}

// commit 全路径：数值事实冲突经 commit 检测并落盘供 editor 消费。
func TestCommitDetectsFactConflict(t *testing.T) {
	st := newCommittedBook(t)
	if err := st.World.AppendStateChanges([]domain.StateChange{
		{Entity: "星辰科技", Field: "人数", NewValue: "41人", Chapter: 2},
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	issues := commitForContinuity(t, st, map[string]any{
		"chapter":    3,
		"summary":    "公司突然扩张",
		"characters": []string{"林砚"},
		"key_events": []string{"扩招"},
		"state_changes": []map[string]any{
			{"entity": "星辰科技", "field": "人数", "new_value": "300人"},
		},
	})
	if issues == nil || len(issues.FactConflicts) != 1 {
		t.Fatalf("expected one fact conflict, got %+v", issues)
	}
	fc := issues.FactConflicts[0]
	if fc.Entity != "星辰科技" || fc.Prev != "41人" || fc.Next != "300人" || fc.Severity != domain.SeverityWarning {
		t.Fatalf("fact conflict fields wrong: %+v", fc)
	}

	// 落盘同步（editor 消费侧）
	persisted := st.World.LoadContinuityIssues(3)
	if persisted == nil || len(persisted.FactConflicts) != 1 {
		t.Fatalf("issues should persist for editor, got %+v", persisted)
	}
}

// ── 世界观硬约束检测（W2）──

func TestDetectHardConstraintViolations(t *testing.T) {
	rules := []domain.WorldRule{
		{Rule: "魔法必须吟唱", Boundary: "禁止瞬发",
			HardConstraint: &domain.HardConstraint{Kind: "prohibition", Field: "power", Value: "瞬发魔法"}},
		{Rule: "死者不可复生",
			HardConstraint: &domain.HardConstraint{Kind: "prohibition", Field: "status", Value: "复活", Entity: "任何人"}},
		{Rule: "纯软设定", Boundary: "无硬约束"},
	}

	t.Run("受控字段取到禁止值→error", func(t *testing.T) {
		got := detectHardConstraintViolations(rules, []domain.StateChange{
			{Entity: "林砚", Field: "power", NewValue: "瞬发魔法", Chapter: 3},
		}, "")
		if len(got) != 1 || got[0].Severity != domain.SeverityError ||
			got[0].Entity != "林砚" || got[0].Rule != "魔法必须吟唱" || got[0].Source != "state_change" {
			t.Fatalf("want one prohibition hit, got %+v", got)
		}
	})

	t.Run("实体限定只命中限定实体", func(t *testing.T) {
		got := detectHardConstraintViolations(rules, []domain.StateChange{
			{Entity: "林砚", Field: "status", NewValue: "复活", Chapter: 3},
		}, "")
		if len(got) != 0 {
			t.Fatalf("entity mismatch must not hit, got %+v", got)
		}
		got = detectHardConstraintViolations(rules, []domain.StateChange{
			{Entity: "任何人", Field: "status", NewValue: "复活", Chapter: 3},
		}, "")
		if len(got) != 1 {
			t.Fatalf("entity match must hit, got %+v", got)
		}
	})

	t.Run("字段/值不匹配不报", func(t *testing.T) {
		got := detectHardConstraintViolations(rules, []domain.StateChange{
			{Entity: "林砚", Field: "power", NewValue: "吟唱魔法", Chapter: 3},
			{Entity: "林砚", Field: "location", NewValue: "瞬发魔法", Chapter: 3},
			{Entity: "林砚", Field: "人数", NewValue: "300人", Chapter: 3},
		}, "")
		if len(got) != 0 {
			t.Fatalf("no hit expected, got %+v", got)
		}
	})

	t.Run("scan_text 正文出现禁止值→warning", func(t *testing.T) {
		scanRules := []domain.WorldRule{
			{Rule: "魔法必须吟唱", Boundary: "禁止瞬发",
				HardConstraint: &domain.HardConstraint{Kind: "prohibition", Field: "power", Value: "瞬发魔法", ScanText: true}},
		}
		got := detectHardConstraintViolations(scanRules, nil, "林砚抬手，瞬发魔法轰向对手。")
		if len(got) != 1 || got[0].Severity != domain.SeverityWarning ||
			got[0].Source != "chapter_text" || got[0].Occurrences != 1 {
			t.Fatalf("want one text hit, got %+v", got)
		}
		// 未开 scan_text 时不扫正文
		got = detectHardConstraintViolations(rules, nil, "林砚瞬发魔法轰向对手。")
		if len(got) != 0 {
			t.Fatalf("no scan_text must not scan content, got %+v", got)
		}
	})
}

// commit 全路径：硬约束违规经 commit 检测并落盘供 editor 消费。
func TestCommitDetectsHardConstraintViolation(t *testing.T) {
	st := newCommittedBook(t)
	if err := st.World.SaveWorldRules([]domain.WorldRule{{
		Category: "magic", Rule: "魔法必须吟唱", Boundary: "禁止瞬发",
		HardConstraint: &domain.HardConstraint{Kind: "prohibition", Field: "power", Value: "瞬发魔法"},
	}}); err != nil {
		t.Fatalf("seed world rules: %v", err)
	}

	issues := commitForContinuity(t, st, map[string]any{
		"chapter":    3,
		"summary":    "林砚瞬发魔法",
		"characters": []string{"林砚"},
		"key_events": []string{"瞬发"},
		"state_changes": []map[string]any{
			{"entity": "林砚", "field": "power", "new_value": "瞬发魔法"},
		},
	})
	if issues == nil || len(issues.HardConstraintViolations) != 1 {
		t.Fatalf("expected one hard constraint violation, got %+v", issues)
	}
	v := issues.HardConstraintViolations[0]
	if v.Entity != "林砚" || v.Field != "power" || v.Value != "瞬发魔法" || v.Severity != domain.SeverityError {
		t.Fatalf("violation fields wrong: %+v", v)
	}

	persisted := st.World.LoadContinuityIssues(3)
	if persisted == nil || len(persisted.HardConstraintViolations) != 1 {
		t.Fatalf("hard constraint violations should persist for editor, got %+v", persisted)
	}
}
