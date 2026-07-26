package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Lint 内置产品底线检查：扫描正文中的机制残留，与用户规则无关，commit 时始终执行。
// 与 Check 同契约——仅返事实（铁律一），不阻断流程，由评审/用户裁定。
//
// 当前六类（全部来自真实长跑产物的实证缺陷）：
//   - markdown_residue：正文残留 markdown 记号（** 加粗、反引号、行首列表/引用标记、
//     首行之外的 # 标题行）——导出 txt 会裸露符号
//   - non_cjk_fragments：连续拉丁字母片段（模型语言混杂，如中文正文裸混 "pattern"）
//   - halfwidth_punctuation：中文字符后紧跟半角标点（,;:!?）——全角被写成半角
//   - straight_quotes：ASCII 直引号（" '）——中文对白应使用弯引号“”‘’
//   - unbalanced_quotes：弯引号开闭计数不等——引号未闭合或配对错乱，对白边界丢失
//   - paragraph_break：疑似段中换行（句未收尾即断行，≥3 处才报）——长段被拦腰折断
func Lint(text string) []Violation {
	var vs []Violation
	vs = appendMarkdownResidue(vs, text)
	vs = appendNonCJKFragments(vs, text)
	vs = appendHalfwidthPunctuation(vs, text)
	vs = appendStraightQuotes(vs, text)
	vs = appendUnbalancedQuotes(vs, text)
	vs = appendParagraphBreaks(vs, text)
	return vs
}

func appendMarkdownResidue(vs []Violation, text string) []Violation {
	if n := strings.Count(text, "**"); n > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "**",
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	if n := strings.Count(text, "`"); n > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "`",
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	headings, markers := 0, 0
	seenContent := false
	for line := range strings.SplitSeq(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// 第一个非空行的 # 标题是章文件的合法格式（不按行号写死，容忍前导空行）
		first := !seenContent
		seenContent = true
		if !first && strings.HasPrefix(t, "#") {
			headings++
		}
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "> ") {
			markers++
		}
	}
	if headings > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "#",
			Actual:   headings,
			Severity: SeverityWarning,
		})
	}
	if markers > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "行首列表/引用标记",
			Actual:   markers,
			Severity: SeverityWarning,
		})
	}
	return vs
}

var latinFragmentRe = regexp.MustCompile(`[A-Za-z]{2,}`)

// appendNonCJKFragments 报告拉丁字母片段的总次数与去重示例。
// 现代题材的合法英文（品牌名/缩写）也会命中——warning 级事实，由评审按题材裁定。
func appendNonCJKFragments(vs []Violation, text string) []Violation {
	matches := latinFragmentRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return vs
	}
	seen := make(map[string]struct{})
	var examples []string
	for _, m := range matches {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		if len(examples) < 3 {
			examples = append(examples, m)
		}
	}
	return append(vs, Violation{
		Rule:     "non_cjk_fragments",
		Target:   strings.Join(examples, "、"),
		Actual:   len(matches),
		Severity: SeverityWarning,
	})
}

// 中文字符后紧跟半角标点：全角标点被写成半角是模型高发 typography 缺陷。
// 只匹配 CJK 之后的 ,;:!?——英文/数字语境的半角符号（版本号 v9.7.3、日志名）不误伤。
var halfwidthPunctRe = regexp.MustCompile(`[\x{4e00}-\x{9fff}][,;:!?]`)

func appendHalfwidthPunctuation(vs []Violation, text string) []Violation {
	matches := halfwidthPunctRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return vs
	}
	seen := make(map[string]struct{})
	var examples []string
	for _, m := range matches {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		if len(examples) < 3 {
			examples = append(examples, m)
		}
	}
	return append(vs, Violation{
		Rule:     "halfwidth_punctuation",
		Target:   strings.Join(examples, "、"),
		Actual:   len(matches),
		Severity: SeverityWarning,
	})
}

// appendStraightQuotes 报告 ASCII 直引号：中文正文的对白/引语应使用弯引号“”‘’，
// 直引号是模型高发 typography 缺陷。系统流/无限流题材的合法半角文本也会命中——
// warning 级事实，由评审按题材裁定。
func appendStraightQuotes(vs []Violation, text string) []Violation {
	dq := strings.Count(text, `"`)
	sq := strings.Count(text, `'`)
	if dq == 0 && sq == 0 {
		return vs
	}
	return append(vs, Violation{
		Rule:     "straight_quotes",
		Target:   fmt.Sprintf(`" ×%d，' ×%d`, dq, sq),
		Actual:   dq + sq,
		Severity: SeverityWarning,
	})
}

// appendUnbalancedQuotes 报告弯引号开闭计数不等：引号未闭合或配对错乱时，
// 对白边界丢失，读者无法分辨哪句是说的、哪句是叙述。
func appendUnbalancedQuotes(vs []Violation, text string) []Violation {
	for _, p := range [][2]string{{"“", "”"}, {"‘", "’"}} {
		open, close := strings.Count(text, p[0]), strings.Count(text, p[1])
		if open == close {
			continue
		}
		diff := open - close
		if diff < 0 {
			diff = -diff
		}
		vs = append(vs, Violation{
			Rule:     "unbalanced_quotes",
			Target:   fmt.Sprintf("%s…%s（开 %d / 闭 %d）", p[0], p[1], open, close),
			Actual:   diff,
			Severity: SeverityWarning,
		})
	}
	return vs
}

// 断行合法收尾符：行以这些字符结尾视为完整句/条目，其后的换行不算段中折断
// （含工单/系统文本的 】、括号闭合 ）、省略 ……、破折 ——）。
const paragraphEnders = "。！？”’」』…：；】—）"

// paragraphBreakMinReports 段中换行的最小报告数：个位数断行可能是诗歌/对话碎片
// 等合法排版，成规模才说明模型在拦腰折断长段。
const paragraphBreakMinReports = 3

// isStructuralLine 识别非散文的结构行（系统工单/楼层图/标签-值条目），
// 这类条目行天然不以句读收尾，不应计入段中换行。
func isStructuralLine(s string) bool {
	if strings.HasPrefix(s, "【") || strings.HasSuffix(s, "】") {
		return true
	}
	r := []rune(s)
	head := r
	if len(head) > 12 {
		head = head[:12]
	}
	if strings.ContainsRune(string(head), '：') {
		return true // 标签-值行（类型：… 地址：…）
	}
	if len(r) >= 6 && strings.ContainsRune(string(r[:6]), '】') {
		return true // 楼层图类（1F】…）
	}
	return false
}

func appendParagraphBreaks(vs []Violation, text string) []Violation {
	lines := strings.Split(text, "\n")
	breaks := 0
	example := ""
	for i, line := range lines[:len(lines)-1] {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") || strings.TrimSpace(lines[i+1]) == "" {
			continue
		}
		if isStructuralLine(s) {
			continue
		}
		r, _ := utf8.DecodeLastRuneInString(s)
		if !strings.ContainsRune(paragraphEnders, r) {
			breaks++
			if example == "" {
				example = "…" + tailRunes(s, 12)
			}
		}
	}
	if breaks < paragraphBreakMinReports {
		return vs
	}
	return append(vs, Violation{
		Rule:     "paragraph_break",
		Target:   example,
		Actual:   breaks,
		Severity: SeverityWarning,
	})
}

// tailRunes 取末尾 n 个 rune（违规示例用，不足则全取）。
func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
