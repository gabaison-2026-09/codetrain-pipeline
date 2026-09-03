// Package generate はポリシー駆動のバッチ生成を組み立てる。
//
// 流れ（実装計画 §6）:
//
//	balancer で割当を決める
//	  → プロンプト成型（前回の不合格理由を添付）
//	  → LLM 依頼
//	  → JSON パース + 吟味（validate）
//	  → 不合格なら GenMaxRetries 回まで再生成
//	  → 合格したら status=needs_review で INSERT + review_queue を開く
package generate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/balancer"
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
	ReportsDir string
	DryRun     bool // LLM 呼び出しも DB 書き込みもせず、割当と依頼キーだけを出力する
}

// Deps は Run の依存。
type Deps struct {
	Repo   *repository.Postgres
	Client llm.Client
	Policy policy.Policy
}

// Run は 1 回のバッチ生成を実行し、レポートを返す。
func Run(ctx context.Context, d Deps, opts Options) (*report.Run, error) {
	if opts.MaxRetries < 1 {
		opts.MaxRetries = 3
	}

	dist, err := d.Repo.QuestionDistribution(ctx)
	if err != nil {
		return nil, fmt.Errorf("現状分布の取得に失敗: %w", err)
	}
	nodes, err := d.Repo.SkillNodes(ctx, d.Policy.SkillNodeSlugs)
	if err != nil {
		return nil, fmt.Errorf("skill_node の取得に失敗: %w", err)
	}
	corpus, err := d.Repo.ExistingCorpus(ctx)
	if err != nil {
		return nil, fmt.Errorf("既存問題コーパスの取得に失敗: %w", err)
	}

	plan := balancer.Plan(dist, nodes, d.Policy)
	run := report.New("generate")
	run.Planned = len(plan)
	slog.Info("生成計画を作成", "assignments", len(plan))

	tmpl := prompt.Generation()
	vopts := validate.Options{
		AllowedLanguages: d.Policy.Languages,
		ExistingCorpus:   corpus,
	}

	for _, a := range plan {
		if opts.DryRun {
			req := tmpl.BuildGeneration(opts.Model, a, nil)
			slog.Info("dry-run", "type", a.Type, "difficulty", a.Difficulty,
				"language", a.Language, "skill_node", a.SkillNode.Slug, "cache_key", req.CacheKey)
			continue
		}

		draft, resp, attempts, issues, ok, pending := attemptOne(ctx, d.Client, tmpl, opts.Model, a, opts.MaxRetries, vopts)

		item := report.Item{
			Assignment: a,
			Attempts:   attempts,
			ModelID:    modelID(resp, opts.Model),
		}
		if pending {
			item.Pending = true
			run.Add(item)
			slog.Info("プロンプト待ち（manual）", "type", a.Type, "difficulty", a.Difficulty,
				"language", a.Language, "skill_node", a.SkillNode.Slug)
			continue
		}
		if !ok {
			item.Accepted = false
			item.Issues = validate.Strings(issues)
			item.RawResponse = resp.Text
			run.Add(item)
			slog.Warn("問題を却下", "type", a.Type, "difficulty", a.Difficulty, "issues", len(issues))
			continue
		}

		q := draft.ToDomain(a.SkillNode.ID)
		meta := repository.GenMeta{
			PromptVersion: tmpl.Version,
			ModelID:       modelID(resp, opts.Model),
			GenTokens:     resp.Usage.Total(),
			GeneratedAt:   time.Now().UTC(),
		}
		id, err := d.Repo.InsertGeneratedQuestion(ctx, q, meta)
		if err != nil {
			return run, fmt.Errorf("question の登録に失敗: %w", err)
		}
		// 同一バッチ内の後続生成が、いま作った問題とも重複しないようにする。
		vopts.ExistingCorpus = append(vopts.ExistingCorpus, q.Title+"\n"+q.Body)

		item.Accepted = true
		item.QuestionID = id
		item.TokensTotal = resp.Usage.Total()
		run.Add(item)
		slog.Info("問題を登録", "id", id, "type", a.Type, "difficulty", a.Difficulty, "attempts", attempts)
	}

	if opts.DryRun {
		return run, nil
	}

	path, err := run.Write(opts.ReportsDir)
	if err != nil {
		slog.Warn("レポートの書き出しに失敗", "error", err)
	} else {
		slog.Info("レポートを書き出し", "path", path)
	}
	return run, nil
}

// attemptOne は 1 つの割当について、合格するまで（or 上限まで）生成を試みる。
func attemptOne(
	ctx context.Context, client llm.Client, tmpl prompt.Template, model string,
	a balancer.Assignment, maxRetries int, vopts validate.Options,
) (draft schema.QuestionDraft, last llm.Response, attempts int, issues []validate.Issue, ok, pending bool) {

	for attempts = 1; attempts <= maxRetries; attempts++ {
		req := tmpl.BuildGeneration(model, a, validate.Strings(issues))
		resp, err := client.Generate(ctx, req)
		if err != nil {
			if llm.IsManualPending(err) {
				return draft, resp, attempts, nil, false, true
			}
			issues = []validate.Issue{{Field: "llm", Reason: err.Error()}}
			return draft, resp, attempts, issues, false, false
		}
		last = resp

		d, perr := schema.Parse(resp.Text)
		if perr != nil {
			issues = []validate.Issue{{Field: "json", Reason: perr.Error()}}
			continue
		}
		if v := validate.Check(d, vopts); len(v) > 0 {
			issues = v
			continue
		}
		return d, resp, attempts, nil, true, false
	}
	attempts = maxRetries
	return draft, last, attempts, issues, false, false
}

func modelID(r llm.Response, fallback string) string {
	if r.ModelID != "" {
		return r.ModelID
	}
	return fallback
}
