package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/rules"
)

// WorldStore 管理时间线、伏笔、人物关系、状态变化、世界规则、风格规则、审阅和交接。
type WorldStore struct{ io *IO }

func NewWorldStore(io *IO) *WorldStore { return &WorldStore{io: io} }

// ── 时间线 ──

// SaveTimeline 全量写入 timeline.json + timeline.md（原子写入）。
func (s *WorldStore) SaveTimeline(events []domain.TimelineEvent) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("timeline.json", events); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("timeline.md", renderTimeline(events))
	})
}

// LoadTimeline 读取时间线。
func (s *WorldStore) LoadTimeline() ([]domain.TimelineEvent, error) {
	var events []domain.TimelineEvent
	if err := s.io.ReadJSON("timeline.json", &events); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return events, nil
}

// AppendTimelineEvents 追加时间线事件。同一事件重复提交时按稳定 key 去重，保证
// commit_chapter 崩溃后重跑不会污染时间线。
func (s *WorldStore) AppendTimelineEvents(newEvents []domain.TimelineEvent) error {
	return s.io.WithWriteLock(func() error {
		var existing []domain.TimelineEvent
		if err := s.io.ReadJSONUnlocked("timeline.json", &existing); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		seen := make(map[string]struct{}, len(existing)+len(newEvents))
		for _, e := range existing {
			seen[timelineEventKey(e)] = struct{}{}
		}
		all := existing
		for _, e := range newEvents {
			key := timelineEventKey(e)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			all = append(all, e)
		}
		if err := s.io.WriteJSONUnlocked("timeline.json", all); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("timeline.md", renderTimeline(all))
	})
}

// LoadRecentTimeline 返回最近 window 章内的时间线事件（含 current 章本身；
// 排除未来章事件——导入管线批量写入后可能存在 Chapter > current 的事件）。
func (s *WorldStore) LoadRecentTimeline(current, window int) ([]domain.TimelineEvent, error) {
	all, err := s.LoadTimeline()
	if err != nil {
		return nil, err
	}
	minCh := max(current-window, 1)
	var filtered []domain.TimelineEvent
	for _, e := range all {
		if e.Chapter >= minCh && e.Chapter <= current {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

// ── 伏笔 ──

// SaveForeshadowLedger 全量写入 foreshadow_ledger.json + foreshadow_ledger.md（原子写入）。
func (s *WorldStore) SaveForeshadowLedger(entries []domain.ForeshadowEntry) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("foreshadow_ledger.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("foreshadow_ledger.md", renderForeshadow(entries))
	})
}

// LoadForeshadowLedger 读取伏笔账本。
func (s *WorldStore) LoadForeshadowLedger() ([]domain.ForeshadowEntry, error) {
	var entries []domain.ForeshadowEntry
	if err := s.io.ReadJSON("foreshadow_ledger.json", &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// UpdateForeshadow 批量应用伏笔增量操作。
func (s *WorldStore) UpdateForeshadow(chapter int, updates []domain.ForeshadowUpdate) error {
	return s.io.WithWriteLock(func() error {
		var entries []domain.ForeshadowEntry
		if err := s.io.ReadJSONUnlocked("foreshadow_ledger.json", &entries); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		idx := make(map[string]int, len(entries))
		for i, e := range entries {
			idx[e.ID] = i
		}
		for _, u := range updates {
			switch u.Action {
			case "plant":
				if i, ok := idx[u.ID]; ok {
					if entries[i].Description == "" {
						entries[i].Description = u.Description
					}
					if entries[i].Kind == "" && u.Kind != "" {
						entries[i].Kind = u.Kind
					}
					if entries[i].ExpectedPayoff == "" && u.ExpectedPayoff != "" {
						entries[i].ExpectedPayoff = u.ExpectedPayoff
					}
					if entries[i].From == "" && u.From != "" {
						entries[i].From = u.From
					}
					if entries[i].To == "" && u.To != "" {
						entries[i].To = u.To
					}
					if entries[i].PlantedAt == 0 {
						entries[i].PlantedAt = chapter
						entries[i].LastTouchedAt = chapter
					}
					if entries[i].Status == "" {
						entries[i].Status = "planted"
					}
					continue
				}
				idx[u.ID] = len(entries)
				entries = append(entries, domain.ForeshadowEntry{
					ID:             u.ID,
					Description:    u.Description,
					Kind:           u.Kind,
					ExpectedPayoff: u.ExpectedPayoff,
					From:           u.From,
					To:             u.To,
					PlantedAt:      chapter,
					Status:         "planted",
					LastTouchedAt:  chapter,
				})
			case "advance":
				if i, ok := idx[u.ID]; ok {
					entries[i].Status = "advanced"
					entries[i].LastTouchedAt = chapter
				}
			case "resolve":
				if i, ok := idx[u.ID]; ok {
					entries[i].Status = "resolved"
					entries[i].ResolvedAt = chapter
				}
			}
		}
		if err := s.io.WriteJSONUnlocked("foreshadow_ledger.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("foreshadow_ledger.md", renderForeshadow(entries))
	})
}

// LoadActiveForeshadow 返回未回收的伏笔条目。
func (s *WorldStore) LoadActiveForeshadow() ([]domain.ForeshadowEntry, error) {
	all, err := s.LoadForeshadowLedger()
	if err != nil {
		return nil, err
	}
	var active []domain.ForeshadowEntry
	for _, e := range all {
		if e.Status != "resolved" {
			active = append(active, e)
		}
	}
	return active, nil
}

// ── 人物关系 ──

// SaveRelationships 全量写入 relationship_state.json + relationship_state.md（原子写入）。
func (s *WorldStore) SaveRelationships(entries []domain.RelationshipEntry) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("relationship_state.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("relationship_state.md", renderRelationships(entries))
	})
}

// LoadRelationships 读取人物关系状态。
func (s *WorldStore) LoadRelationships() ([]domain.RelationshipEntry, error) {
	var entries []domain.RelationshipEntry
	if err := s.io.ReadJSON("relationship_state.json", &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// UpdateRelationships 合并关系变化。
func (s *WorldStore) UpdateRelationships(changes []domain.RelationshipEntry) error {
	return s.io.WithWriteLock(func() error {
		var existing []domain.RelationshipEntry
		if err := s.io.ReadJSONUnlocked("relationship_state.json", &existing); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		idx := make(map[string]int, len(existing))
		for i, e := range existing {
			idx[pairKey(e.CharacterA, e.CharacterB)] = i
		}
		for _, c := range changes {
			key := pairKey(c.CharacterA, c.CharacterB)
			if i, ok := idx[key]; ok {
				existing[i].Relation = c.Relation
				existing[i].Chapter = c.Chapter
			} else {
				idx[key] = len(existing)
				existing = append(existing, c)
			}
		}
		if err := s.io.WriteJSONUnlocked("relationship_state.json", existing); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("relationship_state.md", renderRelationships(existing))
	})
}

// ── 状态变化 ──

// AppendStateChanges 追加角色状态变化。同一状态变化重复提交时按稳定 key 去重。
// 写入前校验 field 非空，并把已知枚举的大小写/空白变体归一（Power→power），
// 消除拼写漂移对 FactConflict 匹配的静默破坏（W5）。
func (s *WorldStore) AppendStateChanges(changes []domain.StateChange) error {
	if err := domain.ValidateStateChanges(changes); err != nil {
		return err
	}
	normalized := make([]domain.StateChange, len(changes))
	for i, c := range changes {
		c.Field = domain.NormalizeStateField(c.Field)
		normalized[i] = c
	}
	return s.io.WithWriteLock(func() error {
		var existing []domain.StateChange
		if err := s.io.ReadJSONUnlocked("meta/state_changes.json", &existing); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		seen := make(map[string]struct{}, len(existing)+len(normalized))
		for _, c := range existing {
			seen[stateChangeKey(c)] = struct{}{}
		}
		all := existing
		for _, c := range normalized {
			key := stateChangeKey(c)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			all = append(all, c)
		}
		return s.io.WriteJSONUnlocked("meta/state_changes.json", all)
	})
}

// LoadStateChanges 读取全部状态变化记录。
func (s *WorldStore) LoadStateChanges() ([]domain.StateChange, error) {
	var changes []domain.StateChange
	if err := s.io.ReadJSON("meta/state_changes.json", &changes); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return changes, nil
}

// ── 世界规则 ──

// SaveWorldRules 全量写入 world_rules.json + world_rules.md（原子写入）。
func (s *WorldStore) SaveWorldRules(rules []domain.WorldRule) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("world_rules.json", rules); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("world_rules.md", renderWorldRules(rules))
	})
}

// LoadWorldRules 读取世界规则。
func (s *WorldStore) LoadWorldRules() ([]domain.WorldRule, error) {
	var rules []domain.WorldRule
	if err := s.io.ReadJSON("world_rules.json", &rules); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return rules, nil
}

// ── 势力与地点（W1）──

// SaveFactions 全量写入 factions.json + factions.md（原子写入）。
func (s *WorldStore) SaveFactions(factions []domain.Faction) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("factions.json", factions); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("factions.md", renderFactions(factions))
	})
}

// LoadFactions 读取势力档案。
func (s *WorldStore) LoadFactions() ([]domain.Faction, error) {
	var factions []domain.Faction
	if err := s.io.ReadJSON("factions.json", &factions); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return factions, nil
}

// SaveLocations 全量写入 locations.json + locations.md（原子写入）。
func (s *WorldStore) SaveLocations(locations []domain.Location) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("locations.json", locations); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("locations.md", renderLocations(locations))
	})
}

// LoadLocations 读取地点档案。
func (s *WorldStore) LoadLocations() ([]domain.Location, error) {
	var locations []domain.Location
	if err := s.io.ReadJSON("locations.json", &locations); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return locations, nil
}

// UpsertFactions 按章增量合并势力档案（commit_chapter.faction_updates 的落点）。
//
// 语义：按 name+aliases 查找——命中则只更新提供的字段（不覆盖未提供的），
// LastUpdated 强制置为本章；未命中则追加新条目（status 必填）。
// status 变化自动派生一条 state_changes 历史轨迹（OldValue 取自档案旧值、
// distance 透传），"已灭势力复出"的回退检测据此零改动工作。
//
// 返回成功合并/新增的条数。幂等：同章重复 commit 同值重写无害、派生轨迹
// 被 AppendStateChanges 的稳定 key 去重。注意锁顺序：档案写锁释放后才追加
// 历史轨迹（两个写锁不可嵌套）。
func (s *WorldStore) UpsertFactions(chapter int, updates []domain.FactionUpdate) (int, error) {
	if chapter <= 0 || len(updates) == 0 {
		return 0, nil
	}
	var derived []domain.StateChange
	count := 0
	if err := s.io.WithWriteLock(func() error {
		var factions []domain.Faction
		if err := s.io.ReadJSONUnlocked("factions.json", &factions); err != nil && !os.IsNotExist(err) {
			return err
		}
		index := make(map[string]int, len(factions))
		for i, f := range factions {
			index[f.Name] = i
			for _, a := range f.Aliases {
				index[a] = i
			}
		}
		for _, u := range updates {
			name := strings.TrimSpace(u.Name)
			if name == "" {
				continue
			}
			i, ok := index[name]
			if !ok {
				// 新势力：status 必填（快照没有状态无意义）
				if strings.TrimSpace(u.Status) == "" {
					continue
				}
				factions = append(factions, domain.Faction{
					Name:        name,
					Status:      u.Status,
					Goal:        u.Goal,
					Relation:    u.Relation,
					Location:    u.Location,
					LastUpdated: chapter,
				})
				index[name] = len(factions) - 1
				derived = append(derived, domain.StateChange{
					Chapter: chapter, Entity: name, Field: "status",
					NewValue: u.Status, Distance: u.Distance,
				})
				count++
				continue
			}
			f := &factions[i]
			if u.Status != "" && u.Status != f.Status {
				derived = append(derived, domain.StateChange{
					Chapter: chapter, Entity: f.Name, Field: "status",
					OldValue: f.Status, NewValue: u.Status, Distance: u.Distance,
				})
				f.Status = u.Status
			}
			if strings.TrimSpace(u.Goal) != "" {
				f.Goal = u.Goal
			}
			if strings.TrimSpace(u.Relation) != "" {
				f.Relation = u.Relation
			}
			if strings.TrimSpace(u.Location) != "" {
				f.Location = u.Location
			}
			f.LastUpdated = chapter
			count++
		}
		if err := s.io.WriteJSONUnlocked("factions.json", factions); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("factions.md", renderFactions(factions))
	}); err != nil {
		return 0, err
	}
	if len(derived) > 0 {
		if err := s.AppendStateChanges(derived); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// ── 追加式台账归档 ──
//
// state_changes/timeline 每次 commit 全量读+全量写，几百章后是 O(N²) 级 IO。
// 热文件不能裁剪（回退/冲突检测需要全量历史基线），因此弧末做**快照归档**：
// 历史留在 meta/archive/v{卷}a{弧}/，diag/排查可读，热文件语义不变。

// ArchiveLedgers 把状态台账与时间线快照归档到指定卷弧目录（best-effort 复制）。
func (s *WorldStore) ArchiveLedgers(volume, arc int) error {
	if volume <= 0 || arc <= 0 {
		return nil
	}
	dir := fmt.Sprintf("meta/archive/v%02da%02d", volume, arc)
	srcs := []string{"meta/state_changes.json", "timeline.json"}
	for _, rel := range srcs {
		data, err := s.io.ReadFile(rel)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		dest := filepath.Join(dir, filepath.Base(rel))
		if err := os.MkdirAll(filepath.Join(s.io.dir, dir), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(s.io.dir, dest), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// UpsertLocations 按章增量合并地点档案（commit_chapter.location_updates 的落点）。
// 与 UpsertFactions 同款：name+aliases 查找、只更新提供字段、新地点 kind 必填。
// 返回成功合并/新增的条数。幂等：同章重复 commit 同值重写无害。
func (s *WorldStore) UpsertLocations(chapter int, updates []domain.LocationUpdate) (int, error) {
	if chapter <= 0 || len(updates) == 0 {
		return 0, nil
	}
	count := 0
	err := s.io.WithWriteLock(func() error {
		var locations []domain.Location
		if err := s.io.ReadJSONUnlocked("locations.json", &locations); err != nil && !os.IsNotExist(err) {
			return err
		}
		index := make(map[string]int, len(locations))
		for i, l := range locations {
			index[l.Name] = i
			for _, a := range l.Aliases {
				index[a] = i
			}
		}
		for _, u := range updates {
			name := strings.TrimSpace(u.Name)
			if name == "" {
				continue
			}
			i, ok := index[name]
			if !ok {
				if strings.TrimSpace(u.Kind) == "" {
					continue // 新地点必须有类型
				}
				locations = append(locations, domain.Location{
					Name: name, Kind: u.Kind, Description: u.Description, OwnerFaction: u.OwnerFaction,
				})
				index[name] = len(locations) - 1
				count++
				continue
			}
			l := &locations[i]
			if strings.TrimSpace(u.Kind) != "" {
				l.Kind = u.Kind
			}
			if strings.TrimSpace(u.Description) != "" {
				l.Description = u.Description
			}
			if strings.TrimSpace(u.OwnerFaction) != "" {
				l.OwnerFaction = u.OwnerFaction
			}
			count++
		}
		if err := s.io.WriteJSONUnlocked("locations.json", locations); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("locations.md", renderLocations(locations))
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ── 风格规则 ──

// SaveStyleRules 保存写作风格规则。
func (s *WorldStore) SaveStyleRules(rules domain.WritingStyleRules) error {
	return s.io.WriteJSON("meta/style_rules.json", rules)
}

// LoadStyleRules 读取写作风格规则。
func (s *WorldStore) LoadStyleRules() (*domain.WritingStyleRules, error) {
	var rules domain.WritingStyleRules
	if err := s.io.ReadJSON("meta/style_rules.json", &rules); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &rules, nil
}

// ── 审阅 ──

// SaveReview 保存审阅结果。
func (s *WorldStore) SaveReview(r domain.ReviewEntry) error {
	rel := fmt.Sprintf("reviews/%02d.json", r.Chapter)
	if r.Scope == "global" {
		rel = fmt.Sprintf("reviews/%02d-global.json", r.Chapter)
	}
	return s.io.WriteJSON(rel, r)
}

// HasArcReview 检查指定章节（弧末章）是否已保存 scope=arc 的评审。
// 读失败按"未保存"处理，让 Router 倾向于重派而不是跳过。
func (s *WorldStore) HasArcReview(chapter int) bool {
	rv, err := s.LoadReview(chapter)
	return err == nil && rv != nil && rv.Scope == "arc"
}

// HasGlobalReview 检查指定章节是否已保存 scope=global 的全局审阅
// (save_review 落盘为 reviews/%02d-global.json;非分层书按 ReviewInterval 触发)。
func (s *WorldStore) HasGlobalReview(chapter int) bool {
	var r domain.ReviewEntry
	err := s.io.ReadJSON(fmt.Sprintf("reviews/%02d-global.json", chapter), &r)
	return err == nil && r.Scope == "global"
}

// LoadReview 读取章节审阅结果。
func (s *WorldStore) LoadReview(chapter int) (*domain.ReviewEntry, error) {
	var r domain.ReviewEntry
	if err := s.io.ReadJSON(fmt.Sprintf("reviews/%02d.json", chapter), &r); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// LoadLastReview 读取最近一次全局审阅。
func (s *WorldStore) LoadLastReview(fromChapter int) (*domain.ReviewEntry, error) {
	for ch := fromChapter; ch >= 1; ch-- {
		var r domain.ReviewEntry
		if err := s.io.ReadJSON(fmt.Sprintf("reviews/%02d-global.json", ch), &r); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		return &r, nil
	}
	return nil, nil
}

// ── render helpers ──

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func timelineEventKey(e domain.TimelineEvent) string {
	chars := append([]string(nil), e.Characters...)
	slices.Sort(chars)
	return fmt.Sprintf("%d|%s|%s|%s", e.Chapter, e.Time, e.Event, strings.Join(chars, ","))
}

func stateChangeKey(c domain.StateChange) string {
	return fmt.Sprintf("%d|%s|%s|%s|%s", c.Chapter, c.Entity, c.Field, c.OldValue, c.NewValue)
}

func renderTimeline(events []domain.TimelineEvent) string {
	var b strings.Builder
	b.WriteString("# 时间线\n\n")
	for _, e := range events {
		chars := ""
		if len(e.Characters) > 0 {
			chars = "（" + strings.Join(e.Characters, "、") + "）"
		}
		distance := ""
		switch domain.FactDistance(e.Distance) {
		case domain.DistanceNear:
			distance = "近"
		case domain.DistanceMid:
			distance = "中"
		case domain.DistanceFar:
			distance = "远"
		case domain.DistanceFog:
			distance = "迷雾"
		}
		if distance != "" {
			distance = "〔" + distance + "〕"
		}
		fmt.Fprintf(&b, "- **第 %d 章 [%s]**：%s%s%s\n", e.Chapter, e.Time, e.Event, chars, distance)
	}
	return b.String()
}

func renderForeshadow(entries []domain.ForeshadowEntry) string {
	var b strings.Builder
	b.WriteString("# 伏笔账本\n\n")
	for _, e := range entries {
		kind := ""
		switch e.Kind {
		case "hook":
			kind = "钩子"
		case "debt":
			kind = "债务"
		}
		status := e.Status
		if e.ResolvedAt > 0 {
			status = fmt.Sprintf("已回收（第 %d 章）", e.ResolvedAt)
		}
		if kind != "" {
			fmt.Fprintf(&b, "- **[%s]（%s）** %s — 埋设于第 %d 章，状态：%s",
				e.ID, kind, e.Description, e.PlantedAt, status)
		} else {
			fmt.Fprintf(&b, "- **[%s]** %s — 埋设于第 %d 章，状态：%s",
				e.ID, e.Description, e.PlantedAt, status)
		}
		if e.ExpectedPayoff != "" {
			fmt.Fprintf(&b, "（预期回收：%s）", e.ExpectedPayoff)
		}
		if e.From != "" && e.To != "" {
			fmt.Fprintf(&b, "（%s 欠 %s）", e.From, e.To)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderRelationships(entries []domain.RelationshipEntry) string {
	var b strings.Builder
	b.WriteString("# 人物关系\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- **%s ↔ %s**：%s（第 %d 章）\n",
			e.CharacterA, e.CharacterB, e.Relation, e.Chapter)
	}
	return b.String()
}

func renderWorldRules(rules []domain.WorldRule) string {
	grouped := make(map[string][]domain.WorldRule)
	var order []string
	for _, r := range rules {
		cat := r.Category
		if cat == "" {
			cat = "other"
		}
		if _, exists := grouped[cat]; !exists {
			order = append(order, cat)
		}
		grouped[cat] = append(grouped[cat], r)
	}

	var b strings.Builder
	b.WriteString("# 世界观规则\n\n")
	for _, cat := range order {
		fmt.Fprintf(&b, "## %s\n\n", cat)
		for _, r := range grouped[cat] {
			fmt.Fprintf(&b, "- **规则**：%s\n", r.Rule)
			if r.Essence != "" {
				fmt.Fprintf(&b, "  - 定调：%s\n", r.Essence)
			}
			if r.Boundary != "" {
				fmt.Fprintf(&b, "  - 边界：%s\n", r.Boundary)
			}
			if hc := r.HardConstraint; hc != nil {
				scope := ""
				if hc.Entity != "" {
					scope = fmt.Sprintf("（实体：%s）", hc.Entity)
				}
				fmt.Fprintf(&b, "  - 硬约束（机械校验）：%s 字段禁止取值 %q%s\n", hc.Field, hc.Value, scope)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderFactions(factions []domain.Faction) string {
	var b strings.Builder
	b.WriteString("# 势力档案\n\n")
	for _, f := range factions {
		fmt.Fprintf(&b, "- **%s**（%s）\n", f.Name, f.Status)
		if len(f.Aliases) > 0 {
			fmt.Fprintf(&b, "  - 别称：%s\n", strings.Join(f.Aliases, "、"))
		}
		if f.Goal != "" {
			fmt.Fprintf(&b, "  - 诉求：%s\n", f.Goal)
		}
		if f.Relation != "" {
			fmt.Fprintf(&b, "  - 与主线关系：%s\n", f.Relation)
		}
		if f.Location != "" {
			fmt.Fprintf(&b, "  - 驻地：%s\n", f.Location)
		}
		fmt.Fprintf(&b, "  - 最近更新：第 %d 章\n\n", f.LastUpdated)
	}
	return b.String()
}

func renderLocations(locations []domain.Location) string {
	var b strings.Builder
	b.WriteString("# 地点档案\n\n")
	for _, l := range locations {
		owner := ""
		if l.OwnerFaction != "" {
			owner = "（属：" + l.OwnerFaction + "）"
		}
		fmt.Fprintf(&b, "- **%s**〔%s〕%s\n", l.Name, l.Kind, owner)
		if len(l.Aliases) > 0 {
			fmt.Fprintf(&b, "  - 别称：%s\n", strings.Join(l.Aliases, "、"))
		}
		if l.Description != "" {
			fmt.Fprintf(&b, "  - %s\n", l.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ── 章节机械违规事实 ──
//
// commit_chapter 的 rule_violations(user_rules 机械检查的 warning 级结果)持久化,
// editor 评审该章时经 novel_context(chapter=N) 读取并映射进七维评审
// (editor.md §机械检查映射)。writer 返工该章时同样可见。追加式,同章最新一条为准。

// ChapterViolations 一章的机械违规记录。
type ChapterViolations struct {
	Chapter    int               `json:"chapter"`
	Violations []rules.Violation `json:"violations"`
	At         string            `json:"at"`
}

const ruleViolationsFile = "meta/rule_violations.jsonl"

// SaveRuleViolations 追加一章的机械违规(空列表也追加——覆盖旧记录,表示重写后已清)。
func (s *WorldStore) SaveRuleViolations(chapter int, violations []rules.Violation) error {
	rec := ChapterViolations{Chapter: chapter, Violations: violations, At: time.Now().Format(time.RFC3339)}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.io.AppendLine(ruleViolationsFile, append(data, '\n'))
}

// LoadRuleViolations 读取某章最新一条机械违规记录;无记录返回 nil。
func (s *WorldStore) LoadRuleViolations(chapter int) []rules.Violation {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	data, err := os.ReadFile(s.io.path(ruleViolationsFile))
	if err != nil {
		return nil
	}
	var latest []rules.Violation
	var found bool
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec ChapterViolations
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Chapter == chapter {
			latest, found = rec.Violations, true
		}
	}
	if !found {
		return nil
	}
	return latest
}

// ── 章节连续性机械检测事实 ──
//
// commit_chapter 的 continuity_issues(状态回退/关系跳变/出场漏报的机械检测结果)
// 持久化,editor 评审该章时经 novel_context(chapter=N) 顶层 continuity_issues 读取,
// 与 rule_violations 并列消费。追加式,同章最新一条为准。

// ChapterContinuity 一章的连续性检测记录。
type ChapterContinuity struct {
	Chapter int                      `json:"chapter"`
	Issues  *domain.ContinuityIssues `json:"issues,omitempty"`
	At      string                   `json:"at"`
}

const continuityIssuesFile = "meta/continuity_issues.jsonl"

// SaveContinuityIssues 追加一章的连续性检测记录(空结果也追加——覆盖旧记录,表示复测后已清)。
func (s *WorldStore) SaveContinuityIssues(chapter int, issues *domain.ContinuityIssues) error {
	rec := ChapterContinuity{Chapter: chapter, Issues: issues, At: time.Now().Format(time.RFC3339)}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.io.AppendLine(continuityIssuesFile, append(data, '\n'))
}

// LoadContinuityIssues 读取某章最新一条连续性检测记录;无记录或结果为空返回 nil。
func (s *WorldStore) LoadContinuityIssues(chapter int) *domain.ContinuityIssues {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	data, err := os.ReadFile(s.io.path(continuityIssuesFile))
	if err != nil {
		return nil
	}
	var latest *domain.ContinuityIssues
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec ChapterContinuity
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Chapter == chapter {
			latest = rec.Issues
		}
	}
	if latest.Empty() {
		return nil
	}
	return latest
}

// LoadAllContinuityIssues 读取全部章节的最新连续性检测记录（同章 latest-wins，
// 空结果=复测合格已清，不入选）。供 /diag 离线聚合，增量读单章用 LoadContinuityIssues。
func (s *WorldStore) LoadAllContinuityIssues() map[int]*domain.ContinuityIssues {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	out := map[int]*domain.ContinuityIssues{}
	data, err := os.ReadFile(s.io.path(continuityIssuesFile))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec ChapterContinuity
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Issues.Empty() {
			delete(out, rec.Chapter) // 复测合格：清除该章旧记录的影响
			continue
		}
		out[rec.Chapter] = rec.Issues
	}
	return out
}

// ── 一致性自审结论 ──
//
// writer 经 report_consistency 工具把 check_consistency 的 LLM 自审结论落盘
// (P3-8 缺口:此前结论只存在于当轮上下文,压缩/换窗口即丢失)。追加式,同章
// 最新一条为准;editor 评审该章时经 novel_context(chapter=N) 顶层
// consistency_check 回溯"writer 当时认为哪些地方可疑"。

// ConsistencyCheckIssue 单条自审发现。
type ConsistencyCheckIssue struct {
	Type        string `json:"type"`        // 问题类型(如 state_fact / timeline / foreshadow / contract)
	Severity    string `json:"severity"`    // warning | error
	Description string `json:"description"` // 问题描述
}

// ConsistencyCheckRecord 一章的自审结论。
type ConsistencyCheckRecord struct {
	Chapter int                     `json:"chapter"`
	Passed  bool                    `json:"passed"`
	Issues  []ConsistencyCheckIssue `json:"issues,omitempty"`
	At      string                  `json:"at"`
}

const consistencyChecksFile = "meta/consistency_checks.jsonl"

// SaveConsistencyCheck 追加一章的自审结论(同章 latest-wins)。
func (s *WorldStore) SaveConsistencyCheck(rec ConsistencyCheckRecord) error {
	if rec.At == "" {
		rec.At = time.Now().Format(time.RFC3339)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.io.AppendLine(consistencyChecksFile, append(data, '\n'))
}

// LoadConsistencyCheck 读取某章最新一条自审结论;无记录返回 nil。
func (s *WorldStore) LoadConsistencyCheck(chapter int) *ConsistencyCheckRecord {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	data, err := os.ReadFile(s.io.path(consistencyChecksFile))
	if err != nil {
		return nil
	}
	var latest *ConsistencyCheckRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec ConsistencyCheckRecord
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Chapter == chapter {
			latest = &rec
		}
	}
	return latest
}
