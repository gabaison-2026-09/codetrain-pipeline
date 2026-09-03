// Package prompt は版付きプロンプトテンプレートを保持し、
// 割当条件やレビュー指摘を差し込んで llm.Request を組み立てる。
//
// テンプレートは internal/prompt/templates/*.md に置き go:embed で焼き込む。
// question_gen.v1.md が全種別共通のベース（指示文 + JSON Schema）で、
// 種別ごとの作問方針・例は templates/types/<type>.v1.md に分割する。
// System = ベース + その種別のガイド。ベース部分は全生成で共通なので
// 将来プロンプトキャッシュのブレークポイントになる（DESIGN.md §7.1）。
package prompt

import (
	"embed"
	"fmt"
	"strings"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/balancer"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/llm"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/schema"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/seeds"
)

//go:embed templates/*.md templates/types/*.md
var templatesFS embed.FS

// knownTypes は種別ガイドを読み込む question_type の一覧。
// codetrain-core の domain.QuestionType と対応する。
var knownTypes = []string{
	"code_reading", "output_prediction", "bug_finding", "fill_in_blank", "best_practice",
}

// Template は 1 つのプロンプト版。
type Template struct {
	Version string
	System  string // 共通ベース（{{SCHEMA}} 展開済み）

	typeGuides map[string]string // question_type -> 種別ガイド本文
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

	guides := make(map[string]string, len(knownTypes))
	for _, t := range knownTypes {
		gp := "templates/types/" + t + ".v1.md"
		gb, err := templatesFS.ReadFile(gp)
		if err != nil {
			panic(fmt.Sprintf("prompt: 種別ガイド %s の読み込みに失敗: %v", gp, err))
		}
		guides[t] = strings.TrimRight(string(gb), "\n")
	}

	return Template{Version: version, System: sys, typeGuides: guides}
}

// systemFor は共通ベースに種別ガイドを連結した System を返す。
// 未知の種別ではベースのみ返す。
func (t Template) systemFor(qType string) string {
	g, ok := t.typeGuides[qType]
	if !ok {
		return t.System
	}
	return t.System + "\n\n## この問題タイプについて\n\n" + g + "\n"
}

// BuildGeneration は割当条件（＋作問条件＋前回の不合格理由）から生成依頼を組み立てる。
func (t Template) BuildGeneration(model string, a balancer.Assignment, cond seeds.Condition, priorIssues []string) llm.Request {
	var b strings.Builder
	fmt.Fprintf(&b, "問題タイプ: %s（%s）\n", a.Type, typeLabelJA(a.Type))
	fmt.Fprintf(&b, "難易度: %d\n", a.Difficulty)
	fmt.Fprintf(&b, "対象言語: %s\n", a.Language)
	if a.SkillNode.Name != "" {
		fmt.Fprintf(&b, "対象トピック: %s — %s\n", a.SkillNode.Name, a.SkillNode.Description)
	}
	if block := cond.PromptBlock(); block != "" {
		b.WriteString("\n")
		b.WriteString(block)
	}
	b.WriteString("\n上記の条件で問題を 1 問作成してください。\n")
	writeIssues(&b, priorIssues)

	slug := a.SkillNode.Slug
	if slug == "" {
		slug = "none"
	}
	cacheKey := fmt.Sprintf("gen-%s-%s-d%d-%s-%s", t.Version, a.Type, a.Difficulty, a.Language, slug)
	if fp := cond.Fingerprint(); fp != "" {
		cacheKey += "-" + fp
	}
	return llm.Request{
		Model:         model,
		PromptVersion: t.Version,
		System:        t.systemFor(a.Type),
		User:          b.String(),
		MaxTokens:     2048,
		Temperature:   0.7,
		CacheKey:      cacheKey,
	}
}

// BuildRegeneration は現在の問題 JSON・レビュー指摘・自動検証の不合格理由から
// 再生成依頼を組み立てる。qType は元の問題の種別（種別ガイドの選択に使う）。
func (t Template) BuildRegeneration(model, keyID, qType, currentJSON, reviewerNotes string, priorIssues []string) llm.Request {
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
		System:        t.systemFor(qType),
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
