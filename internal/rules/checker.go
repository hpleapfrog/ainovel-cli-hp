package rules

import (
	"strings"
)

// Check 对章节正文按结构化规则进行机械检查，返回违规事实列表。
//
// 设计契约：
//   - 仅返事实，不下指令（铁律一）
//   - 不阻断任何调用方流程
//   - severity 按规则类型固定映射（参见 types.go 注释表）
//
// 参数：
//   - text：章节正文（终稿或草稿都可）
//   - s：合并后的结构化规则；IsEmpty 时直接返回 nil。
func Check(text string, s Structured) []Violation {
	if s.IsEmpty() {
		return nil
	}

	var violations []Violation
	violations = appendForbiddenChars(violations, text, s.ForbiddenChars)
	violations = appendForbiddenPhrases(violations, text, s.ForbiddenPhrases)
	violations = appendFatigueWords(violations, text, s.FatigueWords)
	violations = appendPOVPerson(violations, text, s.POVPerson)
	return violations
}

// forbidden_chars：出现 ≥1 次即 error。
// 同一条规则只产生一条 violation，actual 是出现次数。
func appendForbiddenChars(vs []Violation, text string, list []string) []Violation {
	for _, ch := range list {
		if ch == "" {
			continue
		}
		n := strings.Count(text, ch)
		if n == 0 {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "forbidden_chars",
			Target:   ch,
			Actual:   n,
			Severity: SeverityError,
		})
	}
	return vs
}

// forbidden_phrases：出现 ≥1 次即 error；行为与 forbidden_chars 一致，仅 rule 名区分。
func appendForbiddenPhrases(vs []Violation, text string, list []string) []Violation {
	for _, ph := range list {
		if ph == "" {
			continue
		}
		n := strings.Count(text, ph)
		if n == 0 {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "forbidden_phrases",
			Target:   ph,
			Actual:   n,
			Severity: SeverityError,
		})
	}
	return vs
}

// fatigue_words：本章出现次数超过阈值才违规，warning 级。
// 不跨章累计——跨章问题后续交诊断。
func appendFatigueWords(vs []Violation, text string, m map[string]int) []Violation {
	for word, limit := range m {
		if word == "" || limit <= 0 {
			continue
		}
		n := strings.Count(text, word)
		if n <= limit {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "fatigue_words",
			Target:   word,
			Limit:    limit,
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	return vs
}

// pov_person：第三人称约束，warning 级。
// 剥离成对引号包裹的对白/引语后，叙述段第一人称代词超阈值即违规。
// 仅 "third" 有机械检查（第一人称的反向检查不可机械化，见 types.go 字段注释）。
const povFirstPersonLimit = 3 // 每章叙述段第一人称代词容忍次数（书信/档案/歌词引用等合理使用）

// firstPersonRoots 第一人称词根。"我们/咱们/俺们" 已含词根，按词根计不会重复计数。
// 复合词噪音（自我/忘我/敌我）由阈值吸收，是否成立由 editor 核对原文后裁定。
var firstPersonRoots = []string{"我", "咱", "俺"}

func appendPOVPerson(vs []Violation, text, person string) []Violation {
	if person != "third" {
		return vs
	}
	narration := stripQuotedSpans(text)
	n := 0
	for _, p := range firstPersonRoots {
		n += strings.Count(narration, p)
	}
	if n <= povFirstPersonLimit {
		return vs
	}
	return append(vs, Violation{
		Rule:     "pov_person",
		Target:   "third",
		Limit:    povFirstPersonLimit,
		Actual:   n,
		Severity: SeverityWarning,
	})
}

// stripQuotedSpans 剥掉成对引号（“” 「」 『』 "）包裹的内容，返回剩余叙述文本。
// 支持嵌套；引号未闭合时其后内容全部按引语处理（fail-open：少报而不误报）。
func stripQuotedSpans(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	var stack []rune
	for _, r := range text {
		if len(stack) > 0 {
			// 引语内：匹配栈顶闭引号则出栈，嵌套开引号入栈，其余字符吞掉
			if r == stack[len(stack)-1] {
				stack = stack[:len(stack)-1]
			} else if c, ok := quoteOpeners[r]; ok && r != '"' {
				stack = append(stack, c)
			}
			continue
		}
		if c, ok := quoteOpeners[r]; ok {
			stack = append(stack, c)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var quoteOpeners = map[rune]rune{
	'“': '”',
	'「': '」',
	'『': '』',
	'"': '"',
}
