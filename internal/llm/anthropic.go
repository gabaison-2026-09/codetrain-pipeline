package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// anthropicClient は Anthropic Messages API を直接叩く（record / live）。
//
// SDK を持ち込まず net/http で最小限だけ実装する。BaseURL を差し替えれば
// 別ゲートウェイ（llm-proxy コンテナ、社内プロキシ等）にも向けられる。
type anthropicClient struct {
	http    *http.Client
	baseURL string
	apiKey  string
	mode    string
}

func newAnthropic(baseURL, apiKey, mode string) *anthropicClient {
	return &anthropicClient{
		http:    &http.Client{Timeout: 120 * time.Second},
		baseURL: baseURL,
		apiKey:  apiKey,
		mode:    mode,
	}
}

func (c *anthropicClient) Mode() string { return c.mode }

type anthropicReqBody struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRespBody struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *anthropicClient) Generate(ctx context.Context, req Request) (Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}
	body := anthropicReqBody{
		Model:       req.Model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		System:      req.System,
		Messages:    []anthropicMessage{{Role: "user", Content: req.User}},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("Anthropic API 呼び出しに失敗: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	var parsed anthropicRespBody
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("Anthropic 応答のパースに失敗 (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil {
			return Response{}, fmt.Errorf("Anthropic API エラー (%d %s): %s", resp.StatusCode, parsed.Error.Type, parsed.Error.Message)
		}
		return Response{}, fmt.Errorf("Anthropic API エラー (status %d): %s", resp.StatusCode, string(raw))
	}

	var text string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return Response{
		Text:    text,
		ModelID: parsed.Model,
		Usage:   Usage{InputTokens: parsed.Usage.InputTokens, OutputTokens: parsed.Usage.OutputTokens},
	}, nil
}
