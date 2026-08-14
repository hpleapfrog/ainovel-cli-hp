package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// Store 是状态管理的组合根，持有所有子存储。
type Store struct {
	dir string

	Progress    *ProgressStore
	Outline     *OutlineStore
	Drafts      *DraftStore
	Summaries   *SummaryStore
	RunMeta     *RunMetaStore
	UserRules   *UserRulesStore
	Signals     *SignalStore
	Runtime     *RuntimeStore
	Characters  *CharacterStore
	Cast        *CastStore
	World       *WorldStore
	Checkpoints *CheckpointStore
	Sessions    *SessionStore
	Usage       *UsageStore
	Simulation  *SimulationStore
	Decisions   *DecisionStore

	crossMu sync.Mutex // 保护跨域原子操作
}

// NewStore 创建状态管理器，dir 为小说输出根目录。
func NewStore(dir string) *Store {
	outline := NewOutlineStore(newIO(dir))
	return &Store{
		dir:         dir,
		Progress:    NewProgressStore(newIO(dir)),
		Outline:     outline,
		Drafts:      NewDraftStore(newIO(dir)),
		Summaries:   NewSummaryStore(newIO(dir), outline),
		RunMeta:     NewRunMetaStore(newIO(dir)),
		UserRules:   NewUserRulesStore(newIO(dir)),
		Signals:     NewSignalStore(newIO(dir)),
		Runtime:     NewRuntimeStore(newIO(dir)),
		Characters:  NewCharacterStore(newIO(dir), outline),
		Cast:        NewCastStore(newIO(dir)),
		World:       NewWorldStore(newIO(dir)),
		Checkpoints: NewCheckpointStore(newIO(dir)),
		Sessions:    NewSessionStore(newIO(dir)),
		Usage:       NewUsageStore(newIO(dir)),
		Simulation:  NewSimulationStore(newIO(dir)),
		Decisions:   NewDecisionStore(newIO(dir)),
	}
}

// Dir 返回输出根目录。
func (s *Store) Dir() string { return s.dir }

// WipeBook 清空本书全部创作产物（StartPrepared 开新书时调用）。
//
// 契约：StartPrepared 是"开一本新书"——只重置 progress 会让新书继承旧书的
// 时间线/伏笔/关系/状态台账/配角名册/章节文件/用量账本：世界状态台账混入
// 新书会污染一致性检测与上下文召回（与"越写越多"事故同源），旧用量账本会让
// 新书预算从旧书成本起跳。
//
// 保留：meta/run.json（随即被 RunMeta 重写）、meta/user_rules.json
// （PrepareUserRules 在每次启动时重写）、logs/。
func (s *Store) WipeBook() error {
	files := []string{
		"premise.md", "outline.json", "outline.md",
		"layered_outline.json", "layered_outline.md",
		"characters.json", "characters.md",
		"world_rules.json", "world_rules.md",
		"factions.json", "factions.md",
		"locations.json", "locations.md",
		"timeline.json", "timeline.md",
		"foreshadow_ledger.json", "foreshadow_ledger.md",
		"relationship_state.json", "relationship_state.md",
		"meta/progress.json", "meta/checkpoints.jsonl",
		"meta/compass.json", "meta/style_rules.json",
		"meta/state_changes.json", "meta/cast_ledger.json",
		"meta/rule_violations.jsonl", "meta/continuity_issues.jsonl",
		"meta/consistency_checks.jsonl", "meta/outline_feedback.jsonl",
		"meta/decisions.jsonl", "meta/usage.json",
		"meta/pending_commit.json",
	}
	dirs := []string{
		"chapters", "drafts", "reviews", "summaries",
		"meta/runtime", "meta/sessions", "meta/snapshots", "meta/archive",
	}
	var firstErr error
	fail := func(err error) {
		if firstErr == nil && err != nil {
			firstErr = err
		}
	}
	for _, f := range files {
		if err := os.Remove(filepath.Join(s.dir, f)); err != nil && !os.IsNotExist(err) {
			fail(err)
		}
	}
	for _, d := range dirs {
		if err := os.RemoveAll(filepath.Join(s.dir, d)); err != nil {
			fail(err)
		}
	}
	return firstErr
}

// CheckConsistency 对事实层做一次浅层校验，用于启动/恢复时生成 warning。
// 纯只读：不修正数据，仅返回可读的问题描述。调用方决定如何展示（log / UI）。
// 为避免扫全目录带来的 IO 开销，只校验 Progress 的关键点：
//   - 最新完成章节（CompletedChapters 最大值，与 domain.Progress.LatestCompleted 同口径）
//     必须在 chapters/ 下存在终稿
//   - Layered 模式下，当前 Volume/Arc 必须能在 layered_outline 中找到
func (s *Store) CheckConsistency() []string {
	var warnings []string
	progress, err := s.Progress.Load()
	if err != nil || progress == nil {
		return warnings
	}
	if lastCh := progress.LatestCompleted(); lastCh > 0 {
		if text, err := s.Drafts.LoadChapterText(lastCh); err == nil && text == "" {
			warnings = append(warnings, fmt.Sprintf("progress 标记第 %d 章已完成，但 chapters/%02d.md 不存在或为空", lastCh, lastCh))
		}
	}
	if progress.Layered && progress.CurrentVolume > 0 && progress.CurrentArc > 0 {
		volumes, err := s.Outline.LoadLayeredOutline()
		if err == nil && len(volumes) > 0 {
			found := false
			for _, v := range volumes {
				if v.Index != progress.CurrentVolume {
					continue
				}
				for _, a := range v.Arcs {
					if a.Index == progress.CurrentArc {
						found = true
						break
					}
				}
				break
			}
			if !found {
				warnings = append(warnings, fmt.Sprintf("progress 当前 V%d A%d 在分层大纲中找不到对应条目", progress.CurrentVolume, progress.CurrentArc))
			}
		}
	}
	return warnings
}

// FoundationMissing 返回基础设定中尚缺的项，按用于 Prompt/Reminder 的稳定顺序排列。
// 长篇模式（已有 layered_outline）额外要求 compass。
func (s *Store) FoundationMissing() []string {
	var missing []string
	if p, _ := s.Outline.LoadPremise(); p == "" {
		missing = append(missing, "premise")
	}
	if o, _ := s.Outline.LoadOutline(); len(o) == 0 {
		missing = append(missing, "outline")
	}
	if c, _ := s.Characters.Load(); len(c) == 0 {
		missing = append(missing, "characters")
	}
	if r, _ := s.World.LoadWorldRules(); len(r) == 0 {
		missing = append(missing, "world_rules")
	}
	if layered, _ := s.Outline.LoadLayeredOutline(); len(layered) > 0 {
		if c, _ := s.Outline.LoadCompass(); c == nil {
			missing = append(missing, "compass")
		}
	}
	return missing
}

// Init 创建所需的子目录结构。
func (s *Store) Init() error {
	return s.Progress.io.EnsureDirs([]string{
		"chapters", "summaries", "drafts", "reviews", "meta", "meta/runtime", "meta/runtime/tasks", "meta/sessions", "meta/sessions/agents",
	})
}

// ── 跨域协调方法 ──

// ExpandArc 将骨架弧展开为详细章节（Outline + Progress 联动）。
//
// 幂等守卫：目标弧已展开时，仅当传入章节与已落盘内容逐字段一致才放行
// （崩溃/网络重试的同参重放）；内容不同的二次展开一律拒绝——已展开弧被整体
// 覆盖会让已写章节失去大纲锚点、弧边界检测错位。想改动已展开弧的结构请走
// 返工/追加卷，而不是重新展开。
func (s *Store) ExpandArc(volumeIdx, arcIdx int, chapters []domain.OutlineEntry) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	var volumes []domain.VolumeOutline
	if err := s.Outline.io.ReadJSONUnlocked("layered_outline.json", &volumes); err != nil {
		return err
	}
	for _, v := range volumes {
		if v.Index != volumeIdx {
			continue
		}
		for _, a := range v.Arcs {
			if a.Index != arcIdx {
				continue
			}
			if a.IsExpanded() && !sameOutlineChapters(a.Chapters, chapters) {
				return fmt.Errorf("第 %d 卷第 %d 弧已展开且内容不同，禁止覆盖；如需调整结构请改用 append_volume 或经返工流程", volumeIdx, arcIdx)
			}
		}
	}

	updated, err := s.Outline.expandArcUnlocked(volumeIdx, arcIdx, chapters)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.TotalChapters(updated)
	return s.Progress.saveUnlocked(p)
}

// sameOutlineChapters 逐字段比较两组大纲章节（忽略全局章号，落盘形态无章号）。
func sameOutlineChapters(a, b []domain.OutlineEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		x.Chapter, y.Chapter = 0, 0
		if x.Title != y.Title || x.CoreEvent != y.CoreEvent || x.Hook != y.Hook ||
			!slices.Equal(x.Scenes, y.Scenes) {
			return false
		}
	}
	return true
}

// AppendVolume 追加新卷到分层大纲末尾（Outline + Progress 联动）。
func (s *Store) AppendVolume(vol domain.VolumeOutline) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	volumes, err := s.Outline.appendVolumeUnlocked(vol)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.TotalChapters(volumes)
	return s.Progress.saveUnlocked(p)
}

// ClearHandledSteer 原子性清除 PendingSteer 并重置 FlowSteering 状态
// （RunMeta + Progress 联动）。
func (s *Store) ClearHandledSteer() error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.RunMeta.io.mu.Lock()
	defer s.RunMeta.io.mu.Unlock()

	meta, err := s.RunMeta.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if meta != nil && meta.PendingSteer != "" {
		meta.PendingSteer = ""
		if err := s.RunMeta.saveUnlocked(*meta); err != nil {
			return err
		}
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if p != nil && p.Flow == domain.FlowSteering {
		if err := domain.ValidateFlowTransition(p.Flow, domain.FlowWriting); err != nil {
			return err
		}
		p.Flow = domain.FlowWriting
		if err := s.Progress.saveUnlocked(p); err != nil {
			return err
		}
	}
	return nil
}
