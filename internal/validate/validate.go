// Package validate は LLM が返した問題ドラフトの「吟味」を行う。
//
// DESIGN.md §4「品質ゲート（自動）」のうち、サンドボックス実行を伴わない
// 静的チェックを担当する。出力予測型の実行照合は将来（verify サブコマンド）。
//
// Check は構造化した不合格理由 []Issue を返す。空なら合格。
// generate / regenerate はこの理由を次のプロンプトに添えて再生成する。
package validate

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/policy"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/schema"
)

// Issue は 1 件の不合格理由。
type Issue struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (i Issue) String() string { return fmt.Sprintf("%s: %s", i.Field, i.Reason) }

// Options は Check の外部依存。
type Options struct {
	// AllowedLanguages はポリシーで許可された code_language。
	AllowedLanguages []string
	// ExistingCorpus は既存 published / needs_review 問題の "title\nbody" 正規化済み文字列。
	// 近似重複の判定に使う。
	ExistingCorpus []string
	// DuplicateThreshold は近似重複とみなす bigram Jaccard 係数のしきい値（既定 0.85）。
	DuplicateThreshold float64
}

// Strings は Issue のスライスを人間可読な文字列スライスにする（プロンプト添付用）。
func Strings(issues []Issue) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.String()
	}
	return out
}

var codeRequiredTypes = map[string]bool{
	"code_reading":      true,
	"output_prediction": true,
	"bug_finding":       true,
	"fill_in_blank":     true,
}

var singleAnswerTypes = map[string]bool{
	"code_reading":      true,
	"output_prediction": true,
}

// Check はドラフトを検証する。
func Check(d schema.QuestionDraft, opts Options) []Issue {
	var issues []Issue
	add := func(field, reason string) { issues = append(issues, Issue{field, reason}) }

	if !policy.KnownType(d.Type) {
		add("type", fmt.Sprintf("未知の問題タイプ: %q", d.Type))
	}
	if d.Difficulty < 1 || d.Difficulty > 5 {
		add("difficulty", fmt.Sprintf("難易度は 1〜5 です: %d", d.Difficulty))
	}

	if strings.TrimSpace(d.Title) == "" {
		add("title", "空です")
	}
	if strings.TrimSpace(d.Body) == "" {
		add("body", "空です")
	} else if !containsJapanese(d.Body) {
		add("body", "日本語で書かれていません")
	}
	if strings.TrimSpace(d.Explanation) == "" {
		add("explanation", "空です")
	} else if !containsJapanese(d.Explanation) {
		add("explanation", "日本語で書かれていません")
	}

	checkChoices(d, add)
	checkCorrectKeys(d, add)
	checkCode(d, opts.AllowedLanguages, add)

	if dupOf := nearestDuplicate(d, opts); dupOf >= 0 {
		add("body", "既存の問題と内容が近すぎます（重複の疑い）")
	}

	return issues
}

func checkChoices(d schema.QuestionDraft, add func(string, string)) {
	if len(d.Choices) < 3 || len(d.Choices) > 5 {
		add("choices", fmt.Sprintf("選択肢は 3〜5 個です: %d 個", len(d.Choices)))
	}
	seen := map[string]bool{}
	for i, c := range d.Choices {
		if strings.TrimSpace(c.Key) == "" {
			add("choices", fmt.Sprintf("%d 番目の選択肢の key が空です", i+1))
			continue
		}
		if seen[c.Key] {
			add("choices", fmt.Sprintf("key が重複しています: %q", c.Key))
		}
		seen[c.Key] = true
		if strings.TrimSpace(c.Text) == "" {
			add("choices", fmt.Sprintf("key=%s の text が空です", c.Key))
		}
	}
}

func checkCorrectKeys(d schema.QuestionDraft, add func(string, string)) {
	if len(d.CorrectKeys) == 0 {
		add("correct_keys", "正解が指定されていません")
		return
	}
	valid := map[string]bool{}
	for _, c := range d.Choices {
		valid[c.Key] = true
	}
	for _, k := range d.CorrectKeys {
		if !valid[k] {
			add("correct_keys", fmt.Sprintf("choices に無い key を指しています: %q", k))
		}
	}
	if singleAnswerTypes[d.Type] && len(d.CorrectKeys) != 1 {
		add("correct_keys", fmt.Sprintf("%s は正解 1 個です: %d 個", d.Type, len(d.CorrectKeys)))
	}
}

func checkCode(d schema.QuestionDraft, allowed []string, add func(string, string)) {
	if codeRequiredTypes[d.Type] {
		if strings.TrimSpace(d.Code) == "" {
			add("code", fmt.Sprintf("%s ではコード片が必須です", d.Type))
		}
		if d.CodeLanguage == "" {
			add("code_language", "コード必須タイプでは言語を指定してください")
		} else if !contains(allowed, d.CodeLanguage) {
			add("code_language", fmt.Sprintf("ポリシー外の言語です: %q（許可: %s）", d.CodeLanguage, strings.Join(allowed, ", ")))
		}
	}
	if n := strings.Count(d.Code, "\n") + 1; d.Code != "" && n > 12 {
		add("code", fmt.Sprintf("コードが長すぎます: %d 行（12 行以内）", n))
	}
}

// nearestDuplicate は既存コーパスと近似重複していれば、そのインデックスを返す（無ければ -1）。
func nearestDuplicate(d schema.QuestionDraft, opts Options) int {
	th := opts.DuplicateThreshold
	if th <= 0 {
		th = 0.85
	}
	cand := bigrams(normalizeText(d.Title + "\n" + d.Body))
	if len(cand) == 0 {
		return -1
	}
	for i, ex := range opts.ExistingCorpus {
		if jaccard(cand, bigrams(normalizeText(ex))) >= th {
			return i
		}
	}
	return -1
}

func containsJapanese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func normalizeText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func bigrams(s string) map[string]struct{} {
	rs := []rune(s)
	m := make(map[string]struct{})
	for i := 0; i+1 < len(rs); i++ {
		m[string(rs[i:i+2])] = struct{}{}
	}
	return m
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
