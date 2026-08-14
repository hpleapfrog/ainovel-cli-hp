package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// 世界观分级召回（W6 机械版）：规则超过阈值时按本章相关性筛选注入视图，
// 硬约束恒保留；低于阈值时视图与全量一致（不产生裁剪副作用）。
func TestWorldRulesTrim(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init("trim-test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// 45 条规则:1 条硬约束 + 1 条与本章相关的 + 43 条无关
	rules := []domain.WorldRule{{
		Category: "magic", Rule: "魔法必须吟唱", Boundary: "禁止瞬发",
		HardConstraint: &domain.HardConstraint{Kind: "prohibition", Field: "power", Value: "瞬发魔法"},
	}, {
		Category: "geography", Rule: "雾脉自北山发源，贯穿三郡", Essence: "灵气循雾脉流转",
	}}
	for i := 0; i < 43; i++ {
		rules = append(rules, domain.WorldRule{
			Category: "other", Rule: fmt.Sprintf("无关规则 %d：某个遥远的设定", i),
		})
	}
	if err := st.World.SaveWorldRules(rules); err != nil {
		t.Fatalf("SaveWorldRules: %v", err)
	}

	// 本章大纲与"雾脉"相关
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{{
		Chapter: 1, Title: "雾脉", CoreEvent: "主角沿雾脉北上", Hook: "雾脉源头", Scenes: []string{"北山"},
	}}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}

	tool := NewContextTool(st, References{})
	args, _ := json.Marshal(map[string]any{"chapter": 1})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		WorldRules        []domain.WorldRule `json:"world_rules"`
		WorldRulesTrimmed map[string]any     `json:"world_rules_trimmed"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if out.WorldRulesTrimmed == nil {
		t.Fatal("45 条规则应触发裁剪标记 world_rules_trimmed")
	}
	if len(out.WorldRules) != worldRulesTrimCap {
		t.Fatalf("kept = %d, want cap %d", len(out.WorldRules), worldRulesTrimCap)
	}
	// 硬约束恒保留
	foundHC := false
	for _, r := range out.WorldRules {
		if r.HardConstraint != nil {
			foundHC = true
		}
	}
	if !foundHC {
		t.Fatal("hard constraint must always be kept")
	}
	// 相关规则(雾脉)保留
	foundRelated := false
	for _, r := range out.WorldRules {
		if r.Rule == "雾脉自北山发源，贯穿三郡" {
			foundRelated = true
		}
	}
	if !foundRelated {
		t.Fatalf("chapter-relevant rule must be kept: %+v", out.WorldRules)
	}
}

// 低于阈值:视图与全量一致,无裁剪标记。
func TestWorldRulesTrim_BelowThresholdNoop(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init("trim-test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	rules := []domain.WorldRule{{Category: "magic", Rule: "魔法必须吟唱"}}
	if err := st.World.SaveWorldRules(rules); err != nil {
		t.Fatalf("SaveWorldRules: %v", err)
	}

	tool := NewContextTool(st, References{})
	args, _ := json.Marshal(map[string]any{"chapter": 1})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		WorldRules        []domain.WorldRule `json:"world_rules"`
		WorldRulesTrimmed map[string]any     `json:"world_rules_trimmed"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(out.WorldRules) != 1 || out.WorldRulesTrimmed != nil {
		t.Fatalf("below threshold must be noop: rules=%+v marker=%+v", out.WorldRules, out.WorldRulesTrimmed)
	}
}
