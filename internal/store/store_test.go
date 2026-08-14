package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// 重开工况下 CompletedChapters 非严格升序：校验必须针对最大完成章，
// 与 domain.Progress.LatestCompleted 同口径，而不是切片末元素。
func TestCheckConsistencyUsesLatestCompletedMax(t *testing.T) {
	s := newTestStore(t)
	if err := s.Progress.Save(&domain.Progress{CompletedChapters: []int{5, 3}}); err != nil {
		t.Fatalf("Save progress: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(5, "终稿"); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}

	// 最大完成章 5 有终稿 → 无告警；若误用末元素 3 会误报
	if w := s.CheckConsistency(); len(w) != 0 {
		t.Fatalf("不应有告警, got %v", w)
	}

	// 最大完成章终稿缺失 → 告警第 5 章
	if err := os.Remove(filepath.Join(s.Dir(), "chapters", "05.md")); err != nil {
		t.Fatalf("remove chapter: %v", err)
	}
	w := s.CheckConsistency()
	if len(w) != 1 || !strings.Contains(w[0], "第 5 章") {
		t.Fatalf("应告警第 5 章, got %v", w)
	}
}

// StartPrepared 开新书的完整清场：WipeBook 必须移除全部创作产物，
// 只保留 run.json/user_rules（会话配置与每次启动重写的快照）。
func TestWipeBookRemovesAllArtifacts(t *testing.T) {
	s := newTestStore(t)
	// 铺一份"旧书"
	if err := s.Progress.Init("旧书", 5); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, f := range []struct{ path, content string }{
		{"premise.md", "# 旧"},
		{"timeline.json", `[{"chapter":1,"time":"x","event":"e"}]`},
		{"foreshadow_ledger.json", `[{"id":"f1"}]`},
		{"relationship_state.json", `[{"character_a":"a","character_b":"b","relation":"r"}]`},
		{"meta/state_changes.json", `[{"chapter":1,"entity":"e","field":"status","new_value":"x"}]`},
		{"meta/cast_ledger.json", `[{"name":"配角"}]`},
		{"meta/compass.json", `{"ending_direction":"旧方向"}`},
		{"meta/usage.json", `{"schema":1}`},
		{"meta/decisions.jsonl", "x\n"},
		{"meta/pending_commit.json", `{"chapter":1}`},
	} {
		if err := os.WriteFile(filepath.Join(s.Dir(), f.path), []byte(f.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.path, err)
		}
	}
	for _, d := range []string{"chapters", "drafts", "reviews", "summaries", "meta/sessions"} {
		if err := os.MkdirAll(filepath.Join(s.Dir(), d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := s.Drafts.SaveFinalChapter(1, "旧章节正文"); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}

	if err := s.WipeBook(); err != nil {
		t.Fatalf("WipeBook: %v", err)
	}

	for _, rel := range []string{
		"premise.md", "timeline.json", "foreshadow_ledger.json", "relationship_state.json",
		"meta/state_changes.json", "meta/cast_ledger.json", "meta/compass.json",
		"meta/usage.json", "meta/decisions.jsonl", "meta/pending_commit.json",
		"meta/progress.json", "meta/checkpoints.jsonl",
		"chapters", "drafts", "reviews", "summaries", "meta/sessions",
	} {
		if _, err := os.Stat(filepath.Join(s.Dir(), rel)); !os.IsNotExist(err) {
			t.Fatalf("%s 应被清空, stat err=%v", rel, err)
		}
	}
	// 幂等:空目录上重复清场不报错
	if err := s.WipeBook(); err != nil {
		t.Fatalf("second WipeBook: %v", err)
	}
}
