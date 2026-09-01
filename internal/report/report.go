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
	Planned   int       `json:"planned"`
	Accepted  int       `json:"accepted"`
	Rejected  int       `json:"rejected"`
	Items     []Item    `json:"items"`
}

// Item は 1 問分の結果。
type Item struct {
	Assignment  any      `json:"assignment,omitempty"`
	QuestionID  string   `json:"question_id,omitempty"`
	Accepted    bool     `json:"accepted"`
	Attempts    int      `json:"attempts"`
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
	if it.Accepted {
		r.Accepted++
	} else {
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
	return fmt.Sprintf("計画 %d 件 / 合格 %d 件 / 却下 %d 件", r.Planned, r.Accepted, r.Rejected)
}
