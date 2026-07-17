package diag

import (
	"fmt"
	"sort"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// GhostCharacter 检测 core/important 角色长期未出现。
func GhostCharacter(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Characters) == 0 || len(snap.Summaries) == 0 {
		return nil
	}
	completed := snap.CompletedCount()
	if completed < 5 {
		return nil
	}

	// 计算每个角色最后出现的章节号
	lastSeen := make(map[string]int)
	for ch, s := range snap.Summaries {
		for _, name := range s.Characters {
			if ch > lastSeen[name] {
				lastSeen[name] = ch
			}
		}
	}

	threshold := completed / 3
	if threshold < 5 {
		threshold = 5
	}
	latest := snap.LatestCompleted()

	var ghosts []string
	for _, c := range snap.Characters {
		if c.Tier != "core" && c.Tier != "important" {
			continue
		}
		seen, ok := lastSeen[c.Name]
		if !ok {
			// 也检查别名
			for _, alias := range c.Aliases {
				if s, exists := lastSeen[alias]; exists && s > seen {
					seen = s
					ok = true
				}
			}
		}
		gap := latest - seen
		if !ok {
			ghosts = append(ghosts, fmt.Sprintf("%s(从未出现在摘要中)", c.Name))
		} else if gap > threshold {
			ghosts = append(ghosts, fmt.Sprintf("%s(最后出现ch%d,已缺席%d章)", c.Name, seen, gap))
		}
	}
	if len(ghosts) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "GhostCharacter",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.characters",
		Title:      fmt.Sprintf("角色消失: %d 个核心角色长期缺席", len(ghosts)),
		Evidence:   strings.Join(ghosts, "; "),
		Suggestion: "Writer 可能丢失了该角色的追踪。考虑直接在输入框提交干预指令重新引入该角色，或在 characters.json 中降级其 tier。",
	}}
}

// TimelineGaps 检测已完成章节缺少时间线事件。
func TimelineGaps(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.CompletedChapters) == 0 {
		return nil
	}
	if len(snap.Timeline) == 0 && snap.CompletedCount() > 0 {
		return []Finding{{
			Rule:       "TimelineGaps",
			Category:   CatContext,
			Severity:   SevInfo,
			Confidence: ConfMedium,
			AutoLevel:  AutoNone,
			Target:     "context.timeline",
			Title:      "时间线为空",
			Evidence:   fmt.Sprintf("completed=%d, timeline_events=0", snap.CompletedCount()),
			Suggestion: "commit_chapter 的时间线提取可能未生效。检查 Writer 输出是否包含 timeline 字段。",
		}}
	}

	// 建立章节→事件映射
	chaptersWithEvents := make(map[int]bool)
	for _, e := range snap.Timeline {
		chaptersWithEvents[e.Chapter] = true
	}

	var missing []int
	for _, ch := range snap.Progress.CompletedChapters {
		if !chaptersWithEvents[ch] {
			missing = append(missing, ch)
		}
	}
	// 允许少量缺失（某些过渡章可能确实无重大事件）
	if len(missing) == 0 || float64(len(missing))/float64(snap.CompletedCount()) < ThresholdTimelineGapRate {
		return nil
	}
	return []Finding{{
		Rule:       "TimelineGaps",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.timeline",
		Title:      fmt.Sprintf("时间线缺口: %d 章无事件记录", len(missing)),
		Evidence:   fmt.Sprintf("missing=[%s]", intsToStr(missing)),
		Suggestion: "commit_chapter 的时间线提取可能部分失效。检查 Writer 输出的 timeline 字段格式。",
	}}
}

// RelationshipStagnation 检测关系数据停止更新。
func RelationshipStagnation(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Relationships) == 0 {
		return nil
	}
	completed := snap.CompletedCount()
	if completed < 6 {
		return nil
	}

	// 找到关系数据的最新章节
	latestRelCh := 0
	for _, r := range snap.Relationships {
		if r.Chapter > latestRelCh {
			latestRelCh = r.Chapter
		}
	}

	// 如果最新关系数据在前 1/3，判定为停滞
	cutoff := snap.LatestCompleted() - completed/3
	if latestRelCh >= cutoff {
		return nil
	}
	return []Finding{{
		Rule:       "RelationshipStagnation",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfLow,
		AutoLevel:  AutoNone,
		Target:     "context.relationships",
		Title:      fmt.Sprintf("关系数据停滞: 最新更新在第 %d 章", latestRelCh),
		Evidence:   fmt.Sprintf("relationship_entries=%d, latest_update=ch%d, latest_completed=ch%d", len(snap.Relationships), latestRelCh, snap.LatestCompleted()),
		Suggestion: "commit_chapter 的关系更新可能停止工作，或故事关系确实无变化。检查 Writer 输出的 relationships 字段。",
	}}
}

// ContinuityAlerts 聚合 commit 落盘的连续性机械检测记录（meta/continuity_issues.jsonl）：
// ① 已提交章节仍存在 error 级状态回退（如已死亡角色出现新状态）——硬矛盾已出厂；
// ② 已提交章节存在 error 级关系越级跳变；
// ③ 出场漏报跨多章反复出现——摘要/名册事实链在系统性缺角色。
// 证据全部来自落盘记录本身；重写过且复测合格的章节记录已被 latest-wins 覆盖，不会误报。
func ContinuityAlerts(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.ContinuityIssues) == 0 {
		return nil
	}
	completed := make(map[int]bool, snap.CompletedCount())
	for _, ch := range snap.Progress.CompletedChapters {
		completed[ch] = true
	}
	chapters := make([]int, 0, len(snap.ContinuityIssues))
	for ch := range snap.ContinuityIssues {
		chapters = append(chapters, ch)
	}
	sort.Ints(chapters)

	var stateErrs, jumpErrs []string
	unreportedByName := make(map[string][]int)
	for _, ch := range chapters {
		issues := snap.ContinuityIssues[ch]
		if issues == nil || !completed[ch] {
			continue
		}
		for _, r := range issues.StateRegressions {
			if r.Severity == domain.SeverityError {
				stateErrs = append(stateErrs, fmt.Sprintf("ch%d: %s %s %s→%s", ch, r.Entity, r.Field, r.Curr, r.Next))
			}
		}
		for _, j := range issues.RelationshipJumps {
			if j.Severity == domain.SeverityError {
				jumpErrs = append(jumpErrs, fmt.Sprintf("ch%d: %s-%s %s→%s", ch, j.A, j.B, j.Prev, j.Next))
			}
		}
		for _, u := range issues.UnreportedCharacters {
			unreportedByName[u.Name] = append(unreportedByName[u.Name], ch)
		}
	}

	var findings []Finding
	if len(stateErrs) > 0 {
		findings = append(findings, Finding{
			Rule:       "ContinuityAlerts",
			Category:   CatContext,
			Severity:   SevCritical,
			Confidence: ConfHigh,
			AutoLevel:  AutoSuggest,
			Target:     "meta/continuity_issues.jsonl",
			Title:      fmt.Sprintf("已提交章节存在 %d 处硬性状态矛盾", len(stateErrs)),
			Evidence:   strings.Join(stateErrs, "; "),
			Suggestion: "已死亡/离场的角色出现新状态，属硬矛盾。在输入框提交干预指令把相关章节加入返工队列重写；若“复活”本就是剧情设定则属误报，请在 .ainovel/rules/ 用自然语言说明，后续评审会并读该偏好。",
		})
	}
	if len(jumpErrs) > 0 {
		findings = append(findings, Finding{
			Rule:       "ContinuityAlerts",
			Category:   CatContext,
			Severity:   SevWarning,
			Confidence: ConfMedium,
			AutoLevel:  AutoNone,
			Target:     "meta/continuity_issues.jsonl",
			Title:      fmt.Sprintf("已提交章节存在 %d 处关系越级跳变", len(jumpErrs)),
			Evidence:   strings.Join(jumpErrs, "; "),
			Suggestion: "关系等级跨度过大（如仇人骤变恋人）。若中间过程被压缩，用干预指令要求补过渡章或重写相关章；词表分级误伤由 editor 核对原文裁定。",
		})
	}

	// 出场漏报只在慢性化时报：同一角色 ≥ThresholdUnreportedChronic 章未申报。
	// 单章偶发由 editor 当场处理，不进 diag。
	var chronic []string
	for name, chs := range unreportedByName {
		if len(chs) >= ThresholdUnreportedChronic {
			chronic = append(chronic, fmt.Sprintf("%s(%s)", name, intsToStr(chs)))
		}
	}
	sort.Strings(chronic)
	if len(chronic) > 0 {
		findings = append(findings, Finding{
			Rule:       "ContinuityAlerts",
			Category:   CatContext,
			Severity:   SevWarning,
			Confidence: ConfMedium,
			AutoLevel:  AutoNone,
			Target:     "prompt.writer",
			Title:      fmt.Sprintf("出场漏报慢性化: %d 个角色 ≥%d 章未申报", len(chronic), ThresholdUnreportedChronic),
			Evidence:   strings.Join(chronic, "; "),
			Suggestion: "commit_chapter 的 characters / cast_intros 填报执行不严，摘要与配角名册会持续缺这些角色（GhostCharacter 与召回随之失真）。检查 writer.md commit 参数段的执行，或用干预指令要求 editor 复核受影响章。",
		})
	}
	return findings
}
