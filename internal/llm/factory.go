package llm

import (
	"fmt"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/config"
)

// New は LLM_MODE から Client を組み立てる。
//
//   - replay（既定）: カセットのみ。API キー不要。
//   - manual: 実 API を叩かず、プロンプトをファイルへ書き出す。返答ファイルがあれば読む。API キー不要。
//   - record: 実 API + カセット保存。ANTHROPIC_API_KEY と allowLLMCalls が必須。
//   - live:   実 API のみ。同上。
//
// allowLLMCalls は CLI フラグ --allow-llm-calls に対応する。環境変数にキーが
// あるだけで課金が走らないようにするための二重ロック（LOCAL_DEV.md §7.1）。
func New(cfg config.Config, allowLLMCalls bool) (Client, error) {
	switch cfg.LLMMode {
	case config.LLMModeReplay:
		return &cassetteClient{dir: cfg.CassetteDir, mode: "replay"}, nil

	case config.LLMModeManual:
		return &manualClient{dir: cfg.ManualDir}, nil

	case config.LLMModeRecord:
		if err := requireLive(cfg, allowLLMCalls); err != nil {
			return nil, err
		}
		inner := newAnthropic(cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, "record")
		return &cassetteClient{dir: cfg.CassetteDir, inner: inner, mode: "record"}, nil

	case config.LLMModeLive:
		if err := requireLive(cfg, allowLLMCalls); err != nil {
			return nil, err
		}
		return newAnthropic(cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, "live"), nil

	default:
		return nil, fmt.Errorf("未知の LLM_MODE: %q", cfg.LLMMode)
	}
}

func requireLive(cfg config.Config, allow bool) error {
	if !allow {
		return fmt.Errorf("LLM_MODE=%s は実 API を呼びます。--allow-llm-calls を明示してください", cfg.LLMMode)
	}
	if cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("LLM_MODE=%s には ANTHROPIC_API_KEY が必要です", cfg.LLMMode)
	}
	return nil
}
