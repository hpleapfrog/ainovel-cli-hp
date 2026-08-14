package diag

import "fmt"

// PlanActions 根据高置信 Finding 生成可执行动作。
// 只有 Confidence==high && AutoLevel==safe 的 Finding 才会产出 Action。
func PlanActions(findings []Finding) []Action {
	var actions []Action
	seen := make(map[string]struct{})

	for _, f := range findings {
		if f.Confidence != ConfHigh || f.AutoLevel != AutoSafe {
			continue
		}
		if _, ok := seen[f.Rule]; ok {
			continue
		}
		seen[f.Rule] = struct{}{}

		actions = append(actions, planRule(f)...)
	}
	return actions
}

func planRule(f Finding) []Action {
	key := findingFingerprint(f)

	switch f.Rule {
	case "PhaseFlowMismatch":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEmitNotice, Severity: f.Severity, Summary: f.Title, Message: f.Title, Fingerprint: key},
			{SourceRule: f.Rule, Kind: ActionRunRepair, Severity: f.Severity, Summary: "状态机异常修复（flow 复位为初始态）", Message: f.Evidence, Fingerprint: key},
		}
	case "OutlineExhausted":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: "大纲耗尽处理", Message: "已完成章节数达到已规划上限。请优先调用 Architect 展开下一弧或追加新卷，再继续写作。", Fingerprint: key},
		}
	case "OrphanedSteer":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionRunRepair, Severity: f.Severity, Summary: "清除未消费的用户干预指令", Message: f.Evidence, Fingerprint: key},
		}
	case "InvalidPendingRewrites":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionRunRepair, Severity: f.Severity, Summary: "清理返工队列中的未完成章节", Message: f.Evidence, Fingerprint: key},
		}
	default:
		// 其它 AutoSafe 规则若已登记确定性修复实现,同样产出可执行修复动作
		// (repair.go 的 repairFuncs 是唯一事实源,P7-2)。
		if _, repairable := repairFuncs[f.Rule]; repairable {
			return []Action{
				{SourceRule: f.Rule, Kind: ActionRunRepair, Severity: f.Severity, Summary: "自动修复: " + f.Title, Message: f.Evidence, Fingerprint: key},
			}
		}
		return nil
	}
}

func findingFingerprint(f Finding) string {
	return fmt.Sprintf("%s|%s|%s|%s", f.Rule, f.Target, f.Title, f.Evidence)
}
