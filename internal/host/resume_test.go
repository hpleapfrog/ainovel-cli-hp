package host

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// 逐章验收政策且无许可：恢复标签应指向闸门等待（/next），而不是
// "从第 N 章继续"（恢复后会被闸门立即驳回的措辞）。
func TestResumeLabel_ReviewHoldPointsToGate(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Save(&domain.Progress{
		Phase:             domain.PhaseWriting,
		TotalChapters:     10,
		CompletedChapters: []int{1, 2},
	}); err != nil {
		t.Fatalf("Save progress: %v", err)
	}
	if err := st.RunMeta.Save(domain.RunMeta{AdvanceMode: domain.ChapterAdvanceReview}); err != nil {
		t.Fatalf("Save runmeta: %v", err)
	}

	label, err := resumeLabel(st)
	if err != nil {
		t.Fatalf("resumeLabel: %v", err)
	}
	if !strings.Contains(label, "逐章验收") || !strings.Contains(label, "第 3 章") {
		t.Fatalf("review 政策无许可时 label 应指向放行等待, got %q", label)
	}
	if strings.Contains(label, "从第 3 章继续") {
		t.Fatalf("不应出现将被闸门驳回的措辞, got %q", label)
	}

	// auto 政策：照常"从第 N 章继续"
	if err := st.RunMeta.Save(domain.RunMeta{AdvanceMode: domain.ChapterAdvanceAuto}); err != nil {
		t.Fatalf("Save runmeta(auto): %v", err)
	}
	label, err = resumeLabel(st)
	if err != nil {
		t.Fatalf("resumeLabel(auto): %v", err)
	}
	if !strings.Contains(label, "从第 3 章继续") {
		t.Fatalf("auto 政策应保持原措辞, got %q", label)
	}
}
