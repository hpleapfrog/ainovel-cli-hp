package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveReviewTool 保存 Editor 的审阅结果。
type SaveReviewTool struct {
	store *store.Store
}

func NewSaveReviewTool(store *store.Store) *SaveReviewTool {
	return &SaveReviewTool{store: store}
}

func (t *SaveReviewTool) Name() string { return "save_review" }
func (t *SaveReviewTool) Description() string {
	return "保存审阅结果并更新流程状态。verdict 为 accept/polish/rewrite 之一。" +
		"工具内部执行评分卡门禁（可能升级 verdict），直接更新 Progress 的 flow 和 pending_rewrites。" +
		"返回结构化事实：final_verdict / affected_chapters / escalation_reason / next_flow / next_chapter"
}
func (t *SaveReviewTool) Label() string { return "保存审阅" }

// 写工具（同时更新 reviews/ 与 Progress 的 PendingRewrites/Flow），禁止并发。
func (t *SaveReviewTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveReviewTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveReviewTool) Schema() map[string]any {
	issueSchema := schema.Object(
		schema.Property("type", schema.Enum("问题维度", "consistency", "character", "pacing", "continuity", "foreshadow", "hook", "aesthetic")).Required(),
		schema.Property("severity", schema.Enum("严重程度", "critical", "error", "warning")).Required(),
		schema.Property("description", schema.String("问题描述")).Required(),
		schema.Property("evidence", schema.String("证据：原文片段、具体情节或状态数据")).Required(),
		schema.Property("suggestion", schema.String("修改建议")),
	)
	dimensionSchema := schema.Object(
		schema.Property("dimension", schema.Enum("维度", "consistency", "character", "pacing", "continuity", "foreshadow", "hook", "aesthetic")).Required(),
		schema.Property("score", schema.Int("评分（0-100）")).Required(),
		schema.Property("verdict", schema.Enum("维度结论（可省略：系统按 score 自动推导，≥80 pass / ≥60 warning / <60 fail）", "pass", "warning", "fail")),
		schema.Property("comment", schema.String("该维度的简要结论；每个维度必填，aesthetic 必须引用原文或具体统计事实")).Required(),
	)
	return schema.Object(
		schema.Property("chapter", schema.Int("审阅的章节号（全局审阅填最新章节号）")).Required(),
		schema.Property("scope", schema.Enum("审阅范围", "chapter", "global", "arc")).Required(),
		schema.Property("dimensions", schema.Array("分维度评分（七个维度各一条）", dimensionSchema)).Required(),
		schema.Property("issues", schema.Array("发现的问题", issueSchema)).Required(),
		schema.Property("contract_status", schema.Enum("章节契约完成度", "met", "partial", "missed")),
		schema.Property("contract_misses", schema.Array("未完成或违背的 contract 条目", schema.String(""))),
		schema.Property("contract_notes", schema.String("对 contract 履行情况的简要说明")),
		schema.Property("verdict", schema.Enum("审阅结论", "accept", "polish", "rewrite")).Required(),
		schema.Property("summary", schema.String("审阅总结")).Required(),
		schema.Property("affected_chapters", schema.Array("需要重写或打磨的章节号列表（verdict 为 polish/rewrite 时必填）", schema.Int(""))),
		schema.Property("new_foreshadow", schema.Array("正文里导演未计划、但值得追踪的事件（可选，最多 3 条）：入伏笔台账，kind/expected_payoff 语义与 commit_chapter 的 foreshadow_updates 一致，id 用 h### 格式且不与台账重复", schema.Object(
			schema.Property("id", schema.String("伏笔 ID（h### 格式，不与台账重复）")).Required(),
			schema.Property("description", schema.String("一句话描述，具体到可追溯的事件/物件/对话")).Required(),
			schema.Property("kind", schema.Enum("伏笔类型：hook=悬念钩子；debt=叙事债务", "hook", "debt")),
			schema.Property("expected_payoff", schema.String("预期回收区间（如\"3-5章内\"）")),
			schema.Property("from", schema.String("债务人（debt 时有效）")),
			schema.Property("to", schema.String("债权人（debt 时有效）")),
		))),
	)
}

// newForeshadowMax 单次评审最多入账的"计划外伏笔"条数——评审不是大纲导演，
// 只回收最值得追踪的事件，防止滥加污染台账。
const newForeshadowMax = 3

// newForeshadowArgs 是 save_review 的 new_foreshadow 条目。
type newForeshadowArgs struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	Kind           string `json:"kind"`
	ExpectedPayoff string `json:"expected_payoff"`
	From           string `json:"from"`
	To             string `json:"to"`
}

func (t *SaveReviewTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var r domain.ReviewEntry
	var extra struct {
		NewForeshadow []newForeshadowArgs `json:"new_foreshadow"`
	}
	raw := args
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if err := json.Unmarshal(raw, &extra); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if len(extra.NewForeshadow) > newForeshadowMax {
		return nil, fmt.Errorf("new_foreshadow 最多 %d 条，收到 %d", newForeshadowMax, len(extra.NewForeshadow))
	}
	for i, nf := range extra.NewForeshadow {
		if strings.TrimSpace(nf.ID) == "" || strings.TrimSpace(nf.Description) == "" {
			return nil, fmt.Errorf("new_foreshadow[%d] 需要 id 与 description", i)
		}
	}
	if r.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0")
	}
	// verdict 是 score 的纯函数（≥80 pass / ≥60 warning / <60 fail），由代码确定性推导——
	// 不让 LLM 重复提供再校验一致性。既消除冗余，也根除"score=85 却给 warning"这类自相矛盾的参数。
	for i := range r.Dimensions {
		r.Dimensions[i].Verdict = expectedDimensionVerdict(r.Dimensions[i].Score)
	}
	if err := validateReviewEntry(r); err != nil {
		return nil, err
	}

	// 评分卡门禁 — 内联原 policy/review.go 的升级逻辑
	finalVerdict := r.Verdict
	var escalationReason string

	if r.Verdict == "accept" {
		// 合同状态检查
		if r.ContractStatus == "missed" {
			finalVerdict = "rewrite"
			escalationReason = "合同履约状态为 missed，升级为重写"
		} else if r.ContractStatus == "partial" {
			finalVerdict = "polish"
			escalationReason = "合同履约状态为 partial，升级为打磨"
		}
		// 评分卡门禁
		if finalVerdict == "accept" {
			if gate := evaluateScorecardGate(r.Dimensions); gate != "" {
				if strings.Contains(gate, "rewrite") {
					finalVerdict = "rewrite"
				} else {
					finalVerdict = "polish"
				}
				escalationReason = gate
			}
		}
	}

	affected := r.AffectedChapters
	if finalVerdict == "rewrite" || finalVerdict == "polish" {
		if len(affected) == 0 && r.Chapter > 0 {
			affected = []int{r.Chapter}
		}
		if err := t.store.Progress.ValidatePendingRewrites(affected); err != nil {
			return nil, fmt.Errorf("validate pending rewrites: %w", err)
		}
	}

	if err := t.store.World.SaveReview(r); err != nil {
		return nil, fmt.Errorf("save review: %w", err)
	}

	// 计划外伏笔回收（Bishu 后验"unplanned 事件归类"的轻量版）：评审时把正文里
	// 导演未计划、但值得追踪的事件顺手埋进伏笔台账——裁决权留在 editor 现有流程
	// 内，不新增后验管线、不动事实层纪律。plant 幂等（按 ID 合并），best-effort。
	foreshadowPlanted := 0
	if len(extra.NewForeshadow) > 0 {
		updates := make([]domain.ForeshadowUpdate, 0, len(extra.NewForeshadow))
		for _, nf := range extra.NewForeshadow {
			updates = append(updates, domain.ForeshadowUpdate{
				ID:             nf.ID,
				Action:         "plant",
				Description:    nf.Description,
				Kind:           nf.Kind,
				ExpectedPayoff: nf.ExpectedPayoff,
				From:           nf.From,
				To:             nf.To,
			})
		}
		if err := t.store.World.UpdateForeshadow(r.Chapter, updates); err != nil {
			slog.Warn("评审计划外伏笔入账失败", "module", "tools", "chapter", r.Chapter, "err", err)
		} else {
			foreshadowPlanted = len(updates)
		}
	}

	// 根据最终 verdict 更新 Progress。
	// 写失败必须早返回——后续会 append review checkpoint，若此处吞 err，
	// Engine 会在 Store 仍处于旧 Flow / 缺失 PendingRewrites 的中间态下继续。
	progress, _ := t.store.Progress.Load()
	if finalVerdict == "rewrite" || finalVerdict == "polish" {
		flow := domain.FlowRewriting
		if finalVerdict == "polish" {
			flow = domain.FlowPolishing
		}
		// 先切 Flow 再写队列：若目标 Flow 与当前 Flow 构成非法迁移
		// （如返工排空中途 rewriting↔polishing 互斥），提前返回，
		// 避免脏写 PendingRewrites 留下"队列已改、Flow 未改"的部分状态。
		if err := t.store.Progress.SetFlow(flow); err != nil {
			return nil, fmt.Errorf("set flow %s: %w", flow, err)
		}
		if err := t.store.Progress.SetPendingRewrites(affected, r.Summary); err != nil {
			return nil, fmt.Errorf("set pending rewrites: %w", err)
		}
	} else {
		if err := t.store.Progress.SetFlow(domain.FlowWriting); err != nil {
			return nil, fmt.Errorf("set flow writing: %w", err)
		}
	}

	// 读取更新后的 Progress 快照作为事实
	latest, _ := t.store.Progress.Load()
	nextFlow := string(domain.FlowWriting)
	nextChapter := 0
	if latest != nil {
		nextFlow = string(latest.Flow)
		nextChapter = latest.NextChapter()
	}

	// 追加 checkpoint
	scope := domain.ChapterScope(r.Chapter)
	if r.Scope == "arc" {
		vol, arc := 0, 0
		if progress != nil {
			vol, arc = progress.CurrentVolume, progress.CurrentArc
		}
		scope = domain.ArcScope(vol, arc)
	}
	artifact := fmt.Sprintf("reviews/%02d.json", r.Chapter)
	if r.Scope == "global" {
		artifact = fmt.Sprintf("reviews/%02d-global.json", r.Chapter)
	}
	if _, err := t.store.Checkpoints.AppendArtifact(scope, "review", artifact); err != nil {
		return nil, fmt.Errorf("checkpoint review: %w", err)
	}

	return json.Marshal(map[string]any{
		"saved":              true,
		"chapter":            r.Chapter,
		"scope":              r.Scope,
		"verdict":            r.Verdict,
		"final_verdict":      finalVerdict,
		"escalation_reason":  escalationReason,
		"affected_chapters":  affected,
		"issues":             len(r.Issues),
		"next_flow":          nextFlow,
		"next_chapter":       nextChapter,
		"foreshadow_planted": foreshadowPlanted,
	})
}

var expectedReviewDimensions = map[string]struct{}{
	"consistency": {},
	"character":   {},
	"pacing":      {},
	"continuity":  {},
	"foreshadow":  {},
	"hook":        {},
	"aesthetic":   {},
}

func validateReviewEntry(r domain.ReviewEntry) error {
	if strings.TrimSpace(r.Scope) == "" {
		return fmt.Errorf("scope is required")
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	for _, issue := range r.Issues {
		if strings.TrimSpace(issue.Description) == "" {
			return fmt.Errorf("issue description is required")
		}
		if strings.TrimSpace(issue.Evidence) == "" {
			return fmt.Errorf("issue evidence is required")
		}
	}
	if err := validateDimensions(r.Dimensions); err != nil {
		return err
	}
	if (r.Verdict == "rewrite" || r.Verdict == "polish") && len(r.AffectedChapters) == 0 {
		return fmt.Errorf("affected_chapters is required when verdict=%s", r.Verdict)
	}
	return nil
}

func validateDimensions(dimensions []domain.DimensionScore) error {
	if len(dimensions) != len(expectedReviewDimensions) {
		return fmt.Errorf("dimensions must contain exactly %d entries", len(expectedReviewDimensions))
	}

	seen := make(map[string]struct{}, len(dimensions))
	for _, dim := range dimensions {
		if _, ok := expectedReviewDimensions[dim.Dimension]; !ok {
			return fmt.Errorf("unknown dimension: %s", dim.Dimension)
		}
		if _, ok := seen[dim.Dimension]; ok {
			return fmt.Errorf("duplicate dimension: %s", dim.Dimension)
		}
		seen[dim.Dimension] = struct{}{}
		if dim.Score < 0 || dim.Score > 100 {
			return fmt.Errorf("invalid score for %s: %d", dim.Dimension, dim.Score)
		}
		if strings.TrimSpace(dim.Comment) == "" {
			return fmt.Errorf("dimension comment is required: %s", dim.Dimension)
		}
	}
	return nil
}

func expectedDimensionVerdict(score int) string {
	switch {
	case score >= 80:
		return "pass"
	case score >= 60:
		return "warning"
	default:
		return "fail"
	}
}

// criticalDimensions 定义会触发 verdict 升级的关键维度。
var criticalDimensions = map[string]struct{}{
	"consistency": {},
	"character":   {},
	"continuity":  {},
}

// evaluateScorecardGate 检查评分卡是否需要升级 verdict。
// 返回空字符串表示不升级。
func evaluateScorecardGate(dimensions []domain.DimensionScore) string {
	var criticalFails []string
	var polishIssues []string

	for _, dim := range dimensions {
		_, isCritical := criticalDimensions[dim.Dimension]
		if isCritical && (dim.Verdict == "fail" || dim.Score < 60) {
			criticalFails = append(criticalFails, fmt.Sprintf("%s(%d)", dim.Dimension, dim.Score))
		} else if dim.Verdict == "warning" || (isCritical && dim.Score < 80) {
			polishIssues = append(polishIssues, fmt.Sprintf("%s(%d)", dim.Dimension, dim.Score))
		}
	}

	if len(criticalFails) > 0 {
		return fmt.Sprintf("rewrite: 关键维度不合格 %v", criticalFails)
	}
	if len(polishIssues) > 0 {
		return fmt.Sprintf("polish: 部分维度需打磨 %v", polishIssues)
	}
	return ""
}
