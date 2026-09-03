// codetrain-pipeline — LLM 問題生成パイプライン（Go / CLI）。
//
// DESIGN.md §7 のとおり本番は Kubernetes CronJob + Job として起動する。
// このバイナリは「サブコマンドを 1 回実行して終わる」バッチで、HTTP サーバーは持たない。
//
// ローカルでは docker compose 経由で走らせる（LOCAL_DEV.md §6）:
//
//	docker compose -f compose.yaml -f compose.lab.yaml run --rm pipeline generate
//	docker compose -f compose.yaml -f compose.lab.yaml run --rm pipeline regenerate
//
// このファイルの役割はサブコマンドのディスパッチとフラグ解釈だけ。
// 依存の組み立ては internal/app、処理本体は internal/<usecase> にある。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/app"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/generate"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/regenerate"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/repository"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/reviewcli"
)

func main() {
	if err := run(); err != nil {
		slog.Error("失敗しました", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return fmt.Errorf("サブコマンドを指定してください")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sub := os.Args[1]
	args := os.Args[2:]

	switch sub {
	case "generate":
		return cmdGenerate(ctx, args)
	case "regenerate":
		return cmdRegenerate(ctx, args)
	case "questions":
		return cmdQuestions(ctx, args)
	case "approve":
		return cmdReview(ctx, args, repository.ReviewApproved)
	case "reject":
		return cmdReview(ctx, args, repository.ReviewRejected)
	case "verify":
		return fmt.Errorf("verify は未実装です（サンドボックス実コード照合の受け皿。実装計画のスコープ外）")
	case "eval":
		return fmt.Errorf("eval は未実装です（プロンプト/モデル比較。実装計画のスコープ外）")
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("未知のサブコマンド: %q", sub)
	}
}

func cmdGenerate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	allow := fs.Bool("allow-llm-calls", false, "record/live モードで実 API 呼び出しを許可する")
	maxRetries := fs.Int("max-retries", 0, "生成の再試行上限（0 なら GEN_MAX_RETRIES）")
	reportsDir := fs.String("reports-dir", "reports", "実行レポートの出力先")
	dryRun := fs.Bool("dry-run", false, "LLM 呼び出しも DB 書き込みもせず、割当と依頼キーだけを出力する")
	seed := fs.Int64("seed", 0, "作問条件のランダム抽選シード（0 なら実行時刻から導出。policy.diversity 有効時のみ使用）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a, cleanup, err := app.New(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	client, err := a.LLM(ctx, *allow)
	if err != nil {
		return err
	}
	slog.Info("generate を開始", "llm_mode", client.Mode(),
		"llm_provider", a.Cfg.LLMProvider, "model", a.Cfg.ModelID())

	rt := *maxRetries
	if rt == 0 {
		rt = a.Cfg.GenMaxRetries
	}
	rep, err := generate.Run(ctx, generate.Deps{Repo: a.Repo, Client: client, Policy: a.Policy}, generate.Options{
		Model:      a.Cfg.ModelID(),
		MaxRetries: rt,
		ReportsDir: *reportsDir,
		DryRun:     *dryRun,
		Seed:       *seed,
	})
	if err != nil {
		return err
	}
	slog.Info("generate 完了", "summary", rep.Summary())
	return nil
}

func cmdRegenerate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("regenerate", flag.ContinueOnError)
	allow := fs.Bool("allow-llm-calls", false, "record/live モードで実 API 呼び出しを許可する")
	maxRetries := fs.Int("max-retries", 0, "再生成の再試行上限（0 なら GEN_MAX_RETRIES）")
	limit := fs.Int("limit", 50, "1 回で処理する needs_edit の最大件数")
	reportsDir := fs.String("reports-dir", "reports", "実行レポートの出力先")
	dryRun := fs.Bool("dry-run", false, "LLM 呼び出しも DB 書き込みもせず、依頼キーだけを出力する")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a, cleanup, err := app.New(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	client, err := a.LLM(ctx, *allow)
	if err != nil {
		return err
	}
	slog.Info("regenerate を開始", "llm_mode", client.Mode(),
		"llm_provider", a.Cfg.LLMProvider, "model", a.Cfg.ModelID())

	rt := *maxRetries
	if rt == 0 {
		rt = a.Cfg.GenMaxRetries
	}
	rep, err := regenerate.Run(ctx, regenerate.Deps{Repo: a.Repo, Client: client, Policy: a.Policy}, regenerate.Options{
		Model:      a.Cfg.ModelID(),
		MaxRetries: rt,
		Limit:      *limit,
		ReportsDir: *reportsDir,
		DryRun:     *dryRun,
	})
	if err != nil {
		return err
	}
	slog.Info("regenerate 完了", "summary", rep.Summary())
	return nil
}

func cmdQuestions(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("questions", flag.ContinueOnError)
	status := fs.String("status", "", "status で絞る（draft / needs_review / published / rejected）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a, cleanup, err := app.New(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	rows, err := a.Repo.ListQuestions(ctx, *status)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tTYPE\tDIFF\tTITLE")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", r.ID, r.Status, r.Type, r.Difficulty, r.Title)
	}
	w.Flush()
	fmt.Printf("\n%d 件\n", len(rows))
	return nil
}

func cmdReview(ctx context.Context, args []string, decision repository.ReviewDecision) error {
	fs := flag.NewFlagSet(string(decision), flag.ContinueOnError)
	id := fs.String("id", "", "対象 question の UUID（必須）")
	notes := fs.String("notes", "", "レビューコメント")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("--id を指定してください")
	}

	a, cleanup, err := app.New(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := reviewcli.Decide(ctx, a.Repo, *id, decision, *notes); err != nil {
		return err
	}
	slog.Info("レビュー判定を記録", "question_id", *id, "decision", decision)
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `codetrain-pipeline — LLM 問題生成パイプライン

使い方:
  pipeline <サブコマンド> [フラグ]

サブコマンド:
  generate      ポリシー駆動でバッチ生成し status=needs_review で登録する
  regenerate    レビューで needs_edit になった問題を自動で作り直す
  questions     問題を一覧表示する（--status で絞り込み）
  approve       未レビュー行を承認して published にする（--id, --notes）
  reject        未レビュー行を却下して rejected にする（--id, --notes）
  verify        （未実装）サンドボックス実コード照合
  eval          （未実装）プロンプト/モデル比較
  help          このヘルプ

主な環境変数:
  DATABASE_URL, LLM_MODE(replay|record|live|manual),
  LLM_PROVIDER(anthropic|bedrock。既定 bedrock。record/live のみ影響),
  ANTHROPIC_API_KEY, ANTHROPIC_MODEL, ANTHROPIC_BASE_URL,
  BEDROCK_MODEL_ID(既定 apac.anthropic.claude-haiku-4-5-20251001-v1:0), AWS_REGION,
  CASSETTE_DIR, MANUAL_DIR, POLICY_PATH, GEN_MAX_RETRIES

LLM_MODE=manual: 実 API を叩かず MANUAL_DIR（既定 manual/）へプロンプトを書き出す。
  内容をブラウザの LLM に貼り、返答 JSON を <key>.response.txt に保存して再実行すると
  検証・DB 登録まで進む（API リソース未整備時の E2E 確認用）。
`)
}
