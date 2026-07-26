package domain

// ── 连续性机械检测的事实类型 ──
//
// 检测器在 internal/tools/continuity.go，事实类型放 domain：
// store 需要持久化它们（meta/continuity_issues.jsonl），
// novel_context 需要把它们注入 editor 的评审上下文。

type ContinuityIssueSeverity string

const (
	SeverityInfo    ContinuityIssueSeverity = "info"
	SeverityWarning ContinuityIssueSeverity = "warning"
	SeverityError   ContinuityIssueSeverity = "error"
)

// StateRegression 角色/实体状态回退（如境界跌落、已死亡角色出现新状态）。
type StateRegression struct {
	Entity   string                  `json:"entity"`
	Field    string                  `json:"field"`
	Prev     string                  `json:"prev"`
	Curr     string                  `json:"curr"`
	Next     string                  `json:"next"`
	Severity ContinuityIssueSeverity `json:"severity"`
}

// RelationshipJump 关系等级越级跳变（如仇人一章成恋人）。
type RelationshipJump struct {
	A        string                  `json:"a"`
	B        string                  `json:"b"`
	Prev     string                  `json:"prev"`
	Next     string                  `json:"next"`
	Gap      int                     `json:"gap"`
	Severity ContinuityIssueSeverity `json:"severity"`
}

// UnreportedCharacter 正文多次出场但 commit 时未申报（characters / cast_intros）的角色。
// 漏报会让摘要、名册召回、GhostCharacter 等下游事实链静默缺失该角色。
type UnreportedCharacter struct {
	Name     string                  `json:"name"`
	Mentions int                     `json:"mentions"`
	Severity ContinuityIssueSeverity `json:"severity"`
}

// FactConflict 数值类事实与历史记录不一致（如公司人数 41 → 300 且未交代旧值）。
// Prev 是历史最新记录值，Next 是本次 commit 申报的新值。
// old_value 与历史值吻合的申报视为明知旧值的合理变更（剧情内增长），不记。
type FactConflict struct {
	Entity   string                  `json:"entity"`
	Field    string                  `json:"field"`
	Prev     string                  `json:"prev"`
	Next     string                  `json:"next"`
	Severity ContinuityIssueSeverity `json:"severity"`
}

// ContinuityIssues 一章 commit 时的机械连续性检测结果（仅事实，不阻断）。
type ContinuityIssues struct {
	StateRegressions     []StateRegression     `json:"state_regressions,omitempty"`
	RelationshipJumps    []RelationshipJump    `json:"relationship_jumps,omitempty"`
	UnreportedCharacters []UnreportedCharacter `json:"unreported_characters,omitempty"`
	FactConflicts        []FactConflict        `json:"fact_conflicts,omitempty"`
}

// Empty 判定是否没有任何发现。
func (c *ContinuityIssues) Empty() bool {
	return c == nil ||
		(len(c.StateRegressions) == 0 && len(c.RelationshipJumps) == 0 &&
			len(c.UnreportedCharacters) == 0 && len(c.FactConflicts) == 0)
}
