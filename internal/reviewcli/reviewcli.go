// Package reviewcli は、レビュー画面（codetrain-admin）を起動していないときに
// CLI から問題を承認 / 却下するための最小機能を提供する（LOCAL_DEV.md §6.2）。
//
// 本番の運用経路は codetrain-api の /v1/admin/questions/{id}/review であり、
// これはあくまでローカル実験用の抜け道。
package reviewcli

import (
	"context"
	"errors"
	"fmt"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/repository"
)

// Decide は question に対するレビュー判定を確定させる。
func Decide(ctx context.Context, repo *repository.Postgres, questionID string, decision repository.ReviewDecision, notes string) error {
	err := repo.DecideOpenReview(ctx, questionID, decision, notes)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("未レビュー行が見つかりません（既に判定済み、または問題が存在しません）: %s", questionID)
	}
	return err
}
