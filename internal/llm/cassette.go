package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// cassetteClient は記録再生（LOCAL_DEV.md §7）。
//
//   - replay: カセットからのみ応答。ミスヒットはエラー（inner は nil）。
//   - record: inner（実 API）を呼び、結果をカセットへ保存。
//
// カセットは testdata/cassettes/<key>.json（Git 管理）。key は
// プロンプト版 + System + User + モデル + パラメータ のハッシュ（LOCAL_DEV.md §7.2）。
type cassetteClient struct {
	dir   string
	inner Client // record 時のみ非 nil
	mode  string // "replay" | "record"
}

func (c *cassetteClient) Mode() string { return c.mode }

type cassette struct {
	Key           string    `json:"key"`
	PromptVersion string    `json:"prompt_version"`
	Model         string    `json:"model"`
	Temperature   float64   `json:"temperature"`
	System        string    `json:"system"`
	User          string    `json:"user"`
	Response      Response  `json:"response"`
	RecordedAt    time.Time `json:"recorded_at"`
}

func cassetteKey(req Request) string {
	if req.CacheKey != "" {
		return sanitizeKey(req.CacheKey)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n%s\n%s\n",
		req.PromptVersion, req.Model,
		strconv.FormatFloat(req.Temperature, 'f', -1, 64),
		req.System, req.User)
	return hex.EncodeToString(h.Sum(nil))
}

// sanitizeKey は CacheKey をファイル名に使える形にする（英数字・-・_ 以外を _ に）。
func sanitizeKey(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b = append(b, c)
		default:
			b = append(b, '_')
		}
	}
	return string(b)
}

func (c *cassetteClient) path(key string) string {
	return filepath.Join(c.dir, key+".json")
}

func (c *cassetteClient) Generate(ctx context.Context, req Request) (Response, error) {
	key := cassetteKey(req)
	p := c.path(key)

	if b, err := os.ReadFile(p); err == nil {
		var cas cassette
		if err := json.Unmarshal(b, &cas); err != nil {
			return Response{}, fmt.Errorf("カセットの破損 (%s): %w", p, err)
		}
		resp := cas.Response
		resp.RecordedAt = cas.RecordedAt
		return resp, nil
	} else if !os.IsNotExist(err) {
		return Response{}, fmt.Errorf("カセットの読み込みに失敗 (%s): %w", p, err)
	}

	// ここに来た = カセット無し
	if c.mode == "replay" {
		return Response{}, fmt.Errorf("replay モードでカセットが見つかりません (key=%s)。"+
			"新しい入力・プロンプトを試すには LLM_MODE=record --allow-llm-calls で記録してください", key)
	}

	// record: 実 API を叩いて保存
	resp, err := c.inner.Generate(ctx, req)
	if err != nil {
		return Response{}, err
	}
	cas := cassette{
		Key:           key,
		PromptVersion: req.PromptVersion,
		Model:         req.Model,
		Temperature:   req.Temperature,
		System:        req.System,
		User:          req.User,
		Response:      resp,
		RecordedAt:    time.Now().UTC(),
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return Response{}, err
	}
	b, err := json.MarshalIndent(cas, "", "  ")
	if err != nil {
		return Response{}, err
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return Response{}, fmt.Errorf("カセットの保存に失敗 (%s): %w", p, err)
	}
	return resp, nil
}
