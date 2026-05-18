package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoad_ThreeLayers 验证 Default + Global + Project 三层在升序中各就各位。
func TestLoad_ThreeLayers(t *testing.T) {
	rulesFS := fstest.MapFS{
		"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")},
	}
	tmp := t.TempDir()
	globalPath := filepath.Join(tmp, "global.md")
	projectPath := filepath.Join(tmp, "rules.md")
	if err := os.WriteFile(globalPath, []byte("---\nforbidden_chars:\n  - \"——\"\n---\n# 全局偏好\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte("---\nchapter_words: 4000-8000\n---\n# 项目偏好\n"), 0644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{
		RulesFS:          rulesFS,
		HomeRulesPath:    globalPath,
		ProjectRulesPath: projectPath,
	})

	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %+v", len(layers), layers)
	}
	expectKinds := []SourceKind{SourceDefault, SourceGlobal, SourceProject}
	for i, want := range expectKinds {
		if layers[i].Kind != want {
			t.Errorf("layer[%d].Kind=%v, want %v", i, layers[i].Kind, want)
		}
	}
	// Merge 后 project 的 chapter_words 应胜出
	b := Merge(layers)
	if b.Structured.ChapterWords == nil || b.Structured.ChapterWords.Min != 4000 {
		t.Errorf("project chapter_words should win, got %+v", b.Structured.ChapterWords)
	}
	// global 贡献的 forbidden_chars 在 project 未声明时保留
	if len(b.Structured.ForbiddenChars) != 1 || b.Structured.ForbiddenChars[0] != "——" {
		t.Errorf("global forbidden_chars should propagate, got %v", b.Structured.ForbiddenChars)
	}
	if !strings.Contains(b.Preferences, "全局偏好") || !strings.Contains(b.Preferences, "项目偏好") {
		t.Errorf("merged preferences missing body: %q", b.Preferences)
	}
}

func TestLoad_GenreFieldIsPassThrough(t *testing.T) {
	// Phase 1.1：genre 仅作字段透传，不再触发 assets/rules/genres/ 加载。
	// 即使 fs 里放了 genres/xianxia.md 也不会被读出。
	rulesFS := fstest.MapFS{
		"default.md":        {Data: []byte("")},
		"genres/xianxia.md": {Data: []byte("---\nforbidden_chars:\n  - \"——\"\n---\n")},
	}
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "rules.md")
	if err := os.WriteFile(projectPath, []byte("---\ngenre: xianxia\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{
		RulesFS:          rulesFS,
		ProjectRulesPath: projectPath,
	})

	// 期望仅 default + project，无 genre 层
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers (no genre loading), got %d: %+v", len(layers), layers)
	}
	b := Merge(layers)
	if b.Structured.Genre != "xianxia" {
		t.Errorf("genre field should be passed through, got %q", b.Structured.Genre)
	}
	// genre 文件未被加载 → 不应有 "——" 来自题材文件
	if len(b.Structured.ForbiddenChars) != 0 {
		t.Errorf("genres/*.md must not be auto-loaded in Phase 1.1, got %v", b.Structured.ForbiddenChars)
	}
}

func TestLoad_NilFSDoesNotPanic(t *testing.T) {
	// 入参全空：不崩，返回空 layers
	layers := Load(LoadOptions{})
	if len(layers) != 0 {
		t.Errorf("expected 0 layers, got %d", len(layers))
	}
}

func TestLoad_OnlyDefault(t *testing.T) {
	// 仅项目内置默认规则可用，用户两个文件都缺
	rulesFS := fstest.MapFS{
		"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")},
	}
	layers := Load(LoadOptions{RulesFS: rulesFS})
	if len(layers) != 1 || layers[0].Kind != SourceDefault {
		t.Errorf("expected only default layer, got %+v", layers)
	}
}
