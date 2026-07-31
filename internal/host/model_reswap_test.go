package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/voocel/ainovel-cli/internal/bootstrap"
)

// newModelManageTestHost 构造只含模型管理所需字段的 Host，
// 绕开 host.New 的重量级装配（store/agents/后台任务）。
// 配置落盘重定向到临时 HOME，避免污染真实 ~/.ainovel/config.json。
func newModelManageTestHost(t *testing.T, cfg bootstrap.Config) *Host {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows 的 os.UserHomeDir
	models, err := bootstrap.NewModelSet(cfg)
	if err != nil {
		t.Fatalf("NewModelSet: %v", err)
	}
	return &Host{cfg: cfg, models: models, events: make(chan Event, 100)}
}

func reswapTestConfig() bootstrap.Config {
	return bootstrap.Config{
		Provider:  "p1",
		ModelName: "m-old",
		Providers: map[string]bootstrap.ProviderConfig{
			"p1": {Type: "openai", APIKey: "sk-old", BaseURL: "http://old/v1", Models: []string{"m-old"}},
		},
		Roles: map[string]bootstrap.RoleConfig{
			"writer": {Provider: "p1", Model: "m-old"},
		},
	}
}

// 重命名正在使用的模型后，运行时（default + 角色）必须当次会话切到新名——
// 旧行为只改配置引用点，活客户端继续用旧名发请求直到重启。
func TestRenameProviderModelReswapsRuntime(t *testing.T) {
	h := newModelManageTestHost(t, reswapTestConfig())

	if _, m, _ := h.models.CurrentSelection("writer"); m != "m-old" {
		t.Fatalf("前置：writer 应使用 m-old, got %s", m)
	}
	if err := h.RenameProviderModel("p1", "m-old", "m-new"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, m, _ := h.models.CurrentSelection("default"); m != "m-new" {
		t.Errorf("default 应当次会话切到新名, got %s", m)
	}
	if _, m, _ := h.models.CurrentSelection("writer"); m != "m-new" {
		t.Errorf("writer 应当次会话切到新名, got %s", m)
	}
}

// 编辑 provider 字段以框内内容为准（空=清除），且落盘到全局配置文件。
func TestUpdateProviderClearsFieldsAndPersists(t *testing.T) {
	h := newModelManageTestHost(t, reswapTestConfig())

	if err := h.UpdateProvider("p1", "openai", "", ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	pc := h.cfg.Providers["p1"]
	if pc.APIKey != "" || pc.BaseURL != "" {
		t.Fatalf("key/url 应被清除, got key=%q url=%q", pc.APIKey, pc.BaseURL)
	}
	if len(pc.Models) != 1 || pc.Models[0] != "m-old" {
		t.Fatalf("models 应保留, got %v", pc.Models)
	}

	// 落盘验证：直接读临时 HOME 下的全局配置文件
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".ainovel", "config.json"))
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var saved bootstrap.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("parse saved config: %v", err)
	}
	if saved.Providers["p1"].APIKey != "" {
		t.Fatalf("落盘的 api_key 应为空, got %q", saved.Providers["p1"].APIKey)
	}
}
