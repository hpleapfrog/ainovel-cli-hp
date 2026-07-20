package host

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"sync/atomic"
)

// errorKind classifies a runtime error into a stable, short label for log
// filtering and alert routing. Returns "" when no special tag applies.
//
// err is the live error chain (may be nil after JSON serialization); msg is
// the rendered string fallback used when the chain has been flattened
// (e.g. inside sub-agent JSON results).
func errorKind(err error, msg string) string {
	if err != nil && errors.Is(err, agentcore.ErrProviderStreamIdle) {
		return "stream_idle"
	}
	if msg != "" && agentcore.IsStreamIdleMessage(msg) {
		return "stream_idle"
	}
	return ""
}

// 单调递增的事件 ID 计数器；配合时间戳生成稳定 ID。
var eventIDCounter uint64

func nextEventID() string {
	return fmt.Sprintf("e%d", atomic.AddUint64(&eventIDCounter, 1))
}

// activeCall 记录一次正在进行的调用（TOOL / DISPATCH）的 ID、起点时间与 summary。
// summary 在完成事件时回填进 finish Event，保证 replay（runtime queue）能还原行内容。
type activeCall struct {
	id      string
	start   time.Time
	summary string
	depth   int
}

// observer 把 Engine 派发与 Worker 进度投影到 Host 的输出通道。
// 它是纯观察者,不参与任何控制决策。
type observer struct {
	emitEv func(Event)
	emitD  func(string)
	emitC  func()
	store  *storepkg.Store // 用于 runtime queue 持久化（ReplayQueue 消费）

	// mu 统一保护 agents 及下方全部进度/流式状态。写入方不止 engine goroutine：
	// host.go 对 h.runCtx 注册了 WithToolProgress(h.runCtx, h.observer.workerProgress)，
	// 干预 goroutine 中的 Arbiter 调用（含 ReportToolProgress 的 ProgressRetry）会走
	// 同一回调。持锁期间不得调用 emitEv/emitD/emitC 等外部回调（防回调重入死锁）——
	// 一律先在锁内取数/改状态，解锁后再回调。
	mu     sync.Mutex
	agents map[string]*agentState

	// aborting 由 Host 在 Abort()/Close() 入口置位、Start/Resume/Continue 清位。
	// 置位期间所有 context-cancel 衍生的错误事件被抑制（既是用户期望，也避免与
	// "用户手动暂停"事件重复）。真实异常（非 cancel）仍照常上报。
	aborting atomic.Bool

	streamThinking      bool
	lastThinkingByAgent map[string]string          // agent → 最近的累积 thinking 文本（用于提取增量 delta）
	dispatchStarts      map[string]*activeCall     // dispatched agent → 进行中的 DISPATCH 调用
	toolStarts          map[string]*activeCall     // agent → 进行中的 TOOL 调用
	streamExtractors    map[string]*agentExtractor // agent → 当前工具调用 JSON 参数的内容抽取器
	streamArgPrefixes   map[string]string          // agent/tool → 参数流前缀，用于提前识别轻量标签
	streamArgLabels     map[string]string          // agent/tool → 已从参数流提前识别出的展示名
	retryEvents         map[string]string          // retry scope → event ID，用同一行原地更新 (2/7)
	streamHasContent    bool                       // 当前 streamRound 是否已输出过内容（判断是否需要段落分隔）
	streamLastByte      byte                       // 最近一次流式输出的末字节（用于精确补齐换行）
}

// agentExtractor 记录某个 agent 当前正在抽取的工具名与抽取器实例。
// 工具名用于检测"新的工具调用开始了"，避免缓存被上一轮残留污染。
type agentExtractor struct {
	tool       string
	ext        *jsonFieldExtractor
	emittedAny bool // 本 extractor 是否已经产出过内容；用于首次输出前补段落分隔
}

type agentState struct {
	name    string
	state   string
	tool    string
	summary string
	turn    int
	context AgentContextSnapshot
	updated time.Time
}

func newObserver(s *storepkg.Store, emitEv func(Event), emitD func(string), emitC func()) *observer {
	return &observer{
		emitEv:              emitEv,
		emitD:               emitD,
		emitC:               emitC,
		store:               s,
		agents:              make(map[string]*agentState),
		lastThinkingByAgent: make(map[string]string),
		dispatchStarts:      make(map[string]*activeCall),
		toolStarts:          make(map[string]*activeCall),
		streamExtractors:    make(map[string]*agentExtractor),
		streamArgPrefixes:   make(map[string]string),
		streamArgLabels:     make(map[string]string),
		retryEvents:         make(map[string]string),
	}
}

// ── Engine 直驱入口 ──
//
// Engine 直接运行 Worker，事件来源分为两条:
//  1. dispatchStart/dispatchFinish —— Engine 在派发边界直接调用(DISPATCH 行)
//  2. workerProgress —— Worker 的进度中继(ctx ToolProgress)，
//     由 handleToolUpdate 统一处理 TOOL/流式正文/thinking/retry/context
//     (TOOL 行/流式正文/thinking/retry/context)。

// dispatchStart 记录一次 Worker 派发开始并发 DISPATCH 行。
func (o *observer) dispatchStart(agent, task string) {
	summary := dispatchSummary(agent, task)
	o.updateAgent(agent, func(a *agentState) {
		a.state = "working"
		a.tool = ""
		a.summary = fmt.Sprintf("engine → %s", summary)
	})
	id := nextEventID()
	o.mu.Lock()
	o.dispatchStarts[agent] = &activeCall{id: id, start: time.Now(), summary: summary}
	o.mu.Unlock()
	o.emitAndLog(Event{
		ID:       id,
		Time:     time.Now(),
		Category: "DISPATCH",
		Agent:    "engine",
		Summary:  summary,
		Level:    "info",
	})
}

// dispatchFinish 把 DISPATCH 行落成完成态并复位 Worker 状态;
// 清理该 Worker 名下的孤儿 TOOL 行(abort/错误路径 ProgressToolEnd 可能缺席)。
func (o *observer) dispatchFinish(agent string, failed bool) {
	o.updateAgent(agent, func(a *agentState) {
		a.state = "idle"
		a.tool = ""
	})
	// 锁内完成全部状态摘除并取出待完成调用，解锁后再发事件。
	o.mu.Lock()
	delete(o.lastThinkingByAgent, agent)
	toolCall, hasTool := o.toolStarts[agent]
	if hasTool {
		delete(o.toolStarts, agent)
		delete(o.streamExtractors, agent)
	}
	dispatchCall, hasDispatch := o.dispatchStarts[agent]
	if hasDispatch {
		delete(o.dispatchStarts, agent)
	}
	o.mu.Unlock()
	if hasTool {
		o.emitCallFinish(toolCall, "TOOL", agent, failed)
	}
	if hasDispatch {
		o.emitCallFinish(dispatchCall, "DISPATCH", agent, failed)
	}
	o.streamClear()
}

// workerProgress 把 Worker 进度中继适配为既有的 ToolExecUpdate 处理。
func (o *observer) workerProgress(p agentcore.ProgressPayload) {
	payload := p
	o.handleToolUpdate(agentcore.Event{Type: agentcore.EventToolExecUpdate, Progress: &payload})
}

func (o *observer) finalize() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, a := range o.agents {
		a.state = "idle"
		a.tool = ""
	}
}

// setAborting 由 Host 在 Abort/Close/Start 等生命周期切换处调用，控制
// "context canceled" 类衍生事件是否需要抑制（避免与"用户手动暂停"重复）。
func (o *observer) setAborting(v bool) { o.aborting.Store(v) }

func (o *observer) retryEventID(scope string, attempt int) string {
	if strings.TrimSpace(scope) == "" {
		scope = "engine"
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.retryEvents == nil {
		o.retryEvents = make(map[string]string)
	}
	if attempt <= 1 || o.retryEvents[scope] == "" {
		o.retryEvents[scope] = nextEventID()
	}
	return o.retryEvents[scope]
}

// emitAndLog 用于调用类事件的"开始"态：发给 TUI 但不写入 runtime queue，
// 避免 replay 时"开始一行、完成又一行"重复。slog 由 host.emitEvent 统一记录。
func (o *observer) emitAndLog(ev Event) {
	o.emitEv(ev)
}

// persistEvent 把事件写入 runtime queue（slog 由 host.emitEvent 统一记录）。
func (o *observer) persistEvent(ev Event) {
	if o.store == nil || o.store.Runtime == nil {
		return
	}
	priority := domain.RuntimePriorityBackground
	switch ev.Category {
	case "SYSTEM", "ERROR":
		priority = domain.RuntimePriorityControl
	}
	_, _ = o.store.Runtime.AppendQueue(domain.RuntimeQueueItem{
		Time:     ev.Time,
		Kind:     domain.RuntimeQueueUIEvent,
		Priority: priority,
		Category: ev.Category,
		Summary:  ev.Summary,
		Payload:  ev,
	})
}

func (o *observer) updateAgent(name string, fn func(*agentState)) {
	if name == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	a, ok := o.agents[name]
	if !ok {
		a = &agentState{name: name, state: "idle"}
		o.agents[name] = a
	}
	fn(a)
	a.updated = time.Now()
}

func (o *observer) agentSnapshots() []AgentSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	snaps := make([]AgentSnapshot, 0, len(o.agents))
	for _, a := range o.agents {
		snaps = append(snaps, AgentSnapshot{
			Name:      a.name,
			State:     a.state,
			Summary:   a.summary,
			Tool:      a.tool,
			Turn:      a.turn,
			Context:   a.context,
			UpdatedAt: a.updated,
		})
	}
	return snaps
}
