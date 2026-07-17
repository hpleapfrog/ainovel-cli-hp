# AGENTS.md — ainovel-cli

面向 AI 编码代理的仓库指南。假设读者对本项目一无所知；只收录从代码和文档核实过的事实。

## 项目速览

- **ainovel-cli**：全自动 AI 长篇小说创作引擎。核心设计是「事实层确定，语义层自主」——一个串行确定性 Engine（`flow.Route` 按事实路由，主循环零 LLM 开销）、三个自主 Worker（Architect / Writer / Editor，各自是 LLM 循环）、少数按需唤醒的 Arbiter 语义裁定函数、一个文件系统事实层（`store`）。
- Go 单仓库，模块路径 `github.com/voocel/ainovel-cli`，`go.mod` 声明 Go 1.25.5。
- 主入口：`cmd/ainovel-cli/main.go`。
- 关键依赖：`github.com/voocel/agentcore`（Agent 内核：tool-calling + streaming）、`github.com/voocel/litellm`（统一 LLM 接口适配，支持 OpenRouter / Anthropic / Gemini / OpenAI / ollama 等）、Bubble Tea / Bubbles / Lipgloss（TUI）。
- 交互模式：
  - TUI：`go run ./cmd/ainovel-cli`（首次启动进入交互式配置引导）。
  - Headless：`go run ./cmd/ainovel-cli --headless --prompt "写一本悬疑小说"`（必须有已存在的配置）。
- 运行产物默认落到 `./output/novel/`（`Config.FillDefaults` 归一）；换目录启动就是换一本书，删除 `output/` 即重置。输出结构（chapters/ drafts/ reviews/ meta/ checkpoints.jsonl 等）见 README「输出结构」一节。

## 常用命令

```bash
# 本地开发运行（TUI）
go run ./cmd/ainovel-cli

# 无界面运行（必须有配置；首次配置请走 TUI）
go run ./cmd/ainovel-cli --headless --prompt "写一本悬疑小说"

# 构建（Dockerfile 同款）
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ainovel-cli ./cmd/ainovel-cli

# 验证（GoReleaser before-hooks 跑的就是这串，提交前请本地跑同一串）
go mod tidy && go vet ./... && go test -count=1 ./...

# 运行单个测试包/函数
go test -count=1 ./internal/flow/...
go test -count=1 -run TestEngine_WritesBookToCompletion ./internal/host/

# 模型注册表重新生成（需要联网，从 OpenRouter 拉取，不需要 API Key）
go generate ./internal/models/...
```

CLI 子命令：`version`/`--version`、`update [版本]`（自更新，从 GitHub Releases 拉取）、`eval`（离线评测 harness）。`eval` 在常规 flag 解析前被 `main.go` 拦截，参数体系独立。

## 代码结构

| 目录 | 作用 |
|------|------|
| `cmd/ainovel-cli` | 入口；手写 CLI flag 解析，分派 TUI / headless / `eval` / `update` / `version` |
| `internal/bootstrap` | 首次引导流程（`RunSetup`）与配置加载/合并（`LoadConfig`）；`FillDefaults` 归一运行时字段 |
| `internal/entry/tui` | TUI 主循环与命令面板（`/model`、`/review`、`/diag` 等） |
| `internal/entry/headless` | 无界面运行与事件消费 |
| `internal/entry/startup` | 快速开始 / 共创规划两种启动流程，最终收敛为同一份创作指令 |
| `internal/host` | Engine 宿主与事件总线（`host.New`、`engine.go`）；子目录 `exp` 为导出（txt/epub），`imp` 为导入拆分/分析 |
| `internal/flow` | 确定性路由 `flow.Route` + 状态机约束；含 `router_exhaustive_test.go` 穷举规格 |
| `internal/domain` | Phase/Flow 事实类型与状态迁移规则；含 `transitions_test.go` |
| `internal/agents` | 子代理（Worker）配置与包装；子目录 `ctxpack` 为上下文压缩管线，`guard` 为子代理护栏 |
| `internal/tools` | Worker 可调用的工具：plan/draft/check/commit/edit/read_chapter、continuity、premise_structure、novel_context、ask_user 等；每个工具成功后追加 Step 级 checkpoint |
| `internal/arbiter` | Arbiter 语义裁定函数（plan_start / intervention / failure），每次裁定落盘可回放 |
| `internal/store` | 文件系统持久化：progress、outline、checkpoints、decisions、cast、session 等 |
| `internal/models` | 模型注册表与定价；`models_generated.go` 由 `go generate` 生成 |
| `internal/rules` | 写作规则（去 AI 味）加载/lint/快照；`internal/userrules` 为用户规则运行时 |
| `internal/diag` | `/diag` 诊断：快照、规则检查、脱敏导出 |
| `internal/eval` | 离线评测 harness：`ainovel-cli eval --cases ...` |
| `internal/version` | 版本信息解析与自更新 |
| `assets/` | 嵌入的提示词、参考文档、风格预设；经 `//go:embed`（`assets/load.go`）编译进二进制 |
| `evals/cases` | 评测用例（如 `smoke/`）；`docs/` 为各子系统设计文档（架构、上下文管理、评测、voice layer 等） |

## 关键约束

- **位置参数已废弃**：`--prompt` 和 `--prompt-file` 只能在 `--headless` 模式下使用；TUI 不接受命令行需求（`main.go` 直接报错）。
- **首次配置必须走 TUI**：headless 模式下没有配置会报错退出，不会进入引导流程。
- **`provider` 是 `providers` 映射的 key，不是协议名**：`provider`（及 `roles.*.provider`）的值必须在 `providers` 里有同名条目，否则启动报「未配置凭证」。
- **配置加载顺序**：`~/.ainovel/config.json` → `./.ainovel/config.json` → `--config path`（后者覆盖前者）。标量字段后者覆盖前者；`providers` 和 `roles` 按 key 合并、同名字段覆盖；不支持用空字符串清空上层值。配置文件支持 `//` 行注释（见 `config.example.jsonc`）。
- **输出目录是运行时事实**：`internal/host` 默认用 `output/novel`；测试与 eval 自己指定临时目录。
- **`FillDefaults` 必须先于资产加载**：`OutputDir` 默认值在 `FillDefaults` 里归一，`main.go` 依赖此顺序加载本书级文风覆盖。
- **模型注册表是生成的**：`internal/models/models_generated.go` 由 `go generate ./internal/models/...`（`registry.go` 里的 `//go:generate go run gen_models.go`）生成，不要手动编辑。
- **无 Makefile / golangci 配置**：没有独立的 push/PR CI 工作流；`.github/workflows/release.yml` 在打 `v*` tag 时跑 GoReleaser，其 before-hooks 即 `go mod tidy && go vet ./... && go test -count=1 ./...`。本地提交前请跑同一串。
- **Commit 消息约定**：GoReleaser changelog 按前缀分组（`feat` / `fix` / `perf` / `refactor` 入 changelog；`docs:` / `test:` / `chore:` / `ci:` / `style:` 被过滤），请沿用该前缀风格。

## 测试与评测

- 单元/集成测试：`go test -count=1 ./...`。测试大量使用脚本化 `ChatModel` 和 `t.TempDir`，不需要真实 LLM，可离线跑。
- 改 `flow.Route` 或状态迁移规则后，**必须**同步更新 `internal/flow/router_exhaustive_test.go` 和 `internal/domain/transitions_test.go` 里的穷举规格。
- `internal/eval` 是真实 LLM 离线评测 harness：
  ```bash
  go run ./cmd/ainovel-cli eval --cases evals/cases/smoke --config ~/.ainovel/config.json --max-chapters 1 --timeout 10m
  ```
  会实际调用模型并产生费用；CI 不自动运行。
- 辅助脚本 `scripts/check_chapter_wordcount.py`：检查章节文件字数（低于 3000 字提示扩充），独立工具，不参与构建。

## 发布与部署

- **Release**：打 `v*` tag 触发 `.github/workflows/release.yml` → 生成 release notes（`.github/scripts/gen-changelog.sh`，需 LLM API key secret）→ GoReleaser 构建 linux/darwin/windows × amd64/arm64 并发布到 GitHub Releases。版本号经 `-X main.version=...` 注入。
- **Docker**：同一 tag 触发 `.github/workflows/docker.yml`，构建多架构镜像推到 `ghcr.io/voocel/ainovel-cli`。`Dockerfile` 为两阶段构建（golang:1.25 → alpine），ENTRYPOINT 即 CLI，工作目录 `/workspace`。
- **安装脚本**：`scripts/install.sh`（macOS/Linux 一键安装）；Windows 走 Releases 手动下载。

## 开发提示

- `.gitattributes` 强制全仓库 LF 行尾（`* text=auto eol=lf`）；Windows 开发时注意不要以 CRLF 提交。
- `.gitignore` 已排除 `output*`、`workspace/`、`.ainovel/`（含密钥）、`dist/`、`*.exe`、`release-notes.md`。
- 自定义提示词/文风不应改源码：把 `.md` 文件放进 `~/.ainovel/style/` 或 `<outputDir>/style/`；规则见 `docs/voice-layer.md` 和 README「自定义文风（Voice Layer）」。
- 调试运行问题：headless 默认在 `<书目录>/logs/headless.log` 写日志（`internal/entry/headless/run.go` 的 `logger.SetupFile`）；TUI 可用 `/diag` 导出脱敏诊断；启动致命错误会落盘到 `~/.ainovel/last-error.log`。
- 代码与注释主语言为中文；提交信息、代码标识符遵循仓库现有风格。

## 项目认知
- 这是 Go 1.25 的 AI 长篇小说引擎：确定性 Engine（internal/flow、internal/host）按事实路由，
  驱动三个 LLM Worker（architect_long/architect_short、writer、editor，装配在 internal/agents/build.go）
  和 Arbiter 裁定函数（internal/arbiter，提示词在 assets/prompts/arbiter-*.md）。
- 核心设计哲学："事实层确定，语义层自主"。能被代码保证的行为绝不写进 prompt；
  prompt 只装审美标准与判断指引。任何改动前先读 README.md「设计理念」和 assets/README.md「新内容归属判断（五问）」。

## 铁律（违反即返工）
1. 工具只返事实 JSON，绝不往工具返回值里夹带指令字符串；「下一步派谁」归 internal/flow 决策表。
2. 改 internal/flow/router.go 决策表前，必须先改对应的穷举规格测试，再改实现。
3. 改任何 assets/prompts/*.md 时，以下内容一字不动：
   - 工具名、工具参数名与参数形状（save_foundation 的 type 枚举、commit_chapter 参数、
     save_review 的七维数组、arbiter 输出 JSON 的字段名与枚举值）
   - 信封字段路径（working_memory.* / episodic_memory.* / planning_memory.* / foundation_memory.* /
     reference_pack.* / memory_policy / completion_signals / rule_violations / continuity_issues /
     foreshadow_due）
   - writer.md 里的 {{VOICE}} 占位符
   - architect-long.md 里 premise 的 14 个二级标题名（系统按标题名逐字解析）
4. assets/prompts/*.md 中看不到的「仿写画像」段由 assets/load.go 的 WithSimulationGuidance
   在加载时自动追加，不要把这段手工写进 md 文件。
5. 改 assets/prompts/writer.md 或 assets/voice.md 后，必须重新生成
   assets/testdata/writer-golden.md（生成方式：strings.Replace(writer.md 内容,
   "{{VOICE}}", strings.TrimSpace(voice.md 内容), 1)，可用临时 Go 程序 go run 完成），
   否则 assets/load_test.go 的字节一致性测试会红。
6. 每个任务完工门槛：go build ./... && go vet ./... && go test ./... 全部通过。
7. 一个任务一个 git commit，commit message 用中文简述改动意图；不碰与本任务无关的文件。
8. 新增 assets/references/ 顶层写作知识文件需要三处接线（internal/tools 的 References 结构加字段、
   assets/load.go 的 loadReferences 读取、internal/tools/novel_context*.go 的注入），
   放进目录不会自动加载；references/genres/<style>/ 下的题材文件和 styles/<style>.md 则只需放文件。

## 目录地图
- assets/prompts/    Worker 与 Arbiter 提示词（writer.md 含 {{VOICE}} 占位符）
- assets/voice.md    写作标准（文风层，三层覆盖：内置 < ~/.ainovel/style < 书目录/style）
- assets/styles/     题材风格指令，文件名即 config.style 取值（小写字母/数字/连字符）
- assets/references/ 写作知识材料；genres/<style>/ 为题材专属
- internal/rules/    机械规则（SystemDefaults 基线、checker、快照合并）
- internal/userrules/ 用户自然语言规则归一化（~/.ainovel/rules/*.md → meta/user_rules.json）
- internal/tools/    Worker 工具（novel_context / plan / draft / check / commit / save_review ...）
- internal/diag/     /diag 诊断规则（rules_flow / rules_quality / rules_planning / rules_context）
- internal/eval + evals/  离线 A/B 评测（用法见 docs/evaluation-system.md）