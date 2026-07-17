package bootstrap

import (
	"errors"
	"testing"
	"time"

	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/notify"
)

func TestConfigResolveReasoningEffort(t *testing.T) {
	cfg := Config{
		ReasoningEffort: "low", // 顶层默认
		Roles: map[string]RoleConfig{
			"writer":    {Provider: "p", Model: "m", ReasoningEffort: "high"}, // 角色覆盖
			"architect": {Provider: "p", Model: "m"},                          // 无 reasoning_effort，应回落默认
		},
	}

	cases := []struct {
		role string
		want string
	}{
		{"writer", "high"},   // 角色覆盖优先
		{"architect", "low"}, // 角色未配 → 回落顶层默认
		{"editor", "low"},    // 角色不存在 → 顶层默认
		{"", "low"},          // 空 → 顶层默认
		{"default", "low"},   // default → 顶层默认
		{"arbiter", "low"},   // 非配置角色（裁定恒随顶层默认）
	}
	for _, c := range cases {
		if got := cfg.ResolveReasoningEffort(c.role); got != c.want {
			t.Errorf("ResolveReasoningEffort(%q) = %q, want %q", c.role, got, c.want)
		}
	}

	// 顶层默认也为空时，未覆盖角色返回 ""（不覆盖）。
	empty := Config{Roles: map[string]RoleConfig{"writer": {ReasoningEffort: "xhigh"}}}
	if got := empty.ResolveReasoningEffort("editor"); got != "" {
		t.Errorf("空默认下 editor 应返回 \"\"，得 %q", got)
	}
	if got := empty.ResolveReasoningEffort("writer"); got != "xhigh" {
		t.Errorf("空默认下 writer 覆盖应生效，得 %q", got)
	}
}

func TestValidateBaseRejectsNonConfigurableRoles(t *testing.T) {
	for _, role := range []string{"coordinator", "arbiter"} {
		t.Run(role, func(t *testing.T) {
			cfg := Config{
				Provider:  "openrouter",
				ModelName: "test-model",
				Providers: map[string]ProviderConfig{
					"openrouter": {APIKey: "sk-test-123456"},
				},
				Roles: map[string]RoleConfig{
					role: {Provider: "openrouter", Model: "test-model"},
				},
			}

			err := cfg.ValidateBase()
			if err == nil {
				t.Fatalf("roles.%s 应被拒绝", role)
			}
			if !errors.Is(err, errs.ErrConfig) {
				t.Fatalf("应包装 errs.ErrConfig，得到: %v", err)
			}
		})
	}
}

func TestValidateBaseNotifyEventsMatchRuntimeContract(t *testing.T) {
	validConfig := func(events []string) Config {
		return Config{
			Provider:  "openrouter",
			ModelName: "test-model",
			Providers: map[string]ProviderConfig{
				"openrouter": {APIKey: "sk-test-123456"},
			},
			Notify: NotifyConfig{Events: events},
		}
	}

	cfg := validConfig(notify.Kinds())
	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("当前通知事件契约应全部通过配置校验: %v", err)
	}

	cfg = validConfig([]string{"repeat"})
	if err := cfg.ValidateBase(); !errors.Is(err, errs.ErrConfig) {
		t.Fatalf("旧 repeat 事件应被拒绝，得到: %v", err)
	}
}

func TestProviderStreamIdleTimeoutValue(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", defaultStreamIdleTimeout, false},
		{"900s", 15 * time.Minute, false},
		{"15m", 15 * time.Minute, false},
		{"abc", 0, true},
		{"-5s", 0, true},
		{"0", 0, true}, // 不提供"关闭看门狗"——真死流需要有限界
	}
	for _, c := range cases {
		got, err := ProviderConfig{StreamIdleTimeout: c.in}.StreamIdleTimeoutValue()
		if c.wantErr {
			if err == nil {
				t.Errorf("%q 应报错", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q = (%v, %v), want %v", c.in, got, err, c.want)
		}
	}
}

func TestValidateBaseRejectsBadStreamIdleTimeout(t *testing.T) {
	cfg := Config{
		Provider:  "openrouter",
		ModelName: "test-model",
		Providers: map[string]ProviderConfig{
			"openrouter": {APIKey: "sk-test-123456", StreamIdleTimeout: "fast"},
		},
	}
	if err := cfg.ValidateBase(); !errors.Is(err, errs.ErrConfig) {
		t.Fatalf("非法 stream_idle_timeout 应拒绝并包装 ErrConfig，得到: %v", err)
	}
}

func TestAddProviderModel_ErrorsOnMissingProvider(t *testing.T) {
	cfg := Config{Providers: map[string]ProviderConfig{
		"openrouter": {APIKey: "sk-test"},
	}}
	// 拼错的 provider 名不得静默创建空条目（幽灵 provider 会在运行时爆"未配置凭证"）
	if err := cfg.AddProviderModel("openroutr", "gpt-5"); err == nil {
		t.Fatal("missing provider should error")
	}
	if _, ok := cfg.Providers["openroutr"]; ok {
		t.Fatal("missing provider must not be created as side effect")
	}

	// 正常追加 + 重复添加幂等（大小写不敏感）
	if err := cfg.AddProviderModel("openrouter", "gpt-5"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := cfg.AddProviderModel("openrouter", "GPT-5"); err != nil {
		t.Fatalf("dup add should be idempotent: %v", err)
	}
	models := cfg.Providers["openrouter"].Models
	if len(models) != 1 || models[0] != "gpt-5" {
		t.Fatalf("models = %v, want [gpt-5]", models)
	}
}

func TestRemoveProviderModel_FiltersWithoutAliasing(t *testing.T) {
	cfg := Config{Providers: map[string]ProviderConfig{
		"openrouter": {Models: []string{"a", "b", "A", "c"}},
	}}
	cfg.RemoveProviderModel("openrouter", "a")
	models := cfg.Providers["openrouter"].Models
	if len(models) != 2 || models[0] != "b" || models[1] != "c" {
		t.Fatalf("models = %v, want [b c]（大小写不敏感过滤）", models)
	}
	cfg.RemoveProviderModel("openrouter", "b")
	if got := cfg.Providers["openrouter"].Models; len(got) != 1 || got[0] != "c" {
		t.Fatalf("models = %v, want [c]——[:0] 复用底层数组会把后续过滤搞坏", got)
	}
	// 不存在的 provider / 模型：静默无事
	cfg.RemoveProviderModel("nope", "x")
	cfg.RemoveProviderModel("openrouter", "zzz")
	if got := cfg.Providers["openrouter"].Models; len(got) != 1 {
		t.Fatalf("unrelated remove must be no-op, got %v", got)
	}
}

func TestModelUsedBy(t *testing.T) {
	cfg := Config{
		Provider:  "a",
		ModelName: "m1",
		Providers: map[string]ProviderConfig{"a": {}, "b": {}},
		Roles: map[string]RoleConfig{
			"writer": {Provider: "a", Model: "m1"},
			"editor": {Provider: "a", Model: "m2", Fallbacks: []ModelRef{{Provider: "a", Model: "m3"}}},
		},
	}
	if got := cfg.ModelUsedBy("a", "m1"); len(got) != 2 {
		t.Fatalf("m1 应被 default+writer 引用，got %v", got)
	}
	if got := cfg.ModelUsedBy("a", "m3"); len(got) != 1 || got[0] != "editor(fallback)" {
		t.Fatalf("m3 应被 editor fallback 引用，got %v", got)
	}
	if got := cfg.ModelUsedBy("a", "zzz"); len(got) != 0 {
		t.Fatalf("未引用模型应为空，got %v", got)
	}
	// provider 不同不算引用
	if got := cfg.ModelUsedBy("b", "m1"); len(got) != 0 {
		t.Fatalf("provider 不匹配不应计引用，got %v", got)
	}
}

func TestRenameProviderModel_SyncsReferences(t *testing.T) {
	cfg := Config{
		Provider:  "a",
		ModelName: "old",
		Providers: map[string]ProviderConfig{
			"a": {Models: []string{"old", "other"}},
		},
		Roles: map[string]RoleConfig{
			"writer": {Provider: "a", Model: "old", Fallbacks: []ModelRef{{Provider: "a", Model: "old"}, {Provider: "b", Model: "old"}}},
		},
	}
	if err := cfg.RenameProviderModel("a", "old", "new"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	models := cfg.Providers["a"].Models
	if len(models) != 2 || models[0] != "new" || models[1] != "other" {
		t.Fatalf("Models = %v, want [new other]", models)
	}
	if cfg.ModelName != "new" {
		t.Fatalf("默认模型应同步，got %q", cfg.ModelName)
	}
	w := cfg.Roles["writer"]
	if w.Model != "new" || w.Fallbacks[0].Model != "new" {
		t.Fatalf("角色引用应同步，got %+v", w)
	}
	if w.Fallbacks[1].Model != "old" {
		t.Fatalf("其他 provider 的 fallback 不应动，got %+v", w.Fallbacks[1])
	}

	// 错误路径：provider 不存在 / 模型不存在 / 新名冲突
	if err := cfg.RenameProviderModel("nope", "old", "x"); err == nil {
		t.Fatal("missing provider should error")
	}
	if err := cfg.RenameProviderModel("a", "zzz", "x"); err == nil {
		t.Fatal("missing model should error")
	}
	if err := cfg.RenameProviderModel("a", "new", "OTHER"); err == nil {
		t.Fatal("conflicting new name should error (大小写不敏感)")
	}
}
