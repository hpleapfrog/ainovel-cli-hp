package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ReportConsistencyTool 把 writer 的自审结论落盘（meta/consistency_checks.jsonl）。
//
// check_consistency 是只读工具，其 LLM 判断结论只存在于当轮上下文——压缩/换窗口
// 即丢失，editor 无法回溯"writer 当时认为哪些地方可疑"。本工具补上这一可审计缺口
// （P3-8）：commit 前把自审结论落盘，editor 评审该章时经 novel_context 顶层
// consistency_check 读取。
type ReportConsistencyTool struct {
	store *store.Store
}

func NewReportConsistencyTool(store *store.Store) *ReportConsistencyTool {
	return &ReportConsistencyTool{store: store}
}

func (t *ReportConsistencyTool) Name() string { return "report_consistency" }
func (t *ReportConsistencyTool) Description() string {
	return "把 check_consistency 的自审结论落盘供 editor 评审回溯（passed 与发现的问题清单）。" +
		"只在确实发现了值得记录的可疑点、或想留下\"已核对无问题\"的结论时调用一次；" +
		"不要为空洞的\"通过\"刷记录。"
}
func (t *ReportConsistencyTool) Label() string { return "自审结论落盘" }

// 写工具（追加 jsonl），禁止并发。
func (t *ReportConsistencyTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *ReportConsistencyTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *ReportConsistencyTool) Schema() map[string]any {
	issueSchema := schema.Object(
		schema.Property("type", schema.String("问题类型（如 state_fact / timeline / foreshadow / contract）")).Required(),
		schema.Property("severity", schema.Enum("严重程度", "warning", "error")).Required(),
		schema.Property("description", schema.String("问题描述")).Required(),
	)
	return schema.Object(
		schema.Property("chapter", schema.Int("章节号")).Required(),
		schema.Property("passed", schema.Bool("自审是否通过（无硬伤）")).Required(),
		schema.Property("issues", schema.Array("发现的问题清单", issueSchema)),
	)
}

func (t *ReportConsistencyTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int                           `json:"chapter"`
		Passed  bool                          `json:"passed"`
		Issues  []store.ConsistencyCheckIssue `json:"issues"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}

	rec := store.ConsistencyCheckRecord{Chapter: a.Chapter, Passed: a.Passed, Issues: a.Issues}
	if err := t.store.World.SaveConsistencyCheck(rec); err != nil {
		return nil, fmt.Errorf("save consistency check: %w: %w", errs.ErrStoreWrite, err)
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(a.Chapter), "consistency_report",
		"meta/consistency_checks.jsonl",
	); err != nil {
		return nil, fmt.Errorf("checkpoint consistency report: %w: %w", errs.ErrStoreWrite, err)
	}

	return json.Marshal(map[string]any{
		"saved": true, "chapter": a.Chapter, "passed": a.Passed, "issues": len(a.Issues),
	})
}
