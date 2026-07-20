package guard_test

import (
	"context"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/agents/guard"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/flow"
	"github.com/voocel/ainovel-cli/internal/store"
)

// 本文件钉死一条跨包隐式契约：NewEditorStopGuard 靠 Router 任务文案里的
// 工具名子串（save_arc_summary / save_volume_summary）做任务分类。
// 这里不构造任务字符串，而是直接消费 flow.Route 的真实输出——Router 改措辞
// 导致 guard 分类退化为宽松分支时，"错误产物不能交差"的断言会失败。

func arcEndState(hasArcReview, hasArcSummary, hasVolumeSummary, isVolumeEnd bool) flow.State {
	return flow.State{
		Progress: &domain.Progress{
			Phase:             domain.PhaseWriting,
			Layered:           true,
			CompletedChapters: []int{1, 2, 3, 4},
		},
		LastCompleted:    4,
		ArcBoundary:      &store.ArcBoundary{IsArcEnd: true, IsVolumeEnd: isVolumeEnd, Volume: 1, Arc: 2},
		HasArcReview:     hasArcReview,
		HasArcSummary:    hasArcSummary,
		HasVolumeSummary: hasVolumeSummary,
	}
}

func consult(t *testing.T, g agentcore.StopGuard) agentcore.StopDecision {
	t.Helper()
	return g(context.Background(), agentcore.StopInfo{})
}

// 弧摘要任务：review 不能交差，只有 arc_summary 放行。
func TestEditorGuardContract_ArcSummaryTask(t *testing.T) {
	inst := flow.Route(arcEndState(true, false, false, false))
	if inst == nil || inst.Agent != domain.WorkerEditor {
		t.Fatalf("expected editor arc-summary instruction, got %+v", inst)
	}

	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	g := guard.NewEditorStopGuard(st, inst.Task, nil)

	if d := consult(t, g); d.Allow {
		t.Fatal("no checkpoint: expected blocked")
	}
	if _, err := st.Checkpoints.Append(domain.ChapterScope(4), "review", "reviews/04.json", ""); err != nil {
		t.Fatalf("Append review: %v", err)
	}
	if d := consult(t, g); d.Allow {
		t.Fatal("review checkpoint must not satisfy arc-summary task (guard degraded to generic?)")
	}
	if _, err := st.Checkpoints.Append(domain.ArcScope(1, 2), "arc_summary", "summaries/arc-v01a02.json", ""); err != nil {
		t.Fatalf("Append arc_summary: %v", err)
	}
	if d := consult(t, g); !d.Allow {
		t.Fatal("arc_summary checkpoint should satisfy the guard")
	}
}

// 卷摘要任务：review 与 arc_summary 都不能交差，只有 volume_summary 放行。
func TestEditorGuardContract_VolumeSummaryTask(t *testing.T) {
	inst := flow.Route(arcEndState(true, true, false, true))
	if inst == nil || inst.Agent != domain.WorkerEditor {
		t.Fatalf("expected editor volume-summary instruction, got %+v", inst)
	}

	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	g := guard.NewEditorStopGuard(st, inst.Task, nil)

	for _, step := range []string{"review", "arc_summary"} {
		if _, err := st.Checkpoints.Append(domain.ChapterScope(4), step, "x", ""); err != nil {
			t.Fatalf("Append %s: %v", step, err)
		}
		if d := consult(t, g); d.Allow {
			t.Fatalf("%s checkpoint must not satisfy volume-summary task (guard degraded to generic?)", step)
		}
	}
	if _, err := st.Checkpoints.Append(domain.VolumeScope(1), "volume_summary", "summaries/vol-v01.json", ""); err != nil {
		t.Fatalf("Append volume_summary: %v", err)
	}
	if d := consult(t, g); !d.Allow {
		t.Fatal("volume_summary checkpoint should satisfy the guard")
	}
}

// 弧评审任务（无摘要关键词）：走宽松分支，review 即可交差。
func TestEditorGuardContract_ArcReviewTask(t *testing.T) {
	inst := flow.Route(arcEndState(false, false, false, false))
	if inst == nil || inst.Agent != domain.WorkerEditor {
		t.Fatalf("expected editor arc-review instruction, got %+v", inst)
	}

	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	g := guard.NewEditorStopGuard(st, inst.Task, nil)

	if d := consult(t, g); d.Allow {
		t.Fatal("no checkpoint: expected blocked")
	}
	if _, err := st.Checkpoints.Append(domain.ChapterScope(4), "review", "reviews/04.json", ""); err != nil {
		t.Fatalf("Append review: %v", err)
	}
	if d := consult(t, g); !d.Allow {
		t.Fatal("review checkpoint should satisfy the generic review guard")
	}
}
