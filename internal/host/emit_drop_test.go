package host

import "testing"

// TestEmitEventCountsDrops 通道满时丢最旧必须计数；未满通道不计数。
// （"丢新"分支只在并发抢满时可达，单 goroutine 下无法确定性触发，这里只锁驱逐计数。）
func TestEmitEventCountsDrops(t *testing.T) {
	h := &Host{events: make(chan Event, 2), streamCh: make(chan string, 2)}

	h.emitEvent(Event{Summary: "a"})
	h.emitEvent(Event{Summary: "b"})
	if got := h.droppedEvents.Load(); got != 0 {
		t.Fatalf("buffer not full, expected 0 drops, got %d", got)
	}

	h.emitEvent(Event{Summary: "c"}) // 满：驱逐最旧的 a 后入队
	if got := h.droppedEvents.Load(); got != 1 {
		t.Fatalf("expected 1 drop after eviction, got %d", got)
	}
	if len(h.events) != 2 {
		t.Fatalf("channel should stay full, got %d", len(h.events))
	}
	if ev := <-h.events; ev.Summary != "b" {
		t.Fatalf("oldest survivor should be b, got %q", ev.Summary)
	}
}

func TestEmitDeltaCountsDrops(t *testing.T) {
	h := &Host{streamCh: make(chan string, 1)}

	h.emitDelta("x")
	if got := h.droppedDeltas.Load(); got != 0 {
		t.Fatalf("buffer not full, expected 0 drops, got %d", got)
	}

	h.emitDelta("y") // 满：驱逐 x 后入队
	h.emitDelta("z")
	if got := h.droppedDeltas.Load(); got != 2 {
		t.Fatalf("expected 2 drops, got %d", got)
	}
}
