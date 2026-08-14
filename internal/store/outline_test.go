package store

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func setupLayered(t *testing.T, volumes []domain.VolumeOutline) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Outline.SaveLayeredOutline(volumes); err != nil {
		t.Fatalf("SaveLayeredOutline: %v", err)
	}
	if err := s.Progress.SetLayered(true); err != nil {
		t.Fatalf("SetLayered: %v", err)
	}
	return s
}

func TestCheckArcBoundaryNeedsNewVolume(t *testing.T) {
	// 只有 1 卷 1 弧 1 章，且非 Final → 应触发 NeedsNewVolume
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "第一卷", Theme: "起步",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "首弧", Goal: "目标",
			Chapters: []domain.OutlineEntry{{Title: "第一章", CoreEvent: "开局", Hook: "继续"}},
		}},
	}})

	b, err := s.Outline.CheckArcBoundary(1) // 第 1 章 = 弧/卷最后一章
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if b == nil {
		t.Fatal("expected boundary, got nil")
	}
	if !b.IsArcEnd || !b.IsVolumeEnd {
		t.Fatalf("expected arc+volume end, got arc=%v vol=%v", b.IsArcEnd, b.IsVolumeEnd)
	}
	if !b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=true")
	}
	if b.NextVolume != 0 || b.NextArc != 0 {
		t.Fatalf("expected no next, got vol=%d arc=%d", b.NextVolume, b.NextArc)
	}
}

func TestCheckArcBoundaryLastVolumeRequiresDecision(t *testing.T) {
	// 单卷最后一章 → 触发 NeedsNewVolume，让 Router 让架构师二选一：
	// append_volume 续写 / complete_book 收尾。
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "唯一卷", Theme: "主题",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "唯一弧", Goal: "收束",
			Chapters: []domain.OutlineEntry{{Title: "终章", CoreEvent: "结局", Hook: "无"}},
		}},
	}})

	b, err := s.Outline.CheckArcBoundary(1)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if !b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=true at last expanded chapter")
	}
	if b.HasNextArc() {
		t.Fatal("expected no next arc")
	}
}

func TestCheckArcBoundaryNextArcInSameVolume(t *testing.T) {
	// 2 弧：第 1 弧结束应指向第 2 弧，不触发 NeedsNewVolume
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "第一卷", Theme: "起步",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "首弧", Goal: "目标", Chapters: []domain.OutlineEntry{{Title: "章一", CoreEvent: "事件", Hook: "钩子"}}},
			{Index: 2, Title: "次弧", Goal: "目标2", EstimatedChapters: 10},
		},
	}})

	b, err := s.Outline.CheckArcBoundary(1)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if !b.IsArcEnd {
		t.Fatal("expected arc end")
	}
	if b.IsVolumeEnd {
		t.Fatal("expected not volume end (second arc exists)")
	}
	if b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=false")
	}
	if b.NextVolume != 1 || b.NextArc != 2 {
		t.Fatalf("expected next vol=1 arc=2, got vol=%d arc=%d", b.NextVolume, b.NextArc)
	}
	if !b.NeedsExpansion {
		t.Fatal("expected NeedsExpansion=true for skeleton arc")
	}
}

// ── CheckArcBoundary 规格测试 ──
//
// ArcBoundary 是分层书工作流的「事实入口」：IsArcEnd / IsVolumeEnd /
// NeedsExpansion / NeedsNewVolume 直接驱动 Route 分支 6-10。Route 穷举测试钉的是
// 「给定 State → 派谁」，State.ArcBoundary 的正确性依赖本函数——它若在弧边界
// 错位、末章判断、跨卷切分上有边界 bug,会直接导致该评审不评审/该展开不展开/
// 该完本不完本,且 Router 测试完全测不到(State 是喂进去的)。这里单独钉死。

// 构造卷:每章一个 OutlineEntry,卷内弧数可变。
func vol(idx int, arcs ...domain.ArcOutline) domain.VolumeOutline {
	return domain.VolumeOutline{Index: idx, Title: "卷", Theme: "主题", Arcs: arcs}
}

func arc(idx, chapters int) domain.ArcOutline {
	out := domain.ArcOutline{Index: idx, Title: "弧", Goal: "目标"}
	if chapters > 0 {
		for i := 1; i <= chapters; i++ {
			out.Chapters = append(out.Chapters, domain.OutlineEntry{Title: "章", CoreEvent: "事件", Hook: "钩子"})
		}
	} else {
		out.EstimatedChapters = 8
	}
	return out
}

func TestCheckArcBoundary_Spec(t *testing.T) {
	tests := []struct {
		name    string
		volumes []domain.VolumeOutline
		chapter int
		want    *ArcBoundary // nil = 期待返回 nil(nil)
		wantErr bool
	}{
		{
			name:    "空大纲→无边界",
			volumes: []domain.VolumeOutline{},
			chapter: 1,
			want:    nil,
		},
		{
			name: "弧中章→非弧末,无 Next 信息",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 3), arc(2, 0)),
			},
			chapter: 2, // 弧1第2/3章
			want:    &ArcBoundary{Volume: 1, Arc: 1},
		},
		{
			name: "首弧末→指向下一骨架弧并需展开",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 3), arc(2, 0)),
			},
			chapter: 3,
			want: &ArcBoundary{
				IsArcEnd: true, Volume: 1, Arc: 1,
				NextVolume: 1, NextArc: 2, NeedsExpansion: true,
			},
		},
		{
			name: "末弧末且卷非末→跨卷指向下一卷首弧",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 2)),
				vol(2, arc(1, 0)),
			},
			chapter: 2, // 卷1末弧最后一章
			want: &ArcBoundary{
				IsArcEnd: true, IsVolumeEnd: true, Volume: 1, Arc: 1,
				NextVolume: 2, NextArc: 1, NeedsExpansion: true,
			},
		},
		{
			name: "卷末且无下一卷→NeedsNewVolume",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 2)),
			},
			chapter: 2,
			want: &ArcBoundary{
				IsArcEnd: true, IsVolumeEnd: true, Volume: 1, Arc: 1,
				NeedsNewVolume: true,
			},
		},
		{
			name: "卷2首章→定位正确且非弧末",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 2)),
				vol(2, arc(1, 3)),
			},
			chapter: 3,
			want:    &ArcBoundary{Volume: 2, Arc: 1},
		},
		{
			name: "下一弧已展开→NeedsExpansion=false",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 1), arc(2, 2)),
			},
			chapter: 1,
			want: &ArcBoundary{
				IsArcEnd: true, Volume: 1, Arc: 1,
				NextVolume: 1, NextArc: 2,
			},
		},
		{
			name: "单章成弧且卷中多弧→弧末即卷末判断正确",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 1), arc(2, 1)),
			},
			chapter: 2, // 卷末弧的唯一一章
			want: &ArcBoundary{
				IsArcEnd: true, IsVolumeEnd: true, Volume: 1, Arc: 2,
				NeedsNewVolume: true,
			},
		},
		{
			name: "章节超出大纲范围→nil",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 2)),
			},
			chapter: 3,
			want:    nil,
		},
		{
			name: "章节为0→nil",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 2)),
			},
			chapter: 0,
			want:    nil,
		},
		{
			name: "骨架弧中的骨架弧不参与章节定位→定位仅展开弧",
			volumes: []domain.VolumeOutline{
				vol(1, arc(1, 0), arc(2, 2)), // 首弧是骨架,章节从弧2开始
			},
			chapter: 1, // 全局第1章 = 弧2第1/2章
			want:    &ArcBoundary{Volume: 1, Arc: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setupLayered(t, tt.volumes)
			got, err := s.Outline.CheckArcBoundary(tt.chapter)
			if err != nil {
				if tt.wantErr {
					return
				}
				t.Fatalf("CheckArcBoundary: %v", err)
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %+v, got nil", tt.want)
			}
			if *got != *tt.want {
				t.Fatalf("boundary mismatch:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// ExpandArc 幂等守卫:同参重试放行,内容不同的二次展开拒绝(已写章节锚点保护)。
func TestExpandArcGuard_RejectsOverwrite(t *testing.T) {
	ch := func(title string) domain.OutlineEntry {
		return domain.OutlineEntry{Title: title, CoreEvent: "事件", Hook: "钩子"}
	}
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "第一卷", Theme: "主题",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "首弧", Goal: "目标", Chapters: []domain.OutlineEntry{ch("章一"), ch("章二")}},
			{Index: 2, Title: "骨架弧", Goal: "目标", EstimatedChapters: 8},
		},
	}})

	// 首次展开骨架弧:成功
	if err := s.ExpandArc(1, 2, []domain.OutlineEntry{ch("新章一"), ch("新章二")}); err != nil {
		t.Fatalf("first expand: %v", err)
	}

	// 同参重放(崩溃/网络重试):幂等放行
	if err := s.ExpandArc(1, 2, []domain.OutlineEntry{ch("新章一"), ch("新章二")}); err != nil {
		t.Fatalf("identical replay should pass: %v", err)
	}

	// 内容不同的二次展开:拒绝
	if err := s.ExpandArc(1, 2, []domain.OutlineEntry{ch("不同内容")}); err == nil {
		t.Fatal("different-content re-expand must be rejected")
	}

	// 已展开弧(弧1)被误传:同样拒绝
	if err := s.ExpandArc(1, 1, []domain.OutlineEntry{ch("覆盖")}); err == nil {
		t.Fatal("re-expanding an already expanded arc must be rejected")
	}
}

func TestAppendVolumeValidation(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "第一卷", Theme: "起步",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "首弧", Goal: "目标",
			Chapters: []domain.OutlineEntry{{Title: "章", CoreEvent: "事件", Hook: "钩子"}},
		}},
	}})

	validVol := domain.VolumeOutline{
		Index: 2, Title: "第二卷", Theme: "升级",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "弧一", Goal: "目标",
			Chapters: []domain.OutlineEntry{{Title: "新章", CoreEvent: "推进", Hook: "钩子"}},
		}},
	}

	// 正常追加应成功
	if err := s.AppendVolume(validVol); err != nil {
		t.Fatalf("AppendVolume valid: %v", err)
	}

	// Index 不递增 → 失败
	if err := s.AppendVolume(domain.VolumeOutline{
		Index: 1, Title: "重复", Theme: "x",
		Arcs: []domain.ArcOutline{{Index: 1, Title: "弧", Goal: "g", Chapters: []domain.OutlineEntry{{Title: "ch", CoreEvent: "e", Hook: "h"}}}},
	}); err == nil {
		t.Fatal("expected error for non-increasing index")
	}

	// 无弧 → 失败
	if err := s.AppendVolume(domain.VolumeOutline{Index: 3, Title: "空", Theme: "x"}); err == nil {
		t.Fatal("expected error for volume with no arcs")
	}

	// 首弧无章节 → 失败
	if err := s.AppendVolume(domain.VolumeOutline{
		Index: 3, Title: "骨架", Theme: "x",
		Arcs: []domain.ArcOutline{{Index: 1, Title: "弧", Goal: "g", EstimatedChapters: 10}},
	}); err == nil {
		t.Fatal("expected error for first arc without chapters")
	}
}

// 注：原先用 Final 卷拒绝 append 的语义已下沉到 save_foundation 层（Phase=Complete 拒绝），
// 见 save_foundation_test.go::TestSaveFoundationAppendVolumeRejectsAfterComplete。
// store 层只保留结构性校验（Index 递增 / 首弧含章节等）。

func TestSaveAndLoadCompass(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// 空 direction 应失败
	if err := s.Outline.SaveCompass(domain.StoryCompass{EstimatedScale: "3 卷"}); err == nil {
		t.Fatal("expected error for empty ending_direction")
	}

	// 正常保存
	compass := domain.StoryCompass{
		EndingDirection: "主角面对最终抉择",
		OpenThreads:     []string{"线索A", "关系B"},
		EstimatedScale:  "预计 4-6 卷",
		LastUpdated:     12,
	}
	if err := s.Outline.SaveCompass(compass); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}

	loaded, err := s.Outline.LoadCompass()
	if err != nil {
		t.Fatalf("LoadCompass: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected compass, got nil")
	}
	if loaded.EndingDirection != "主角面对最终抉择" {
		t.Fatalf("expected direction %q, got %q", "主角面对最终抉择", loaded.EndingDirection)
	}
	if len(loaded.OpenThreads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(loaded.OpenThreads))
	}
}

// TestOutlineFeedbackPool 反馈池闭环:commit 落盘 → 跨重启可读 → 结构操作消费清空。
func TestOutlineFeedbackPool(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Outline.AppendOutlineFeedback(ChapterFeedback{Chapter: 3, Deviation: "支线膨胀", Suggestion: "下一弧收线"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.Outline.AppendOutlineFeedback(ChapterFeedback{Chapter: 4, Suggestion: "反派提前登场"}); err != nil {
		t.Fatalf("append2: %v", err)
	}

	// 跨重启(新 Store 实例)可读——不是内存态
	s2 := NewStore(dir)
	fbs := s2.Outline.LoadPendingOutlineFeedback()
	if len(fbs) != 2 || fbs[0].Chapter != 3 || fbs[1].Suggestion != "反派提前登场" {
		t.Fatalf("跨重启读取失败: %+v", fbs)
	}
	for _, fb := range fbs {
		if fb.At == "" {
			t.Fatal("At 应自动补齐")
		}
	}

	if err := s2.Outline.ClearOutlineFeedback(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if left := s2.Outline.LoadPendingOutlineFeedback(); len(left) != 0 {
		t.Fatalf("消费后应为空: %+v", left)
	}
	// 幂等清空
	if err := s2.Outline.ClearOutlineFeedback(); err != nil {
		t.Fatalf("clear idempotent: %v", err)
	}
}
