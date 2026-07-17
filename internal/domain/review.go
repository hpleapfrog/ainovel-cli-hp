package domain

// TimelineEvent 时间线事件。
type TimelineEvent struct {
	Chapter    int      `json:"chapter"`
	Time       string   `json:"time"`
	Event      string   `json:"event"`
	Characters []string `json:"characters,omitempty"`
}

// ForeshadowEntry 伏笔条目。
type ForeshadowEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	PlantedAt   int    `json:"planted_at"`
	Status      string `json:"status"` // planted / advanced / resolved
	ResolvedAt  int    `json:"resolved_at,omitempty"`
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
