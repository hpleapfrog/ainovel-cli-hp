package domain

// Novel 小说元信息。
type Novel struct {
	Name          string `json:"name"`
	TotalChapters int    `json:"total_chapters"`
}

// OutlineEntry 大纲条目，对应一章。
type OutlineEntry struct {
	Chapter   int      `json:"chapter"`
	Title     string   `json:"title"`
	CoreEvent string   `json:"core_event"`
	Hook      string   `json:"hook"`
	Scenes    []string `json:"scenes"`
}

// Character 角色档案。
type Character struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"` // 别名/称号/绰号（如"废物少年"、"炎哥"）
	Role        string   `json:"role"`
	Description string   `json:"description"`
	Arc         string   `json:"arc"`
	Traits      []string `json:"traits"`
	Tier        string   `json:"tier,omitempty"` // core / important / secondary / decorative（默认 important）
}

// VolumeOutline 卷级大纲（长篇分层模式）。
type VolumeOutline struct {
	Index int          `json:"index"`
	Title string       `json:"title"`
	Theme string       `json:"theme"`           // 本卷核心冲突/主题
	Final bool         `json:"final,omitempty"` // 收官卷：全书在本卷收束（架构师 append_volume 时宣告）
	Arcs  []ArcOutline `json:"arcs"`
}

// IsExpanded 判断卷是否已展开（有弧级结构）。
func (v *VolumeOutline) IsExpanded() bool { return len(v.Arcs) > 0 }

// FinaleVolume 返回已宣告的收官卷序号，未宣告返回 0。
// 收官事实 = "最后一卷带 Final 标记"：宣告后全书进入收束态（规划收线、终卷结构
// 写完即完结）；若此后又追加了未标记的新卷，新卷成为最后一卷，收束态自然解除——
// 因此无需撤销工具，状态永远可从大纲数据推导。
func FinaleVolume(volumes []VolumeOutline) int {
	if n := len(volumes); n > 0 && volumes[n-1].Final {
		return volumes[n-1].Index
	}
	return 0
}

// StoryCompass 终局方向指南针，替代固定的骨架卷列表。
// Architect 在每次卷边界时可更新，允许故事方向随创作演化。
type StoryCompass struct {
	EndingDirection string   `json:"ending_direction"`          // 终局方向（主题性描述）
	OpenThreads     []string `json:"open_threads,omitempty"`    // 活跃长线（需收束才能结局）
	EstimatedScale  string   `json:"estimated_scale,omitempty"` // 模糊规模（如"预计 4-6 卷"）
	LastUpdated     int      `json:"last_updated,omitempty"`    // 更新时的已完成章节数
}

// Faction 势力实体（W1）：宗门/家族/国家/帮派等"水下世界"的组织。
//
// 与既有纪律对齐的极简扁平实体——不做通用图模型。势力档案由 architect 在
// 规划/结构操作时全量维护（save_foundation type=factions）；兴衰变化（如
// "青云宗被灭"）走 state_changes 管道登记（Entity=势力名、Field=status、
// distance 标注距主角远近），机械检测与角色状态回退共用同一套。
type Faction struct {
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"`
	Goal     string   `json:"goal"`     // 势力诉求
	Relation string   `json:"relation"` // 与主角/主线的关系
	Status   string   `json:"status"`   // 兴衰状态（如 鼎盛/崛起/衰败/被灭）
	// Location 关联地点名（对应 Location.Name；一个主驻地）。
	Location string `json:"location,omitempty"`
	// LastUpdated 状态最近更新的章节号（architect 落盘时由工具层强制覆盖为当前已完成章）。
	LastUpdated int `json:"last_updated"`
}

// Location 地点实体（W1）：城/域/秘境/宗门驻地等。给"谁在哪、哪章去了哪"
// 提供结构化锚点（与 StateChange.Field=location 的申报互相印证）。
type Location struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Kind    string   `json:"kind"` // 城 / 域 / 秘境 / 宗门驻地 / 国 / 其他
	// Description 场景级可感信息：这里长什么样、什么人怎么活（写手直接能用）。
	Description string `json:"description"`
	// OwnerFaction 所属势力名（对应 Faction.Name）；空=无主。
	OwnerFaction string `json:"owner_faction,omitempty"`
}

// FactionUpdate 是按章增量更新势力档案的申报（commit_chapter.faction_updates）。
//
// writer 在 commit 时申报正文里发生的势力变化，工具层做确定性 Upsert 合并
// （只更新提供的字段、LastUpdated 代码填、自动派生 state_changes 历史轨迹）；
// 没有 LLM 决策环节——这是事实同步，不是设定创作。全量覆盖仍走
// save_foundation(type=factions)（architect 规划期/结构操作）。
type FactionUpdate struct {
	// Name 势力正式名或其别名（按 factions.json 的 name+aliases 查找）。
	Name string `json:"name"`
	// Status 兴衰状态（鼎盛/崛起/衰败/被灭等）；新势力必填。
	Status string `json:"status,omitempty"`
	// Goal 势力诉求（变化时提供）。
	Goal string `json:"goal,omitempty"`
	// Relation 与主线关系（变化时提供）。
	Relation string `json:"relation,omitempty"`
	// Location 驻地（变化时提供）。
	Location string `json:"location,omitempty"`
	// Distance 距主角距离标注（派生到状态历史轨迹，near/mid/far/fog）。
	Distance string `json:"distance,omitempty"`
}

// LocationUpdate 是按章增量更新地点档案的申报（commit_chapter.location_updates）。
// 与 FactionUpdate 同款语义：writer 申报 → 工具层确定性 Upsert 合并，无 LLM 决策。
type LocationUpdate struct {
	// Name 地点正式名或其别名（按 locations.json 的 name+aliases 查找）。
	Name string `json:"name"`
	// Kind 地点类型（城/域/秘境/宗门驻地/国/其他）；新地点必填。
	Kind string `json:"kind,omitempty"`
	// Description 场景级可感信息（变化时提供）。
	Description string `json:"description,omitempty"`
	// OwnerFaction 所属势力（易主时提供）。
	OwnerFaction string `json:"owner_faction,omitempty"`
}

// ArcOutline 弧级大纲。
type ArcOutline struct {
	Index             int            `json:"index"` // 卷内弧序号
	Title             string         `json:"title"`
	Goal              string         `json:"goal"`                         // 弧目标（起承转合）
	EstimatedChapters int            `json:"estimated_chapters,omitempty"` // 骨架弧的预估章数（展开后清零）
	Chapters          []OutlineEntry `json:"chapters"`
}

// IsExpanded 判断弧是否已展开（有详细章节）。
func (a *ArcOutline) IsExpanded() bool { return len(a.Chapters) > 0 }

// TotalChapters 计算分层大纲的当前规划总章数。
// 已展开弧按真实章节数计，骨架弧按 EstimatedChapters 计。
// Progress.TotalChapters 用它判断长篇上下文策略；真正可写章节仍来自 FlattenOutline。
func TotalChapters(volumes []VolumeOutline) int {
	n := 0
	for _, v := range volumes {
		for _, a := range v.Arcs {
			if a.IsExpanded() {
				n += len(a.Chapters)
			} else {
				n += a.EstimatedChapters
			}
		}
	}
	return n
}

// FlattenOutline 将分层大纲展开为扁平章节列表，保持全局章节号连续。
func FlattenOutline(volumes []VolumeOutline) []OutlineEntry {
	var result []OutlineEntry
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			for _, e := range a.Chapters {
				e.Chapter = ch
				result = append(result, e)
				ch++
			}
		}
	}
	return result
}

// WorldRule 世界观规则条目。
type WorldRule struct {
	Category string `json:"category"` // magic / technology / geography / society / history_culture / existence / information / other
	Rule     string `json:"rule"`     // 规则描述
	Boundary string `json:"boundary"` // 不可违反的边界
	// Essence 可选：≤50 字定调句，用本书专有名词写"这一维度的独特性"。
	// 例：「灵气循雾脉流转，离脉则力竭——力量有了代价。」空泛表述（"有独特的灵气体系"）不如不写。
	Essence string `json:"essence,omitempty"`
	// HardConstraint 可选:把"不可违反"从 LLM 自觉升级为可机械校验的硬约束(W2)。
	// 软设定(文风/氛围/价值观)仍走 check_consistency + editor 评审,不落这里。
	HardConstraint *HardConstraint `json:"hard_constraint,omitempty"`
}

// HardConstraint 世界规则的可机械校验硬约束。最小形态=「禁则 + 受控字段」:
// 某受控字段(与 StateChange.Field 枚举对齐)不可取某值。commit 时由机械检测
// 对照 state_changes 校验,结果并入 continuity_issues。表达力够了再扩,不退化成
// 无法校验的自由文本。
type HardConstraint struct {
	// Kind 约束类型。当前仅支持 prohibition(禁止):受控字段不可取 Value。
	Kind string `json:"kind"` // prohibition
	// Field 受控字段,与 StateField 枚举对齐(realm/location/status/power/rank/relation/other)。
	Field string `json:"field"`
	// Value 禁止值(prohibition:new_value 与之相等即违规)。
	Value string `json:"value"`
	// Entity 可选:限定实体名(角色/组织);空=对所有实体生效。
	Entity string `json:"entity,omitempty"`
	// ScanText 可选:除了对照 state_changes 声明,commit 时还扫描章节正文——
	// 正文出现 Value 原文即记一条 warning 级事实(如"魔法必须吟唱"配 Value
	// "瞬发魔法",正文写了"瞬发魔法"就抓得到)。仅适合精确词/短语级禁则,
	// 描述性句子(整句)不适合,误报由 editor 裁定兜底。
	ScanText bool `json:"scan_text,omitempty"`
}
