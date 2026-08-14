package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/store"
)

// 冷启动共创：从零澄清需求，产出整本书的创作指令。
const coCreateSystemPrompt = `你是一个小说共创助手。你的任务不是直接开始写小说，而是通过多轮简短对话帮助用户澄清创作需求，并持续整理出一段可直接交给创作引擎的中文创作指令。

每一轮回复严格输出**一个 JSON 对象**（不要 Markdown 代码围栏、不要对象之外的任何文字），字段如下：

{
  "reply": "给用户看的中文自然回复：先回应用户的输入，再最多提出 1 到 2 个当前最关键的问题。如果信息已足够开始创作，告诉用户可以按 Ctrl+S 开始。",
  "draft": "当前完整的创作指令草稿，使用 Markdown：直接从二级标题开始，例如 \"## 主题\"、\"## 关键要素\"、\"## 待澄清信息\"；用项目符号列出要点。每一轮都要在已有结论上**累积更新**，吸收用户最新意图；即使本轮没有新增也要把完整草稿原样再写一次——不要省略、不要写\"（保持上一轮）\"之类的占位。",
  "ready": false,
  "suggestions": ["用户接下来可能想说的话 1", "用户接下来可能想说的话 2", "用户接下来可能想说的话 3"]
}
` + coCreateProtocolTail

// 阶段共创：小说已写了一部分，规划"后续阶段"的走向。调用方需把当前故事状态摘要
// 追加到本 prompt 之后（"## 当前故事状态" 段），让模型在已写内容的基础上规划。
const stageCoCreateSystemPrompt = `你是一个小说"阶段共创"助手。这本小说已经写了一部分（进度见下方"当前故事状态"）。用户暂停下来，想和你一起规划"后续阶段"的走向，再继续创作。

你的任务不是续写正文，而是通过多轮简短对话帮用户想清楚后面这一段（接下来若干章 / 下一弧 / 下一卷）要往哪走，并持续整理出一段"后续方向 brief"，供创作引擎据此推进。

铁律：所有建议必须与"当前故事状态"里已发生的剧情、人物、伏笔一致，绝不推翻或忽略已写内容；只规划"后续怎么走"，不重新设计整本书。

每一轮回复严格输出**一个 JSON 对象**（不要 Markdown 代码围栏、不要对象之外的任何文字），字段如下：

{
  "reply": "给用户看的中文自然回复：先回应用户的输入，再最多提出 1 到 2 个当前最关键的问题。如果后续方向已足够清晰，告诉用户可以按 Ctrl+S 把方向交给创作引擎、继续创作。",
  "draft": "当前完整的\"后续方向 brief\"，使用 Markdown：直接从二级标题开始，例如 \"## 后续走向\"、\"## 关键转折\"、\"## 要收的伏笔\"、\"## 节奏与篇幅\"；用项目符号列出要点。每一轮都要在已有结论上**累积更新**，吸收用户最新意图；即使本轮没有新增也要把完整 brief 原样再写一次——不要省略、不要写\"（保持上一轮）\"之类的占位。",
  "ready": false,
  "suggestions": ["用户接下来可能想说的话 1", "用户接下来可能想说的话 2", "用户接下来可能想说的话 3"]
}
` + coCreateProtocolTail

// coCreateProtocolTail 是两种共创模式共用的输出协议尾部（suggestions 内容 + 输出规范）。
// 两模式只在开场语境与 draft 语义上不同，协议完全一致。
const coCreateProtocolTail = `

字段要求：
- reply / draft 是字符串，内部的换行写成 \n、双引号转义为 \"，禁止字面换行或未转义引号。
- ready 是布尔值：信息已足够时填 true。
- suggestions 是 1-3 条"用户接下来可能想说的话"，按数字键填入输入框，用户可再编辑后发送。
  要求：站在用户口吻，像用户对你说的话，不要写成助手反问；每条不超过 25 字，多样化句式，
  避免千篇一律；给倾向 / 选择 / 补充意图，不要一句话替用户写完整设定。ready 为 true 时可为空数组。

输出规范：
- 必须只输出上述一个 JSON 对象，字段名（reply / draft / ready / suggestions）一字不差，不要改写。
- 不要 Markdown 代码围栏、不要对象外任何说明、思考或标签。`

// CoCreateProgressKind 标识流式回调的内容类型。
const (
	CoCreateProgressThinking = "thinking"
	CoCreateProgressReply    = "reply"
)

// 四段式 XML 标签输出（JSON 协议的降级兼容路径）。保留原因是历史会话与
// 部分模型仍会按旧协议输出；解析层先试 JSON、再回落 XML。
const (
	tagReply       = "reply"
	tagDraft       = "draft"
	tagReady       = "ready"
	tagSuggestions = "suggestions"
)

// coCreateStream 跑一轮共创对话。record 非 nil 时把本次 LLM 消耗记入 UsageTracker
// （身份 "cocreate"）——共创不进 Engine/session 日志，不在这里记账预算就对它失明。
func coCreateStream(ctx context.Context, models *bootstrap.ModelSet, sessions *store.SessionStore, record func(string, string, agentcore.AgentMessage), sysPrompt string, history []CoCreateMessage, onProgress func(kind, text string)) (reply CoCreateReply, err error) {
	if len(history) == 0 {
		return CoCreateReply{}, fmt.Errorf("cocreate history is empty")
	}

	model := models.ForRole("thinking")
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	msgs := []agentcore.Message{agentcore.SystemMsg(sysPrompt)}
	for _, item := range history {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "assistant":
			msgs = append(msgs, assistantMsg(content))
		default:
			msgs = append(msgs, agentcore.UserMsg(content))
		}
	}

	var raw, thinking strings.Builder

	// 排查 "cocreate empty response" 等偶发问题需要看到模型实际返回什么。
	// 每轮全程落盘到 <output>/meta/sessions/cocreate.jsonl，与正式创作的 session 日志同位。
	start := time.Now()
	defer func() {
		if sessions == nil {
			return
		}
		_ = sessions.LogCoCreate(coCreateLogEntry{
			Time:         time.Now(),
			DurationMS:   time.Since(start).Milliseconds(),
			InputHistory: history,
			RawResponse:  raw.String(),
			RawLen:       len([]rune(raw.String())),
			Thinking:     thinking.String(),
			ParsedReply:  reply.Message,
			ParsedDraft:  reply.Prompt,
			ParsedReady:  reply.Ready,
			ParsedSugs:   reply.Suggestions,
			Error:        errString(err),
		})
	}()

	streamCh, err := model.GenerateStream(ctx, msgs, nil, agentcore.WithMaxTokens(2048))
	if err != nil {
		return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", err)
	}

	var streamed bool
	for ev := range streamCh {
		switch ev.Type {
		case agentcore.StreamEventThinkingDelta:
			thinking.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressThinking, thinking.String())
			}
		case agentcore.StreamEventTextDelta:
			streamed = true
			raw.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressReply, extractReplyPreview(raw.String()))
			}
		case agentcore.StreamEventDone:
			// 流结束时的最终消息携带 Usage，据它记账；无 Usage 则无量可计
			// （与 usageTrackedModel 同一口径，不触发 missingAssistantUsage 诊断）。
			if record != nil && ev.Message.Usage != nil {
				record("cocreate", "", ev.Message)
			}
			if !streamed {
				raw.WriteString(ev.Message.TextContent())
			}
		case agentcore.StreamEventError:
			if ev.Err != nil {
				return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", ev.Err)
			}
			return CoCreateReply{}, fmt.Errorf("cocreate generate failed")
		}
	}

	// Channel fallback：思考型模型（R1/GLM-Z1/QwQ 等）偶发把完整答案写进
	// reasoning_content 后没切回 final answer 通道，导致 raw 为空但 thinking 含
	// 完整四段。实测见 meta/sessions/cocreate.jsonl —— 直接拿 thinking 当 raw 解析，
	// 协议层已有降级处理（无 [REPLY] 标记时整段当 reply），救场后 UI 体验无差别。
	rawText := raw.String()
	if strings.TrimSpace(rawText) == "" {
		if t := strings.TrimSpace(thinking.String()); t != "" {
			rawText = t
		}
	}
	reply, err = parseCoCreateResponse(rawText)
	return reply, err
}

// coCreateLogEntry 是写入 meta/sessions/cocreate.jsonl 的一行结构。
// 字段命名贴近 jsonl 直查习惯（snake_case），方便 jq 过滤。
type coCreateLogEntry struct {
	Time         time.Time         `json:"time"`
	DurationMS   int64             `json:"duration_ms"`
	InputHistory []CoCreateMessage `json:"input_history"`
	RawResponse  string            `json:"raw_response"`
	RawLen       int               `json:"raw_len"`
	Thinking     string            `json:"thinking,omitempty"`
	ParsedReply  string            `json:"parsed_reply"`
	ParsedDraft  string            `json:"parsed_draft"`
	ParsedReady  bool              `json:"parsed_ready"`
	ParsedSugs   []string          `json:"parsed_sugs,omitempty"`
	Error        string            `json:"error,omitempty"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func assistantMsg(text string) agentcore.Message {
	return agentcore.Message{
		Role:      agentcore.RoleAssistant,
		Content:   []agentcore.ContentBlock{agentcore.TextBlock(text)},
		Timestamp: time.Now(),
	}
}

// parseCoCreateResponse 解析 LLM 输出。首选结构化 JSON（与 Arbiter 的
// extractJSON+Validate 同款路径）；XML 标签仅作降级兼容；两者皆不遵守时
// （直接说自然语言）整段作为 reply 显示，draft 留空让 session 保留上一轮。
func parseCoCreateResponse(raw string) (CoCreateReply, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CoCreateReply{}, fmt.Errorf("cocreate empty response")
	}

	if reply, ok := parseCoCreateJSON(raw); ok {
		return reply, nil
	}

	reply, draft, ready, suggestions := splitCoCreateMarkers(raw)
	if reply == "" {
		// 模型没遵守 JSON/XML 协议：整段作为 reply。
		return CoCreateReply{Message: raw, Prompt: "", Ready: false, Raw: raw}, nil
	}
	return CoCreateReply{
		Message:     reply,
		Prompt:      draft,
		Ready:       ready,
		Suggestions: suggestions,
		Raw:         raw,
	}, nil
}

// coCreateJSONPayload 是共创 JSON 协议的目标形状。
type coCreateJSONPayload struct {
	Reply       string   `json:"reply"`
	Draft       string   `json:"draft"`
	Ready       bool     `json:"ready"`
	Suggestions []string `json:"suggestions"`
}

// parseCoCreateJSON 尝试按结构化 JSON 解析；成功返回 true。
func parseCoCreateJSON(raw string) (CoCreateReply, bool) {
	obj := extractJSONObject(raw)
	if obj == "" {
		return CoCreateReply{}, false
	}
	var p coCreateJSONPayload
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return CoCreateReply{}, false
	}
	if strings.TrimSpace(p.Reply) == "" && strings.TrimSpace(p.Draft) == "" {
		return CoCreateReply{}, false
	}
	if strings.TrimSpace(p.Reply) == "" {
		return CoCreateReply{}, false // reply 是显示主体,缺失不算合法 JSON 协议
	}
	suggestions := make([]string, 0, len(p.Suggestions))
	for _, s := range p.Suggestions {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		suggestions = append(suggestions, s)
		if len(suggestions) >= 3 {
			break
		}
	}
	return CoCreateReply{
		Message:     strings.TrimSpace(p.Reply),
		Prompt:      strings.TrimSpace(p.Draft),
		Ready:       p.Ready,
		Suggestions: suggestions,
		Raw:         raw,
	}, true
}

// extractJSONObject 从模型输出中截取首个平衡的 JSON 对象（容忍围栏/前后缀文本）。
func extractJSONObject(raw string) string {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			switch {
			case escape:
				escape = false
			case c == '\\':
				escape = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

// splitCoCreateMarkers 按四个 XML 标签切分文本。
// 标签可能缺失（流式中段或模型遗漏），缺失部分对应字段为空 / false / nil。
// 缺失闭标签时，extractTagContent 会取到字符串末尾，仍尽力解析。
func splitCoCreateMarkers(s string) (reply, draft string, ready bool, suggestions []string) {
	reply = extractTagContent(s, tagReply)
	draft = extractTagContent(s, tagDraft)
	readyStr := strings.ToLower(extractTagContent(s, tagReady))
	ready = readyStr == "true" || readyStr == "yes"
	suggestions = parseSuggestions(extractTagContent(s, tagSuggestions))
	return
}

// extractTagContent 从 s 中抠出 <tag>...</tag> 之间的文本。
// 三种偶发故障场景兜底，避免直接走降级丢字段：
//  1. 有开无闭（流式中段）→ 切到下一个已知开标签前
//  2. 无开有闭（模型 typo，如 <suggestions> 写成 <uggestions>）→ 从最近一个已知
//     完整闭合标签的结束位置开始，到 </tag> 之前
//  3. reply 完全无开标签（模型直接以自然语言开篇，末尾贴 </reply>）→ 从开头到 </reply>
func extractTagContent(s, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	oIdx := strings.Index(s, open)
	if oIdx >= 0 {
		rest := s[oIdx+len(open):]
		if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
			return strings.TrimSpace(rest[:cIdx])
		}
		// 有开无闭 → 切到下一个已知开标签前
		for _, other := range []string{"<reply>", "<draft>", "<ready>", "<suggestions>"} {
			if other == open {
				continue
			}
			if idx := strings.Index(rest, other); idx >= 0 {
				rest = rest[:idx]
			}
		}
		return strings.TrimSpace(rest)
	}

	// 无开有闭 → 从最近一个已知完整闭合标签的结束位置开始，到 </tag>。
	if cIdx := strings.Index(s, closeTag); cIdx >= 0 {
		prefix := s[:cIdx]
		start := 0
		for _, t := range []string{"</reply>", "</draft>", "</ready>", "</suggestions>"} {
			if t == closeTag {
				continue
			}
			if i := strings.LastIndex(prefix, t); i >= 0 {
				if end := i + len(t); end > start {
					start = end
				}
			}
		}
		return strings.TrimSpace(prefix[start:])
	}
	return ""
}

// parseSuggestions 把 <suggestions> 段每行抠出来，去掉 "- " / "* " / "1. " 等列表前缀。
// 最多保留 3 条；空行、过短（<2 字）、整行像 XML 标签的（typo 开标签兜底残留，
// 例如 <uggestions>）忽略。
func parseSuggestions(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 整行像 XML 标签 → 跳过（防 typo 开标签污染）
		if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") {
			continue
		}
		// 剥列表前缀
		switch {
		case strings.HasPrefix(line, "- "):
			line = strings.TrimSpace(line[2:])
		case strings.HasPrefix(line, "* "):
			line = strings.TrimSpace(line[2:])
		case isOrderedSuggestion(line):
			line = stripOrderedPrefix(line)
		}
		if len([]rune(line)) < 2 {
			continue
		}
		out = append(out, line)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

// isOrderedSuggestion 判断行首是否形如 "1. " / "12. "（数字+点+空格）。
func isOrderedSuggestion(line string) bool {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(line) && line[i] == '.' && line[i+1] == ' '
}

func stripOrderedPrefix(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return line
	}
	return strings.TrimSpace(line[i+2:])
}

// extractReplyPreview 流式预览：raw 还在生长时给 UI 一段可显示的文本。
// JSON 协议优先：找到 "reply" 字段的字符串值（支持未闭合的流式中段，截到下一个
// 字段或引号前）；模型半遵守（漏 "reply" 键）或走 XML 降级时,回落到标签解析:
// 找到 <reply> 之后的内容，切到 </reply> 或下一个开标签 <draft> 之前。
func extractReplyPreview(raw string) string {
	trimmed := strings.TrimSpace(raw)

	if obj := extractJSONObject(trimmed); obj != "" {
		if v := extractJSONStringField(obj, "reply"); v != "" {
			return unescapeJSONBasic(v)
		}
	}

	// JSON 流式中段（对象未闭合）:从 "reply" 键后取字符串值开头,直到下一个字段或结尾。
	if i := strings.Index(trimmed, `"reply"`); i >= 0 {
		rest := trimmed[i+len(`"reply"`):]
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, ":") {
			rest = strings.TrimSpace(rest[1:])
			if strings.HasPrefix(rest, `"`) {
				body := rest[1:]
				// 截到未转义的双引号（字符串结束）或下一个已知字段键之前。
				if v := cutJSONStringPreview(body); v != "" {
					return unescapeJSONBasic(v)
				}
			}
		}
	}

	open := "<" + tagReply + ">"
	closeTag := "</" + tagReply + ">"
	draftOpen := "<" + tagDraft + ">"

	rest := trimmed
	if rIdx := strings.Index(trimmed, open); rIdx >= 0 {
		rest = trimmed[rIdx+len(open):]
	}
	if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
		return strings.TrimSpace(rest[:cIdx])
	}
	if dIdx := strings.Index(rest, draftOpen); dIdx >= 0 {
		rest = rest[:dIdx]
	}
	return strings.TrimSpace(rest)
}

// extractJSONStringField 从已闭合 JSON 对象中取字符串字段值（如 "reply"）。
func extractJSONStringField(obj, field string) string {
	key := `"` + field + `"`
	i := strings.Index(obj, key)
	if i < 0 {
		return ""
	}
	rest := obj[i+len(key):]
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, ":") {
		return ""
	}
	rest = strings.TrimSpace(rest[1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	return readJSONString(rest)
}

// readJSONString 从以引号开头的片段读完整 JSON 字符串值并返回其内容（已去转义）。
func readJSONString(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) && s[i] != '"' {
		i++
	}
	if i >= len(s) {
		return ""
	}
	i++ // 跳过开引号
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			switch next := s[i+1]; next {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"', '\\', '/':
				b.WriteByte(next)
			default:
				b.WriteByte(next)
			}
			i += 2
		case c == '"':
			return b.String()
		default:
			b.WriteByte(c)
			i++
		}
	}
	return ""
}

// cutJSONStringPreview 取流式中段字符串值的可见部分：截到未转义双引号或结尾。
func cutJSONStringPreview(body string) string {
	var b strings.Builder
	escape := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		if escape {
			escape = false
			b.WriteByte(c)
			continue
		}
		if c == '\\' {
			escape = true
			b.WriteByte(c)
			continue
		}
		if c == '"' {
			break
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
}

// unescapeJSONBasic 把流式预览里的 \n \" \\ 还原为可见文本。
func unescapeJSONBasic(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`)
	return r.Replace(s)
}
