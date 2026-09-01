// Package prompt は版付きプロンプトテンプレートを保持し、
// 割当条件やレビュー指摘を差し込んで llm.Request を組み立てる。
//
// テンプレートは internal/prompt/templates/*.md に置き go:embed で焼き込む。
// System（静的プレフィックス。指示文 + JSON Schema + 例）は全生成で共通なので
// 将来プロンプトキャッシュのブレークポイントになる（DESIGN.md §7.1）。
package prompt

import (
	"embed"
	"fmt"
	"strings"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/balancer"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/llm"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/schema"
)

//go:embed templates/*.md
var templatesFS embed.FS

// Template は 1 つのプロンプト版。
type Template struct {
	Version string
	System  string
}

var (
	generation   = mustLoad("templates/question_gen.v1.md", "question_gen.v1")
	regeneration = mustLoad("templates/regenerate.v1.md", "regenerate.v1")
)

// Generation は新規生成用テンプレートを返す。
func Generation() Template { return generation }

// Regeneration は needs_edit 再生成用テンプレートを返す。
func Regeneration() Template { return regeneration }

func mustLoad(path, version string) Template {
	b, err := templatesFS.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("prompt: テンプレート %s の読み込みに失敗: %v", path, err))
	}
	sys := strings.ReplaceAll(string(b), "{{SCHEMA}}", schema.JSONSchema)
	return Template{Version: version, System: sys}
}

// BuildGeneration は割当条件（＋前回の不合格理由）から生成依頼を組み立てる。
func (t Template) BuildGeneration(model string, a balancer.Assignment, priorIssues []string) llm.Request {
	var b strings.Builder
	fmt.Fprintf(&b, "問題タイプ: %s（%s）\n", a.Type, typeLabelJA(a.Type))
	fmt.Fprintf(&b, "難易度: %d\n", a.Difficulty)
	fmt.Fprintf(&b, "対象言語: %s\n", a.Language)
	if a.SkillNode.Name != "" {
		fmt.Fprintf(&b, "対象トピック: %s — %s\n", a.SkillNode.Name, a.SkillNode.Description)
	}
	b.WriteString("\n上記の条件で問題を 1 問作成してください。\n")
	writeIssues(&b, priorIssues)

	slug := a.SkillNode.Slug
	if slug == "" {
		slug = "none"
	}
	return llm.Request{
		Model:         model,
		PromptVersion: t.Version,
		System:        t.System,
		User:          b.String(),
		MaxTokens:     2048,
		Temperature:   0.7,
		CacheKey:      fmt.Sprintf("gen-%s-%s-d%d-%s-%s", t.Version, a.Type, a.Difficulty, a.Language, slug),
	}
}

// BuildRegeneration は現在の問題 JSON・レビュー指摘・自動検証の不合格理由から
// 再生成依頼を組み立てる。
func (t Template) BuildRegeneration(model, keyID, currentJSON, reviewerNotes string, priorIssues []string) llm.Request {
	var b strings.Builder
	b.WriteString("## 現在の問題（JSON）\n")
	b.WriteString(currentJSON)
	b.WriteString("\n\n## レビュー指摘\n")
	if strings.TrimSpace(reviewerNotes) == "" {
		b.WriteString("（コメントなし。全体を見直して品質を上げてください）\n")
	} else {
		b.WriteString(reviewerNotes)
		b.WriteString("\n")
	}
	writeIssues(&b, priorIssues)
	b.WriteString("\n上記を反映して作り直してください。\n")

	return llm.Request{
		Model:         model,
		PromptVersion: t.Version,
		System:        t.System,
		User:          b.String(),
		MaxTokens:     2048,
		Temperature:   0.4,
		CacheKey:      fmt.Sprintf("regen-%s-%s", t.Version, keyID),
	}
}

func writeIssues(b *strings.Builder, issues []string) {
	if len(issues) == 0 {
		return
	}
	b.WriteString("\n## 前回の出力は自動検証で不合格でした。以下を必ず直してください。\n")
	for _, is := range issues {
		fmt.Fprintf(b, "- %s\n", is)
	}
}

func typeLabelJA(t string) string {
	switch t {
	case "code_reading":
		return "コード読解"
	case "output_prediction":
		return "出力予測"
	case "bug_finding":
		return "バグ発見"
	case "fill_in_blank":
		return "穴埋め"
	case "best_practice":
		return "ベストプラクティス選択"
	}
	return t
}
