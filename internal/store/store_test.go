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
