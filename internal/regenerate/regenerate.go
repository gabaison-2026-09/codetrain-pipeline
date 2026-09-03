// Package regenerate は、レビューで needs_edit になった問題を自動で作り直す。
//
// codetrain-api の POST /v1/admin/questions/{id}/review が decision=needs_edit を
// 記録すると（status は needs_review のまま。API_DESIGN §3）、このサブコマンドが
// それを拾い、reviewer の notes を追加コンテキストにして再生成 → 再検証 →
// question を UPDATE し、新しい未レビュー行を開いて再レビューに回す。
package regenerate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gabaison-2026-09/codetrain-core/pkg/domain"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/llm"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/policy"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/prompt"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/report"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/repository"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/schema"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/validate"
)

// Options は Run の調整パラメータ。
type Options struct {
	Model      string
	MaxRetries int
	Limit      int
	ReportsDir string
	DryRun     bool // LLM 呼び出しも DB 書き込みもせず、依頼内容だけを出力する
}

// Deps は Run の依存。
type Deps struct {
	Repo   *repository.Postgres
	Client llm.Client
	Policy policy.Policy
}

// Run は needs_edit の問題を順に再生成する。
func Run(ctx context.Context, d Deps, opts Options) (*report.Run, error) {
	if opts.MaxRetries < 1 {
		opts.MaxRetries = 3
	}

	targets, err := d.Repo.QuestionsNeedingEdit(ctx, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("needs_edit 問題の取得に失敗: %w", err)
	}

	run := report.New("regenerate")
	run.Planned = len(targets)
	slog.Info("再生成対象", "count", len(targets))

	tmpl := prompt.Regeneration()
	vopts := validate.Options{AllowedLanguages: d.Policy.Languages}

	for _, t := range targets {
		currentJSON := marshalCurrent(t.Current)
		keyID := shortHash(currentJSON + "\x00" + t.Notes)

		if opts.DryRun {
			req := tmpl.BuildRegeneration(opts.Model, keyID, currentJSON, t.Notes, nil)
			slog.Info("dry-run", "question_id", t.Current.ID, "cache_key", req.CacheKey)
			run.Add(report.Item{QuestionID: t.Current.ID, Accepted: false})
			continue
		}

		var (
			issues   []validate.Issue
			last     llm.Response
			draft    schema.QuestionDraft
			accepted bool
			pending  bool
			attempt  int
		)
		for attempt = 1; attempt <= opts.MaxRetries; attempt++ {
			req := tmpl.BuildRegeneration(opts.Model, keyID, currentJSON, t.Notes, validate.Strings(issues))
			resp, err := d.Client.Generate(ctx, req)
			if err != nil {
				if llm.IsManualPending(err) {
					pending = true
					break
				}
				issues = []validate.Issue{{Field: "llm", Reason: err.Error()}}
				break
			}
			last = resp

			pd, perr := schema.Parse(resp.Text)
			if perr != nil {
				issues = []validate.Issue{{Field: "json", Reason: perr.Error()}}
				continue
			}
			// タイプと難易度は元の問題から動かさない（テンプレートでも指示済み）。
			pd.Type = string(t.Current.Type)
			pd.Difficulty = t.Current.Difficulty
			if v := validate.Check(pd, vopts); len(v) > 0 {
				issues = v
				continue
			}
			draft, accepted = pd, true
			break
		}

		item := report.Item{
			QuestionID: t.Current.ID,
			Attempts:   attempt,
			ModelID:    modelID(last, opts.Model),
		}
		if pending {
			item.Pending = true
			run.Add(item)
			slog.Info("プロンプト待ち（manual）", "question_id", t.Current.ID)
			continue
		}
		if !accepted {
			item.Accepted = false
			item.Issues = validate.Strings(issues)
			item.RawResponse = last.Text
			run.Add(item)
			slog.Warn("再生成に失敗（人手介入が必要）", "question_id", t.Current.ID, "issues", len(issues))
			continue
		}

		sn := ""
		if t.Current.SkillNodeID != nil {
			sn = *t.Current.SkillNodeID
		}
		q := draft.ToDomain(sn)
		meta := repository.GenMeta{
			PromptVersion: tmpl.Version,
			ModelID:       modelID(last, opts.Model),
			GenTokens:     last.Usage.Total(),
			GeneratedAt:   time.Now().UTC(),
		}
		if err := d.Repo.ApplyRegeneration(ctx, t.Current.ID, q, meta); err != nil {
			return run, fmt.Errorf("再生成結果の反映に失敗 (question %s): %w", t.Current.ID, err)
		}

		item.Accepted = true
		item.TokensTotal = last.Usage.Total()
		run.Add(item)
		slog.Info("問題を再生成し再レビューへ", "question_id", t.Current.ID, "attempts", attempt)
	}

	if opts.DryRun {
		return run, nil
	}
	if path, err := run.Write(opts.ReportsDir); err == nil {
		slog.Info("レポートを書き出し", "path", path)
	}
	return run, nil
}

func marshalCurrent(q domain.Question) string {
	b, err := json.MarshalIndent(struct {
		Type         domain.QuestionType `json:"type"`
		Difficulty   int                 `json:"difficulty"`
		Title        string              `json:"title"`
		Body         string              `json:"body"`
		Code         string              `json:"code,omitempty"`
		CodeLanguage string              `json:"code_language,omitempty"`
		Choices      []domain.Choice     `json:"choices"`
		CorrectKeys  []string            `json:"correct_keys"`
		Explanation  string              `json:"explanation,omitempty"`
		Tags         []string            `json:"tags,omitempty"`
	}{
		q.Type, q.Difficulty, q.Title, q.Body, q.Code, q.CodeLanguage,
		q.Choices, q.CorrectKeys, q.Explanation, q.Tags,
	}, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

func modelID(r llm.Response, fallback string) string {
	if r.ModelID != "" {
		return r.ModelID
	}
	return fallback
}
