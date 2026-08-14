package store

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func newCastTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func TestCastMergeAppearances_NewEntries(t *testing.T) {
	s := newCastTestStore(t)
	intros := []domain.CastIntro{{Name: "老周", BriefRole: "客栈老板"}}
	if err := s.Cast.MergeAppearances(5, []string{"老周", "阿云"}, intros, nil); err != nil {
		t.Fatalf("MergeAppearances: %v", err)
	}

	entries, err := s.Cast.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.FirstSeenChapter != 5 || e.LastSeenChapter != 5 || e.AppearanceCount != 1 {
			t.Errorf("entry %s: unexpected appearance fields %+v", e.Name, e)
		}
		if e.Name == "老周" && e.BriefRole != "客栈老板" {
			t.Errorf("expected BriefRole 客栈老板 for 老周, got %q", e.BriefRole)
		}
		if e.Name == "阿云" && e.BriefRole != "" {
			t.Errorf("阿云 没有 intro，BriefRole 应为空，得到 %q", e.BriefRole)
		}
	}
}

func TestCastMergeAppearances_AccumulatesOnRepeat(t *testing.T) {
	s := newCastTestStore(t)
	if err := s.Cast.MergeAppearances(5, []string{"老周"}, nil, nil); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	if err := s.Cast.MergeAppearances(8, []string{"老周"}, nil, nil); err != nil {
		t.Fatalf("second merge: %v", err)
	}

	entries, _ := s.Cast.Load()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.FirstSeenChapter != 5 || e.LastSeenChapter != 8 || e.AppearanceCount != 2 {
		t.Fatalf("expected first=5,last=8,count=2; got %+v", e)
	}
	if len(e.AppearanceChapters) != 2 || e.AppearanceChapters[0] != 5 || e.AppearanceChapters[1] != 8 {
		t.Errorf("AppearanceChapters wrong: %v", e.AppearanceChapters)
	}
}

func TestCastMergeAppearances_IsIdempotent(t *testing.T) {
	s := newCastTestStore(t)
	if err := s.Cast.MergeAppearances(5, []string{"老周"}, nil, nil); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	// 同一章 commit 重复触发（崩溃恢复或重写场景）
	if err := s.Cast.MergeAppearances(5, []string{"老周"}, nil, nil); err != nil {
		t.Fatalf("second merge: %v", err)
	}

	entries, _ := s.Cast.Load()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].AppearanceCount != 1 {
		t.Errorf("expected AppearanceCount=1 after duplicate, got %d", entries[0].AppearanceCount)
	}
}

func TestCastMergeAppearances_FiltersCoreCharacters(t *testing.T) {
	s := newCastTestStore(t)
	core := map[string]bool{"林墨": true, "李清砚": true}
	if err := s.Cast.MergeAppearances(3, []string{"林墨", "李清砚", "老周"}, nil, core); err != nil {
		t.Fatalf("MergeAppearances: %v", err)
	}

	entries, _ := s.Cast.Load()
	if len(entries) != 1 || entries[0].Name != "老周" {
		t.Fatalf("expected only 老周 in ledger, got %+v", entries)
	}
}

func TestCastMergeAppearances_BackfillsBriefRole(t *testing.T) {
	s := newCastTestStore(t)
	// 第 5 章引入老周但 Writer 忘填 brief_role
	if err := s.Cast.MergeAppearances(5, []string{"老周"}, nil, nil); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	// 第 8 章再次出现，Writer 这次补了 brief_role
	intros := []domain.CastIntro{{Name: "老周", BriefRole: "客栈老板"}}
	if err := s.Cast.MergeAppearances(8, []string{"老周"}, intros, nil); err != nil {
		t.Fatalf("second merge: %v", err)
	}

	entries, _ := s.Cast.Load()
	if entries[0].BriefRole != "客栈老板" {
		t.Errorf("expected BriefRole 客栈老板 backfilled, got %q", entries[0].BriefRole)
	}
}

func TestCastMergeAppearances_NoOverwriteBriefRole(t *testing.T) {
	s := newCastTestStore(t)
	// 第 5 章定下 BriefRole=客栈老板
	if err := s.Cast.MergeAppearances(5,
		[]string{"老周"},
		[]domain.CastIntro{{Name: "老周", BriefRole: "客栈老板"}},
		nil,
	); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	// 第 8 章 Writer 错误地传了不同的 BriefRole（不应覆盖）
	if err := s.Cast.MergeAppearances(8,
		[]string{"老周"},
		[]domain.CastIntro{{Name: "老周", BriefRole: "赌坊打手"}},
		nil,
	); err != nil {
		t.Fatalf("second merge: %v", err)
	}

	entries, _ := s.Cast.Load()
	if entries[0].BriefRole != "客栈老板" {
		t.Errorf("expected BriefRole NOT overwritten, got %q", entries[0].BriefRole)
	}
}

func TestCastRecentActive_OrdersByLastSeen(t *testing.T) {
	s := newCastTestStore(t)
	_ = s.Cast.MergeAppearances(3, []string{"A"}, nil, nil)
	_ = s.Cast.MergeAppearances(10, []string{"B"}, nil, nil)
	_ = s.Cast.MergeAppearances(7, []string{"C"}, nil, nil)

	recent, err := s.Cast.RecentActive(2)
	if err != nil {
		t.Fatalf("RecentActive: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2, got %d", len(recent))
	}
	if recent[0].Name != "B" || recent[1].Name != "C" {
		t.Errorf("expected order B, C; got %s, %s", recent[0].Name, recent[1].Name)
	}
}

func TestCastRecentActive_SkipsPromoted(t *testing.T) {
	s := newCastTestStore(t)
	if err := s.Cast.Save([]domain.CastEntry{
		{Name: "已升核心", LastSeenChapter: 20, AppearanceCount: 8, Promoted: true},
		{Name: "活跃配角", LastSeenChapter: 18, AppearanceCount: 3},
		{Name: "另一配角", LastSeenChapter: 15, AppearanceCount: 2},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	recent, err := s.Cast.RecentActive(10)
	if err != nil {
		t.Fatalf("RecentActive: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 (Promoted excluded), got %d: %+v", len(recent), recent)
	}
	for _, e := range recent {
		if e.Promoted {
			t.Errorf("Promoted entry leaked into RecentActive: %+v", e)
		}
	}
	if recent[0].Name != "活跃配角" {
		t.Errorf("expected first=活跃配角, got %s", recent[0].Name)
	}
}

func TestCastMergeAppearances_NoOpOnEmpty(t *testing.T) {
	s := newCastTestStore(t)
	if err := s.Cast.MergeAppearances(5, nil, nil, nil); err != nil {
		t.Fatalf("MergeAppearances empty: %v", err)
	}
	if err := s.Cast.MergeAppearances(0, []string{"老周"}, nil, nil); err != nil {
		t.Fatalf("MergeAppearances chapter=0: %v", err)
	}
	entries, _ := s.Cast.Load()
	if len(entries) != 0 {
		t.Errorf("expected empty ledger, got %d entries", len(entries))
	}
}

// ── Promote ──

func TestCastPromote_MarksAndMergesAliases(t *testing.T) {
	s := newCastTestStore(t)
	_ = s.Cast.MergeAppearances(5, []string{"李掌柜"}, []domain.CastIntro{{Name: "李掌柜", BriefRole: "当铺老板"}}, nil)

	if err := s.Cast.Promote("李掌柜", []string{"老李", "李掌柜"}); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	entries, _ := s.Cast.Load()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if !e.Promoted {
		t.Fatalf("expected Promoted=true, got %+v", e)
	}
	if len(e.Aliases) != 1 || e.Aliases[0] != "老李" {
		t.Fatalf("expected aliases=[老李] (正式名不重复入别名), got %v", e.Aliases)
	}

	recent, err := s.Cast.RecentActive(10)
	if err != nil {
		t.Fatalf("RecentActive: %v", err)
	}
	if len(recent) != 0 {
		t.Fatalf("promoted entry must be skipped in RecentActive, got %+v", recent)
	}
}

func TestCastPromote_FindsByAliasAndIsIdempotent(t *testing.T) {
	s := newCastTestStore(t)
	_ = s.Cast.MergeAppearances(3, []string{"老李"}, nil, nil)

	// 按别名升格，正式名归一为传入名
	if err := s.Cast.Promote("李掌柜", []string{"老李"}); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	// 重复升格幂等
	if err := s.Cast.Promote("李掌柜", []string{"老李", "李叔"}); err != nil {
		t.Fatalf("second Promote: %v", err)
	}

	entries, _ := s.Cast.Load()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Name != "李掌柜" || !e.Promoted {
		t.Fatalf("expected canonical name=李掌柜 Promoted=true, got %+v", e)
	}
	if len(e.Aliases) != 2 {
		t.Fatalf("expected aliases=[老李 李叔], got %v", e.Aliases)
	}
}

func TestCastPromote_MissingEntry(t *testing.T) {
	s := newCastTestStore(t)
	if err := s.Cast.Promote("不存在", nil); err == nil {
		t.Fatal("expected error for missing entry")
	}
}
