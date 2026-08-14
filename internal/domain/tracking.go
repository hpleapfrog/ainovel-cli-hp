package domain

import (
	"fmt"
	"strings"
)

// StateField 是 StateChange.Field 的受控枚举，也是 WorldRule.HardConstraint.Field
// 的对齐锚点（同一拼写，机械校验才能跨层匹配）。
//
// 历史教训：Field 曾是自由字符串（power / Power / 战力 三写法并存），
// FactConflict 检测按 Entity+Field 匹配历史值，拼写漂移会让数值冲突检测
// 静默失效——连续性检测的可靠性依赖一个未约束的自由字符串。
//
// 收紧方式（W5）：写路径（store.AppendStateChanges）把已知枚举的大小写/空白
// 变体确定性归一到受控拼写；writer.md 文档化的「数值事实登记」允许自定义事实名
// （如 field="人数"）作为例外原样保留——硬性拒绝未知值会破坏该既有机制，
// 枚举职责收敛为「角色状态字段的规范拼写 + 硬约束锚点」。
type StateField string

const (
	FieldRealm    StateField = "realm"    // 修炼境界/段位
	FieldLocation StateField = "location" // 所处位置
	FieldStatus   StateField = "status"   // 生死/受伤等状态
	FieldPower    StateField = "power"    // 战力/实力
	FieldRank     StateField = "rank"     // 军衔/品阶/等级
	FieldRelation StateField = "relation" // 与他人的关系
	FieldOther    StateField = "other"    // 兜底：枚举外的受控字段
)

var knownStateFields = map[StateField]bool{
	FieldRealm: true, FieldLocation: true, FieldStatus: true,
	FieldPower: true, FieldRank: true, FieldRelation: true, FieldOther: true,
}

// Valid 判断是否为受控枚举成员（含 other 兜底）。
func (f StateField) Valid() bool { return knownStateFields[f] }

// NormalizeStateField 归一化 field：去空白；已知枚举的大小写变体（Power→power）
// 归一到受控拼写；枚举外的自定义事实名原样保留。
func NormalizeStateField(f string) string {
	f = strings.TrimSpace(f)
	if knownStateFields[StateField(strings.ToLower(f))] {
		return strings.ToLower(f)
	}
	return f
}

// ValidateStateChanges 校验状态变化清单；返回首个非法项。
// field 必须非空；已知枚举的拼写变体由写路径归一，不再视为非法。
// distance 非空时必须是受控枚举（near/mid/far/fog）。
func ValidateStateChanges(changes []StateChange) error {
	for i, c := range changes {
		if strings.TrimSpace(c.Field) == "" {
			return fmt.Errorf("state_changes[%d].field 不能为空", i)
		}
		if !FactDistance(c.Distance).Valid() {
			return fmt.Errorf("state_changes[%d].distance %q 非法：可选 near/mid/far/fog", i, c.Distance)
		}
	}
	return nil
}

// StateChange 角色/实体状态变化记录。
type StateChange struct {
	Chapter  int    `json:"chapter"`
	Entity   string `json:"entity"`              // 角色名或实体名
	Field    string `json:"field"`               // 变化属性：受控枚举（见 StateField）；数值事实可用自定义事实名
	OldValue string `json:"old_value,omitempty"` // 变化前（首次出现可空）
	NewValue string `json:"new_value"`           // 变化后
	Reason   string `json:"reason,omitempty"`    // 变化原因
	// Distance 可选：该事实距主角的距离标注（near/mid/far/fog）。
	Distance string `json:"distance,omitempty"`
}
