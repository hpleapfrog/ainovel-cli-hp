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
// 当前十一类（全部来自真实长跑产物的实证缺陷 + Bishu 写手约束中可机械化的子集）：
//   - markdown_residue：正文残留 markdown 记号（** 加粗、反引号、行首列表/引用标记、
//     首行之外的 # 标题行）——导出 txt 会裸露符号
//   - non_cjk_fragments：连续拉丁字母片段（模型语言混杂，如中文正文裸混 "pattern"）
//   - halfwidth_punctuation：中文字符后紧跟半角标点（,;:!?）——全角被写成半角
//   - straight_quotes：ASCII 直引号（" '）——中文对白应使用弯引号“”‘’
//   - unbalanced_quotes：弯引号开闭计数不等——引号未闭合或配对错乱，对白边界丢失
//   - paragraph_break：疑似段中换行（句未收尾即断行，≥3 处才报）——长段被拦腰折断
//   - dash_abuse：全角破折号「——」高频出现（>8 处/章）——破折号是模型最典型的
//     AI 味标点之一（Bishu 全禁）；少数用作剧情转折/语气停顿合法，成规模即病
//   - soft_filler：「了一下」高频出现（>6 处/章）——动词弱化填充，把具体动作
//     泛化为"做了那么一下"
//   - ai_marker_words：高频 AI 标记词（仿佛/忽然/竟然/猛地/猛然/不禁/宛如）——
//     Bishu 口径"每三千字最多一次"，这里放宽为每 3000 字 2 次
//   - cliche_micro_expressions：俗套微表情/身体反应（嘴角勾起/瞳孔一缩/倒吸一口凉气等）——
//     单章合计 >3 处即成模型腔
//   - narrator_intrusion：叙事者闯入（元叙事/编剧旁白/群像反应/段尾升华的固定句式）——
//     出现即 warning
func Lint(text string) []Violation {
	var vs []Violation
	vs = appendMarkdownResidue(vs, text)
	vs = appendNonCJKFragments(vs, text)
	vs = appendHalfwidthPunctuation(vs, text)
	vs = appendStraightQuotes(vs, text)
	vs = appendUnbalancedQuotes(vs, text)
	vs = appendParagraphBreaks(vs, text)
	vs = appendDashAbuse(vs, text)
	vs = appendSoftFiller(vs, text)
	vs = appendAIMarkerWords(vs, text)
	vs = appendClicheMicroExpressions(vs, text)
	vs = appendNarratorIntrusion(vs, text)
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

// dashAbuseLimit 单章全角破折号「——」的容忍次数。少数剧情转折/语气停顿合法；
// 成规模出现是模型最典型的 AI 味标点（Bishu 侧全禁，这里 warning 级事实交评审裁定）。
const dashAbuseLimit = 8

func appendDashAbuse(vs []Violation, text string) []Violation {
	n := strings.Count(text, "——")
	if n <= dashAbuseLimit {
		return vs
	}
	return append(vs, Violation{
		Rule:     "dash_abuse",
		Target:   "——",
		Limit:    dashAbuseLimit,
		Actual:   n,
		Severity: SeverityWarning,
	})
}

// softFillerLimit 「了一下」单章容忍次数：动词弱化填充，把具体动作泛化为"做了那么一下"。
const softFillerLimit = 6

func appendSoftFiller(vs []Violation, text string) []Violation {
	n := strings.Count(text, "了一下")
	if n <= softFillerLimit {
		return vs
	}
	return append(vs, Violation{
		Rule:     "soft_filler",
		Target:   "了一下",
		Limit:    softFillerLimit,
		Actual:   n,
		Severity: SeverityWarning,
	})
}

// ── AI 标记词（Bishu 写手约束 #13）──

// aiMarkerWords 高频 AI 标记词：不是绝对禁用，但成规模出现是模型腔。
// Bishu 口径"每三千字最多一次"，这里放宽为每 3000 字 2 次（合计）。
var aiMarkerWords = []string{"仿佛", "忽然", "竟然", "猛地", "猛然", "不禁", "宛如"}

func appendAIMarkerWords(vs []Violation, text string) []Violation {
	count := 0
	var hit []string
	for _, w := range aiMarkerWords {
		if n := strings.Count(text, w); n > 0 {
			count += n
			hit = append(hit, fmt.Sprintf("%s×%d", w, n))
		}
	}
	if count == 0 {
		return vs
	}
	limit := 2 * max(1, len([]rune(text))/3000)
	if count <= limit {
		return vs
	}
	return append(vs, Violation{
		Rule:     "ai_marker_words",
		Target:   strings.Join(hit, "、"),
		Limit:    limit,
		Actual:   count,
		Severity: SeverityWarning,
	})
}

// ── 俗套微表情（Bishu 写手约束 #14）──

// clicheMicroExpressions 俗套微表情/身体反应清单：单章合计超阈值即模型腔。
var clicheMicroExpressions = []string{
	"嘴角勾起", "嘴角上扬", "眼里闪过一丝", "眸色一沉", "眼神暗了暗", "眉头微皱",
	"呼吸一滞", "倒吸一口凉气", "喉结微滚", "浑身一震", "身子一僵", "指尖泛白",
}

// clicheMicroLimit 单章合计容忍次数。
const clicheMicroLimit = 3

func appendClicheMicroExpressions(vs []Violation, text string) []Violation {
	count := 0
	var hit []string
	for _, w := range clicheMicroExpressions {
		if n := strings.Count(text, w); n > 0 {
			count += n
			hit = append(hit, fmt.Sprintf("%s×%d", w, n))
		}
	}
	if count <= clicheMicroLimit {
		return vs
	}
	return append(vs, Violation{
		Rule:     "cliche_micro_expressions",
		Target:   strings.Join(hit, "、"),
		Limit:    clicheMicroLimit,
		Actual:   count,
		Severity: SeverityWarning,
	})
}

// ── 叙事者闯入（Bishu 写手约束 #10/#11/#12）──

// narratorIntrusions 元叙事/编剧旁白/群像反应/段尾升华的固定句式清单：出现即违规。
var narratorIntrusions = []string{
	"故事发展到这一步", "读者也许会想", "接下来发生的事", "一切才刚刚开始",
	"有些事情永远改变", "再也回不去", "那天晚上改变所有人的命运",
	"全场震惊", "所有人都惊呆了", "众人倒吸一口凉气", "所有人心头一紧",
	"众人面面相觑",
}

func appendNarratorIntrusion(vs []Violation, text string) []Violation {
	var hit []string
	for _, w := range narratorIntrusions {
		if n := strings.Count(text, w); n > 0 {
			hit = append(hit, fmt.Sprintf("%s×%d", w, n))
		}
	}
	if len(hit) == 0 {
		return vs
	}
	return append(vs, Violation{
		Rule:     "narrator_intrusion",
		Target:   strings.Join(hit, "、"),
		Actual:   len(hit),
		Severity: SeverityWarning,
	})
}
