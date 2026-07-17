package rules

import (
	"testing"
)

// findViolation 在结果中按 rule + target 查找第一条违规。
func findViolation(vs []Violation, rule, target string) *Violation {
	for i := range vs {
		if vs[i].Rule == rule && vs[i].Target == target {
			return &vs[i]
		}
	}
	return nil
}

func TestCheck_EmptyStructured(t *testing.T) {
	vs := Check("任何内容", Structured{})
	if vs != nil {
		t.Errorf("empty structured should return nil, got %+v", vs)
	}
}

func TestCheck_ForbiddenChars(t *testing.T) {
	text := "他笑了——又叹了口气——离去。"
	vs := Check(text, Structured{
		ForbiddenChars: []string{"——"},
	})
	v := findViolation(vs, "forbidden_chars", "——")
	if v == nil {
		t.Fatal("expected forbidden_chars violation")
	}
	if v.Severity != SeverityError {
		t.Errorf("severity=%s, want error", v.Severity)
	}
	if v.Actual != 2 {
		t.Errorf("actual=%v, want 2", v.Actual)
	}
}

func TestCheck_ForbiddenCharsNotPresent(t *testing.T) {
	vs := Check("普通文本无违规", Structured{
		ForbiddenChars: []string{"——"},
	})
	if len(vs) != 0 {
		t.Errorf("expected no violations, got %+v", vs)
	}
}

func TestCheck_ForbiddenPhrases(t *testing.T) {
	text := "不是……而是真相被掩盖了。这里探讨核心动机。"
	vs := Check(text, Structured{
		ForbiddenPhrases: []string{"不是……而是", "核心动机"},
	})
	if len(vs) != 2 {
		t.Errorf("expected 2 violations, got %d: %+v", len(vs), vs)
	}
	for _, v := range vs {
		if v.Severity != SeverityError {
			t.Errorf("severity=%s, want error", v.Severity)
		}
	}
}

func TestCheck_FatigueWordsUnderLimit(t *testing.T) {
	text := "他不禁笑了。"
	vs := Check(text, Structured{
		FatigueWords: map[string]int{"不禁": 1},
	})
	if len(vs) != 0 {
		t.Errorf("under limit should not violate, got %+v", vs)
	}
}

func TestCheck_FatigueWordsAtLimit(t *testing.T) {
	// limit=1，actual=1 → 不违规
	text := "他不禁笑了。"
	vs := Check(text, Structured{
		FatigueWords: map[string]int{"不禁": 1},
	})
	if len(vs) != 0 {
		t.Errorf("at limit should not violate (limit 1 actual 1), got %+v", vs)
	}
}

func TestCheck_FatigueWordsOverLimit(t *testing.T) {
	// limit=1，actual=3 → warning
	text := "他不禁笑了，又不禁皱眉，最后不禁离去。"
	vs := Check(text, Structured{
		FatigueWords: map[string]int{"不禁": 1},
	})
	v := findViolation(vs, "fatigue_words", "不禁")
	if v == nil {
		t.Fatal("expected fatigue_words violation")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity=%s, want warning", v.Severity)
	}
	if v.Limit != 1 {
		t.Errorf("limit=%v, want 1", v.Limit)
	}
	if v.Actual != 3 {
		t.Errorf("actual=%v, want 3", v.Actual)
	}
}

func TestCheck_MultipleRulesAtOnce(t *testing.T) {
	text := "他不禁——又不禁——离去。"
	s := Structured{
		ForbiddenChars: []string{"——"},
		FatigueWords:   map[string]int{"不禁": 1},
	}
	vs := Check(text, s)

	// 应同时触发两类：forbidden_chars + fatigue_words
	rules := map[string]bool{}
	for _, v := range vs {
		rules[v.Rule] = true
	}
	if !rules["forbidden_chars"] || !rules["fatigue_words"] {
		t.Errorf("expected both rules triggered, got %+v", rules)
	}
}

func TestCheck_FatigueZeroLimitSkipped(t *testing.T) {
	// limit=0 是非法值，应跳过整条规则（parser 也会过滤，这里防御）
	text := "不禁不禁不禁"
	vs := Check(text, Structured{
		FatigueWords: map[string]int{"不禁": 0},
	})
	if len(vs) != 0 {
		t.Errorf("limit=0 should be skipped, got %+v", vs)
	}
}

func TestCheck_EmptyTargetsSkipped(t *testing.T) {
	// 空字符串目标不应导致 false positive
	vs := Check("任何文本", Structured{
		ForbiddenChars:   []string{""},
		ForbiddenPhrases: []string{""},
		FatigueWords:     map[string]int{"": 1},
	})
	if len(vs) != 0 {
		t.Errorf("empty targets should be skipped, got %+v", vs)
	}
}

// ── pov_person ──

func TestCheck_POVThirdPersonViolation(t *testing.T) {
	// 叙述段（无引号）出现 4 次第一人称词根 → 超阈值 warning
	text := "他走进客栈。我想起了一件事。他坐下。我问自己。他喝酒。我觉得不对劲。我决定了。"
	vs := Check(text, Structured{POVPerson: "third"})
	v := findViolation(vs, "pov_person", "third")
	if v == nil {
		t.Fatal("expected pov_person violation")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity=%s, want warning", v.Severity)
	}
	if v.Limit != povFirstPersonLimit {
		t.Errorf("limit=%v, want %d", v.Limit, povFirstPersonLimit)
	}
	if v.Actual != 4 {
		t.Errorf("actual=%v, want 4", v.Actual)
	}
}

func TestCheck_POVDialogueNotCounted(t *testing.T) {
	// 对白内的第一人称不计入叙述段
	text := "他走进客栈。“我早就知道你会来。”他说。“我们的事，改天再谈。”她笑了笑。"
	vs := Check(text, Structured{POVPerson: "third"})
	if len(vs) != 0 {
		t.Errorf("对白内的第一人称不应违规, got %+v", vs)
	}
}

func TestCheck_POVAtLimitPasses(t *testing.T) {
	// 恰好 3 次（阈值）→ 不违规
	text := "他笑。我想。他走。我看。他坐。我等。"
	vs := Check(text, Structured{POVPerson: "third"})
	if len(vs) != 0 {
		t.Errorf("at limit should not violate (limit 3 actual 3), got %+v", vs)
	}
}

func TestCheck_POVPluralNoDoubleCount(t *testing.T) {
	// "我们" 按词根计一次，不复数双计
	text := "我们出发。我们前进。我们胜利。我们回家。"
	vs := Check(text, Structured{POVPerson: "third"})
	v := findViolation(vs, "pov_person", "third")
	if v == nil {
		t.Fatal("expected pov_person violation")
	}
	if v.Actual != 4 {
		t.Errorf("actual=%v, want 4 (词根计数,不复数双计)", v.Actual)
	}
}

func TestCheck_POVNonThirdNoCheck(t *testing.T) {
	text := "我想。我看。我决定。我离开。我沉默。"
	for _, person := range []string{"", "first"} {
		if vs := Check(text, Structured{POVPerson: person}); len(vs) != 0 {
			t.Errorf("person=%q 不应触发 pov_person 检查, got %+v", person, vs)
		}
	}
}

func TestCheck_POVUnclosedQuoteFailOpen(t *testing.T) {
	// 引号未闭合：其后内容按引语处理，宁可漏报不误报
	text := "他说：“我想起来了，我决定，我必须，我要。"
	vs := Check(text, Structured{POVPerson: "third"})
	if len(vs) != 0 {
		t.Errorf("未闭合引号应 fail-open, got %+v", vs)
	}
}

func TestCheck_POVNestedQuotes(t *testing.T) {
	// 嵌套引号内的第一人称同样剥离
	text := "他回忆道：“她当时说『我们一起走吧』，我没应声。”他叹了口气。我想起来了。我觉得。我决定。我必须走。"
	vs := Check(text, Structured{POVPerson: "third"})
	v := findViolation(vs, "pov_person", "third")
	if v == nil {
		t.Fatal("expected pov_person violation")
	}
	if v.Actual != 4 {
		t.Errorf("actual=%v, want 4 (嵌套引号内不计)", v.Actual)
	}
}
