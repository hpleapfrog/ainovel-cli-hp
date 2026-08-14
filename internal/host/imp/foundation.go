package imp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// FoundationResult 是 Foundation 反推的结构化产物。
type FoundationResult struct {
	Premise    string                 // Markdown 字符串
	Characters []domain.Character     // 角色档案
	WorldRules []domain.WorldRule     // 世界规则
	Factions   []domain.Faction       // 势力档案（W1：正文实际出现的势力）
	Locations  []domain.Location      // 地点档案（W1：正文实际出现的地点）
	Volumes    []domain.VolumeOutline // 分层大纲：导入正文作为第一卷（可续写、可扩展）
	Compass    *domain.StoryCompass   // 续写方向锚点（ending_direction / open_threads / estimated_scale）
}

// foundationStageThreshold 超过该章数时反推拆两阶段：先核心设定（premise/characters/
// world_rules，小且稳定），再结构（layered_outline/compass，大且依赖前者）。
// 一次调用承载几百章会让上下文爆掉、反推质量随章节数下降。
const foundationStageThreshold = 100

// foundationJSON 是结构化 JSON 输出的目标形状（旧 === TAG === 信封降级仍受支持）。
type foundationJSON struct {
	Premise    string                 `json:"premise"`
	Characters []domain.Character     `json:"characters"`
	WorldRules []domain.WorldRule     `json:"world_rules"`
	Factions   []domain.Faction       `json:"factions"`
	Locations  []domain.Location      `json:"locations"`
	Volumes    []domain.VolumeOutline `json:"layered_outline"`
	Compass    *domain.StoryCompass   `json:"compass"`
}

// stageFields 是各阶段要求的字段清单（渲染进 prompt 的 ${stage_fields} 占位符）。
const (
	stageFieldsFull       = "premise / characters / world_rules / factions / locations / layered_outline / compass 七个字段全部输出（factions/locations 正文没有实际势力/地点时输出空数组）"
	stageFieldsCore       = "只输出 premise / characters / world_rules / factions / locations 五个字段，其余字段不要输出"
	stageFieldsStructures = "只输出 layered_outline / compass 两个字段，其余字段不要输出"
)

// LLMChat 是 imp 包对 ChatModel 的最小依赖：仅需要一次普通文本生成。
// 抽出独立接口便于单测注入 mock，避免直接耦合 agentcore 客户端。
type LLMChat interface {
	Generate(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error)
}

// ReverseFoundation 从已切分的章节正文反推 foundation。大文本（> 阈值）拆两阶段
// 调用：核心设定先、结构后，避免一次调用承载过多。不调用 save_foundation，纯函数；
// 持久化由调用方决定。
func ReverseFoundation(ctx context.Context, llm LLMChat, systemPrompt string, chapters []Chapter) (*FoundationResult, error) {
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters to analyze")
	}
	if llm == nil {
		return nil, fmt.Errorf("llm is nil")
	}

	if len(chapters) <= foundationStageThreshold {
		out, err := reverseFoundationStage(ctx, llm, systemPrompt, chapters, stageFieldsFull)
		if err != nil {
			return nil, err
		}
		return parseFoundationOutput(out, len(chapters))
	}

	// 两阶段：核心设定（小、稳定）→ 结构（大、依赖前者）。
	coreOut, err := reverseFoundationStage(ctx, llm, systemPrompt, chapters, stageFieldsCore)
	if err != nil {
		return nil, fmt.Errorf("stage core: %w", err)
	}
	structOut, err := reverseFoundationStage(ctx, llm, systemPrompt, chapters, stageFieldsStructures)
	if err != nil {
		return nil, fmt.Errorf("stage structures: %w", err)
	}
	core, err := parseFoundationCore(coreOut)
	if err != nil {
		return nil, fmt.Errorf("stage core parse: %w", err)
	}
	structs, err := parseFoundationStructures(structOut, len(chapters))
	if err != nil {
		return nil, fmt.Errorf("stage structures parse: %w", err)
	}
	return &FoundationResult{
		Premise:    core.Premise,
		Characters: core.Characters,
		WorldRules: core.WorldRules,
		Factions:   core.Factions,
		Locations:  core.Locations,
		Volumes:    structs.Volumes,
		Compass:    structs.Compass,
	}, nil
}

// reverseFoundationStage 跑一次反推 LLM 调用并返回原始文本。
func reverseFoundationStage(ctx context.Context, llm LLMChat, systemPrompt string, chapters []Chapter, stageFields string) (string, error) {
	system := strings.ReplaceAll(systemPrompt, "${chapter_count}", fmt.Sprintf("%d", len(chapters)))
	system = strings.ReplaceAll(system, "${stage_fields}", stageFields)
	user := buildFoundationUserPrompt(chapters)

	resp, err := llm.Generate(ctx, []agentcore.Message{
		agentcore.SystemMsg(system),
		agentcore.UserMsg(user),
	}, nil)
	if err != nil {
		return "", fmt.Errorf("llm generate: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("llm returned nil response")
	}
	return resp.Message.TextContent(), nil
}

// buildFoundationUserPrompt 拼装用户提示：所有章节顺序拼接，附章号锚点便于 LLM 引用。
func buildFoundationUserPrompt(chapters []Chapter) string {
	var sb strings.Builder
	sb.WriteString("以下是已完成的 ")
	fmt.Fprintf(&sb, "%d", len(chapters))
	sb.WriteString(" 章正文。请严格按系统提示反推 foundation，只输出一个 JSON 对象。\n\n")
	for i, ch := range chapters {
		fmt.Fprintf(&sb, "## 第 %d 章：%s\n\n", i+1, ch.Title)
		sb.WriteString(ch.Content)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String()
}

// parseFoundationOutput 解析 LLM 输出并校验关键约束。
// 首选结构化 JSON（主流程已统一到 extractJSON+Validate 范式）；=== TAG === 信封仅作降级兼容。
func parseFoundationOutput(text string, expectChapters int) (*FoundationResult, error) {
	if fr, err := parseFoundationJSON(text, expectChapters); err == nil {
		return fr, nil
	}
	return parseFoundationEnvelope(text, expectChapters)
}

// parseFoundationCore 解析核心设定阶段输出（premise/characters/world_rules）。
func parseFoundationCore(text string) (*FoundationResult, error) {
	f, err := extractFoundationJSON(text)
	if err != nil {
		// 降级信封：只取核心段
		return parseFoundationEnvelope(text, 0)
	}
	return validateFoundationCore(f, false, 0)
}

// parseFoundationStructures 解析结构阶段输出（layered_outline/compass）。
func parseFoundationStructures(text string, expectChapters int) (*FoundationResult, error) {
	f, err := extractFoundationJSON(text)
	if err != nil {
		return parseFoundationEnvelope(text, expectChapters)
	}
	if _, err := validateFoundationStructures(f, expectChapters); err != nil {
		return nil, err
	}
	return &FoundationResult{Volumes: f.Volumes, Compass: f.Compass}, nil
}

// extractFoundationJSON 从文本中截取首个 JSON 对象并解码为 foundationJSON。
func extractFoundationJSON(text string) (*foundationJSON, error) {
	raw := extractJSONObject(text)
	if raw == "" {
		return nil, fmt.Errorf("no JSON object found in output")
	}
	var f foundationJSON
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, fmt.Errorf("parse foundation JSON: %w", err)
	}
	return &f, nil
}

// parseFoundationJSON 解析全量结构化 JSON 输出并校验关键约束。
func parseFoundationJSON(text string, expectChapters int) (*FoundationResult, error) {
	f, err := extractFoundationJSON(text)
	if err != nil {
		return nil, err
	}
	return validateFoundationCore(f, true, expectChapters)
}

// validateFoundationCore 校验 premise/characters 关键约束；requireStructures=true
// 时一并校验大纲章数与 compass（全量模式）。
func validateFoundationCore(f *foundationJSON, requireStructures bool, expectChapters int) (*FoundationResult, error) {
	premise := stripFences(f.Premise)
	if !strings.HasPrefix(strings.TrimLeft(premise, " \t\n"), "#") {
		return nil, fmt.Errorf("premise must start with a Markdown heading line (# 书名)")
	}
	if len(f.Characters) == 0 {
		return nil, fmt.Errorf("characters array is empty")
	}
	if requireStructures {
		if _, err := validateFoundationStructures(f, expectChapters); err != nil {
			return nil, err
		}
	}
	return &FoundationResult{
		Premise:    premise,
		Characters: f.Characters,
		WorldRules: f.WorldRules,
		Factions:   f.Factions,
		Locations:  f.Locations,
		Volumes:    f.Volumes,
		Compass:    f.Compass,
	}, nil
}

// validateFoundationStructures 校验大纲章数与 compass（结构阶段/全量共用）。
func validateFoundationStructures(f *foundationJSON, expectChapters int) (*FoundationResult, error) {
	if len(f.Volumes) == 0 {
		return nil, fmt.Errorf("layered_outline is empty")
	}
	if got := len(domain.FlattenOutline(f.Volumes)); got != expectChapters {
		return nil, fmt.Errorf("layered outline chapter count mismatch: got %d, want %d", got, expectChapters)
	}
	if f.Compass == nil || strings.TrimSpace(f.Compass.EndingDirection) == "" {
		return nil, fmt.Errorf("compass.ending_direction is required")
	}
	return &FoundationResult{Volumes: f.Volumes, Compass: f.Compass}, nil
}

// parseFoundationEnvelope 解析旧版 === TAG === 信封输出（降级路径）。
// expectChapters>0 = 全量解析(五段齐备);=0 = 核心阶段降级(仅需前三段)。
func parseFoundationEnvelope(text string, expectChapters int) (*FoundationResult, error) {
	env := parseTaggedEnvelope(text)
	if env == nil {
		return nil, fmt.Errorf("no === TAG === envelope found in LLM output")
	}
	tags := []string{"PREMISE", "CHARACTERS", "WORLD_RULES"}
	if expectChapters > 0 {
		tags = append(tags, "LAYERED_OUTLINE", "COMPASS")
	}
	if err := requireTags(env, tags...); err != nil {
		return nil, err
	}

	premise := stripFences(env["PREMISE"])
	if !strings.HasPrefix(strings.TrimLeft(premise, " \t\n"), "#") {
		return nil, fmt.Errorf("premise must start with a Markdown heading line (# 书名)")
	}

	var characters []domain.Character
	if err := decodeJSON("characters", env["CHARACTERS"], &characters); err != nil {
		return nil, err
	}
	if len(characters) == 0 {
		return nil, fmt.Errorf("characters array is empty")
	}

	var worldRules []domain.WorldRule
	if err := decodeJSON("world_rules", env["WORLD_RULES"], &worldRules); err != nil {
		return nil, err
	}

	var factions []domain.Faction
	if body := strings.TrimSpace(env["FACTIONS"]); body != "" {
		if err := decodeJSON("factions", body, &factions); err != nil {
			return nil, err
		}
	}
	var locations []domain.Location
	if body := strings.TrimSpace(env["LOCATIONS"]); body != "" {
		if err := decodeJSON("locations", body, &locations); err != nil {
			return nil, err
		}
	}

	var volumes []domain.VolumeOutline
	if expectChapters > 0 {
		if err := decodeJSON("layered_outline", env["LAYERED_OUTLINE"], &volumes); err != nil {
			return nil, err
		}
		// 导入大纲必须把全部 N 章实展开（FlattenOutline 只数真实章节，骨架弧不计），
		// 否则逐章 commit 时会有章节落在大纲范围外、被越界守卫拒绝。
		if got := len(domain.FlattenOutline(volumes)); got != expectChapters {
			return nil, fmt.Errorf("layered outline chapter count mismatch: got %d, want %d", got, expectChapters)
		}
	}

	var compass domain.StoryCompass
	if expectChapters > 0 {
		if err := decodeJSON("compass", env["COMPASS"], &compass); err != nil {
			return nil, err
		}
	}

	return &FoundationResult{
		Premise:    premise,
		Characters: characters,
		WorldRules: worldRules,
		Factions:   factions,
		Locations:  locations,
		Volumes:    volumes,
		Compass:    &compass,
	}, nil
}

// PersistFoundation 把反推结果写入 Store，顺序与 Architect 长篇 prompt 一致：
// premise → characters → world_rules → layered_outline → compass。导入正文作为第一卷
// 落成分层大纲，使导入的书可被续写、可扩展。每步都触发 save_foundation 同款落盘逻辑。
//
// 不直接调 SaveFoundationTool 是因为这里是确定性回放，无需走 LLM 工具调度。
// 但保持与 SaveFoundationTool 相同的副作用：phase 推进、checkpoint 追加。
func PersistFoundation(ctx context.Context, st *store.Store, scale domain.PlanningTier, fr *FoundationResult) error {
	if fr == nil {
		return fmt.Errorf("nil foundation result")
	}
	if err := st.RunMeta.SetPlanningTier(scale); err != nil {
		return fmt.Errorf("save planning tier: %w", err)
	}

	// 1. premise
	if err := st.Outline.SavePremise(fr.Premise); err != nil {
		return fmt.Errorf("save premise: %w", err)
	}
	if name := domain.ExtractNovelNameFromPremise(fr.Premise); name != "" {
		_ = st.Progress.SetNovelName(name)
	}
	_ = st.Progress.UpdatePhase(domain.PhasePremise)
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "premise", "premise.md"); err != nil {
		return fmt.Errorf("checkpoint premise: %w", err)
	}

	// 2. characters
	if err := st.Characters.Save(fr.Characters); err != nil {
		return fmt.Errorf("save characters: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "characters", "characters.json"); err != nil {
		return fmt.Errorf("checkpoint characters: %w", err)
	}

	// 3. world_rules
	if err := st.World.SaveWorldRules(fr.WorldRules); err != nil {
		return fmt.Errorf("save world_rules: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "world_rules", "world_rules.json"); err != nil {
		return fmt.Errorf("checkpoint world_rules: %w", err)
	}

	// 3b. factions / locations（W1）：正文反推出的势力/地点档案；空则跳过。
	if len(fr.Factions) > 0 {
		if err := st.World.SaveFactions(fr.Factions); err != nil {
			return fmt.Errorf("save factions: %w", err)
		}
		if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "factions", "factions.json"); err != nil {
			return fmt.Errorf("checkpoint factions: %w", err)
		}
	}
	if len(fr.Locations) > 0 {
		if err := st.World.SaveLocations(fr.Locations); err != nil {
			return fmt.Errorf("save locations: %w", err)
		}
		if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "locations", "locations.json"); err != nil {
			return fmt.Errorf("checkpoint locations: %w", err)
		}
	}

	// 4. layered outline（导入正文作为第一卷 → 分层模式，可续写、可扩展）
	if err := st.Outline.SaveLayeredOutline(fr.Volumes); err != nil {
		return fmt.Errorf("save layered outline: %w", err)
	}
	if err := st.Outline.SaveOutline(domain.FlattenOutline(fr.Volumes)); err != nil {
		return fmt.Errorf("save flattened outline: %w", err)
	}
	_ = st.Progress.UpdatePhase(domain.PhaseOutline)
	_ = st.Progress.SetTotalChapters(domain.TotalChapters(fr.Volumes))
	_ = st.Progress.SetLayered(true)
	if len(fr.Volumes) > 0 && len(fr.Volumes[0].Arcs) > 0 {
		_ = st.Progress.UpdateVolumeArc(fr.Volumes[0].Index, fr.Volumes[0].Arcs[0].Index)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "layered_outline", "layered_outline.json"); err != nil {
		return fmt.Errorf("checkpoint layered outline: %w", err)
	}

	// 5. compass（续写方向锚点）：让 layeredBookComplete 据 open_threads 判定，
	//    避免导入即被判完结；也给续写时的方向/篇幅一个基准。
	if err := st.Outline.SaveCompass(*fr.Compass); err != nil {
		return fmt.Errorf("save compass: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "compass", "meta/compass.json"); err != nil {
		return fmt.Errorf("checkpoint compass: %w", err)
	}

	// 6. foundation 完整 → 推进到 writing 阶段（与 save_foundation 末尾逻辑一致）
	if len(st.FoundationMissing()) == 0 {
		if p, _ := st.Progress.Load(); p != nil &&
			p.Phase != domain.PhaseWriting && p.Phase != domain.PhaseComplete {
			_ = st.Progress.UpdatePhase(domain.PhaseWriting)
		}
	}
	return nil
}

// decodeJSON 解析 JSON（数组或对象）并附上标签，便于调试。
func decodeJSON(label, body string, out any) error {
	body = stripFences(body)
	if body == "" {
		return fmt.Errorf("%s body is empty", label)
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("parse %s JSON: %w", label, err)
	}
	return nil
}

// stripFences 去掉首尾 ``` 代码围栏（含语言标签），LLM 偶尔会自作主张包一层。
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}
