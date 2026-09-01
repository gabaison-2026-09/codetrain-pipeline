// Package repository は PostgreSQL へのアクセスをまとめる。
//
// codetrain-api/internal/repository と同じ方針:
//   - スキーマ定義は codetrain-core（migrations）が持つ。pipeline は読み書きするが
//     マイグレーションは打たない（REPOSITORIES.md §2.1）。
//   - この層の責務は行の取得・書き込みと型の詰め替えだけ。組み立ては呼び出し側。
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound は対象レコードが無いことを表す。
var ErrNotFound = errors.New("not found")

// Postgres は pgx のコネクションプールを保持する。
type Postgres struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("DB プールの作成に失敗: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

// Ping は DB へ到達できるか確かめる。
func (p *Postgres) Ping(ctx context.Context) error {
	var one int
	return p.pool.QueryRow(ctx, "SELECT 1").Scan(&one)
}
