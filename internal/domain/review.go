package domain

// FactDistance 世界事实距主角的距离标注（Bishu observer 口径）：
// 用于 writer 感知"这条线离主角多远"、diag 识别"水下世界是否被遗忘"，
// 也是将来势力实体（W1 证据驱动）的数据底子。
type FactDistance string

const (
	DistanceNear FactDistance = "near" // 主角身边：本场景可感
	DistanceMid  FactDistance = "mid"  // 同城/同局：短期内可能接触
	DistanceFar  FactDistance = "far"  // 远处暗线：只影响氛围与伏笔
	DistanceFog  FactDistance = "fog"  // 迷雾：读者与主角都未看清的未知
)

// Valid 判断距离标注是否为受控枚举（空值合法=旧数据/未标注）。
func (d FactDistance) Valid() bool {
	return d == "" || d == DistanceNear || d == DistanceMid || d == DistanceFar || d == DistanceFog
}

// TimelineEvent 时间线事件。
type TimelineEvent struct {
	Chapter    int      `json:"chapter"`
	Time       string   `json:"time"`
	Event      string   `json:"event"`
	Characters []string `json:"characters,omitempty"`
	// Distance 可选：该事件距主角的距离标注（near/mid/far/fog）。
	Distance string `json:"distance,omitempty"`
}

// ForeshadowEntry 伏笔条目。
type ForeshadowEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	// Kind 区分两类叙事债务（Bishu 口径）：hook=读者想知道答案（悬念钩子）；
	// debt=角色欠角色（人物层面的义务/亏欠，需剧情偿还）。空值兼容旧台账。
	Kind string `json:"kind,omitempty"` // hook / debt
	// ExpectedPayoff 预期回收区间（如"3-5章内"）。foreshadow_due 与 editor 评审
	// 据此对照"是否已逾期"，比只看休眠章数多一个承诺口径。
	ExpectedPayoff string `json:"expected_payoff,omitempty"`
	// From/To 仅 debt 有效：债务人（欠下亏欠的一方）与债权人（被欠的一方）。
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	PlantedAt  int    `json:"planted_at"`
	Status     string `json:"status"` // planted / advanced / resolved
	ResolvedAt int    `json:"resolved_at,omitempty"`
	// LastTouchedAt 是最近一次被触及（埋设或推进）的章节号。
	// 没有这个字段时，"距上次推进多久"对模型、召回与 diag 都不可计算
	// （advance 只翻转 Status，推进史会丢）。
	LastTouchedAt int `json:"last_touched_at,omitempty"`
}

// DormantSince 返回伏笔最近一次被触及的章节号；旧数据无 LastTouchedAt 时回退到埋设章。
// 账龄/停滞口径统一走这里：休眠期 = 当前章 - DormantSince。
func (e ForeshadowEntry) DormantSince() int {
	if e.LastTouchedAt > 0 {
		return e.LastTouchedAt
	}
	return e.PlantedAt
}

// ForeshadowDueChapters 是「伏笔该推进了」的共享阈值（章）：
// Writer 的 foreshadow_due 清单与 diag 的 StaleForeshadow 停滞下限共用，
// 线上提醒与离线诊断看到的是同一把尺子（与 editor.md 评审的"5 章未推进"口径对齐）。
const ForeshadowDueChapters = 5

// ForeshadowStatus 是注入给 LLM 的伏笔视图：台账原始字段 + 代码派生的休眠章数。
// 休眠章数由注入方按当前章现算，不持久化（它是"相对当前章的值"，不是台账事实）。
type ForeshadowStatus struct {
	ForeshadowEntry
	ChaptersSinceLastTouch int `json:"chapters_since_last_touch"`
}

// ForeshadowUpdate 伏笔增量操作。
type ForeshadowUpdate struct {
	ID          string `json:"id"`
	Action      string `json:"action"` // plant / advance / resolve
	Description string `json:"description,omitempty"`
	// Kind 仅 plant 时有效：hook（读者想知道答案）/ debt（角色欠角色）。
	Kind string `json:"kind,omitempty"`
	// ExpectedPayoff 仅 plant 时有效：预期回收区间（如"3-5章内"）。
	ExpectedPayoff string `json:"expected_payoff,omitempty"`
	// From/To 仅 plant + kind=debt 时有效：债务人/债权人（谁欠谁）。
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// RelationshipEntry 人物关系条目。
type RelationshipEntry struct {
	CharacterA string `json:"character_a"`
	CharacterB string `json:"character_b"`
	Relation   string `json:"relation"`
	Chapter    int    `json:"chapter"`
}

// ConsistencyIssue 一致性问题。
type ConsistencyIssue struct {
	Type        string `json:"type"`     // consistency / character / pacing / continuity / foreshadow / hook / aesthetic
	Severity    string `json:"severity"` // critical / error / warning
	Description string `json:"description"`
	Evidence    string `json:"evidence,omitempty"` // 证据：原文片段、具体情节或状态数据
	Suggestion  string `json:"suggestion,omitempty"`
}

// DimensionScore 单维度评审评分。
type DimensionScore struct {
	Dimension string `json:"dimension"`         // consistency / character / pacing / continuity / foreshadow / hook / aesthetic
	Score     int    `json:"score"`             // 0-100
	Verdict   string `json:"verdict"`           // pass / warning / fail
	Comment   string `json:"comment,omitempty"` // 该维度的简要结论
}

// ReviewEntry Editor 的审阅条目。
type ReviewEntry struct {
	Chapter          int                `json:"chapter"`
	Scope            string             `json:"scope"` // chapter / global / arc
	Issues           []ConsistencyIssue `json:"issues"`
	Dimensions       []DimensionScore   `json:"dimensions,omitempty"`      // 分维度评分
	ContractStatus   string             `json:"contract_status,omitempty"` // met / partial / missed
	ContractMisses   []string           `json:"contract_misses,omitempty"` // 未达成的 contract 条目
	ContractNotes    string             `json:"contract_notes,omitempty"`  // 对 contract 履行情况的简述
	Verdict          string             `json:"verdict"`                   // accept / polish / rewrite
	Summary          string             `json:"summary"`
	AffectedChapters []int              `json:"affected_chapters,omitempty"` // 需要重写/打磨的章节号
}

// CriticalCount 返回 critical 级别问题数量。
func (r *ReviewEntry) CriticalCount() int {
	n := 0
	for _, issue := range r.Issues {
		if issue.Severity == "critical" {
			n++
		}
	}
	return n
}

// ErrorCount 返回 error 级别问题数量。
func (r *ReviewEntry) ErrorCount() int {
	n := 0
	for _, issue := range r.Issues {
		if issue.Severity == "error" {
			n++
		}
	}
	return n
}

// Dimension 返回指定维度的评分；不存在则返回 nil。
func (r *ReviewEntry) Dimension(name string) *DimensionScore {
	if r == nil {
		return nil
	}
	for i := range r.Dimensions {
		if r.Dimensions[i].Dimension == name {
			return &r.Dimensions[i]
		}
	}
	return nil
}
