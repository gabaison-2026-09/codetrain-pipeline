package llm

import (
	"context"
	"testing"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/config"
)

func TestReplayNeedsNoKey(t *testing.T) {
	c, err := New(config.Config{LLMMode: config.LLMModeReplay, CassetteDir: t.TempDir()}, false)
	if err != nil {
		t.Fatalf("replay の組み立てに失敗: %v", err)
	}
	if c.Mode() != "replay" {
		t.Fatalf("mode が replay でない: %s", c.Mode())
	}
	// カセット無し → エラーで停止（黙って課金しない）。
	if _, err := c.Generate(context.Background(), Request{PromptVersion: "v1", User: "x"}); err == nil {
		t.Fatal("カセット無しなのにエラーにならなかった")
	}
}

func TestRecordRequiresFlagAndKey(t *testing.T) {
	// フラグ無し
	if _, err := New(config.Config{LLMMode: config.LLMModeRecord, AnthropicAPIKey: "k"}, false); err == nil {
		t.Fatal("--allow-llm-calls 無しで record が通った")
	}
	// キー無し
	if _, err := New(config.Config{LLMMode: config.LLMModeLive}, true); err == nil {
		t.Fatal("API キー無しで live が通った")
	}
}

func TestCassetteKeyStable(t *testing.T) {
	r := Request{PromptVersion: "v1", Model: "m", System: "s", User: "u", Temperature: 0.7}
	if cassetteKey(r) != cassetteKey(r) {
		t.Fatal("同じ Request でキーが変わる")
	}
	r2 := r
	r2.User = "u2"
	if cassetteKey(r) == cassetteKey(r2) {
		t.Fatal("User が違うのにキーが同じ")
	}
}
