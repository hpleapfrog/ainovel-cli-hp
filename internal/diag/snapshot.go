package diag

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Snapshot 是对 output 目录全部工件的只读快照。
// 所有规则函数只接收 Snapshot，不直接访问文件系统。
type Snapshot struct {
	Progress      *domain.Progress
	RunMeta       *domain.RunMeta
	Compass       *domain.StoryCompass
	Outline       []domain.OutlineEntry
	Volumes       []domain.VolumeOutline
	Characters    []domain.Character
	CastLedger    []domain.CastEntry
	WorldRules    []domain.WorldRule
	Factions      []domain.Faction  // 势力档案（W1）
	Locations     []domain.Location // 地点档案（W1）
	Timeline      []domain.TimelineEvent
	Foreshadow    []domain.ForeshadowEntry
	Relationships []domain.RelationshipEntry
	StateChanges  []domain.StateChange
	StyleRules    *domain.WritingStyleRules
	Reviews       map[int]*domain.ReviewEntry
	Plans         map[int]*domain.ChapterPlan
	Summaries     map[int]*domain.ChapterSummary
	// ContinuityIssues 是 meta/continuity_issues.jsonl 的按章最新记录
	// （commit 时机械检测的状态回退/关系跳变/出场漏报；空=复测合格已清）。
	ContinuityIssues map[int]*domain.ContinuityIssues
	// UserRules 是本书归一化后的用户规则快照（meta/user_rules.json）。
	UserRules *rules.Snapshot
	// ChapterTexts 是已完成章节的正文（仅当 UserRules 含疲劳词时加载，
	// 供 CrossChapterFatigue 做跨章统计；无疲劳词时保持 nil 省 IO）。
	ChapterTexts map[int]string

	LoadErrors []string // 非 NotExist 的加载失败，区分"无数据"和"读取出错"
}

// Load 从 store 中读取全部工件，构建只读快照。
// 文件不存在视为"无数据"（字段保持零值）；其他错误记录到 LoadErrors。
func Load(s *store.Store) Snapshot {
	snap := Snapshot{
		Reviews:   make(map[int]*domain.ReviewEntry),
		Plans:     make(map[int]*domain.ChapterPlan),
		Summaries: make(map[int]*domain.ChapterSummary),
	}

	check := func(name string, err error) {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			snap.LoadErrors = append(snap.LoadErrors, fmt.Sprintf("%s: %v", name, err))
		}
	}

	var err error
	snap.Progress, err = s.Progress.Load()
	check("progress", err)
	snap.RunMeta, err = s.RunMeta.Load()
	check("run_meta", err)
	snap.Compass, err = s.Outline.LoadCompass()
	check("compass", err)
	snap.Outline, err = s.Outline.LoadOutline()
	check("outline", err)
	snap.Volumes, err = s.Outline.LoadLayeredOutline()
	check("volumes", err)
	snap.Characters, err = s.Characters.Load()
	check("characters", err)
	snap.CastLedger, err = s.Cast.Load()
	check("cast_ledger", err)
	snap.WorldRules, err = s.World.LoadWorldRules()
	check("world_rules", err)
	snap.Factions, err = s.World.LoadFactions()
	check("factions", err)
	snap.Locations, err = s.World.LoadLocations()
	check("locations", err)
	snap.Timeline, err = s.World.LoadTimeline()
	check("timeline", err)
	snap.Foreshadow, err = s.World.LoadForeshadowLedger()
	check("foreshadow", err)
	snap.Relationships, err = s.World.LoadRelationships()
	check("relationships", err)
	snap.StateChanges, err = s.World.LoadStateChanges()
	check("state_changes", err)
	snap.StyleRules, err = s.World.LoadStyleRules()
	check("style_rules", err)
	snap.ContinuityIssues = s.World.LoadAllContinuityIssues()
	snap.UserRules, err = s.UserRules.Load()
	check("user_rules", err)

	if snap.Progress != nil {
		// 跨章疲劳词统计需要正文;仅在用户规则含疲劳词时加载(P8-3)。
		needTexts := snap.UserRules != nil && len(snap.UserRules.Structured.FatigueWords) > 0
		if needTexts {
			snap.ChapterTexts = make(map[int]string, len(snap.Progress.CompletedChapters))
		}
		for _, ch := range snap.Progress.CompletedChapters {
			if plan, err := s.Drafts.LoadChapterPlan(ch); err == nil && plan != nil {
				snap.Plans[ch] = plan
			} else {
				check(fmt.Sprintf("plan_ch%d", ch), err)
			}
			if summary, err := s.Summaries.LoadSummary(ch); err == nil && summary != nil {
				snap.Summaries[ch] = summary
			} else {
				check(fmt.Sprintf("summary_ch%d", ch), err)
			}
			if review, err := s.World.LoadReview(ch); err == nil && review != nil {
				snap.Reviews[ch] = review
			} else {
				check(fmt.Sprintf("review_ch%d", ch), err)
			}
			if needTexts {
				if text, err := s.Drafts.LoadChapterText(ch); err == nil && text != "" {
					snap.ChapterTexts[ch] = text
				}
			}
		}
	}

	return snap
}

// CompletedCount 返回已完成章节数（安全访问）。
func (s *Snapshot) CompletedCount() int {
	if s.Progress == nil {
		return 0
	}
	return len(s.Progress.CompletedChapters)
}

// LatestCompleted 返回最大已完成章节号；无则返回 0。
func (s *Snapshot) LatestCompleted() int {
	if s.Progress == nil {
		return 0
	}
	max := 0
	for _, ch := range s.Progress.CompletedChapters {
		if ch > max {
			max = ch
		}
	}
	return max
}

// appendOnlyFiles 是追加式/全量重写式台账文件的相对路径清单——它们的体积随
// 章节数线性增长,是"何时归档压缩"决策的依据(数据先行)。
var appendOnlyFiles = []string{
	"meta/state_changes.json",
	"timeline.json",
	"meta/checkpoints.jsonl",
	"meta/decisions.jsonl",
	"meta/rule_violations.jsonl",
	"meta/continuity_issues.jsonl",
	"meta/consistency_checks.jsonl",
	"meta/cast_ledger.json",
	"relationship_state.json",
	"foreshadow_ledger.json",
}

// AppendOnlyBytes 统计追加式台账文件的体积合计;失败文件静默跳过(指标 best-effort)。
func AppendOnlyBytes(s *store.Store) int64 {
	if s == nil {
		return 0
	}
	var total int64
	for _, rel := range appendOnlyFiles {
		if info, err := os.Stat(filepath.Join(s.Dir(), rel)); err == nil {
			total += info.Size()
		}
	}
	return total
}
