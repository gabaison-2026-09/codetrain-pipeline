package llm

import (
	"context"
	"fmt"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/config"
)

// New は LLM_MODE / LLM_PROVIDER から Client を組み立てる。
//
//   - replay（既定モード）: カセットのみ。API キー / AWS 認証不要。プロバイダ非依存。
//   - manual: 実 API を叩かず、プロンプトをファイルへ書き出す。返答ファイルがあれば読む。プロバイダ非依存。
//   - record: 実 API + カセット保存。allowLLMCalls 必須。プロバイダごとに実クライアントを選ぶ。
//   - live:   実 API のみ。同上。
//
// allowLLMCalls は CLI フラグ --allow-llm-calls に対応する。環境変数に認証情報が
// あるだけで課金が走らないようにするための二重ロック（LOCAL_DEV.md §7.1）。
func New(ctx context.Context, cfg config.Config, allowLLMCalls bool) (Client, error) {
	switch cfg.LLMMode {
	case config.LLMModeReplay:
		return &cassetteClient{dir: cfg.CassetteDir, mode: "replay"}, nil

	case config.LLMModeManual:
		return &manualClient{dir: cfg.ManualDir}, nil

	case config.LLMModeRecord:
		if err := requireLive(cfg, allowLLMCalls); err != nil {
			return nil, err
		}
		inner, err := liveClient(ctx, cfg, "record")
		if err != nil {
			return nil, err
		}
		return &cassetteClient{dir: cfg.CassetteDir, inner: inner, mode: "record"}, nil

	case config.LLMModeLive:
		if err := requireLive(cfg, allowLLMCalls); err != nil {
			return nil, err
		}
		return liveClient(ctx, cfg, "live")

	default:
		return nil, fmt.Errorf("未知の LLM_MODE: %q", cfg.LLMMode)
	}
}

// liveClient は LLM_PROVIDER に応じた実 API クライアントを返す。
//
// Bedrock 固有の依存はこの分岐と bedrock.go に閉じている。剥がすときは
// bedrock ケースと bedrock.go を消し、config の既定を anthropic へ戻すだけでよい。
func liveClient(ctx context.Context, cfg config.Config, mode string) (Client, error) {
	switch cfg.LLMProvider {
	case config.LLMProviderAnthropic:
		return newAnthropic(cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, mode), nil
	case config.LLMProviderBedrock:
		return newBedrock(ctx, cfg.AWSRegion, mode)
	default:
		return nil, fmt.Errorf("未知の LLM_PROVIDER: %q", cfg.LLMProvider)
	}
}

func requireLive(cfg config.Config, allow bool) error {
	if !allow {
		return fmt.Errorf("LLM_MODE=%s は実 API を呼びます。--allow-llm-calls を明示してください", cfg.LLMMode)
	}
	// Anthropic 直のときだけ API キーを必須にする。Bedrock は AWS 認証情報チェーン
	// （環境変数 / 共有 config / EKS Pod Identity / IRSA）に委ね、未設定なら呼び出し時に落ちる。
	if cfg.LLMProvider == config.LLMProviderAnthropic && cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("LLM_MODE=%s（LLM_PROVIDER=anthropic）には ANTHROPIC_API_KEY が必要です", cfg.LLMMode)
	}
	return nil
}
