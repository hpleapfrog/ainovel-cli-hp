package rules

import (
	"strings"
	"testing"
)

func TestLint_CleanText(t *testing.T) {
	if vs := Lint("# 第一章 风起\n他迈步向前。\n夜色渐深。"); len(vs) != 0 {
		t.Errorf("clean text should pass: %+v", vs)
	}
}

func TestLint_MarkdownResidue(t *testing.T) {
	text := "# 第一章\n这是**重点**内容。\n## 小标题\n正文。"
	vs := Lint(text)
	bold := findViolation(vs, "markdown_residue", "**")
	if bold == nil || bold.Actual != 2 {
		t.Errorf("expected ** residue x2: %+v", vs)
	}
	heading := findViolation(vs, "markdown_residue", "#")
	if heading == nil || heading.Actual != 1 {
		t.Errorf("expected 1 heading beyond first line: %+v", vs)
	}
}

func TestLint_NonCJKFragments(t *testing.T) {
	text := "# 第一章\n他发现了一个pattern，这个pattern像DNA一样规律。"
	vs := Lint(text)
	var v *Violation
	for i := range vs {
		if vs[i].Rule == "non_cjk_fragments" {
			v = &vs[i]
			break
		}
	}
	if v == nil {
		t.Fatalf("expected non_cjk violation: %+v", vs)
	}
	if v.Actual != 3 {
		t.Errorf("total count: got %v want 3", v.Actual)
	}
	if !strings.Contains(v.Target, "pattern") || !strings.Contains(v.Target, "DNA") {
		t.Errorf("examples should be distinct: %q", v.Target)
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity: %v", v.Severity)
	}
}

func TestLint_MarkdownResidueMarkers(t *testing.T) {
	text := "# 第一章\n使用`代码`残留。\n- 列表项\n> 引用行\n正文。"
	vs := Lint(text)
	bt := findViolation(vs, "markdown_residue", "`")
	if bt == nil || bt.Actual != 2 {
		t.Errorf("expected backtick residue x2: %+v", vs)
	}
	lm := findViolation(vs, "markdown_residue", "行首列表/引用标记")
	if lm == nil || lm.Actual != 2 {
		t.Errorf("expected 2 line-start markers: %+v", vs)
	}
}

func TestLint_HalfwidthPunctuation(t *testing.T) {
	// 昏黄, / 停下; / 回头! 三处；v9.7.3 的半角点号在数字语境不误伤
	text := "# 第一章\n灯光昏黄,照不清大堂。他停下;她回头!\n版本号 v9.7.3 不算。"
	vs := Lint(text)
	v := findViolation(vs, "halfwidth_punctuation", "黄,、下;、头!")
	if v == nil {
		t.Fatalf("expected halfwidth_punctuation violation: %+v", vs)
	}
	if v.Actual != 3 {
		t.Errorf("count: got %v want 3", v.Actual)
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity: %v", v.Severity)
	}
}

func TestLint_HalfwidthPunctuationClean(t *testing.T) {
	text := "# 第一章\n灯光昏黄，照不清大堂。他停下；她回头！\n他说：“好。”"
	if vs := Lint(text); len(vs) != 0 {
		t.Errorf("full-width punctuation should pass: %+v", vs)
	}
}

func TestLint_ParagraphBreak(t *testing.T) {
	// 看/见、灯/火、熄/灭 三处断行（句未收尾）
	text := "# 第一章\n他抬头看\n见远处的灯\n火慢慢熄\n灭。\n正常段落。"
	vs := Lint(text)
	v := findViolation(vs, "paragraph_break", "…他抬头看")
	if v == nil {
		t.Fatalf("expected paragraph_break violation: %+v", vs)
	}
	if v.Actual != 3 {
		t.Errorf("count: got %v want 3", v.Actual)
	}
}

func TestLint_ParagraphBreakBelowThreshold(t *testing.T) {
	// 仅 1 处断行，不到 3 处不报（诗歌/对话碎片容忍）
	text := "# 第一章\n他抬头看\n见远处的灯火。\n夜色渐深。\n风停了。"
	if vs := Lint(text); len(vs) != 0 {
		t.Errorf("single break should not be reported: %+v", vs)
	}
}

func TestLint_ParagraphBreakSkipsStructuralLines(t *testing.T) {
	// 系统工单/标签-值条目行天然无句读收尾，不算段中换行（ch01 实证误报场景）。
	// 注意 "CN" 会被 non_cjk_fragments 正常命中，这里只断言 paragraph_break 不报。
	text := "# 第一章 雨夜的回滚\n【异常事故工单 CN-7-44109】\n类型：未授权术式执行／校验和失败（疑似回滚）\n地址：棠溪村东四巷17号602\n1F】照明回路·已登记／安防总线·已登记\n他站在门口。\n雨还在下。"
	vs := Lint(text)
	for i := range vs {
		if vs[i].Rule == "paragraph_break" {
			t.Fatalf("structural lines should not be reported as paragraph_break: %+v", vs[i])
		}
	}
}
