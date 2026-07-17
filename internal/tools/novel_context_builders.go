package tools

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/stylestat"
)

type contextBuildState struct {
	chapter         int
	profile         domain.ContextProfile
	progress        *domain.Progress
	runMeta         *domain.RunMeta
	currentEntry    *domain.OutlineEntry
	chapterPlan     *domain.ChapterPlan
	storyThreads    []domain.RecallItem
	foreshadow      []domain.ForeshadowEntry
	relationships   []domain.RelationshipEntry
	allStateChanges []domain.StateChange
	styleRules      *domain.WritingStyleRules
}

type chapterContextEnvelope struct {
	Working    map[string]any
	Episodic   map[string]any
	References map[string]any
	Selected   map[string]any
}

type architectContextEnvelope struct {
	Planning   map[string]any
	Foundation map[string]any
	References map[string]any
}

func newChapterContextEnvelope() chapterContextEnvelope {
	return chapterContextEnvelope{
		Working:    make(map[string]any),
		Episodic:   make(map[string]any),
		References: make(map[string]any),
		Selected:   make(map[string]any),
	}
}

func newArchitectContextEnvelope() architectContextEnvelope {
	return architectContextEnvelope{
		Planning:   make(map[string]any),
		Foundation: make(map[string]any),
		References: make(map[string]any),
	}
}

func (e chapterContextEnvelope) apply(result map[string]any) {
	// 合并而非替换：Execute 的章节路径会先后 apply 两个信封（seed + buildChapterContext），
	// 整体赋值会让第二次 apply 丢弃 seed 的容器内容，working_memory.* 等 canonical
	// 路径随之失效（prompt 指针指向空气，模型只能靠顶层镜像模糊容错）。
	mergeEnvelopeSection(result, "working_memory", e.Working)
	mergeEnvelopeSection(result, "episodic_memory", e.Episodic)
	mergeEnvelopeSection(result, "reference_pack", e.References)
	if len(e.Selected) > 0 {
		mergeEnvelopeSection(result, "selected_memory", e.Selected)
	}
	mergeContextSection(result, e.Working)
	mergeContextSection(result, e.Episodic)
	mergeContextSection(result, e.References)
}

// mergeEnvelopeSection 把 section 合并进 result[key] 的既有容器；容器不存在时直接挂载。
func mergeEnvelopeSection(result map[string]any, key string, section map[string]any) {
	if existing, ok := result[key].(map[string]any); ok {
		for k, v := range section {
			existing[k] = v
		}
		return
	}
	result[key] = section
}

func (e architectContextEnvelope) apply(result map[string]any) {
	result["planning_memory"] = e.Planning
	result["foundation_memory"] = e.Foundation
	result["reference_pack"] = e.References
	mergeContextSection(result, e.Planning)
	mergeContextSection(result, e.Foundation)
	mergeContextSection(result, e.References)
}

func mergeContextSection(result map[string]any, section map[string]any) {
	for key, value := range section {
		result[key] = value
	}
}

// buildProgressStatus 在 Architect 不传 chapter 时返回进度摘要。
// Writer/Editor 的章节路径不需要这些信息，避免干扰写作。
func (t *ContextTool) buildProgressStatus(result map[string]any) {
	progress, err := t.store.Progress.Load()
	if err != nil || progress == nil {
		return
	}
	status := map[string]any{
		"phase":              string(progress.Phase),
		"flow":               string(progress.Flow),
		"completed_chapters": len(progress.CompletedChapters),
		"total_chapters":     progress.TotalChapters,
		"next_chapter":       progress.NextChapter(),
		"total_word_count":   progress.TotalWordCount,
	}
	if progress.InProgressChapter > 0 {
		status["in_progress_chapter"] = progress.InProgressChapter
	}
	if len(progress.PendingRewrites) > 0 {
		status["pending_rewrites"] = progress.PendingRewrites
		status["rewrite_reason"] = progress.RewriteReason
	}
	if progress.Layered {
		status["layered"] = true
		status["current_volume"] = progress.CurrentVolume
		status["current_arc"] = progress.CurrentArc
	}
	if progress.Phase == domain.PhaseComplete {
		status["finished"] = true
	}
	result["progress_status"] = status
}

// buildUserRules 把合并后的 Bundle 注入 working_memory.user_rules（canonical 路径）。
//
// 单点注入：writer / editor / architect 任一路径调用 novel_context
// 都能在 working_memory.user_rules 拿到一致的偏好。architect 路径原本没有 working_memory，
// 由本函数按需新建（仅装 user_rules）；chapter > 0 路径下 working_memory 已存在，直接嵌入。
//
// 即便 Bundle 为空也注入，保持字段稳定，避免 LLM 看到 user_rules=null 而走异常分支。
//
// 注入策略：只给 LLM 看 structured + preferences——这两项才是创作时需要遵循的偏好。
// sources / conflicts 是诊断信息（用户冲突排查），不进 LLM；由 CLI 启动诊断面板按需展示。
func (t *ContextTool) buildUserRules(result map[string]any) {
	snap, err := t.store.UserRules.Load()
	if err != nil || snap == nil {
		// 快照尚未初始化时使用代码内置默认，保证机械底线（字数/禁语/疲劳词）始终存在。
		def := rules.BuildSnapshot([]rules.Candidate{rules.SystemDefaults()})
		snap = &def
	}
	working, ok := result["working_memory"].(map[string]any)
	if !ok {
		working = map[string]any{}
		result["working_memory"] = working
	}
	working["user_rules"] = snap.Payload()
}

func (t *ContextTool) buildSimulationProfile(result map[string]any, sectionKey string, warn func(string, error)) {
	profile, err := t.store.Simulation.Load()
	if err != nil {
		warn("simulation_profile", err)
		return
	}
	compact := domain.CompactSimulationProfile(profile)
	if compact == nil {
		return
	}
	section, ok := result[sectionKey].(map[string]any)
	if !ok {
		section = map[string]any{}
		result[sectionKey] = section
	}
	section["simulation_profile"] = compact
	result["simulation_profile"] = true
}

func (t *ContextTool) buildBaseContext(result map[string]any, warn func(string, error)) {
	if premise, err := t.store.Outline.LoadPremise(); err == nil && premise != "" {
		result["premise"] = premise
		if sections := parsePremiseSections(premise); len(sections) > 0 {
			result["premise_sections"] = sections
		}
		tier := domain.PlanningTier("")
		if meta, err := t.store.RunMeta.Load(); err == nil && meta != nil {
			tier = meta.PlanningTier
		}
		result["premise_structure"] = premiseStructure(premise, tier)
	} else {
		warn("premise", err)
	}
	if outline, err := t.store.Outline.LoadOutline(); err == nil && outline != nil {
		result["outline"] = outline
	} else {
		warn("outline", err)
	}
	if rules, err := t.store.World.LoadWorldRules(); err == nil && len(rules) > 0 {
		result["world_rules"] = rules
	} else {
		warn("world_rules", err)
	}
}

func (t *ContextTool) prepareChapterContext(chapter int, envelope *chapterContextEnvelope, warn func(string, error)) contextBuildState {
	state := contextBuildState{
		chapter: chapter,
		profile: domain.NewContextProfile(0),
	}

	progress, err := t.store.Progress.Load()
	warn("progress", err)
	runMeta, err := t.store.RunMeta.Load()
	warn("run_meta", err)
	state.progress = progress
	state.runMeta = runMeta

	if runMeta != nil && runMeta.PlanningTier != "" {
		envelope.Episodic["planning_tier"] = runMeta.PlanningTier
	}
	if progress != nil && progress.TotalChapters > 0 {
		state.profile = domain.NewContextProfile(progress.TotalChapters)
	}
	if progress == nil || !progress.Layered {
		state.profile.Layered = false
	}

	currentEntry, currentEntryErr := t.store.Outline.GetChapterOutline(chapter)
	if currentEntryErr == nil {
		envelope.Working["current_chapter_outline"] = currentEntry
	} else {
		warn("current_chapter_outline", currentEntryErr)
	}
	state.currentEntry = currentEntry

	chapterPlan, chapterPlanErr := t.store.Drafts.LoadChapterPlan(chapter)
	if chapterPlanErr == nil && chapterPlan != nil {
		envelope.Working["chapter_plan"] = chapterPlan
		if len(chapterPlan.Contract.RequiredBeats) > 0 ||
			len(chapterPlan.Contract.ForbiddenMoves) > 0 ||
			len(chapterPlan.Contract.ContinuityChecks) > 0 ||
			len(chapterPlan.Contract.EvaluationFocus) > 0 ||
			chapterPlan.Contract.EmotionTarget != "" ||
			len(chapterPlan.Contract.PayoffPoints) > 0 ||
			chapterPlan.Contract.HookGoal != "" {
			envelope.Working["chapter_contract"] = chapterPlan.Contract
		}
	} else {
		warn("chapter_plan", chapterPlanErr)
	}
	state.chapterPlan = chapterPlan

	// 是否正在重写本章：决定 novel_context 是否补"重写专用"事实。
	isRewrite := progress != nil && slices.Contains(progress.PendingRewrites, chapter)

	// 暴露 draft 是否已存在的事实：让 writer 被重派时能自行判断跳过重写还是覆盖。
	// 只暴露 exists + word_count，不注入正文（正文让 writer 按需用 read_chapter 拉）。
	if _, draftWords, draftErr := t.store.Drafts.LoadChapterContent(chapter); draftErr == nil && draftWords > 0 {
		envelope.Working["chapter_draft"] = map[string]any{
			"exists":     true,
			"word_count": draftWords,
		}
	} else if draftErr != nil {
		warn("chapter_draft", draftErr)
	}

	// 重写时把"为什么改 + 改哪里"交给 writer：理由来自返工队列，具体批评来自本章评审
	// （selectReviewLessons 只召回 chapter-1..chapter-3，恰好漏掉本章本身，writer 又无读评审的工具）。
	// 正文不在此注入——保持"正文按需 read_chapter 拉"的约定不破。
	if isRewrite {
		brief := map[string]any{"reason": progress.RewriteReason}
		if review, reviewErr := t.store.World.LoadReview(chapter); reviewErr == nil && review != nil {
			if review.Summary != "" {
				brief["review_summary"] = review.Summary
			}
			if len(review.Issues) > 0 {
				brief["issues"] = review.Issues
			}
			if len(review.ContractMisses) > 0 {
				brief["contract_misses"] = review.ContractMisses
			}
		} else if reviewErr != nil {
			warn("rewrite_review", reviewErr)
		}
		envelope.Working["rewrite_brief"] = brief
	}

	foreshadow, foreshadowErr := t.store.World.LoadActiveForeshadow()
	warn("foreshadow_ledger", foreshadowErr)
	state.foreshadow = foreshadow

	relationships, relErr := t.store.World.LoadRelationships()
	warn("relationship_state", relErr)
	if len(relationships) > 0 {
		envelope.Episodic["relationship_state"] = relationships
	}
	state.relationships = relationships

	allStateChanges, scErr := t.store.World.LoadStateChanges()
	warn("recent_state_changes", scErr)
	state.allStateChanges = allStateChanges
	if len(allStateChanges) > 0 {
		start := max(chapter-2, 1)
		var recent []domain.StateChange
		for _, c := range allStateChanges {
			if c.Chapter >= start && c.Chapter < chapter {
				recent = append(recent, c)
			}
		}
		if len(recent) > 0 {
			envelope.Episodic["recent_state_changes"] = recent
		}
	}

	styleRules, styleErr := t.store.World.LoadStyleRules()
	warn("style_rules", styleErr)
	state.styleRules = styleRules
	state.storyThreads = t.selectStoryThreads(state)
	if len(state.storyThreads) > 0 && len(state.storyThreads) < storyThreadRecallMinSelected {
		state.storyThreads = nil
	}

	return state
}

func (t *ContextTool) buildChapterContext(result map[string]any, state contextBuildState, warn func(string, error)) {
	envelope := newChapterContextEnvelope()
	result["memory_policy"] = domain.NewChapterMemoryPolicy(state.progress, state.profile, state.currentEntry != nil)

	if state.profile.Layered {
		t.loadLayeredCharacters(envelope.Episodic, state.chapter, warn)
	} else {
		t.loadFilteredCharacters(envelope.Episodic, state.chapter, warn)
	}

	t.buildChapterEpisodicMemory(&envelope, state, warn)
	t.buildChapterWorkingMemory(&envelope, state, warn)
	t.buildChapterReferencePack(&envelope, state)
	t.buildChapterSelectedMemory(&envelope, state, warn)
	t.buildStyleStats(&envelope, state)
	envelope.apply(result)
}

// buildStyleStats 对全部已完成章节做全书级风格统计，注入 episodic_memory.style_stats。
// 弧内评审窗口对"章均几十次的句式 tic、章末形态同构、跨章复读"天然失明，只有
// 全书统计能暴露——统计归代码（确定性），裁定归 LLM（editor 在 aesthetic 维度
// 按数字判分，writer 据此自避免）。章数不足时 stylestat 返回 nil，不注入。
func (t *ContextTool) buildStyleStats(envelope *chapterContextEnvelope, state contextBuildState) {
	if state.progress == nil || len(state.progress.CompletedChapters) == 0 {
		return
	}
	completed := slices.Clone(state.progress.CompletedChapters)
	slices.Sort(completed)
	chapters := make([]string, 0, len(completed))
	for _, ch := range completed {
		// 个别章读取失败跳过：统计是 best-effort 事实，不因单章缺失放弃全书视野
		if text, err := t.store.Drafts.LoadChapterText(ch); err == nil && text != "" {
			chapters = append(chapters, text)
		}
	}

	var titles []string
	if outline, err := t.store.Outline.LoadOutline(); err == nil {
		for _, entry := range outline {
			titles = append(titles, entry.Title)
		}
	}

	stats := stylestat.Compute(stylestat.Input{
		Chapters:  chapters,
		Titles:    titles,
		Stopwords: t.styleStopwords(),
	})
	if stats == nil {
		return
	}
	envelope.Episodic["style_stats"] = stats
}

// styleStopwords 收集角色名与别名供短语挖掘过滤——出场人名天然高频，不是文风问题。
func (t *ContextTool) styleStopwords() []string {
	var words []string
	if chars, err := t.store.Characters.Load(); err == nil {
		for _, c := range chars {
			words = append(words, c.Name)
			words = append(words, c.Aliases...)
		}
	}
	if cast, err := t.store.Cast.RecentActive(50); err == nil {
		for _, e := range cast {
			words = append(words, e.Name)
			words = append(words, e.Aliases...)
		}
	}
	return words
}

func (t *ContextTool) buildChapterWorkingMemory(envelope *chapterContextEnvelope, state contextBuildState, warn func(string, error)) {
	if next, err := t.store.Outline.GetChapterOutline(state.chapter + 1); err == nil && next != nil {
		envelope.Working["next_chapter_outline"] = next
	}

	if state.profile.Layered {
		t.loadLayeredSummaries(envelope.Working, state.chapter, state.profile.SummaryWindow, warn)
		// 收官纪律：本章属于已宣告的收官卷时注入，防 writer 在收官段临章再开新钩子
		//（收官卷写完即自动完结，此时新埋的伏笔永远没有机会回收）。
		if volumes, err := t.store.Outline.LoadLayeredOutline(); err == nil {
			if fv := domain.FinaleVolume(volumes); fv > 0 {
				if b, berr := t.store.Outline.CheckArcBoundary(state.chapter); berr == nil && b != nil && b.Volume == fv {
					envelope.Working["finale"] = "本卷为全书收官卷：不再新开长线或埋新伏笔，优先回收既有伏笔、收拢关系线，按大纲把故事推向终局。"
				}
			}
		}
	} else {
		if summaries, err := t.store.Summaries.LoadRecentSummaries(state.chapter, state.profile.SummaryWindow); err == nil && len(summaries) > 0 {
			envelope.Working["recent_summaries"] = summaries
		} else {
			warn("recent_summaries", err)
		}
	}

	if timeline, err := t.store.World.LoadRecentTimeline(state.chapter, state.profile.TimelineWindow); err == nil && len(timeline) > 0 {
		envelope.Working["timeline"] = timeline
	} else {
		warn("timeline", err)
	}

	if state.progress != nil {
		checkpoint := map[string]any{
			"in_progress_chapter": state.progress.InProgressChapter,
		}
		if len(state.progress.StrandHistory) > 0 {
			checkpoint["strand_history"] = state.progress.StrandHistory
		}
		if len(state.progress.HookHistory) > 0 {
			checkpoint["hook_history"] = state.progress.HookHistory
		}
		envelope.Working["checkpoint"] = checkpoint
	}

	if state.chapter > 1 {
		if prevText, err := t.store.Drafts.LoadChapterText(state.chapter - 1); err == nil && prevText != "" {
			runes := []rune(prevText)
			if len(runes) > 800 {
				runes = runes[len(runes)-800:]
			}
			envelope.Working["previous_tail"] = string(runes)
		}
	}

	// 伏笔到期清单：休眠超过 domain.ForeshadowDueChapters 的活跃伏笔，按最久未动降序。
	// 纯事实字段（不含指令）——哪条该推进，writer 不用再脑算章数。
	if due := foreshadowDue(state.foreshadow, state.chapter); len(due) > 0 {
		envelope.Working["foreshadow_due"] = due
	}

	t.buildContinuityCard(envelope, state, warn)
}

func (t *ContextTool) buildChapterSelectedMemory(envelope *chapterContextEnvelope, state contextBuildState, warn func(string, error)) {
	if len(state.storyThreads) > 0 {
		envelope.Selected["story_threads"] = state.storyThreads
	}
	if lessons := t.selectReviewLessons(state.chapter, warn); len(lessons) > 0 {
		envelope.Selected["review_lessons"] = lessons
	}
}

func (t *ContextTool) buildChapterEpisodicMemory(envelope *chapterContextEnvelope, state contextBuildState, warn func(string, error)) {
	if len(state.foreshadow) > 0 && len(state.storyThreads) == 0 {
		envelope.Episodic["foreshadow_ledger"] = foreshadowWithDormancy(state.foreshadow, state.chapter)
	}

	// 配角名册：召回最近活跃的次要角色，让 Writer 在引入旧角色时能保持口吻/定位一致
	// 不召回所有条目（长篇会膨胀），只给最近活跃的前 N 个，按 LastSeenChapter 倒序
	if recentCast, err := t.store.Cast.RecentActive(15); err == nil && len(recentCast) > 0 {
		simplified := make([]map[string]any, 0, len(recentCast))
		for _, e := range recentCast {
			item := map[string]any{
				"name":             e.Name,
				"first_seen":       e.FirstSeenChapter,
				"last_seen":        e.LastSeenChapter,
				"appearance_count": e.AppearanceCount,
			}
			if e.BriefRole != "" {
				item["brief_role"] = e.BriefRole
			}
			if len(e.Aliases) > 0 {
				item["aliases"] = e.Aliases
			}
			simplified = append(simplified, item)
		}
		envelope.Episodic["recent_cast"] = simplified
	} else if err != nil {
		warn("recent_cast", err)
	}

	if state.progress != nil && state.progress.TotalChapters > 30 && state.currentEntry != nil {
		if related := t.buildRelatedChapters(
			state.chapter,
			state.currentEntry,
			state.foreshadow,
			state.relationships,
			state.allStateChanges,
		); len(related) > 0 {
			envelope.Episodic["related_chapters"] = related
		}
	}

	if state.profile.Layered && state.progress != nil {
		pos := map[string]any{
			"volume": state.progress.CurrentVolume,
			"arc":    state.progress.CurrentArc,
		}
		if volumes, err := t.store.Outline.LoadLayeredOutline(); err == nil {
			globalCh := 1
			for _, v := range volumes {
				if v.Index == state.progress.CurrentVolume {
					pos["volume_title"] = v.Title
					pos["volume_theme"] = v.Theme
				}
				for _, arc := range v.Arcs {
					if v.Index == state.progress.CurrentVolume && arc.Index == state.progress.CurrentArc {
						pos["arc_title"] = arc.Title
						pos["arc_goal"] = arc.Goal
						if n := len(arc.Chapters); n > 0 {
							pos["arc_total_chapters"] = n
							pos["arc_chapter_index"] = state.chapter - globalCh + 1
						}
					}
					globalCh += len(arc.Chapters)
				}
			}
		} else {
			warn("layered_outline", err)
		}
		envelope.Episodic["position"] = pos
	}
}

func (t *ContextTool) buildChapterReferencePack(envelope *chapterContextEnvelope, state contextBuildState) {
	if state.styleRules != nil {
		envelope.References["style_rules"] = state.styleRules
	} else {
		var maxCompleted int
		if state.progress != nil {
			maxCompleted = maxCompletedChapter(state.progress.CompletedChapters)
		}
		if anchors := t.store.Drafts.ExtractStyleAnchors(3, maxCompleted); len(anchors) > 0 {
			envelope.References["style_anchors"] = anchors
		}

		if state.currentEntry != nil {
			var voiceSamples []map[string]any
			chars, _ := t.store.Characters.Load()
			for _, c := range chars {
				if c.Tier == "secondary" || c.Tier == "decorative" {
					continue
				}
				samples := t.store.Drafts.ExtractDialogue(c.Name, c.Aliases, 3, maxCompleted)
				if len(samples) > 0 {
					voiceSamples = append(voiceSamples, map[string]any{
						"character": c.Name,
						"samples":   samples,
					})
				}
				if len(voiceSamples) >= 5 {
					break
				}
			}
			if len(voiceSamples) > 0 {
				envelope.References["voice_samples"] = voiceSamples
			}
		}
	}

	envelope.References["references"] = t.writerReferences(state.chapter)
}

func (t *ContextTool) buildArchitectContext(result map[string]any, warn func(string, error)) {
	envelope := newArchitectContextEnvelope()
	result["memory_policy"] = domain.NewArchitectMemoryPolicy()
	t.buildArchitectPlanning(&envelope, warn)
	t.buildArchitectFoundation(&envelope, warn)
	t.buildArchitectReferences(&envelope, warn)
	envelope.apply(result)
}

func (t *ContextTool) buildArchitectPlanning(envelope *architectContextEnvelope, warn func(string, error)) {
	runMeta, err := t.store.RunMeta.Load()
	warn("run_meta", err)
	if runMeta != nil && runMeta.PlanningTier != "" {
		envelope.Planning["planning_tier"] = runMeta.PlanningTier
	}

	var layered []domain.VolumeOutline
	if l, err := t.store.Outline.LoadLayeredOutline(); err == nil && len(l) > 0 {
		layered = l
		envelope.Planning["layered_outline"] = layered
		var skeletonArcs []map[string]any
		for _, v := range layered {
			for _, a := range v.Arcs {
				if !a.IsExpanded() {
					skeletonArcs = append(skeletonArcs, map[string]any{
						"volume":             v.Index,
						"arc":                a.Index,
						"title":              a.Title,
						"goal":               a.Goal,
						"estimated_chapters": a.EstimatedChapters,
					})
				}
			}
		}
		if len(skeletonArcs) > 0 {
			envelope.Planning["skeleton_arcs"] = skeletonArcs
		}
	} else {
		warn("layered_outline", err)
	}

	var compass *domain.StoryCompass
	if c, err := t.store.Outline.LoadCompass(); err == nil && c != nil {
		compass = c
		envelope.Planning["compass"] = compass
	} else {
		warn("compass", err)
	}
	if volSummaries, err := t.store.Summaries.LoadAllVolumeSummaries(); err == nil && len(volSummaries) > 0 {
		envelope.Planning["volume_summaries"] = volSummaries
	} else {
		warn("volume_summaries", err)
	}

	// completion_signals 把"全书是否该结尾"的关键事实集中呈现，
	// 让架构师在裁定 complete_book / append_volume 时一眼看到对照面。
	// 散落在 progress / compass / foreshadow / layered_outline 里靠 LLM 脑算容易漏。
	envelope.Planning["completion_signals"] = t.completionSignals(layered, compass)
}

func (t *ContextTool) completionSignals(layered []domain.VolumeOutline, compass *domain.StoryCompass) map[string]any {
	signals := map[string]any{}
	if progress, _ := t.store.Progress.Load(); progress != nil {
		signals["completed_chapters"] = len(progress.CompletedChapters)
		signals["total_word_count"] = progress.TotalWordCount
		signals["phase"] = string(progress.Phase)
	}
	if len(layered) > 0 {
		signals["planned_chapters"] = len(domain.FlattenOutline(layered))
		signals["volumes_total"] = len(layered)
		if fv := domain.FinaleVolume(layered); fv > 0 {
			signals["final_volume"] = fv
		}
	}
	if compass != nil {
		if compass.EstimatedScale != "" {
			signals["compass_estimated_scale"] = compass.EstimatedScale
		}
		signals["open_threads_count"] = len(compass.OpenThreads)
	}
	if active, err := t.store.World.LoadActiveForeshadow(); err == nil {
		signals["active_foreshadow_count"] = len(active)
	}
	return signals
}

func (t *ContextTool) buildArchitectFoundation(envelope *architectContextEnvelope, warn func(string, error)) {
	if premise, err := t.store.Outline.LoadPremise(); err == nil && premise != "" {
		if sections := parsePremiseSections(premise); len(sections) > 0 {
			envelope.Foundation["premise_sections"] = sections
		}
		tier := domain.PlanningTier("")
		if meta, err := t.store.RunMeta.Load(); err == nil && meta != nil {
			tier = meta.PlanningTier
		}
		envelope.Foundation["premise_structure"] = premiseStructure(premise, tier)
	} else {
		warn("premise", err)
	}

	if chars, err := t.store.Characters.Load(); err == nil && chars != nil {
		envelope.Foundation["characters"] = chars
	} else {
		warn("characters", err)
	}

	if snapshots, err := t.store.Characters.LoadLatestSnapshots(); err == nil && len(snapshots) > 0 {
		envelope.Foundation["character_snapshots"] = snapshots
	} else {
		warn("character_snapshots", err)
	}
	if rules, err := t.store.World.LoadWorldRules(); err == nil && len(rules) > 0 {
		envelope.Foundation["world_rules"] = rules
	} else {
		warn("world_rules", err)
	}
	if foreshadow, err := t.store.World.LoadActiveForeshadow(); err == nil && len(foreshadow) > 0 {
		envelope.Foundation["foreshadow_ledger"] = foreshadowWithDormancy(foreshadow, t.currentChapterHint())
	} else {
		warn("foreshadow_ledger", err)
	}
	envelope.Foundation["foundation_status"] = t.foundationStatus()
	// Writer 反馈池:commit_chapter 落盘的大纲偏离/建议,规划下一弧/卷时必须参考;
	// expand_arc / append_volume / update_compass 成功后自动清空(已消费)。
	if fbs := t.store.Outline.LoadPendingOutlineFeedback(); len(fbs) > 0 {
		envelope.Foundation["writer_feedback"] = fbs
	}
}

func (t *ContextTool) buildArchitectReferences(envelope *architectContextEnvelope, warn func(string, error)) {
	if styleRules, err := t.store.World.LoadStyleRules(); err == nil && styleRules != nil {
		envelope.References["style_rules"] = styleRules
	} else {
		warn("style_rules", err)
	}

	envelope.References["references"] = t.architectReferences()
}

// buildContinuityCard 从已落盘的状态数据中提取人物当前状态、关系、时间线关键事实，
// 注入 working_memory.continuity_card。只反映最新一版事实，不包含推测或建议。
func (t *ContextTool) buildContinuityCard(envelope *chapterContextEnvelope, state contextBuildState, warn func(string, error)) {
	if state.progress == nil || len(state.progress.CompletedChapters) == 0 {
		return
	}
	card := make([]string, 0, 8)

	// 1. 角色当前状态：从 state_changes 中取每个 entity+field 的最新值
	charStates := deriveCharacterState(state.allStateChanges)
	if len(charStates) > 0 {
		card = append(card, "## 角色当前状态（写本章前的事实基线）")
		for _, cs := range charStates {
			if len(cs.summary) > 0 {
				card = append(card, fmt.Sprintf("- %s：%s", cs.name, strings.Join(cs.summary, "， ")))
			}
		}
		card = append(card, "")
	}

	// 2. 最近关系变化
	if len(state.relationships) > 0 {
		recent := state.relationships
		if len(recent) > 6 {
			recent = recent[len(recent)-6:]
		}
		card = append(card, "## 当前关系状态")
		seen := make(map[string]struct{})
		for _, r := range recent {
			key := r.CharacterA + "↔" + r.CharacterB
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			card = append(card, fmt.Sprintf("- %s ↔ %s：%s", r.CharacterA, r.CharacterB, r.Relation))
		}
		card = append(card, "")
	}

	// 3. 时间线锚点
	var lastTime, lastEvent string
	lastCh := -1
	if timeline, err := t.store.World.LoadRecentTimeline(state.chapter, state.profile.TimelineWindow); err == nil && len(timeline) > 0 {
		last := timeline[len(timeline)-1]
		lastTime, lastEvent, lastCh = last.Time, last.Event, last.Chapter
		card = append(card, "## 时间锚点")
		card = append(card, fmt.Sprintf("- 上一章时间：%s", lastTime))
		card = append(card, fmt.Sprintf("- 上一章事件：%s", lastEvent))
		card = append(card, "")
	}

	// 3b. 跨窗口绝对时间锚点：TimelineWindow 随全书规模收缩（长篇仅 5 章），
	// "腊月初七"这类锚点滚出窗口后彻底不可见，跨章时间推算失去参照。
	// 从全量时间线捞最近几条含绝对时间词的事件补上（代码派生，不评判）。
	if anchors := t.absoluteTimeAnchors(state.chapter, 3, lastCh, lastTime); len(anchors) > 0 {
		card = append(card, "## 绝对时间锚点（跨窗口召回）")
		card = append(card, anchors...)
		card = append(card, "")
	}

	// 4. 硬约束：角色死亡/离开/不可用
	var hardConstraints []string
	for _, cs := range charStates {
		if isHardConstraint(cs) {
			hardConstraints = append(hardConstraints, fmt.Sprintf("- %s：%s", cs.name, cs.summary[len(cs.summary)-1]))
		}
	}
	if len(hardConstraints) > 0 {
		card = append(card, "## 写本章必须遵守的硬约束")
		card = append(card, hardConstraints...)
		card = append(card, "")
	}

	if len(card) > 0 {
		card = append([]string{"本章写作前，以下事实已在之前章节中建立，写本章时**不得矛盾**："}, card...)
		envelope.Working["continuity_card"] = strings.Join(card, "\n")
	}
}

// absoluteTimeMarkers 粗略识别"腊月初七/试炼第二年/冬至"这类可定位的时间表述。
// 只匹配 Time 字段（很短），不匹配事件正文；"年/月"会捎上"三年后"这类时间跳跃点——
// 知道何时发生过跳跃正是防时间漂移需要的锚点。
var absoluteTimeMarkers = []string{"年", "月", "初", "腊", "节", "生辰", "春", "夏", "秋", "冬", "正旦"}

// absoluteTimeAnchors 从全量时间线取最近 limit 条含绝对时间词的事件（不含本章及之后），
// 按时间正序返回。skipCh/skipTime 用于剔除"上一章锚点"已展示过的同一条，避免重复。
func (t *ContextTool) absoluteTimeAnchors(chapter, limit, skipCh int, skipTime string) []string {
	all, err := t.store.World.LoadTimeline()
	if err != nil || len(all) == 0 {
		return nil
	}
	var out []string
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		e := all[i]
		if e.Chapter >= chapter {
			continue
		}
		if e.Chapter == skipCh && e.Time == skipTime {
			continue
		}
		matched := false
		for _, m := range absoluteTimeMarkers {
			if strings.Contains(e.Time, m) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		out = append(out, fmt.Sprintf("- 第%d章 %s：%s", e.Chapter, e.Time, e.Event))
	}
	slices.Reverse(out)
	return out
}

type charState struct {
	name    string
	summary []string
}

func deriveCharacterState(changes []domain.StateChange) []charState {
	if len(changes) == 0 {
		return nil
	}
	latest := make(map[string]map[string]domain.StateChange)
	for _, c := range changes {
		if c.Entity == "" {
			continue
		}
		if latest[c.Entity] == nil {
			latest[c.Entity] = make(map[string]domain.StateChange)
		}
		latest[c.Entity][c.Field] = c
	}

	var result []charState
	for name, fields := range latest {
		cs := charState{name: name}
		for _, field := range []string{"status", "location", "realm", "power", "rank", "condition"} {
			if c, ok := fields[field]; ok {
				cs.summary = append(cs.summary, fmt.Sprintf("%s=%s", field, c.NewValue))
			}
		}
		// 补充：取最后一条其他字段作为兜底描述
		if len(cs.summary) == 0 {
			for _, field := range []string{"alive", "affiliation", "identity", "role"} {
				if c, ok := fields[field]; ok {
					cs.summary = append(cs.summary, fmt.Sprintf("%s=%s", field, c.NewValue))
				}
			}
		}
		if len(cs.summary) > 0 {
			result = append(result, cs)
		}
	}
	return result
}

func isHardConstraint(cs charState) bool {
	for _, s := range cs.summary {
		if strings.Contains(s, "死亡") || strings.Contains(s, "dead") ||
			strings.Contains(s, "离开") || strings.Contains(s, "消失") ||
			strings.Contains(s, "gone") || strings.Contains(s, "destroyed") {
			return true
		}
	}
	return false
}

// ── 伏笔休眠视图 ──

// foreshadowWithDormancy 给每条活跃伏笔附上 chapters_since_last_touch
// （当前章 - 最近一次 plant/advance 的章号，经 DormantSince 取值）。
// 纯代码推导：把「哪条伏笔该推进了」从模型的脑算题变成事实字段，不新增 LLM 调用。
func foreshadowWithDormancy(entries []domain.ForeshadowEntry, chapter int) []domain.ForeshadowStatus {
	if len(entries) == 0 {
		return nil
	}
	out := make([]domain.ForeshadowStatus, 0, len(entries))
	for _, e := range entries {
		out = append(out, domain.ForeshadowStatus{
			ForeshadowEntry:        e,
			ChaptersSinceLastTouch: max(chapter-e.DormantSince(), 0),
		})
	}
	return out
}

// foreshadowDue 筛出休眠超过 domain.ForeshadowDueChapters 的活跃伏笔，按休眠降序
// （最久未动的在最前）。与 diag 的 StaleForeshadow 停滞下限共用同一阈值常量。
func foreshadowDue(entries []domain.ForeshadowEntry, chapter int) []domain.ForeshadowStatus {
	var due []domain.ForeshadowStatus
	for _, s := range foreshadowWithDormancy(entries, chapter) {
		if s.ChaptersSinceLastTouch > domain.ForeshadowDueChapters {
			due = append(due, s)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		return due[i].ChaptersSinceLastTouch > due[j].ChaptersSinceLastTouch
	})
	return due
}

// currentChapterHint 返回 architect 路径（无章节参数）下"当前章"的 best-effort 基准：
// 下一待写章号；无进度时为 1。让 architect 看到的休眠章数与 writer 路径同一基准。
func (t *ContextTool) currentChapterHint() int {
	if progress, err := t.store.Progress.Load(); err == nil && progress != nil {
		if n := progress.NextChapter(); n > 0 {
			return n
		}
	}
	return 1
}
