package diag

import (
	"fmt"
	"slices"

	"github.com/voocel/ainovel-cli/internal/store"
)

// RepairResult 是一条自动修复的执行结果。
type RepairResult struct {
	Rule    string // 来源规则名
	Summary string // 人类可读的修复描述
	Err     error  // 非 nil 表示修复失败
}

// repairFuncs 规则名 → 修复实现。
// 只登记事实层、确定性、零 LLM 的修复（状态机/队列/孤立 steer）；
// 章节正文、摘要、伏笔等内容类问题永远只建议、不自动改。
var repairFuncs = map[string]func(s *store.Store) (string, error){
	"OrphanedSteer":          repairOrphanedSteer,
	"PhaseFlowMismatch":      repairPhaseFlowMismatch,
	"InvalidPendingRewrites": repairInvalidPendingRewrites,
}

// Repairable 筛选出可自动修复的 Finding：已登记修复实现 && 高置信 && AutoSafe。
func Repairable(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Confidence != ConfHigh || f.AutoLevel != AutoSafe {
			continue
		}
		if _, ok := repairFuncs[f.Rule]; ok {
			out = append(out, f)
		}
	}
	return out
}

// Repair 对可自动修复的 Finding 逐条执行修复，单条失败不影响其余。
func Repair(s *store.Store, findings []Finding) []RepairResult {
	var results []RepairResult
	for _, f := range Repairable(findings) {
		fn := repairFuncs[f.Rule]
		summary, err := fn(s)
		results = append(results, RepairResult{Rule: f.Rule, Summary: summary, Err: err})
	}
	return results
}

// repairOrphanedSteer 清除未被消费的转向指令。
func repairOrphanedSteer(s *store.Store) (string, error) {
	if err := s.RunMeta.ClearPendingSteer(); err != nil {
		return "清除未消费的转向指令", err
	}
	return "已清除未消费的转向指令", nil
}

// repairPhaseFlowMismatch 把损坏的 flow 复位为初始态。
// 损坏状态可能无合法转移路径（如 reviewing → ""），必须走 ForceFlow。
func repairPhaseFlowMismatch(s *store.Store) (string, error) {
	if err := s.Progress.ForceFlow(""); err != nil {
		return "复位 flow 为初始态", err
	}
	return "已将 flow 复位为初始态", nil
}

// repairInvalidPendingRewrites 把未完成章节从返工队列中剔除；
// 队列因此为空时复位 flow=writing 并清空 rewrite_reason。
func repairInvalidPendingRewrites(s *store.Store) (string, error) {
	p, err := s.Progress.Load()
	if err != nil {
		return "清理返工队列", err
	}
	if p == nil {
		return "清理返工队列", fmt.Errorf("progress 不存在")
	}
	valid := make([]int, 0, len(p.PendingRewrites))
	removed := make([]int, 0)
	for _, ch := range p.PendingRewrites {
		if ch > 0 && slices.Contains(p.CompletedChapters, ch) {
			valid = append(valid, ch)
		} else {
			removed = append(removed, ch)
		}
	}
	summary := fmt.Sprintf("已从返工队列剔除未完成章节: %v", removed)
	if len(valid) > 0 {
		return summary, s.Progress.SetPendingRewrites(valid, p.RewriteReason)
	}
	// 队列排空：优先走正常的 ClearPendingRewrites（自带 flow→writing 转移校验）；
	// flow 值本身损坏（未知状态、无合法路径）时回退 ForceResetRewrites 强制复位。
	if err := s.Progress.ClearPendingRewrites(); err != nil {
		if ferr := s.Progress.ForceResetRewrites(); ferr != nil {
			return summary, fmt.Errorf("清空队列后复位失败: %w（原错误: %v）", ferr, err)
		}
	}
	return summary + "，队列已空，flow 复位为 writing", nil
}
