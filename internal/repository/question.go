package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gabaison-2026-09/codetrain-core/pkg/domain"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/balancer"
)

// バンクの分布・重複判定の母集団に含めるステータス。
// draft / rejected は「まだ / もう」バンクに数えない。
const bankStatusFilter = `status IN ('needs_review', 'published')`

// GenMeta は生成メタデータ（OPEN_ISSUES C-3）。question の列に記録する。
type GenMeta struct {
	PromptVersion string
	ModelID       string
	GenTokens     int
	GeneratedAt   time.Time
}

// QuestionRow は一覧表示（questions サブコマンド）用の軽量ビュー。
type QuestionRow struct {
	ID         string
	Type       string
	Status     string
	Difficulty int
	Title      string
	CreatedAt  time.Time
}

// QuestionDistribution は (type, difficulty) ごとの現状件数を返す。
func (p *Postgres) QuestionDistribution(ctx context.Context) ([]balancer.DistRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT type::text, difficulty, count(*)
		  FROM question
		 WHERE `+bankStatusFilter+`
		 GROUP BY type, difficulty`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (balancer.DistRow, error) {
		var d balancer.DistRow
		err := r.Scan(&d.Type, &d.Difficulty, &d.Count)
		return d, err
	})
}

// SkillNodes はトピック文脈として渡す skill_node を返す。
// slugs が空なら全件、指定があればその slug に限定する。
func (p *Postgres) SkillNodes(ctx context.Context, slugs []string) ([]balancer.SkillNodeRef, error) {
	q := `SELECT id::text, slug, name, COALESCE(description, '')
	        FROM skill_node`
	var args []any
	if len(slugs) > 0 {
		q += ` WHERE slug = ANY($1)`
		args = append(args, slugs)
	}
	q += ` ORDER BY skill_id, display_order, id`

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (balancer.SkillNodeRef, error) {
		var n balancer.SkillNodeRef
		err := r.Scan(&n.ID, &n.Slug, &n.Name, &n.Description)
		return n, err
	})
}

// ExistingCorpus は近似重複チェック用に、既存問題の "title\nbody" を返す。
func (p *Postgres) ExistingCorpus(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT title || E'\n' || body
		  FROM question
		 WHERE `+bankStatusFilter)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
		var s string
		err := r.Scan(&s)
		return s, err
	})
}

// ListQuestions は status で絞った一覧を返す（status が空なら全件）。
func (p *Postgres) ListQuestions(ctx context.Context, status string) ([]QuestionRow, error) {
	q := `SELECT id::text, type::text, status::text, difficulty, title, created_at
	        FROM question`
	var args []any
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC LIMIT 200`

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (QuestionRow, error) {
		var it QuestionRow
		err := r.Scan(&it.ID, &it.Type, &it.Status, &it.Difficulty, &it.Title, &it.CreatedAt)
		return it, err
	})
}

// InsertGeneratedQuestion は生成された問題を needs_review で INSERT し、
// 同一トランザクションで review_queue に未レビュー行（decision IS NULL）を開く。
// raw_source_id は PoC 方針で LLM 生成ダミー行を指す（domain.LLMGeneratedRawSourceID）。
func (p *Postgres) InsertGeneratedQuestion(ctx context.Context, q domain.Question, meta GenMeta) (string, error) {
	choices, correct, err := marshalJSONB(q)
	if err != nil {
		return "", err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO question (
			skill_node_id, raw_source_id, type, status, difficulty,
			title, body, code, code_language, choices, correct_keys,
			explanation, tags, prompt_version, model_id, gen_tokens, generated_at)
		VALUES (
			$1, $2, $3, 'needs_review', $4,
			$5, $6, $7, $8, $9::jsonb, $10::jsonb,
			$11, $12, $13, $14, $15, $16)
		RETURNING id::text`,
		nullableSkillNode(q), domain.LLMGeneratedRawSourceID, string(q.Type), q.Difficulty,
		q.Title, q.Body, nullableString(q.Code), nullableString(q.CodeLanguage), choices, correct,
		nullableString(q.Explanation), q.Tags, meta.PromptVersion, meta.ModelID, meta.GenTokens, meta.GeneratedAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("question の INSERT に失敗: %w", err)
	}

	if err := openReviewTx(ctx, tx, id); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

// ApplyRegeneration は再生成結果で question を UPDATE し、開いていた needs_edit の
// レビュー行を確定させ、新しい未レビュー行を開く（すべて同一トランザクション）。
func (p *Postgres) ApplyRegeneration(ctx context.Context, questionID string, q domain.Question, meta GenMeta) error {
	choices, correct, err := marshalJSONB(q)
	if err != nil {
		return err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	ct, err := tx.Exec(ctx, `
		UPDATE question SET
			type = $2, difficulty = $3, title = $4, body = $5,
			code = $6, code_language = $7, choices = $8::jsonb, correct_keys = $9::jsonb,
			explanation = $10, tags = $11,
			prompt_version = $12, model_id = $13, gen_tokens = $14, generated_at = $15,
			status = 'needs_review', updated_at = now()
		WHERE id = $1`,
		questionID, string(q.Type), q.Difficulty, q.Title, q.Body,
		nullableString(q.Code), nullableString(q.CodeLanguage), choices, correct,
		nullableString(q.Explanation), q.Tags,
		meta.PromptVersion, meta.ModelID, meta.GenTokens, meta.GeneratedAt,
	)
	if err != nil {
		return fmt.Errorf("question の UPDATE に失敗: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}

	// needs_edit で開いていた行を確定（履歴として残す）。
	if _, err := tx.Exec(ctx, `
		UPDATE review_queue SET reviewed_at = COALESCE(reviewed_at, now()), updated_at = now()
		WHERE question_id = $1 AND decision = 'needs_edit' AND reviewed_at IS NULL`, questionID); err != nil {
		return err
	}
	// 再レビュー用の未レビュー行を開く。
	if err := openReviewTx(ctx, tx, questionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func marshalJSONB(q domain.Question) (choices, correct string, err error) {
	c := q.Choices
	if c == nil {
		c = []domain.Choice{}
	}
	k := q.CorrectKeys
	if k == nil {
		k = []string{}
	}
	cb, err := json.Marshal(c)
	if err != nil {
		return "", "", err
	}
	kb, err := json.Marshal(k)
	if err != nil {
		return "", "", err
	}
	return string(cb), string(kb), nil
}

func nullableSkillNode(q domain.Question) any {
	if q.SkillNodeID == nil || *q.SkillNodeID == "" {
		return nil
	}
	return *q.SkillNodeID
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
