// Package llm は LLM 呼び出しを抽象化する。
//
// 実装計画のとおり Client インタフェース 1 本に閉じ込め、generate / regenerate は
// 具体実装（Anthropic 直、記録再生、将来の別プロバイダ）を知らない。
// 差し替えは factory.New に分岐を足すだけで済む。
package llm

import (
	"context"
	"time"
)

// Request は 1 回の生成依頼。
//
// System はプロンプトの静的プレフィックス（指示文 + JSON Schema + Few-shot）で、
// 全生成で共通なため将来プロンプトキャッシュのブレークポイントになる（DESIGN.md §7.1）。
// User は可変部（問題タイプ・難易度・言語・トピック・前回の不合格理由）。
type Request struct {
	Model         string
	PromptVersion string
	System        string
	User          string
	MaxTokens     int
	Temperature   float64

	// CacheKey は記録再生カセットの安定キー。プロンプトの文面ではなく
	// 「何を生成させたいか」（タイプ・難易度・言語・トピック / 対象問題）で決める。
	// LOCAL_DEV.md §7.2 の「プロンプト版 + 入力チャンク」に相当し、これにより
	// 文言を微調整してもカセットが無駄にミスヒットしない。空なら System+User をハッシュする。
	CacheKey string
}

// Usage はトークン使用量。カセットに保存してオフラインでコスト試算できるようにする。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Total は入出力合計トークン。question.gen_tokens に記録する。
func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

// Response は生成結果。
type Response struct {
	Text       string    `json:"text"`
	ModelID    string    `json:"model_id"`
	Usage      Usage     `json:"usage"`
	RecordedAt time.Time `json:"recorded_at,omitempty"` // replay 時のみ意味を持つ
}

// Client は LLM 実装の共通インタフェース。
type Client interface {
	// Generate は req に対する応答を返す。
	// replay モードでカセットが無い場合はエラーを返す（黙って課金しない）。
	Generate(ctx context.Context, req Request) (Response, error)
	// Mode は "replay" / "record" / "live" を返す（ログ用）。
	Mode() string
}
