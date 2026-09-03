package config

import "testing"

func TestLoadProviderDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load に失敗: %v", err)
	}
	if cfg.LLMProvider != LLMProviderBedrock {
		t.Fatalf("LLM_PROVIDER 既定が bedrock でない: %q", cfg.LLMProvider)
	}
	if cfg.ModelID() != cfg.BedrockModelID {
		t.Fatalf("bedrock 時の ModelID が BedrockModelID でない: %q", cfg.ModelID())
	}
}

func TestLoadProviderAnthropic(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_MODEL", "claude-haiku-4-5")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load に失敗: %v", err)
	}
	if cfg.ModelID() != "claude-haiku-4-5" {
		t.Fatalf("anthropic 時の ModelID が ANTHROPIC_MODEL でない: %q", cfg.ModelID())
	}
}

func TestLoadProviderInvalid(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("LLM_PROVIDER", "bogus")
	if _, err := Load(); err == nil {
		t.Fatal("不正な LLM_PROVIDER が通った")
	}
}
