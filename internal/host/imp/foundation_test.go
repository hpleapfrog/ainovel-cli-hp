package imp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

type mockLLM struct {
	out string
	err error
	got []agentcore.Message
}

func (m *mockLLM) Generate(_ context.Context, msgs []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	m.got = msgs
	if m.err != nil {
		return nil, m.err
	}
	return &agentcore.LLMResponse{
		Message: agentcore.Message{
			Role:      agentcore.RoleAssistant,
			Content:   []agentcore.ContentBlock{agentcore.TextBlock(m.out)},
			Timestamp: time.Now(),
		},
	}, nil
}

const validEnvelope = `=== PREMISE ===
# 测试书名

## 题材和基调
现代都市悬疑

## 核心冲突
新闻记者追查连环失踪案

## 主角目标
找出真凶并自证清白

## 结局方向
真相大白，主角抉择

## 写作禁区
血腥猎奇，跳脱现实

## 差异化卖点
- 双线叙事
- 女性视角

## 差异化钩子
失踪者全部姓"陈"

## 核心兑现承诺
追完能体验完整悬疑解谜

=== CHARACTERS ===
[
  {"name":"林晚","role":"主角","description":"独立记者","arc":"前期被动追案，后期主动出击","traits":["敏锐","固执"]},
  {"name":"陈沉","role":"反派","description":"幕后凶手","arc":"前期隐蔽，后期暴露","traits":["冷静","残忍"]}
]

=== WORLD_RULES ===
[
  {"category":"society","rule":"现代都市背景，警力体系完备","boundary":"不超自然"}
]

=== LAYERED_OUTLINE ===
[
  {
    "index":1,
    "title":"失踪疑云",
    "theme":"记者追查连环失踪案",
    "arcs":[
      {
        "index":1,
        "title":"初查",
        "goal":"林晚接案并锁定陈姓线索",
        "chapters":[
          {"title":"初遇","core_event":"林晚收到匿名爆料","hook":"线索指向陈姓家族","scenes":["编辑部","咖啡馆"]},
          {"title":"循迹","core_event":"林晚走访失踪者家属","hook":"发现共同祭品符号","scenes":["旧宅","档案馆"]}
        ]
      }
    ]
  }
]

=== COMPASS ===
{
  "ending_direction":"真相大白，主角在揭露与自保间抉择",
  "open_threads":["陈姓家族的祭品仪式真相","林晚的清白指控"],
  "estimated_scale":"预计 20-40 章"
}
`

func TestReverseFoundation_ParsesValid(t *testing.T) {
	llm := &mockLLM{out: validEnvelope}
	chapters := []Chapter{
		{Title: "初遇", Content: "林晚翻开匿名信..."},
		{Title: "循迹", Content: "她敲响那栋旧宅的门..."},
	}
	got, err := ReverseFoundation(context.Background(), llm, "system prompt with ${chapter_count}", chapters)
	if err != nil {
		t.Fatalf("ReverseFoundation: %v", err)
	}
	if !strings.HasPrefix(got.Premise, "# 测试书名") {
		t.Errorf("premise head: %q", got.Premise[:20])
	}
	if len(got.Characters) != 2 || got.Characters[0].Name != "林晚" {
		t.Errorf("characters wrong: %+v", got.Characters)
	}
	if len(got.Volumes) != 1 || len(domain.FlattenOutline(got.Volumes)) != 2 {
		t.Errorf("volumes wrong: %+v", got.Volumes)
	}
	if got.Compass == nil || len(got.Compass.OpenThreads) == 0 {
		t.Errorf("compass should be parsed with open_threads: %+v", got.Compass)
	}
	if !strings.Contains(llm.got[0].TextContent(), "with 2") {
		t.Errorf("system prompt expected ${chapter_count}=2 substituted, got: %q",
			llm.got[0].TextContent())
	}
	if !strings.Contains(llm.got[1].TextContent(), "林晚翻开匿名信") {
		t.Errorf("user prompt should contain chapter 1 content")
	}
}

func TestReverseFoundation_RejectsLengthMismatch(t *testing.T) {
	llm := &mockLLM{out: validEnvelope}
	chapters := []Chapter{
		{Title: "ch1", Content: "..."},
		{Title: "ch2", Content: "..."},
		{Title: "ch3", Content: "..."},
	}
	_, err := ReverseFoundation(context.Background(), llm, "x", chapters)
	if err == nil || !strings.Contains(err.Error(), "chapter count mismatch") {
		t.Fatalf("want chapter-count-mismatch error, got %v", err)
	}
}

func TestReverseFoundation_MissingTagFails(t *testing.T) {
	llm := &mockLLM{out: "=== PREMISE ===\n# x\n"}
	_, err := ReverseFoundation(context.Background(), llm,
		"x", []Chapter{{Title: "a", Content: "b"}})
	if err == nil || !strings.Contains(err.Error(), "missing required tags") {
		t.Fatalf("want missing-tags error, got %v", err)
	}
}

func TestParseFoundation_FencedJSONStripped(t *testing.T) {
	src := strings.ReplaceAll(validEnvelope,
		`=== CHARACTERS ===
[`,
		"=== CHARACTERS ===\n```json\n[",
	)
	src = strings.ReplaceAll(src, `]

=== WORLD_RULES ===`, "]\n```\n\n=== WORLD_RULES ===")
	got, err := parseFoundationOutput(src, 2)
	if err != nil {
		t.Fatalf("fenced parse: %v", err)
	}
	if len(got.Characters) != 2 {
		t.Errorf("characters: %+v", got.Characters)
	}
}

func TestPersistFoundation_PromotesPhaseToWriting(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Progress.Init("import-test", 0); err != nil {
		t.Fatalf("init progress: %v", err)
	}

	fr := mustParse(t, validEnvelope, 2)
	if err := PersistFoundation(context.Background(), st, domain.PlanningTierShort, fr); err != nil {
		t.Fatalf("PersistFoundation: %v", err)
	}

	prog, err := st.Progress.Load()
	if err != nil {
		t.Fatalf("load progress: %v", err)
	}
	if prog.Phase != domain.PhaseWriting {
		t.Errorf("phase: got %q want writing", prog.Phase)
	}
	if prog.TotalChapters != 2 {
		t.Errorf("total chapters: %d", prog.TotalChapters)
	}
	if !prog.Layered {
		t.Errorf("imported book must be layered so it can be continued/extended")
	}
	if c, _ := st.Outline.LoadCompass(); c == nil {
		t.Errorf("compass must be saved for continuation")
	}
	if prog.NovelName != "测试书名" {
		t.Errorf("novel name: %q", prog.NovelName)
	}
	if got := st.FoundationMissing(); len(got) != 0 {
		t.Errorf("foundation should be complete, missing: %v", got)
	}
}

func mustParse(t *testing.T, raw string, expect int) *FoundationResult {
	t.Helper()
	fr, err := parseFoundationOutput(raw, expect)
	if err != nil {
		t.Fatalf("parse helper: %v", err)
	}
	return fr
}

// ── 结构化 JSON 输出（B2）──

func foundationJSONOutput() string {
	return `{
  "premise": "# 测试书名\n\n## 题材和基调\n现代都市悬疑\n\n## 核心冲突\n记者追查\n\n## 主角目标\n自证清白\n\n## 结局方向\n真相大白\n\n## 写作禁区\n无\n\n## 差异化卖点\n- 双线\n\n## 差异化钩子\n陈姓\n\n## 核心兑现承诺\n解谜",
  "characters": [
    {"name":"林晚","role":"主角","description":"独立记者","arc":"前期被动","traits":["敏锐"]},
    {"name":"陈沉","role":"反派","description":"幕后凶手","arc":"前期隐蔽","traits":["冷静"]}
  ],
  "world_rules": [
    {"category":"society","rule":"现代都市背景","boundary":"不超自然"}
  ],
  "layered_outline": [
    {
      "index": 1,
      "title": "失踪疑云",
      "theme": "追查",
      "arcs": [
        {
          "index": 1,
          "title": "初查",
          "goal": "接案",
          "chapters": [
            {"title": "初遇", "core_event": "收到爆料", "hook": "陈姓", "scenes": ["编辑部"]},
            {"title": "循迹", "core_event": "走访家属", "hook": "祭品符号", "scenes": ["旧宅"]}
          ]
        }
      ]
    }
  ],
  "compass": {
    "ending_direction": "真相大白",
    "open_threads": ["祭品仪式真相"],
    "estimated_scale": "预计 20-40 章"
  }
}`
}

func TestReverseFoundation_ParsesStructuredJSON(t *testing.T) {
	llm := &mockLLM{out: "一些前置文字\n```json\n" + foundationJSONOutput() + "\n```\n后置说明"}
	chapters := []Chapter{
		{Title: "初遇", Content: "林晚翻开匿名信..."},
		{Title: "循迹", Content: "她敲响那栋旧宅的门..."},
	}
	got, err := ReverseFoundation(context.Background(), llm, "sys ${chapter_count} ${stage_fields}", chapters)
	if err != nil {
		t.Fatalf("ReverseFoundation: %v", err)
	}
	if !strings.HasPrefix(got.Premise, "# 测试书名") {
		t.Errorf("premise: %q", got.Premise[:10])
	}
	if len(got.Characters) != 2 || len(got.WorldRules) != 1 {
		t.Errorf("characters/world_rules wrong: %+v", got)
	}
	if len(domain.FlattenOutline(got.Volumes)) != 2 || got.Compass == nil {
		t.Errorf("volumes/compass wrong: %+v", got)
	}
	if sys := llm.got[0].TextContent(); !strings.Contains(sys, "sys 2") || !strings.Contains(sys, "七个字段全部输出") {
		t.Errorf("system prompt placeholders not substituted: %q", sys)
	}
}

// W1：factions/locations 从正文反推并落盘（结构化 JSON 路径）。
func TestReverseFoundation_ParsesFactionsLocations(t *testing.T) {
	out := strings.Replace(foundationJSONOutput(), `"compass":`, `"factions": [{"name": "黑鸦会", "goal": "夺权", "relation": "敌对", "status": "活跃", "location": "城西码头"}], "locations": [{"name": "城西码头", "kind": "域", "owner_faction": "黑鸦会"}], "compass":`, 1)
	llm := &mockLLM{out: out}
	chapters := []Chapter{{Title: "初遇", Content: "正文"}, {Title: "循迹", Content: "正文"}}
	got, err := ReverseFoundation(context.Background(), llm, "sys ${chapter_count} ${stage_fields}", chapters)
	if err != nil {
		t.Fatalf("ReverseFoundation: %v", err)
	}
	if len(got.Factions) != 1 || got.Factions[0].Name != "黑鸦会" || got.Factions[0].Location != "城西码头" {
		t.Fatalf("factions parse wrong: %+v", got.Factions)
	}
	if len(got.Locations) != 1 || got.Locations[0].OwnerFaction != "黑鸦会" {
		t.Fatalf("locations parse wrong: %+v", got.Locations)
	}

	// 落盘联动
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Progress.Init("import-faction", 0); err != nil {
		t.Fatalf("init progress: %v", err)
	}
	if err := PersistFoundation(context.Background(), st, domain.PlanningTierShort, got); err != nil {
		t.Fatalf("PersistFoundation: %v", err)
	}
	factions, _ := st.World.LoadFactions()
	locations, _ := st.World.LoadLocations()
	if len(factions) != 1 || len(locations) != 1 {
		t.Fatalf("factions/locations not persisted: %+v / %+v", factions, locations)
	}
}

func TestReverseFoundation_StagedForLargeInput(t *testing.T) {
	coreOut := `{"premise": "# 测试书名\n\n## 题材和基调\n都市\n\n## 核心冲突\n追查\n\n## 主角目标\n真相\n\n## 结局方向\n揭晓\n\n## 写作禁区\n无\n\n## 差异化卖点\n- 双线\n\n## 差异化钩子\n陈姓\n\n## 核心兑现承诺\n解谜", "characters": [{"name":"林晚","role":"主角","description":"记者","arc":"前期被动","traits":["敏锐"]}], "world_rules": [{"category":"society","rule":"现代背景","boundary":"不超自然"}]}`

	total := foundationStageThreshold + 1
	chapters := make([]Chapter, total)
	var outlineChapters strings.Builder
	outlineChapters.WriteString("[")
	for i := range chapters {
		chapters[i] = Chapter{Title: fmt.Sprintf("ch%d", i+1), Content: "正文"}
		if i > 0 {
			outlineChapters.WriteString(",")
		}
		fmt.Fprintf(&outlineChapters, `{"title":"ch%d","core_event":"事件","hook":"钩子","scenes":["场景"]}`, i+1)
	}
	outlineChapters.WriteString("]")
	structOut := `{"layered_outline": [{"index":1,"title":"卷","theme":"追查","arcs":[{"index":1,"title":"弧","goal":"接案","chapters":` + outlineChapters.String() + `}]}], "compass": {"ending_direction": "真相大白", "open_threads": ["仪式真相"], "estimated_scale": "预计 20-40 章"}}`

	llm := &scriptedLLM{responses: []string{coreOut, structOut}}
	got, err := ReverseFoundation(context.Background(), llm, "sys ${chapter_count} ${stage_fields}", chapters)
	if err != nil {
		t.Fatalf("staged ReverseFoundation: %v", err)
	}
	if got.Characters[0].Name != "林晚" || len(got.WorldRules) != 1 {
		t.Errorf("core stage merge wrong: %+v", got)
	}
	if len(domain.FlattenOutline(got.Volumes)) != total {
		t.Errorf("volumes: got %d want %d", len(domain.FlattenOutline(got.Volumes)), total)
	}
	if got.Compass == nil || got.Compass.EndingDirection != "真相大白" {
		t.Errorf("compass merge wrong: %+v", got.Compass)
	}
	if len(llm.got) != 4 {
		t.Errorf("expected 2 LLM calls (2 messages each), got %d messages", len(llm.got))
	}
	sys0 := llm.got[0].TextContent()
	if !strings.Contains(sys0, "只输出 premise / characters / world_rules / factions / locations 五个字段") {
		t.Errorf("stage core fields not substituted: %q", sys0)
	}
	sys1 := llm.got[2].TextContent()
	if !strings.Contains(sys1, "只输出 layered_outline / compass 两个字段") {
		t.Errorf("stage structures fields not substituted: %q", sys1)
	}
}
