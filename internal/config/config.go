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
)

type Config struct {
	DatabaseURL string

	LLMMode          LLMMode
	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicBaseURL string
	CassetteDir      string

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
		AnthropicAPIKey:  env("ANTHROPIC_API_KEY", ""),
		AnthropicModel:   env("ANTHROPIC_MODEL", "claude-haiku-4-5"),
		AnthropicBaseURL: strings.TrimRight(env("ANTHROPIC_BASE_URL", "https://api.anthropic.com"), "/"),
		CassetteDir:      env("CASSETTE_DIR", "testdata/cassettes"),
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
	case LLMModeReplay, LLMModeRecord, LLMModeLive:
	default:
		return Config{}, fmt.Errorf("LLM_MODE は replay / record / live のいずれかです: %q", cfg.LLMMode)
	}
	if cfg.GenMaxRetries < 1 {
		return Config{}, fmt.Errorf("GEN_MAX_RETRIES は 1 以上です: %d", cfg.GenMaxRetries)
	}

	return cfg, nil
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
