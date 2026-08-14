package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// PromoteCharacterTool 把配角名册（cast_ledger）中的角色升格为核心角色档案
// （characters.json），补全「角色重要性随剧情演化」的闭环（W3）。
//
// 长篇里某个龙套因剧情/读者反馈变重要时，Architect 可经此工具把它转为 Character：
// 名册条目置 Promoted=true（RecentActive 不再召回，避免与核心档案重复），
// 别名合并写入名册（Aliases 字段的写入通道落地）。
type PromoteCharacterTool struct {
	store *store.Store
}

func NewPromoteCharacterTool(store *store.Store) *PromoteCharacterTool {
	return &PromoteCharacterTool{store: store}
}

func (t *PromoteCharacterTool) Name() string { return "promote_character" }
func (t *PromoteCharacterTool) Description() string {
	return "把配角名册（cast_ledger）中的角色升格为核心角色档案（characters.json）。" +
		"后期涌现的重要配角用这个工具转正：填入 name（正式名）、aliases（曾用名/称号，用于合并身份）、role/tier/description/arc/traits。" +
		"升格后该角色从配角名册召回中退出，进入核心档案（editor 评审与上下文召回可见）。" +
		"若 characters.json 已有同名角色，本次调用只合并别名并同步名册标记，不覆盖已有档案。"
}
func (t *PromoteCharacterTool) Label() string { return "升格角色" }

// 写工具（跨 cast_ledger 与 characters.json 两个文件），禁止并发。
func (t *PromoteCharacterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *PromoteCharacterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *PromoteCharacterTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("name", schema.String("正式名（配角名册中的名字或其别名）")).Required(),
		schema.Property("aliases", schema.Array("曾用名/称号（用于合并身份，如把'李掌柜'与'老李'声明为同一人）", schema.String(""))),
		schema.Property("role", schema.String("角色定位（如 配角 / 盟友 / 反派）；缺省取名册 BriefRole，再缺省为\"配角\"")),
		schema.Property("tier", schema.Enum("重要性分级", "core", "important", "secondary", "decorative")),
		schema.Property("description", schema.String("整体描述（缺省取名册 BriefRole）")),
		schema.Property("arc", schema.String("角色弧线（可选）")),
		schema.Property("traits", schema.Array("特质列表", schema.String(""))),
	)
}

func (t *PromoteCharacterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Name        string   `json:"name"`
		Aliases     []string `json:"aliases"`
		Role        string   `json:"role"`
		Tier        string   `json:"tier"`
		Description string   `json:"description"`
		Arc         string   `json:"arc"`
		Traits      []string `json:"traits"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return nil, fmt.Errorf("name is required: %w", errs.ErrToolArgs)
	}
	if a.Tier != "" {
		switch a.Tier {
		case "core", "important", "secondary", "decorative":
		default:
			return nil, fmt.Errorf("tier 非法: %q（可选 core/important/secondary/decorative）: %w", a.Tier, errs.ErrToolArgs)
		}
	}

	// 名册里取不到 BriefRole 时降级为空,不阻断升格。
	briefRole := ""
	if entries, err := t.store.Cast.Load(); err == nil {
		for _, e := range entries {
			if e.Name == a.Name || slices.Contains(e.Aliases, a.Name) {
				briefRole = e.BriefRole
				break
			}
		}
	}

	chars, err := t.store.Characters.Load()
	if err != nil {
		return nil, fmt.Errorf("load characters: %w: %w", errs.ErrStoreRead, err)
	}

	result := map[string]any{"promoted": true, "name": a.Name}

	existing := -1
	for i, c := range chars {
		if c.Name == a.Name {
			existing = i
			break
		}
	}
	if existing >= 0 {
		// 已有核心档案：只合并别名（不覆盖既有字段），名册标记同步。
		chars[existing].Aliases = mergeAliases(chars[existing].Aliases, a.Aliases, a.Name)
		result["already_exists"] = true
	} else {
		role := a.Role
		if role == "" {
			role = briefRole
		}
		if role == "" {
			role = "配角"
		}
		tier := a.Tier
		if tier == "" {
			tier = "secondary"
		}
		description := a.Description
		if description == "" {
			description = briefRole
		}
		chars = append(chars, domain.Character{
			Name:        a.Name,
			Aliases:     mergeAliases(nil, a.Aliases, a.Name),
			Role:        role,
			Description: description,
			Arc:         a.Arc,
			Traits:      a.Traits,
			Tier:        tier,
		})
		result["role"] = role
		result["tier"] = tier
	}
	if err := t.store.Characters.Save(chars); err != nil {
		return nil, fmt.Errorf("save characters: %w: %w", errs.ErrStoreWrite, err)
	}

	// 名册标记 best-effort：条目不存在（角色自始就在核心档案/已被过滤）时只 warn——
	// 升格本身已完成，名册没有对应条目说明召回侧本就不需要跳过它。
	if err := t.store.Cast.Promote(a.Name, a.Aliases); err != nil {
		slog.Warn("配角名册升格标记失败（条目不存在则无害）", "module", "tools", "name", a.Name, "err", err)
	} else {
		result["ledger_promoted"] = true
	}

	if _, err := t.store.Checkpoints.AppendArtifact(domain.GlobalScope(), "promote_character", "characters.json"); err != nil {
		return nil, fmt.Errorf("checkpoint promote_character: %w: %w", errs.ErrStoreWrite, err)
	}
	return json.Marshal(result)
}

// mergeAliases 合并别名列表：去空白去重，保持既有顺序在前、新别名在后，排除正式名本身。
func mergeAliases(existing, incoming []string, name string) []string {
	var out []string
	seen := map[string]bool{name: true}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, v := range existing {
		add(v)
	}
	for _, v := range incoming {
		add(v)
	}
	return out
}
