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
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/balancer"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/llm"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/policy"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/prompt"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/report"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/repository"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/schema"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/seeds"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/validate"
)

// Options は Run の調整パラメータ。
type Options struct {
	Model      string
	MaxRetries int
	ReportsDir string
	DryRun     bool  // LLM 呼び出しも DB 書き込みもせず、割当と依頼キーだけを出力する
	Seed       int64 // 作問条件のランダム抽選シード。0 なら実行時刻から導出する
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

	// 作問条件のランダム付与（policy.diversity.enabled のときだけ）。
	div := d.Policy.Diversity
	var catalog seeds.Catalog
	if div.Enabled {
		catalog, err = seeds.Load()
		if err != nil {
			return nil, fmt.Errorf("作問条件カタログの読み込みに失敗: %w", err)
		}
		run.Seed = opts.Seed
		if run.Seed == 0 {
			run.Seed = run.StartedAt.UnixNano()
		}
		slog.Info("作問条件のランダム付与を有効化", "seed", run.Seed)
	}
	pickConditions := func(i int, lang string) seeds.Condition {
		if !div.Enabled {
			return seeds.Condition{}
		}
		rng := rand.New(rand.NewSource(run.Seed + int64(i)))
		return catalog.Pick(lang, seeds.Counts{
			Methods:    div.PerPrompt.Methods,
			Patterns:   div.PerPrompt.Patterns,
			SpecTopics: div.PerPrompt.SpecTopics,
		}, rng)
	}

	tmpl := prompt.Generation()
	vopts := validate.Options{
		AllowedLanguages: d.Policy.Languages,
		ExistingCorpus:   corpus,
	}

	// dry-run では LLM へ投げる直前のプロンプト（中間生成物）を書き出す。
	var promptsDir string
	if opts.DryRun {
		promptsDir = filepath.Join(opts.ReportsDir, run.StartedAt.Format("20060102-150405"), "prompts")
		if err := os.MkdirAll(promptsDir, 0o755); err != nil {
			return nil, fmt.Errorf("プロンプト書き出し先の作成に失敗: %w", err)
		}
	}

	for i, a := range plan {
		cond := pickConditions(i, a.Language)

		if opts.DryRun {
			req := tmpl.BuildGeneration(opts.Model, a, cond, nil)
			path := filepath.Join(promptsDir, llm.PromptKey(req)+".md")
			if err := os.WriteFile(path, []byte(llm.RenderPromptFile(req)), 0o644); err != nil {
				return nil, fmt.Errorf("プロンプトの書き出しに失敗 (%s): %w", path, err)
			}
			slog.Info("dry-run", "type", a.Type, "difficulty", a.Difficulty,
				"language", a.Language, "skill_node", a.SkillNode.Slug,
				"cache_key", req.CacheKey, "prompt", path)
			continue
		}

		draft, resp, attempts, issues, ok, pending := attemptOne(ctx, d.Client, tmpl, opts.Model, a, cond, opts.MaxRetries, vopts)

		item := report.Item{
			Assignment: a,
			Attempts:   attempts,
			ModelID:    modelID(resp, opts.Model),
		}
		if labels := cond.Labels(); len(labels) > 0 {
			item.Conditions = labels
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
		slog.Info("dry-run 完了。プロンプトを書き出しました", "dir", promptsDir, "count", len(plan))
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
	a balancer.Assignment, cond seeds.Condition, maxRetries int, vopts validate.Options,
) (draft schema.QuestionDraft, last llm.Response, attempts int, issues []validate.Issue, ok, pending bool) {

	for attempts = 1; attempts <= maxRetries; attempts++ {
		req := tmpl.BuildGeneration(model, a, cond, validate.Strings(issues))
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
