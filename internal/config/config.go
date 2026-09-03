// Package config は環境変数から構成値を読む。
//
// codetrain-api/internal/config と同じ方針: 構成値はすべて環境変数で与え、
// 実行環境ごとの分岐をコードに書かない（LOCAL_DEV.md §5.3）。
// Ministack と実 AWS の差もエンドポイントの差し替えだけで吸収する。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LLMProvider は実 API（record / live）をどのプロバイダへ向けるか。
//
// デプロイ時は Amazon Bedrock（Claude Haiku）を使うと決定したため既定は bedrock。
// 将来 Bedrock を剥がして Anthropic 直（または別ゲートウェイ）へ戻す場合は
// LLM_PROVIDER=anthropic を指定するだけで切り替わる（internal/llm/bedrock.go を
// 削除して factory の分岐を外せば依存も落とせる）。
// replay / manual モードはプロバイダに依存しない。
type LLMProvider string

const (
	// LLMProviderAnthropic は Anthropic Messages API を直接叩く（net/http）。
	LLMProviderAnthropic LLMProvider = "anthropic"
	// LLMProviderBedrock は Amazon Bedrock 経由で Claude を叩く（anthropic-sdk-go の Bedrock backend）。
	LLMProviderBedrock LLMProvider = "bedrock"
)

// LLMMode は LLM 呼び出しの動作モード（LOCAL_DEV.md §7.1）。
type LLMMode string

const (
	// LLMModeReplay はカセットからのみ応答する。ヒットしなければエラーで停止し、
	// 黙って実 API に落ちない。日常開発・CI の既定。
	LLMModeReplay LLMMode = "replay"
	// LLMModeRecord は実 API を叩き、応答をカセットへ保存する。
	LLMModeRecord LLMMode = "record"
	// LLMModeLive は実 API を叩くが保存しない。
	LLMModeLive LLMMode = "live"
	// LLMModeManual は実 API を一切叩かず、プロンプト全文をファイルへ書き出す。
	// 利用者がそれをブラウザの LLM に貼り、返答を <key>.response.txt に保存して
	// 再実行すると、その返答を使って検証・DB 登録まで進む（API リソース未整備時の E2E 確認用）。
	LLMModeManual LLMMode = "manual"
)

type Config struct {
	DatabaseURL string

	LLMMode          LLMMode
	LLMProvider      LLMProvider
	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicBaseURL string
	BedrockModelID   string
	CassetteDir      string
	ManualDir        string

	PolicyPath    string
	GenMaxRetries int

	// 収集（ingest）導入時に使う。今は器だけ持つ。
	AWSEndpointURL string
	AWSRegion      string
	S3Bucket       string
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:      env("DATABASE_URL", ""),
		LLMMode:          LLMMode(env("LLM_MODE", string(LLMModeReplay))),
		LLMProvider:      LLMProvider(env("LLM_PROVIDER", string(LLMProviderBedrock))),
		AnthropicAPIKey:  env("ANTHROPIC_API_KEY", ""),
		AnthropicModel:   env("ANTHROPIC_MODEL", "claude-haiku-4-5"),
		AnthropicBaseURL: strings.TrimRight(env("ANTHROPIC_BASE_URL", "https://api.anthropic.com"), "/"),
		BedrockModelID:   env("BEDROCK_MODEL_ID", "jp.anthropic.claude-haiku-4-5-20251001-v1:0"),
		CassetteDir:      env("CASSETTE_DIR", "testdata/cassettes"),
		ManualDir:        env("MANUAL_DIR", "manual"),
		PolicyPath:       env("POLICY_PATH", "policy/policy.yaml"),
		GenMaxRetries:    envInt("GEN_MAX_RETRIES", 3),
		AWSEndpointURL:   env("AWS_ENDPOINT_URL", ""),
		AWSRegion:        env("AWS_REGION", "ap-northeast-1"),
		S3Bucket:         env("S3_BUCKET", ""),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL が設定されていません")
	}
	switch cfg.LLMMode {
	case LLMModeReplay, LLMModeRecord, LLMModeLive, LLMModeManual:
	default:
		return Config{}, fmt.Errorf("LLM_MODE は replay / record / live / manual のいずれかです: %q", cfg.LLMMode)
	}
	switch cfg.LLMProvider {
	case LLMProviderAnthropic, LLMProviderBedrock:
	default:
		return Config{}, fmt.Errorf("LLM_PROVIDER は anthropic / bedrock のいずれかです: %q", cfg.LLMProvider)
	}
	if cfg.GenMaxRetries < 1 {
		return Config{}, fmt.Errorf("GEN_MAX_RETRIES は 1 以上です: %d", cfg.GenMaxRetries)
	}

	return cfg, nil
}

// ModelID は現在の LLM_PROVIDER で使うモデル識別子を返す。
//
//   - bedrock: BEDROCK_MODEL_ID（推論プロファイル ID / モデル ID）
//   - anthropic: ANTHROPIC_MODEL
//
// generate / regenerate はこの値をそのまま LLM リクエストの Model に載せる。
func (c Config) ModelID() string {
	if c.LLMProvider == LLMProviderBedrock {
		return c.BedrockModelID
	}
	return c.AnthropicModel
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
