package tools

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
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
