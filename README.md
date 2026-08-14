# ainovel-cli

全自动 AI 长篇小说创作引擎。从一句话需求到完整小说，全程无需人工干预。

核心设计：**事实层确定，语义层自主**。Engine 按事实路由驱动 Architect / Writer / Editor 三个自主创作代理，语义裁定按需唤醒 Arbiter，每个决策与每条事实落盘可审计。

<p align="center">
  <img src="scripts/sample.gif" alt="ainovel-cli demo" width="800">
  <img src="scripts/novel.png" alt="ainovel-cli bg" width="800">
</p>

## 核心特性

- **确定性引擎 + 多智能体协作** — Engine 按决策表调度 Worker，主循环零 LLM 开销，行为可穷举测试
- **语义裁定可审计** — 规划师选择、干预分诊、失败出路均由 Arbiter 单次调用完成，每次裁定落盘可回放
- **事实层连续性** — 数值事实（人数/金额/年龄/日期）、势力/地点档案、配角名册、伏笔台账全部结构化落盘，机械检测兜底
- **Step 级断点恢复** — 每个工具成功后写 checkpoint，崩溃后精确到 plan/draft/check/commit 步骤级恢复
- **卷弧双层滚动规划** — 初始只规划指南针 + 起步卷弧骨架 + 第 1 弧详细章节，后续按写作推进展开，远期规划不空洞
- **相关章节智能推荐** — 从伏笔、角色出场、状态变化、关系四维度推荐历史章节，支撑 500+ 章长篇连续性
- **自适应上下文策略** — 分层摘要 + 分级压缩管线 + 世界规则按章相关性召回，CJK Token 估算，绿/黄/红实时健康度提示
- **七维质量评审** — 设定一致性、角色行为、节奏、叙事连贯、伏笔、钩子、审美品质，每项引用原文举证
- **用户实时干预** — 写作中随时注入修改意见，系统自动裁定影响范围并执行
- **可选逐章验收** — 默认全自动；`/review on` 后每次 `/next` 只放行一个新章节
- **多 LLM 支持** — OpenRouter / Anthropic / Gemini / OpenAI / DeepSeek / 通义 / Ollama 等随意切换

## 架构

```
┌─────────────────────────────────────────────────┐
│              Host / Engine（确定性）              │
│  读 Store → Route → 直接运行 Worker → 循环        │
│  启动裁定 / 干预分诊 / 失败僵局 → 按需咨询 Arbiter  │
└────┬──────────┬──────────┬─────────────┬────────┘
     │          │          │             │
 ┌───▼────┐ ┌───▼───┐ ┌────▼────┐   ┌────▼────┐
 │Architect│ │Writer │ │ Editor  │   │ Arbiter │
 │(LLM循环)│ │(LLM循环)│ │(LLM循环)│   │(LLM函数)│
 └───┬────┘ └───┬───┘ └────┬────┘   └─────────┘
     └──────────┼──────────┘
                │ 工具调用（IO + checkpoint）
┌───────────────▼─────────────────────────────────┐
│                   Store                         │
│  Progress / Checkpoint / Outline / Drafts / ... │
└─────────────────────────────────────────────────┘
```

| 角色 | 职责 | 工具 |
|------|------|------|
| **Arbiter** | 语义裁定：启动选规划师、用户干预分诊、失败/僵局出路 | 无（单次 LLM 调用，结构化决策） |
| **Architect** | 生成前提、大纲、角色档案、世界规则；展开弧/卷 | `novel_context` `save_foundation` `promote_character` |
| **Writer** | 自主完成一章的构思、写作、自审和提交 | `novel_context` `read_chapter` `plan_chapter` `draft_chapter` `check_consistency` `report_consistency` `edit_chapter` `commit_chapter` |
| **Editor** | 阅读原文，从结构和审美两个层面审阅 | `novel_context` `read_chapter` `save_review` `save_arc_summary` `save_volume_summary` |

### 写作流程

```
用户需求 → Arbiter 选规划师 → Architect 规划骨架+首弧 → Writer 逐章写作 → Editor 弧级评审
              (裁定落盘)                                     ↑                   │
                                                            ├── 重写/打磨 ◄──────┘
                                                            │
                                                     Architect 展开下一弧/卷
                                                    （参考前文摘要+角色快照）
```

下一步派谁由 Engine 的 `flow.Route` 从 Store 事实推导（万级组合穷举测试钉死），不消耗任何 LLM 调用。

Writer 每章按固定顺序执行：

1. `novel_context` — 加载上下文（含章节契约、伏笔到期清单、数值事实基线）
2. `read_chapter` — 回读前文找回语气和节奏
3. `plan_chapter` — 构思本章目标、冲突、情绪弧线
4. `draft_chapter` — 写入整章正文
5. `check_consistency` — 对照状态数据检查一致性，`report_consistency` 落盘自审结论供 Editor 回溯
6. `commit_chapter` — 提交终稿；机械检查（数值事实、用户规则、11 类 lint）自动执行并落盘

## 快速开始

```bash
# 一键安装（macOS / Linux）
curl -fsSL https://raw.githubusercontent.com/voocel/ainovel-cli/main/scripts/install.sh | sh

# 安装指定版本
curl -fsSL https://raw.githubusercontent.com/voocel/ainovel-cli/main/scripts/install.sh | sh -s -- v1.2.3

# 或通过 Go 安装
go install github.com/voocel/ainovel-cli/cmd/ainovel-cli@latest

# 首次运行，自动进入引导流程
ainovel-cli

# 无界面运行（需已有配置）
ainovel-cli --headless --prompt "写一本悬疑小说"
```

> Windows 或手动安装：前往 [Releases](https://github.com/voocel/ainovel-cli/releases/latest) 下载对应平台包。

### CLI 子命令

```text
ainovel-cli version            # 查看版本（--version / -v 等价）
ainovel-cli update [版本]      # 自更新到最新版或指定版本（从 GitHub Releases 拉取）
ainovel-cli eval --cases ...   # 离线评测 harness（见下文「评测」）
```

### Docker

```bash
mkdir -p config workspace

# TUI
docker run --rm -it \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  ghcr.io/voocel/ainovel-cli:latest

# Headless
docker run --rm \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  ghcr.io/voocel/ainovel-cli:latest \
  --headless --prompt "写一本东方玄幻长篇，主角从边陲小城起步"
```

也支持 `docker compose run --rm ainovel`。

## 配置文件

配置文件决定调用哪个模型、走哪家 API、各创作角色用多大推理强度。首次运行 TUI 会自动引导生成；手动编辑可参考仓库根目录的 `config.example.jsonc`（支持 `//` 行注释）。

### 文件位置

| 路径 | 作用 |
|------|------|
| `~/.ainovel/config.json` | 全局配置，所有书共享 |
| `./.ainovel/config.json` | 本书/本项目级覆盖 |
| `--config path.json` | 命令行指定，最高优先级 |

`.ainovel/` 目录含 API Key，已默认加入 `.gitignore`，不要提交到仓库。

### 最小可用配置

```jsonc
{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": {
      "api_key": "sk-or-v1-xxx",
      "base_url": "https://openrouter.ai/api/v1"
    }
  }
}
```

### 核心字段说明

#### `provider`

**它是 `providers` 对象里的 key，不是协议名。**

```jsonc
{
  "provider": "openrouter",          // ← 指向 providers.openrouter
  "providers": {
    "openrouter": {                  // ← 这个 key 才是 provider 的值
      "api_key": "...",
      "base_url": "..."
    }
  }
}
```

如果你想切换到另一个账号/代理，必须保证 `providers` 里有同名条目：

```jsonc
{
  "provider": "anthropic",
  "providers": {
    "openrouter": { "api_key": "..." },
    "anthropic": { "api_key": "..." }    // 缺少这一项会报“未配置凭证”
  }
}
```

#### `providers.<name>`

每个 provider 条目描述一家账号或一个代理：

| 字段 | 说明 |
|------|------|
| `api_key` | 多数托管接口必填；Ollama / Bedrock / 显式 `type` 自定义代理可省略 |
| `base_url` | API 端点基地址，例如 `https://openrouter.ai/api/v1` |
| `type` | 协议类型，可选 `openai` / `anthropic` / `gemini`；已知 provider（按 key 名识别）可省略 |
| `models` | 该 provider 下可在 TUI `/model` 面板切换的模型列表 |
| `api` | 仅 `type: "openai"` 生效，选 `chat`（默认）或 `responses`；Codex 类代理通常用 `responses` |
| `extra_body` | 逐字并入请求体的额外参数（temperature 等采样参数），仅 OpenAI 兼容类 provider 生效 |
| `extra` | 传给底层 HTTP 客户端的选项，如 `user_agent`、`headers`、`anthropic_beta` |
| `stream_idle_timeout` | 流式空闲看门狗（如 `"15m"`，默认 `"5m"`）；本地慢推理建议放宽 |

#### `model` 与 `reasoning_effort`

- `model`：默认模型 ID，例如 `google/gemini-2.5-flash`、`claude-sonnet-4`、`gpt-5.4`。
- `reasoning_effort`：默认推理强度，可选 `off` / `low` / `medium` / `high` / `xhigh` / `max`；省略则沿用模型/provider 默认，模型不支持所配档位时自动降到最近可用档。

#### `style`

写作风格预设，对应 `assets/styles/<style>.md` 或自定义 `style/styles/<style>.md`，例如 `default` / `suspense` / `fantasy` / `romance`。

#### `roles`

为不同智能体单独指定模型和推理强度，并支持故障回退链。未配置的角色使用顶层 `provider` / `model` / `reasoning_effort`。

```jsonc
{
  "roles": {
    "architect": { "provider": "openrouter", "model": "google/gemini-2.5-pro" },
    "writer":    {
      "provider": "anthropic",
      "model": "claude-sonnet-4",
      "reasoning_effort": "high",
      "fallbacks": [{ "provider": "openrouter", "model": "google/gemini-3.0-pro" }]
    },
    "editor":    { "reasoning_effort": "medium" }
  }
}
```

- 可配置角色只有 `architect` / `writer` / `editor`。Arbiter 始终使用 default 模型。
- `roles.*.provider` 同样是「指针」，必须指向 `providers` 里存在的 key。
- `fallbacks`：主模型限流/超时/断流/网络错误时按序尝试的备用 provider/model。

#### `context_window` / `context_windows`

显式指定上下文压缩窗口。省略/0 = 按模型名自动解析（registry 命中用真实窗口，未命中兜底 200k）。只影响压缩阈值，不改变 API 实际请求长度。

```jsonc
{
  "context_window": 300000,
  "context_windows": { "my-256k-model": 256000 }
}
```

#### `budget`

单本书（output 目录）的成本预算，挂机保险丝：

```jsonc
{
  "budget": {
    "book_usd": 50,      // 预算上限（美元），> 0 才启用
    "warn_ratio": 0.8,   // 告警水位，默认 0.8
    "hard_stop": false   // true = 越线立即停；false = 等当前子代理任务结束后优雅停机
  }
}
```

停机后上调 `book_usd` 即可恢复续跑。自定义模型若注册表查不到价格且 provider 不自报 cost，预算形同虚设——此时可用 `pricing` 声明单价。

#### `pricing`

模型定价覆盖：官方直连 / 自定义模型不在 OpenRouter 注册表（或注册表价含抽成与直连价不一致）时，声明 $/1M token 单价，记账与预算优先使用：

```jsonc
{
  "pricing": {
    "my-company/novel-70b": { "input_per_1m": 2.5, "output_per_1m": 7.5 },
    "anthropic/claude-sonnet-4-5": { "equivalent": "claude-sonnet-4-5" }
  }
}
```

键为 `"模型名"` 或 `"provider/model"`（键不同时后者优先）；单价全 0 时可只写 `equivalent` 声明「按注册表某等价模型计费」。

#### `notify`

无人值守告警，缺省启用（Windows 通知 / macOS osascript / Linux notify-send）。可配置自定义命令（手机推送走这里，字段经 `NOTIFY_TITLE` / `NOTIFY_BODY` / `NOTIFY_KIND` / `NOTIFY_LEVEL` 环境变量传入）：

```jsonc
{
  "notify": {
    "command": "curl -s --data-urlencode \"title=$NOTIFY_TITLE\" --data-urlencode \"body=$NOTIFY_BODY\" https://api.day.app/<key>",
    "events": ["run_end", "budget", "deadlock", "worker_failure", "failover", "missing_pricing", "dropped_events"]
  }
}
```

事件类型：`run_end` / `budget` / `advance_gate` / `stop_guard` / `plan_start` / `deadlock` / `worker_failure` / `failover`（provider 故障切换）/ `missing_pricing`（记账无法解析价格的模型）/ `dropped_events`（UI 事件在高压下丢弃）；缺省全部开启。

### 配置合并规则

三层配置按顺序加载，后者覆盖前者：

1. `~/.ainovel/config.json`
2. `./.ainovel/config.json`
3. `--config path.json`

合并细节：

- **标量字段**（`provider`、`model`、`style`、`reasoning_effort` 等）：后者完全覆盖前者
- **`providers` 和 `roles`**：按 key 合并，同名 key 内部再按字段覆盖
- **`budget` / `notify`**：整块覆盖
- **未填写字段**：继承上层配置
- **不支持用空字符串清空上层值**：要清空请直接编辑更高优先级的配置文件

### 常见错误

| 报错 | 原因 | 解决 |
|------|------|------|
| 未配置凭证 | `provider` 指向的 key 在 `providers` 中不存在，或缺少 `api_key` / `base_url` | 检查 `provider` 值与 `providers` 的 key 是否一致 |
| 模型找不到 | `model` 不在当前 provider 可用列表，或 `base_url` 写错 | 确认 `base_url` 和模型 ID |
| 配置不生效 | 项目级配置覆盖了全局，但只写了部分字段 | 理解按 key 合并规则，必要时用 `--config` 显式指定 |

### 支持的 Provider

`openrouter` / `anthropic` / `gemini` / `openai` / `deepseek` / `qwen` / `glm` / `grok` / `mimo` / `ollama` / `bedrock`，以及任意 OpenAI/Anthropic 协议自定义代理。

### 自定义代理示例

```jsonc
{
  "provider": "my-proxy",
  "model": "gpt-4o",
  "providers": {
    "my-proxy": {
      "type": "openai",
      "base_url": "https://proxy.example.com/v1",
      "extra": {
        "user_agent": "my-client/1.0",
        "headers": { "X-Custom-Client": "my-client" }
      }
    }
  }
}
```

如果代理是 Anthropic 协议，设置 `type: "anthropic"`；如果上游限制只能由 Claude Code / Codex 客户端访问，用 `extra` 透传 `user_agent` / `anthropic_beta` / headers（完整示例见 `config.example.jsonc`）。

### 本地 Ollama 示例

```jsonc
{
  "provider": "ollama",
  "model": "qwen3:latest",
  "providers": {
    "ollama": {
      "base_url": "http://localhost:11434/v1",
      "stream_idle_timeout": "15m"
    }
  }
}
```

## 长篇滚动规划

传统方案一次规划所有章节，300+ 章时大纲空洞。本系统采用**指南针 + 视野滚动规划**：

```
初始规划                     弧结束时                      卷结束时
┌────────────────────┐    ┌─────────────────────┐    ┌─────────────────────┐
│ 终局方向（指南针）    │    │ Editor 弧级评审      │    │ Editor 卷级评审       │
│ 起步 2 卷，后续按需   │    │ 弧摘要 + 角色快照     │    │ 卷摘要               │
│ 第1弧详细章节        │ →  │ Architect 展开下一弧  │ →  │ Architect 自主创建   │
│ 角色 + 世界观        │    │ Writer 继续写作      │    │ 下一卷 + 更新指南针    │
└────────────────────┘    └─────────────────────┘    └─────────────────────┘
```

- **指南针（Compass）** — 终局方向 + 活跃长线 + 规模估计，每次卷边界更新
- **按需生成** — 当前卷写完后自主创建下一卷；初始 2 卷起步
- **骨架弧** — 只有 goal + 预估章数，到达时再展开详细章节
- **渐进细化** — 每次展开参考前文摘要、角色快照、风格规则

## 长篇上下文管理

```
卷（Volume）→ 卷摘要
└── 弧（Arc）→ 弧摘要 + 角色快照 + 风格规则
    └── 章（Chapter）→ 章摘要（滑窗最近3章）
```

- **分层摘要** — 近处用章摘要，中距离用弧摘要，远处用卷摘要
- **相关章节推荐** — 从伏笔、角色出场、状态变化、关系四维度反查历史章节
- **世界规则分级召回** — 世界规则随篇幅膨胀时，按「当前章相关性」确定性筛选注入视图：硬约束恒保留，其余按大纲关键词排序取前 N；check_consistency 与 Architect 路径始终全量，不受影响
- **下一章预告** — 加载下一章大纲，辅助设计章末钩子和伏笔衔接

### 上下文压缩管线

当对话超出窗口时，按代价从低到高逐级压缩：

```
ToolResultMicrocompact → LightTrim → StoreSummaryCompact → FullSummary
     清理旧工具结果        截断长文本      store 零 LLM 压缩      LLM 摘要兜底
```

- **StoreSummaryCompact** — Writer 专用，用已有摘要、快照、伏笔台账直接替换旧消息，零 LLM 开销
- **FullSummary 小说定制** — 面向叙事连续性，保留角色状态、伏笔线索、审稿待修项、风格锚点
- **压缩后恢复包** — 自动注入当前章节计划、大纲和角色快照，防止压缩后失忆
- **熔断器** — 压缩连续失败时跳过并告警，半开模式下轮自动重试
- **CJK Token 估算** — 中文 `runes × 1.5`，避免压缩触发滞后

## 事实层：确定性连续性与机械检查

写作的连贯性不靠模型自觉，靠落盘事实和机械校验：

- **数值事实连续性** — 组织规模、金额、年龄、日期等数值事实在 `commit_chapter` 登记（entity + field + new_value）；与历史值冲突（如公司人数 41→300 且未交代）会被机械检测为 `fact_conflicts`
- **势力/地点档案** — 宗门/家族/国家的兴衰走 `faction_updates` 申报，自动合并档案并留历史轨迹；「已灭势力复出」式矛盾被机械拦下。地点同理（`location_updates`）
- **配角名册** — 有名字的次要角色自动追踪（出场章/角色简介/频次），Writer 可回读上次口吻避免「老周变陌生人」，重要配角可升格为核心档案
- **伏笔台账** — plant / advance / resolve 全程追踪，超过 5 章未推进的伏笔自动浮出 `foreshadow_due` 清单
- **用户规则机械检查** — 禁用字、禁用短语、疲劳词、叙述人称，commit 时强制检查并落盘 `rule_violations`
- **11 类 lint 机械检查** — markdown 残留、中英混杂、直引号、未闭合引号、段中换行、半角标点、破折号滥用、软填充词、AI 标记词、俗套微表情、叙事者闯入——全部 warning 级事实交 Editor 裁定
- **Writer 自审落盘** — `report_consistency` 把「本章哪些地方可疑」落盘，压缩/换窗口后 Editor 仍能回溯 Writer 当时的自审结论
- **全书风格统计** — 代码对全部已写章节做确定性统计（句式模式、跨章重复句、章末形态、标题格式），Editor 据此抓「每章都正常、全书是病」的问题

## 输出结构

每本小说绑定到启动目录，产物默认落在 `{cwd}/output/novel/`。换目录启动 = 换一本书，`cd` 回去启动 = 自动从最近 checkpoint 恢复。

```
output/novel/
├── chapters/              # 终稿（Markdown）
├── drafts/                # 章节草稿
├── reviews/               # 评审报告（含全局评审）
├── summaries/             # 章/弧/卷摘要（JSON）
├── premise.md             # 故事前提
├── characters.json        # 角色档案
├── outline.json           # 扁平章节大纲（短篇）
├── layered_outline.json   # 卷弧分层大纲（长篇）
├── world_rules.json       # 世界规则
├── factions.json          # 势力档案
├── locations.json         # 地点档案
├── timeline.json          # 时间线
├── foreshadow_ledger.json # 伏笔台账
├── relationship_state.json# 关系状态
└── meta/
    ├── compass.json       # 终局方向指南针
    ├── progress.json      # 进度状态
    ├── state_changes.json # 状态变化台账
    ├── style_rules.json   # 写作风格规则
    ├── cast_ledger.json   # 配角名册
    ├── user_rules.json    # 归一化用户规则
    ├── usage.json         # token / 成本累计
    ├── run.json           # 运行元数据（干预历史等）
    ├── checkpoints.jsonl  # Step 级 checkpoint
    ├── decisions.jsonl    # Arbiter 裁定记录
    ├── rule_violations.jsonl      # 用户规则机械检查结果
    ├── continuity_issues.jsonl    # 状态/关系/出场连续性检测结果
    ├── consistency_checks.jsonl   # Writer 自审结论
    ├── outline_feedback.jsonl     # 大纲反馈闭环
    ├── simulation_profile.json    # 仿写画像
    ├── diag-export.md     # /diag 脱敏诊断导出
    ├── snapshots/         # 角色状态快照（按卷弧）
    ├── sessions/          # 子代理会话日志
    ├── runtime/           # 运行时事件队列
    └── archive/           # 台账弧末快照归档
```

## 断点恢复

同一目录再次运行时自动恢复，无需手动操作：

| 中断时机 | 恢复行为 |
|---|---|
| 规划阶段 | 检查已保存设定，自动补全缺失项 |
| 某章正在写作 | 从该章续写，读取已有草稿继续 |
| 审阅进行中 | 重新触发 Editor 评审 |
| 重写/打磨队列未清空 | 继续处理待重写的章节 |
| 弧/卷展开中断 | 检测骨架弧/卷，触发 Architect 展开 |
| 用户干预未完成 | 重新注入上次干预指令 |
| 正常写作中断 | 从下一章继续 |

所有写入使用 temp + fsync + rename 原子操作，断电不会损坏已有数据。

## TUI 交互

启动后进入 TUI 实时观察进度。启动阶段可选：

- **快速开始** — 一句话直接进入创作
- **共创规划** — 与 AI 多轮澄清需求，右侧实时同步创作指令草稿；每轮提供 1-3 条引导建议，按数字键一键填入，`Ctrl+S` 进入正式创作

常用命令：

```text
/help                # 查看命令列表
/model [role]        # 切换默认或角色模型
/setup               # 管理 Provider API 和角色分配（别名 /config /settings）
/key <provider>      # 快捷修改 Provider API Key
/diag                # 诊断当前小说，输出可执行建议与脱敏导出
/review on|off       # 切换逐章验收模式
/next                # 验收后放行下一章
/import <路径>       # 导入已有小说并接力续写
/cocreate            # 暂停创作，共创规划后续阶段走向（别名 /plan）
/simulate            # 读取 ./simulate 生成或增量更新仿写画像
/importsim <文件>    # 导入已有仿写画像
/export              # 导出 TXT/EPUB
```

## 实时干预（Steer）

创作过程中随时在底部输入框注入修改意见，无需暂停：

```text
❯ 把感情线提前到第4章，增加男女主的对手戏
```

系统会：

1. 记录干预指令到 `run.json`
2. Arbiter 立即裁定（秒级回显）
3. 按裁定执行：修改设定走 Architect、重写已有章节走 Editor 入队、写作规则即时落盘为用户规则

## 诊断报告

`/diag` 对当前小说产物进行四维诊断：

- **流程** — 改写循环卡顿、未消费转向指令、状态异常、章节跳号
- **质量** — 评审低分、合同履约率、改写率、字数异常、跨章疲劳词覆盖
- **规划** — 伏笔停滞、指南针过时、大纲耗尽、摘要缺失、势力档案与历史轨迹漂移
- **上下文** — 角色消失、时间线缺口、关系数据停滞、连续性告警、台账体积（归档压缩建议）

每条发现包含问题描述、数据证据、改进建议（指向具体 prompt/flow/config）。事实层损坏（状态机不一致、返工队列、孤立转向指令）可在报告中按 `f` 自动修复。同时输出已脱敏的 `meta/diag-export.md`，方便贴到 GitHub issue 定位问题。

## 仿写画像

把参考文章放到启动目录的 `simulate/` 文件夹，然后输入 `/simulate`。系统递归读取 `.txt` / `.md` / `.markdown`，分析语料后写入：

```text
output/novel/meta/simulation_profile.json
```

再次运行 `/simulate` 会按 `relative_path + sha256` 跳过未变化文件。也可用 `/importsim ./profile.json` 导入已有画像。画像只借鉴结构、节奏、钩子和吸引读者手法，不复制原文表达或专有设定。

## 导入与导出

### 导入

```text
/import ~/我的小说.txt              # 从头导入并反推 foundation
/import ~/我的小说.txt from=50      # 从第 50 章接着导入（跳过反推）
```

自动识别章节标题格式：`第一章` / `第3回` / `Chapter 1` / `Prologue` / `序章` / `楔子` 等。导入完成后自动接力续写。

### 导出

```text
/export                            # 默认 TXT
/export ~/光斑.txt                  # TXT
/export ~/光斑.epub                 # EPUB
/export from=10 to=30 --overwrite  # 章节区间 + 覆盖
```

- **TXT** — 含书名、卷分隔、章节正文； premise 和弧分隔不进入导出
- **EPUB** — EPUB 3 标准容器，按章拆分 XHTML，标识符基于内容稳定派生

## 评测（eval）

`ainovel-cli eval` 是离线评测 harness，用于提示词 A/B 对比或冒烟验证：

```bash
ainovel-cli eval --cases evals/cases/smoke --config ~/.ainovel/config.json --max-chapters 1 --timeout 10m
```

| 参数 | 说明 |
|------|------|
| `--cases` | case 目录或单个 .json 文件（必填） |
| `--variant` | variant 提示词覆盖目录（含 writer.md 等），存在时走 A/B 模式 |
| `--max-chapters` | 覆盖所有 case 的章数上限 |
| `--timeout` | 单 case 墙钟上限 |
| `--repeat` | 每个 case 重复次数（降低模型随机性影响） |

评测会实际调用模型并产生费用，CI 不自动运行。详情见 `docs/evaluation-system.md`。

## 自定义规则与文风

### 去 AI 味与自定义规则

内置去 AI 味基线：机械黑名单 + 语义判据。叠加个人偏好无需改源码，在 `~/.ainovel/rules/*.md`（全局）或 `./.ainovel/rules/*.md`（本书）用大白话写即可，例如：

```text
主角别写成圣母
多用身体感知
每章 3000 字左右
不要出现「某种程度上」
```

系统会把自然语言归一化为结构化约束（`meta/user_rules.json`），提交时自动机械自检（禁用字/短语/疲劳词/叙述人称），自然语言偏好注入上下文供三个 Worker 遵守。运行时不直接读 rules 文件，只认归一化产物。

### 自定义文风（Voice Layer）

写作标准与去 AI 味判据可直接覆盖，目录优先级：`<输出目录>/style/` > `~/.ainovel/style/`。

```
style/
├── voice.md                    # 写作标准追加段
├── anti-ai-tone.md             # 去 AI 味判据追加段
├── styles/
│   └── xianxia.md              # 自定义风格预设（文件名即风格名）
└── genres/
    └── xianxia/
        └── style-references.md # 该风格题材参考
```

语义速记：**指导性文本追加，风格预设整文件替换**。机械强制约束（禁用词、字数）请放在 `rules/` 目录。详情见 `docs/voice-layer.md`。

## 设计理念

> **事实层确定，语义层自主。** 模型自由在验证不可能的地方，被约束在验证可能的地方。

- **可枚举的迁移归代码** — `flow.Route` 纯函数，万级组合穷举测试
- **边界清晰的判断归 Arbiter** — 事实进、结构化决策出、机械校验兜底
- **开放式创作归 Worker** — 一章之内 Writer 完全自主
- **工具只返事实** — 不夹带任何指令字符串
- **事实护栏，不是行为护栏** — 只认落盘产物，行为正确时零成本
- **拒绝复杂编排** — 一个串行循环 + 一张决策表 + 几个裁定函数
- **模型越强收益越大** — 创作与裁定质量随模型升级线性受益

## 技术栈

- **Go 1.25** — 主语言
- **[agentcore](https://github.com/voocel/agentcore)** — Agent 内核（tool-calling + streaming）
- **[litellm](https://github.com/voocel/litellm)** — 统一 LLM 接口适配
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** — 终端 TUI 框架

## License

MIT

本项目积极参与并认可 [linux.do 社区](https://linux.do/)。
