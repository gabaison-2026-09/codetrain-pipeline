package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gabaison-2026-09/codetrain-core/pkg/domain"
)

// ReviewDecision は review_queue.decision（DB の review_decision ENUM）。
type ReviewDecision string

const (
	ReviewApproved  ReviewDecision = "approved"
	ReviewRejected  ReviewDecision = "rejected"
	ReviewNeedsEdit ReviewDecision = "needs_edit"
)

// NeedsEditItem は「最新のレビュー判定が needs_edit の問題」1 件。
// regenerate が current の内容と notes を使って再生成する。
type NeedsEditItem struct {
	Current domain.Question
	Notes   string
}

// tx を跨いで使う共通の「未レビュー行を開く」処理。
// 既に開いている（decision IS NULL）行があれば何もしない（partial UNIQUE を尊重）。
func openReviewTx(ctx context.Context, tx pgx.Tx, questionID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO review_queue (question_id)
		SELECT $1
		WHERE NOT EXISTS (
			SELECT 1 FROM review_queue WHERE question_id = $1 AND decision IS NULL)`, questionID)
	if err != nil {
		return fmt.Errorf("review_queue の未レビュー行作成に失敗: %w", err)
	}
	return nil
}

// QuestionsNeedingEdit は、question ごとに最新の review_queue 行を見て
// decision = 'needs_edit' のものを返す。
func (p *Postgres) QuestionsNeedingEdit(ctx context.Context, limit int) ([]NeedsEditItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (question_id)
			       question_id, decision, notes
			  FROM review_queue
			 ORDER BY question_id, created_at DESC
		)
		SELECT q.id::text, q.skill_node_id::text, q.type::text, q.status::text, q.difficulty,
		       q.title, q.body, COALESCE(q.code, ''), COALESCE(q.code_language, ''),
		       q.choices, q.correct_keys, COALESCE(q.explanation, ''), q.tags,
		       COALESCE(l.notes, '')
		  FROM latest l
		  JOIN question q ON q.id = l.question_id
		 WHERE l.decision = 'needs_edit'
		 ORDER BY q.updated_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NeedsEditItem
	for rows.Next() {
		var (
			it              NeedsEditItem
			skillNode       *string
			choicesJSON     []byte
			correctKeysJSON []byte
		)
		q := &it.Current
		if err := rows.Scan(
			&q.ID, &skillNode, &q.Type, &q.Status, &q.Difficulty,
			&q.Title, &q.Body, &q.Code, &q.CodeLanguage,
			&choicesJSON, &correctKeysJSON, &q.Explanation, &q.Tags,
			&it.Notes,
		); err != nil {
			return nil, err
		}
		if skillNode != nil && *skillNode != "" {
			q.SkillNodeID = skillNode
		}
		if err := json.Unmarshal(choicesJSON, &q.Choices); err != nil {
			return nil, fmt.Errorf("choices のデコードに失敗 (question %s): %w", q.ID, err)
		}
		if err := json.Unmarshal(correctKeysJSON, &q.CorrectKeys); err != nil {
			return nil, fmt.Errorf("correct_keys のデコードに失敗 (question %s): %w", q.ID, err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DecideOpenReview は開いている未レビュー行（decision IS NULL）を確定させ、
// decision に応じて question.status を遷移させる（reviewcli の approve / reject 用）。
// 開いている行が無ければ ErrNotFound（＝既に判定済み）。
func (p *Postgres) DecideOpenReview(ctx context.Context, questionID string, decision ReviewDecision, notes string) error {
	nextStatus, ok := statusFor(decision)
	if !ok {
		return fmt.Errorf("未知のレビュー判定: %q", decision)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	ct, err := tx.Exec(ctx, `
		UPDATE review_queue
		   SET decision = $2, notes = NULLIF($3, ''), reviewed_at = now(), updated_at = now()
		 WHERE question_id = $1 AND decision IS NULL`,
		questionID, string(decision), notes)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}

	if nextStatus != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE question SET status = $2, updated_at = now() WHERE id = $1`,
			questionID, nextStatus); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func statusFor(d ReviewDecision) (domain.QuestionStatus, bool) {
	switch d {
	case ReviewApproved:
		return domain.QuestionStatusPublished, true
	case ReviewRejected:
		return domain.QuestionStatusRejected, true
	case ReviewNeedsEdit:
		return "", true // status は needs_review のまま（API_DESIGN §3）
	}
	return "", false
}
