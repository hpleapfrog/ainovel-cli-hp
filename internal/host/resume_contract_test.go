package host

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/flow"
	"github.com/voocel/ainovel-cli/internal/store"
)

// 契约测试（P6-2）：resume.go 的 describeResume/describeArcEndLabel 手工维护了一份
// Router 决策优先级的"影子副本"。本测试对一组关键 progress 状态断言"恢复去向"与
// flow.Route 首条指令的 Agent/Chapter 一致——Router 一旦调整分支顺序/条件而 label
// 未跟进，这里立即红灯。
//
// 唯一例外：逐章验收等待放行——label 说的是"等待放行"，Route 会正常派 writer N，
// 但 ChapterAdvanceGate 在派发前拦截（引擎暂停）。该组合单独断言其已知分歧。

func newResumeStore(t *testing.T, volumes []domain.VolumeOutline) *store.Store {
	t.Helper()
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init("契约测试", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := st.RunMeta.Init("default", "test", "test-model"); err != nil {
		t.Fatalf("InitRunMeta: %v", err)
	}
	if len(volumes) > 0 {
		if err := st.Outline.SaveLayeredOutline(volumes); err != nil {
			t.Fatalf("SaveLayeredOutline: %v", err)
		}
		if err := st.Progress.SetLayered(true); err != nil {
			t.Fatalf("SetLayered: %v", err)
		}
	}
	return st
}

func completeChapters(t *testing.T, st *store.Store, chapters ...int) {
	t.Helper()
	for _, ch := range chapters {
		if err := st.Progress.MarkChapterComplete(ch, 3000, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete(%d): %v", ch, err)
		}
	}
}

func arcV1A1() domain.VolumeOutline {
	return domain.VolumeOutline{
		Index: 1, Title: "第一卷", Theme: "主题",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "首弧", Goal: "目标", Chapters: []domain.OutlineEntry{
				{Title: "章一", CoreEvent: "事件", Hook: "钩子"},
				{Title: "章二", CoreEvent: "事件", Hook: "钩子"},
			}},
		},
	}
}

func TestResumeLabel_MatchesRouteDispatch(t *testing.T) {
	t.Run("正常续写→writer 下一章", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1, 2)
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "第 3 章继续") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "writer", 3)
	})

	t.Run("返工队列→writer 队列头", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1, 2)
		if err := st.Progress.SetPendingRewrites([]int{2}, "评审返工"); err != nil {
			t.Fatalf("SetPendingRewrites: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "重写") || !strings.Contains(label, "待处理") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "writer", 2)
	})

	t.Run("打磨队列→writer 队列头", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1, 2)
		if err := st.Progress.SetPendingRewrites([]int{1}, "打磨"); err != nil {
			t.Fatalf("SetPendingRewrites: %v", err)
		}
		if err := st.Progress.SetFlow(domain.FlowPolishing); err != nil {
			t.Fatalf("SetFlow: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "打磨") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "writer", 1)
	})

	t.Run("章节进行中→writer 该章", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1, 2)
		if err := st.Progress.StartChapter(3); err != nil {
			t.Fatalf("StartChapter: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "第 3 章进行中") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "writer", 3)
	})

	t.Run("提交中断→writer 该章", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1, 2)
		if err := st.Signals.SavePendingCommit(domain.PendingCommit{Chapter: 3, Stage: domain.CommitStageStarted}); err != nil {
			t.Fatalf("SavePendingCommit: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "第 3 章提交中断") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "writer", 3)
	})

	t.Run("提交中断与返工队列并存→label 与 Route 同指队列头", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1, 2)
		if err := st.Signals.SavePendingCommit(domain.PendingCommit{Chapter: 3, Stage: domain.CommitStageStarted}); err != nil {
			t.Fatalf("SavePendingCommit: %v", err)
		}
		if err := st.Progress.SetPendingRewrites([]int{2}, "评审返工"); err != nil {
			t.Fatalf("SetPendingRewrites: %v", err)
		}
		progress, _ := st.Progress.Load()

		// Route 的返工队列是绝对最高优先级:label 必须同指队列头,
		// 不能显示"提交中断"(那与恢复后的第一条真实指令不符)。
		label := describeResume(st, progress)
		if strings.Contains(label, "提交中断") {
			t.Fatalf("label must prefer rewrite queue over pending commit, got %q", label)
		}
		if !strings.Contains(label, "重写") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "writer", 2)
	})

	t.Run("弧末无评审→editor 弧评审", func(t *testing.T) {
		st := newResumeStore(t, []domain.VolumeOutline{arcV1A1()})
		completeChapters(t, st, 1, 2)
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "弧末评审") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "editor", 0)
	})

	t.Run("弧末有评审无摘要→editor 弧摘要", func(t *testing.T) {
		st := newResumeStore(t, []domain.VolumeOutline{arcV1A1()})
		completeChapters(t, st, 1, 2)
		if err := st.World.SaveReview(domain.ReviewEntry{Chapter: 2, Scope: "arc", Verdict: "accept"}); err != nil {
			t.Fatalf("SaveReview: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "弧摘要") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "editor", 0)
	})

	t.Run("卷末有弧摘要无卷摘要→editor 卷摘要", func(t *testing.T) {
		st := newResumeStore(t, []domain.VolumeOutline{arcV1A1()})
		completeChapters(t, st, 1, 2)
		if err := st.World.SaveReview(domain.ReviewEntry{Chapter: 2, Scope: "arc", Verdict: "accept"}); err != nil {
			t.Fatalf("SaveReview: %v", err)
		}
		if err := st.Summaries.SaveArcSummary(domain.ArcSummary{Volume: 1, Arc: 1, Title: "弧", Summary: "摘要"}); err != nil {
			t.Fatalf("SaveArcSummary: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "卷摘要") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "editor", 0)
	})

	t.Run("弧末待展开下一弧→architect_long", func(t *testing.T) {
		st := newResumeStore(t, []domain.VolumeOutline{{
			Index: 1, Title: "第一卷", Theme: "主题",
			Arcs: []domain.ArcOutline{
				{Index: 1, Title: "首弧", Goal: "目标", Chapters: []domain.OutlineEntry{
					{Title: "章一", CoreEvent: "事件", Hook: "钩子"},
				}},
				{Index: 2, Title: "骨架弧", Goal: "目标", EstimatedChapters: 8},
			},
		}})
		completeChapters(t, st, 1)
		if err := st.World.SaveReview(domain.ReviewEntry{Chapter: 1, Scope: "arc", Verdict: "accept"}); err != nil {
			t.Fatalf("SaveReview: %v", err)
		}
		if err := st.Summaries.SaveArcSummary(domain.ArcSummary{Volume: 1, Arc: 1, Title: "弧", Summary: "摘要"}); err != nil {
			t.Fatalf("SaveArcSummary: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "待展开下一弧") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "architect_long", 0)
	})

	t.Run("卷末待决策下一卷→architect_long", func(t *testing.T) {
		st := newResumeStore(t, []domain.VolumeOutline{arcV1A1()})
		completeChapters(t, st, 1, 2)
		if err := st.World.SaveReview(domain.ReviewEntry{Chapter: 2, Scope: "arc", Verdict: "accept"}); err != nil {
			t.Fatalf("SaveReview: %v", err)
		}
		if err := st.Summaries.SaveArcSummary(domain.ArcSummary{Volume: 1, Arc: 1, Title: "弧", Summary: "摘要"}); err != nil {
			t.Fatalf("SaveArcSummary: %v", err)
		}
		if err := st.Summaries.SaveVolumeSummary(domain.VolumeSummary{Volume: 1, Title: "卷", Summary: "摘要"}); err != nil {
			t.Fatalf("SaveVolumeSummary: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "待决策下一卷") {
			t.Fatalf("label = %q", label)
		}
		assertRouteAgent(t, st, "architect_long", 0)
	})

	t.Run("逐章验收等待放行→label 与 Route 的已知分歧", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1, 2)
		if err := st.RunMeta.SetAdvanceMode(domain.ChapterAdvanceReview); err != nil {
			t.Fatalf("SetAdvanceMode: %v", err)
		}
		progress, _ := st.Progress.Load()

		label := describeResume(st, progress)
		if !strings.Contains(label, "逐章验收等待放行") {
			t.Fatalf("label = %q", label)
		}
		// Route 仍派 writer 3;ChapterAdvanceGate 在派发前拦截——label 说的是
		// 被闸门拦下后的真实去向,此分歧是设计意图,不是漂移。
		assertRouteAgent(t, st, "writer", 3)
	})
}

// assertRouteAgent 断言 Route 首条指令的 Agent 与 Chapter 与期望一致。
func assertRouteAgent(t *testing.T, st *store.Store, wantAgent string, wantChapter int) {
	t.Helper()
	inst := flow.Route(flow.LoadState(st))
	if inst == nil {
		t.Fatalf("Route returned nil, want %s chapter %d", wantAgent, wantChapter)
	}
	if inst.Agent != wantAgent {
		t.Fatalf("Route agent = %q, want %q (task=%q)", inst.Agent, wantAgent, inst.Task)
	}
	if inst.Chapter != wantChapter {
		t.Fatalf("Route chapter = %d, want %d (task=%q)", inst.Chapter, wantChapter, inst.Task)
	}
}

// 自愈：旧版 StartPrepared 半重置的历史损伤修复。
func TestHealProgressAfterReset(t *testing.T) {
	t.Run("分层书被重置→恢复分层模式与总章数", func(t *testing.T) {
		st := newResumeStore(t, []domain.VolumeOutline{{
			Index: 1, Title: "第一卷", Theme: "主题",
			Arcs: []domain.ArcOutline{
				{Index: 1, Title: "首弧", Goal: "目标", Chapters: []domain.OutlineEntry{
					{Title: "章一", CoreEvent: "事件", Hook: "钩子"},
					{Title: "章二", CoreEvent: "事件", Hook: "钩子"},
				}},
				{Index: 2, Title: "骨架弧", Goal: "目标", EstimatedChapters: 8},
			},
		}})
		completeChapters(t, st, 1)
		// 模拟旧 bug 半重置：progress 清零、Layered=false，layered_outline 仍在
		if err := st.Progress.Save(&domain.Progress{
			Phase: domain.PhaseWriting, CompletedChapters: []int{1}, Layered: false, TotalChapters: 0,
		}); err != nil {
			t.Fatalf("Save damage: %v", err)
		}

		repaired := healProgressAfterReset(st)
		if len(repaired) == 0 {
			t.Fatal("expected self-heal")
		}
		p, _ := st.Progress.Load()
		if !p.Layered {
			t.Fatalf("Layered must be restored, repairs=%v", repaired)
		}
		if p.TotalChapters != domain.TotalChapters(func() []domain.VolumeOutline {
			v, _ := st.Outline.LoadLayeredOutline()
			return v
		}()) {
			t.Fatalf("TotalChapters = %d, want layered total", p.TotalChapters)
		}
		if p.Phase != domain.PhaseWriting {
			t.Fatalf("分层书越界不自动完结（layeredComplete 管），phase=%s", p.Phase)
		}
	})

	t.Run("非分层越界→推导总章数并完结", func(t *testing.T) {
		st := newResumeStore(t, nil)
		if err := st.Outline.SaveOutline([]domain.OutlineEntry{
			{Chapter: 1, Title: "首章", CoreEvent: "起", Hook: "续"},
			{Chapter: 2, Title: "末章", CoreEvent: "终", Hook: ""},
		}); err != nil {
			t.Fatalf("SaveOutline: %v", err)
		}
		if err := st.Progress.Save(&domain.Progress{
			Phase: domain.PhaseWriting, CompletedChapters: []int{1, 2, 3}, Layered: false, TotalChapters: 0,
		}); err != nil {
			t.Fatalf("Save damage: %v", err)
		}

		repaired := healProgressAfterReset(st)
		p, _ := st.Progress.Load()
		if p.Phase != domain.PhaseComplete {
			t.Fatalf("expected complete, got %s (repairs=%v)", p.Phase, repaired)
		}
		if p.TotalChapters != 2 {
			t.Fatalf("TotalChapters = %d, want 2", p.TotalChapters)
		}
	})

	t.Run("健康书不自愈", func(t *testing.T) {
		st := newResumeStore(t, nil)
		completeChapters(t, st, 1)
		if err := st.Progress.SetTotalChapters(5); err != nil {
			t.Fatalf("SetTotalChapters: %v", err)
		}
		if repaired := healProgressAfterReset(st); len(repaired) != 0 {
			t.Fatalf("healthy book must not be touched: %v", repaired)
		}
	})
}
