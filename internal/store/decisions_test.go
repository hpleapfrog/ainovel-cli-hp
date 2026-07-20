package store

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDecisionStore_AppendAndRecent(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	first, err := s.Decisions.Append(DecisionRecord{
		Kind: "intervention", Decider: "arbiter",
		Input: "重写第3章", Facts: json.RawMessage(`{"phase":"writing"}`),
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if first.ID == "" || first.At == "" || first.SchemaVersion != decisionSchemaVersion {
		t.Fatalf("Append 应补齐 ID/At/SchemaVersion: %+v", first)
	}

	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "继续写"}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	// 失败裁定:error 是审计事实,必须原样落盘并可读回。
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "plan_start", Decider: "arbiter", Input: "凡人修仙", Error: "USER_INACTIVE"}); err != nil {
		t.Fatalf("append 3: %v", err)
	}

	recent, err := s.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("应有 3 条记录, got %d", len(recent))
	}
	if recent[2].Error != "USER_INACTIVE" || len(recent[2].Decision) != 0 {
		t.Fatalf("失败裁定应带 error 且无 decision: %+v", recent[2])
	}
	if recent[0].Input != "重写第3章" || recent[1].Input != "继续写" {
		t.Fatalf("记录顺序应为旧→新: %+v", recent)
	}

	// n 截取:只要最近 1 条
	last, err := s.Decisions.Recent(1)
	if err != nil || len(last) != 1 || last[0].Input != "凡人修仙" {
		t.Fatalf("Recent(1) 应取最新一条, got %+v err=%v", last, err)
	}
}

func TestDecisionStore_InputTruncation(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	huge := strings.Repeat("长", maxDecisionInputBytes) // 3 字节/字,远超上限
	rec, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: huge})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !rec.InputTruncated || len(rec.Input) > maxDecisionInputBytes {
		t.Fatalf("超限 input 必须截断并标记: truncated=%v len=%d", rec.InputTruncated, len(rec.Input))
	}
	// 截断后的记录仍然可读回
	recent, err := s.Decisions.Recent(1)
	if err != nil || len(recent) != 1 {
		t.Fatalf("读回失败: %v", err)
	}
}

func TestDecisionStore_RecentSkipsCorruptLines(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "好的"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// 模拟崩溃留下的尾部残行
	if err := s.Decisions.io.AppendLine(decisionsFile, []byte(`{"schema_version":1,"kind":"interv`)); err != nil {
		t.Fatalf("append corrupt: %v", err)
	}
	recent, err := s.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("损坏行不应让读取失败: %v", err)
	}
	if len(recent) != 1 || recent[0].Input != "好的" {
		t.Fatalf("应跳过损坏行保留完整记录, got %+v", recent)
	}
}

// 截断必须回退到 rune 边界：多字节字符不被切断，且不超 8192 字节上限。
func TestDecisionStore_InputTruncationRuneBoundary(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	// 多字节字符恰跨越 8192 字节边界：整字丢弃，不留无效尾巴
	input := strings.Repeat("a", maxDecisionInputBytes-3) + "长" + "尾"
	rec, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: input})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !rec.InputTruncated || len(rec.Input) != maxDecisionInputBytes {
		t.Fatalf("应截到 %d 字节: truncated=%v len=%d", maxDecisionInputBytes, rec.InputTruncated, len(rec.Input))
	}
	if !strings.HasSuffix(rec.Input, "长") || !utf8.ValidString(rec.Input) {
		t.Fatalf("截断点应在完整字符边界: suffix=%q valid=%v", rec.Input[len(rec.Input)-3:], utf8.ValidString(rec.Input))
	}

	// 多字节字符从边界前一字节开始、跨界：整字丢弃
	input = strings.Repeat("a", maxDecisionInputBytes-2) + "长"
	rec, err = s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: input})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !rec.InputTruncated || len(rec.Input) != maxDecisionInputBytes-2 || !utf8.ValidString(rec.Input) {
		t.Fatalf("跨界字符应整体丢弃: truncated=%v len=%d valid=%v", rec.InputTruncated, len(rec.Input), utf8.ValidString(rec.Input))
	}

	// 多字节字符恰好在边界内完整结束：不触发截断
	input = strings.Repeat("a", maxDecisionInputBytes-3) + "长"
	rec, err = s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: input})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if rec.InputTruncated || rec.Input != input {
		t.Fatalf("恰好 %d 字节不应截断: truncated=%v len=%d", maxDecisionInputBytes, rec.InputTruncated, len(rec.Input))
	}
}
