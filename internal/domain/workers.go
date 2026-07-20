package domain

// Worker agent 名字的唯一事实源。
// agents.BuildWorkers 注册、flow.Route 派发、arbiter 派单校验共用同一组常量，
// 防止多处硬编码漂移（新增 Worker 只改这里，三处同时生效）。
const (
	WorkerArchitectShort = "architect_short"
	WorkerArchitectLong  = "architect_long"
	WorkerWriter         = "writer"
	WorkerEditor         = "editor"
)
