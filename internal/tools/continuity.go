package tools

import (
	"fmt"
	"slices"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// 事实类型（ContinuityIssues / StateRegression / RelationshipJump /
// UnreportedCharacter / ContinuityIssueSeverity）在 internal/domain/continuity.go——
// store 要持久化、novel_context 要注入，检测器只负责算。

// ── 1. 状态回退检测 ──

var regressionFields = []string{"status", "realm", "power", "rank"}

func detectStateRegression(allChanges []domain.StateChange, incoming []domain.StateChange) []domain.StateRegression {
	if len(incoming) == 0 || len(allChanges) == 0 {
		return nil
	}
	latest := make(map[string]map[string]domain.StateChange)
	prev := make(map[string]map[string]domain.StateChange)
	for _, c := range allChanges {
		if latest[c.Entity] == nil {
			latest[c.Entity] = make(map[string]domain.StateChange)
			prev[c.Entity] = make(map[string]domain.StateChange)
		}
		if cur, ok := latest[c.Entity][c.Field]; ok {
			prev[c.Entity][c.Field] = cur
		}
		latest[c.Entity][c.Field] = c
	}

	var result []domain.StateRegression
	for _, ic := range incoming {
		if !slices.Contains(regressionFields, ic.Field) {
			continue
		}
		currState, hasCurr := latest[ic.Entity][ic.Field]
		if !hasCurr {
			continue
		}
		prevState, hasPrev := prev[ic.Entity][ic.Field]

		// 硬冲突：前态已死亡，新态不可能有效
		if ic.Field == "status" && isDeadState(currState.NewValue) {
			result = append(result, domain.StateRegression{
				Entity: ic.Entity, Field: ic.Field,
				Curr: currState.NewValue, Next: ic.NewValue,
				Severity: domain.SeverityError,
			})
			continue
		}

		// 回退检测：新值等于前值但不等于当前值
		if hasPrev && ic.NewValue == prevState.NewValue && ic.NewValue != currState.NewValue {
			result = append(result, domain.StateRegression{
				Entity: ic.Entity, Field: ic.Field,
				Prev: prevState.NewValue, Curr: currState.NewValue, Next: ic.NewValue,
				Severity: domain.SeverityWarning,
			})
		}
	}
	return result
}

func isDeadState(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "死亡") || strings.Contains(s, "dead") ||
		strings.Contains(s, "陨落") || strings.Contains(s, "牺牲") ||
		strings.Contains(s, "毙命") || strings.Contains(s, "逝去")
}

// ── 2. 关系跳跃检测 ──

var relationLevels = map[string]int{
	"仇人": -3, "死敌": -3, "不共戴天": -3,
	"敌人": -2, "对手": -2, "宿敌": -2,
	"疏远": -1, "冷淡": -1, "不和": -1, "芥蒂": -1,
	"陌生人": 0, "路人": 0, "萍水相逢": 0,
	"认识": 1, "相识": 1, "点头之交": 1,
	"朋友": 2, "同伴": 2, "盟友": 2, "师徒": 2, "主仆": 2, "同门": 2, "搭档": 2,
	"恋人": 3, "挚友": 3, "夫妻": 3, "生死之交": 3, "道侣": 3, "结义": 3,
}

func detectRelationshipJump(allRelations []domain.RelationshipEntry, incoming []domain.RelationshipEntry) []domain.RelationshipJump {
	if len(incoming) == 0 {
		return nil
	}
	lastByPair := make(map[string]domain.RelationshipEntry)
	lastChapterByPair := make(map[string]int)
	for _, r := range allRelations {
		key := relationKey(r.CharacterA, r.CharacterB)
		lastByPair[key] = r
		lastChapterByPair[key] = r.Chapter
	}

	var result []domain.RelationshipJump
	for _, inc := range incoming {
		key := relationKey(inc.CharacterA, inc.CharacterB)
		last, exists := lastByPair[key]
		if !exists {
			continue
		}
		newLevel := classifyRelation(inc.Relation)
		oldLevel := classifyRelation(last.Relation)
		if newLevel == 0 && oldLevel == 0 {
			continue
		}
		diff := newLevel - oldLevel
		gap := inc.Chapter - lastChapterByPair[key]

		if abs(diff) >= 3 {
			result = append(result, domain.RelationshipJump{
				A: inc.CharacterA, B: inc.CharacterB,
				Prev: last.Relation, Next: inc.Relation,
				Gap:      gap,
				Severity: jumpSeverity(diff, gap),
			})
		} else if diff > 0 && oldLevel < 0 && gap < 3 {
			result = append(result, domain.RelationshipJump{
				A: inc.CharacterA, B: inc.CharacterB,
				Prev: last.Relation, Next: inc.Relation,
				Gap:      gap,
				Severity: domain.SeverityInfo,
			})
		} else if diff < 0 && oldLevel > 0 && gap < 3 {
			result = append(result, domain.RelationshipJump{
				A: inc.CharacterA, B: inc.CharacterB,
				Prev: last.Relation, Next: inc.Relation,
				Gap:      gap,
				Severity: domain.SeverityInfo,
			})
		}
	}
	return result
}

func relationKey(a, b string) string {
	if a < b {
		return a + "|||" + b
	}
	return b + "|||" + a
}

func classifyRelation(rel string) int {
	rel = strings.TrimSpace(rel)
	for keyword, level := range relationLevels {
		if strings.Contains(rel, keyword) {
			return level
		}
	}
	// 兜底：正向词 vs 负向词
	positive := []string{"好", "友", "亲", "爱", "恋", "恩", "盟", "信任", "帮助"}
	negative := []string{"敌", "仇", "恨", "厌", "恶", "疏", "冷", "威胁", "背叛"}
	for _, w := range positive {
		if strings.Contains(rel, w) {
			return 2
		}
	}
	for _, w := range negative {
		if strings.Contains(rel, w) {
			return -2
		}
	}
	return 0
}

func jumpSeverity(diff, gap int) domain.ContinuityIssueSeverity {
	absDiff := abs(diff)
	if absDiff >= 4 {
		return domain.SeverityError
	}
	if absDiff >= 3 && gap <= 1 {
		return domain.SeverityError
	}
	if absDiff >= 3 {
		return domain.SeverityWarning
	}
	return domain.SeverityInfo
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ── 3. 出场申报核对 ──

// detectUnreportedCharacters 机械核对 commit 自报的出场名单与正文实体：
// 已知角色（characters.json ∪ 配角名册，含别名）在正文出现 ≥2 次，
// 但其正式名不在 characters / cast_intros 中 → 记一条漏报事实。
// 漏报会让摘要、名册召回、GhostCharacter 等下游事实链静默缺失该角色。
// 子串匹配天然有误伤（回忆/转述提及也算"出场"），故只返事实供 editor 裁定，不阻断。
func detectUnreportedCharacters(content string, reported []string, chars []domain.Character, cast []domain.CastEntry) []domain.UnreportedCharacter {
	if content == "" {
		return nil
	}
	reportedSet := make(map[string]bool, len(reported))
	for _, r := range reported {
		reportedSet[r] = true
	}
	type known struct {
		canonical string
		names     []string
	}
	var knowns []known
	for _, c := range chars {
		names := append([]string{c.Name}, c.Aliases...)
		knowns = append(knowns, known{c.Name, names})
	}
	for _, e := range cast {
		names := append([]string{e.Name}, e.Aliases...)
		knowns = append(knowns, known{e.Name, names})
	}

	var out []domain.UnreportedCharacter
	for _, k := range knowns {
		if k.canonical == "" || reportedSet[k.canonical] {
			continue
		}
		mentions := 0
		for _, n := range k.names {
			if len([]rune(n)) < 2 {
				continue // 单字名/别名子串误伤率太高，不匹配
			}
			mentions += strings.Count(content, n)
		}
		if mentions < 2 {
			continue
		}
		sev := domain.SeverityInfo
		if mentions >= 3 {
			sev = domain.SeverityWarning
		}
		out = append(out, domain.UnreportedCharacter{Name: k.canonical, Mentions: mentions, Severity: sev})
	}
	return out
}

// ── 4. 写前规划校验 ──

type PlanWarning struct {
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

func checkPlanContinuity(st *store.Store, plan domain.ChapterPlan) []PlanWarning {
	if st == nil {
		return nil
	}
	var warnings []PlanWarning
	chapter := plan.Chapter

	progress, _ := st.Progress.Load()
	allChanges, _ := st.World.LoadStateChanges()
	chars, _ := st.Characters.Load()

	// 角色约束检测
	nameSet := make(map[string]bool)
	for _, c := range chars {
		nameSet[c.Name] = true
		for _, alias := range c.Aliases {
			nameSet[alias] = true
		}
	}
	planText := plan.Goal + " " + plan.Conflict + " " + plan.Hook + " " + plan.Notes
	for _, beat := range plan.Contract.RequiredBeats {
		planText += " " + beat
	}

	// 检查计划中涉及的每个角色是否有 hard constraint
	if len(allChanges) > 0 {
		latest := deriveCharacterState(allChanges)
		for _, cs := range latest {
			if !nameSet[cs.name] && !strings.Contains(planText, cs.name) {
				continue
			}
			for _, s := range cs.summary {
				if isHardConstraint(cs) {
					warnings = append(warnings, PlanWarning{
						Rule:     "character_constraint",
						Message:  fmt.Sprintf("角色 %s 当前状态为：%s，请在计划中明确如何对待此角色", cs.name, s),
						Severity: "warning",
					})
				}
			}
		}
	}

	// 大纲约束：plan 是否包含 forbidden_moves
	for _, forbidden := range plan.Contract.ForbiddenMoves {
		if strings.Contains(planText, forbidden) || strings.Contains(forbidden, planText) {
			warnings = append(warnings, PlanWarning{
				Rule:     "contract_violation",
				Message:  fmt.Sprintf("计划疑似违反合约禁止项：%s", forbidden),
				Severity: "warning",
			})
		}
	}

	// 时间约束
	if progress != nil && chapter > 1 {
		timeline, _ := st.World.LoadRecentTimeline(chapter, 2)
		if len(timeline) > 0 {
			lastTime := timeline[len(timeline)-1].Time
			hasTransition := strings.Contains(planText, "后") || strings.Contains(planText, "翌日") ||
				strings.Contains(planText, "次日") || strings.Contains(planText, "天") ||
				strings.Contains(planText, "月") || strings.Contains(planText, "年") ||
				strings.Contains(planText, "之前") || strings.Contains(planText, "之后")
			if !hasTransition {
				warnings = append(warnings, PlanWarning{
					Rule:     "timeline_gap",
					Message:  fmt.Sprintf("上一章时间点为「%s」，但本章计划中未提及时间过渡。如需跨越时间请明确描述，如无需跨越请确保时间连续", lastTime),
					Severity: "info",
				})
			}
		}
	}

	// 活跃伏笔提醒
	if progress != nil && progress.TotalChapters > 10 {
		active, _ := st.World.LoadActiveForeshadow()
		if len(active) > 0 {
			touched := 0
			for _, f := range active {
				if strings.Contains(planText, f.ID) || strings.Contains(planText, f.Description) {
					touched++
				}
			}
			if touched == 0 && len(active) >= 5 {
				warnings = append(warnings, PlanWarning{
					Rule:     "foreshadow_neglect",
					Message:  fmt.Sprintf("当前有 %d 条活跃伏笔未回收，但本章计划未涉及任一伏笔。请确认是否有机会推进或回收", len(active)),
					Severity: "info",
				})
			}
		}
	}

	return warnings
}
