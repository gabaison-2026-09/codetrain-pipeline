// Package app は各サブコマンドの依存を組み立てる（DI）。
// codetrain-api の cmd/api/main.go の run() 相当を、CLI 向けに切り出したもの。
package app

import (
	"context"
	"fmt"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/config"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/llm"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/policy"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/repository"
)

// App は全サブコマンドが共有する依存。
type App struct {
	Cfg    config.Config
	Repo   *repository.Postgres
	Policy policy.Policy
}

// New は config を読み、DB プールとポリシーを用意する。
// 返す cleanup は必ず defer で呼ぶこと。
func New(ctx context.Context) (*App, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	pol, err := policy.Load(cfg.PolicyPath)
	if err != nil {
		return nil, nil, err
	}
	repo, err := repository.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	if err := repo.Ping(ctx); err != nil {
		repo.Close()
		return nil, nil, fmt.Errorf("DB に接続できません: %w", err)
	}
	return &App{Cfg: cfg, Repo: repo, Policy: pol}, repo.Close, nil
}

// LLM は LLM クライアントを組み立てる。allowLLMCalls は --allow-llm-calls フラグ。
// ctx は Bedrock backend（AWS 認証情報の解決）の初期化に使う。
func (a *App) LLM(ctx context.Context, allowLLMCalls bool) (llm.Client, error) {
	return llm.New(ctx, a.Cfg, allowLLMCalls)
}
