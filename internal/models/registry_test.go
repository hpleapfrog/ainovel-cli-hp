package models

import "testing"

// 免费刷新：数据源返回了 pricing 块但价格为 0（模型降价为免费）时，
// 必须覆盖旧价格——不能把"全零"当作"无价格信息"而保留旧价。
func TestMergeModelsFreePriceOverwrites(t *testing.T) {
	r := &ModelRegistry{models: []ModelEntry{
		{Provider: "openai", ID: "gpt-x", InputCostPer1M: 5, OutputCostPer1M: 15, HasPricing: true},
	}}
	r.MergeModels([]ModelEntry{
		{Provider: "openai", ID: "gpt-x", HasPricing: true},
	})
	m := r.models[0]
	if m.InputCostPer1M != 0 || m.OutputCostPer1M != 0 {
		t.Fatalf("free model should overwrite prices to 0, got %v/%v", m.InputCostPer1M, m.OutputCostPer1M)
	}
	if !m.HasPricing {
		t.Fatal("HasPricing should stay true after merge")
	}
}

// 无价格信息（HasPricing=false）的刷新不得清掉旧价格；其余非零字段正常覆盖。
func TestMergeModelsNoPricingKeepsOld(t *testing.T) {
	r := &ModelRegistry{models: []ModelEntry{
		{Provider: "openai", ID: "gpt-x", InputCostPer1M: 5, OutputCostPer1M: 15, HasPricing: true},
	}}
	r.MergeModels([]ModelEntry{
		{Provider: "openai", ID: "gpt-x", ContextWindow: 128000},
	})
	m := r.models[0]
	if m.InputCostPer1M != 5 || m.OutputCostPer1M != 15 {
		t.Fatalf("missing pricing must not zero out prices, got %v/%v", m.InputCostPer1M, m.OutputCostPer1M)
	}
	if m.ContextWindow != 128000 {
		t.Fatalf("context window should be overwritten, got %d", m.ContextWindow)
	}
}

// 合并键 provider+id 大小写不敏感；未知模型直接追加。
func TestMergeModelsCaseInsensitiveAndAppend(t *testing.T) {
	r := &ModelRegistry{models: []ModelEntry{
		{Provider: "Gemini", ID: "Flash-2.5", InputCostPer1M: 1, HasPricing: true},
	}}
	r.MergeModels([]ModelEntry{
		{Provider: "gemini", ID: "flash-2.5", OutputCostPer1M: 3, HasPricing: true},
		{Provider: "qwen", ID: "qwen3", Name: "Qwen3"},
	})
	if len(r.models) != 2 {
		t.Fatalf("expected 2 models after merge, got %d", len(r.models))
	}
	if got := r.models[0].OutputCostPer1M; got != 3 {
		t.Fatalf("case-insensitive merge failed, output cost = %v", got)
	}
	if r.models[1].Name != "Qwen3" {
		t.Fatalf("new model should be appended, got %+v", r.models[1])
	}
}
