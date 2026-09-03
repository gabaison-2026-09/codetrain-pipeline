// Package report は生成・再生成の実行結果を reports/<timestamp>/ に JSON で残す。
//
// OPEN_ISSUES C-9「リトライも失敗した入力は捨てずに記録して後で分析」に対応する。
// reports/ は .gitignore 対象（再現可能な派生物）。
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Run は 1 回の実行の集計。
type Run struct {
	Command   string    `json:"command"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	// Seed は作問条件のランダム抽選に使ったシード。0 以外なら --seed で固定するか
	// StartedAt から導出した値。同じ seed + policy で再実行すると条件が再現する。
	Seed     int64  `json:"seed,omitempty"`
	Planned  int    `json:"planned"`
	Accepted int    `json:"accepted"`
	Rejected int    `json:"rejected"`
	Pending  int    `json:"pending"` // LLM_MODE=manual でプロンプト待ちの件数
	Items    []Item `json:"items"`
}

// Item は 1 問分の結果。
type Item struct {
	Assignment  any      `json:"assignment,omitempty"`
	QuestionID  string   `json:"question_id,omitempty"`
	Accepted    bool     `json:"accepted"`
	Pending     bool     `json:"pending,omitempty"` // manual モードでプロンプト待ち
	Attempts    int      `json:"attempts"`
	Conditions  []string `json:"conditions,omitempty"` // 付与した作問条件（diversity 有効時）
	Issues      []string `json:"issues,omitempty"`
	TokensTotal int      `json:"tokens_total,omitempty"`
	ModelID     string   `json:"model_id,omitempty"`
	RawResponse string   `json:"raw_response,omitempty"` // 却下時のみ（分析用）
}

// New はコマンド名を指定して Run を開始する。
func New(command string) *Run {
	return &Run{Command: command, StartedAt: time.Now().UTC()}
}

func (r *Run) Add(it Item) {
	r.Items = append(r.Items, it)
	switch {
	case it.Pending:
		r.Pending++
	case it.Accepted:
		r.Accepted++
	default:
		r.Rejected++
	}
}

// Write は reports/<timestamp>/<command>.json に書き出し、そのパスを返す。
func (r *Run) Write(dir string) (string, error) {
	r.EndedAt = time.Now().UTC()
	if dir == "" {
		dir = "reports"
	}
	sub := filepath.Join(dir, r.StartedAt.Format("20060102-150405"))
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(sub, r.Command+".json")
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Summary は 1 行サマリ（ログ用）。
func (r *Run) Summary() string {
	if r.Pending > 0 {
		return fmt.Sprintf("計画 %d 件 / 合格 %d 件 / 却下 %d 件 / 保留 %d 件",
			r.Planned, r.Accepted, r.Rejected, r.Pending)
	}
	return fmt.Sprintf("計画 %d 件 / 合格 %d 件 / 却下 %d 件", r.Planned, r.Accepted, r.Rejected)
}
