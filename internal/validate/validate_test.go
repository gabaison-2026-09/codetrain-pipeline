package validate

import (
	"strings"
	"testing"

	"github.com/gabaison-2026-09/codetrain-core/pkg/domain"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/schema"
)

func goodDraft() schema.QuestionDraft {
	return schema.QuestionDraft{
		Type:         "output_prediction",
		Title:        "map の返り値",
		Body:         "次のコードの出力として正しいものはどれですか。",
		Code:         "const a=[1,2,3];\nconsole.log(a.map(x=>x*2));",
		CodeLanguage: "javascript",
		Choices: []domain.Choice{
			{Key: "a", Text: "[1,2,3]"}, {Key: "b", Text: "[2,4,6]"},
			{Key: "c", Text: "6"}, {Key: "d", Text: "エラー"},
		},
		CorrectKeys: []string{"b"},
		Explanation: "map は各要素に関数を適用した新しい配列を返します。",
		Difficulty:  2,
		Tags:        []string{"array"},
	}
}

var opts = Options{AllowedLanguages: []string{"javascript", "typescript"}}

func TestCheckAcceptsGoodDraft(t *testing.T) {
	if issues := Check(goodDraft(), opts); len(issues) != 0 {
		t.Fatalf("正常なドラフトが不合格になった: %v", issues)
	}
}

func TestCheckRejectsCorrectKeyNotInChoices(t *testing.T) {
	d := goodDraft()
	d.CorrectKeys = []string{"z"}
	issues := Check(d, opts)
	if !hasField(issues, "correct_keys") {
		t.Fatalf("choices に無い正解キーが検出されなかった: %v", issues)
	}
}

func TestCheckRejectsNonJapaneseBody(t *testing.T) {
	d := goodDraft()
	d.Body = "Which output is correct?"
	if !hasField(Check(d, opts), "body") {
		t.Fatal("非日本語の本文が検出されなかった")
	}
}

func TestCheckRejectsWrongChoiceCount(t *testing.T) {
	d := goodDraft()
	d.Choices = d.Choices[:2]
	if !hasField(Check(d, opts), "choices") {
		t.Fatal("選択肢が 2 個なのに検出されなかった")
	}
}

func TestCheckRejectsLanguageOutsidePolicy(t *testing.T) {
	d := goodDraft()
	d.CodeLanguage = "ruby"
	if !hasField(Check(d, opts), "code_language") {
		t.Fatal("ポリシー外の言語が検出されなかった")
	}
}

func TestCheckDetectsNearDuplicate(t *testing.T) {
	d := goodDraft()
	o := opts
	o.ExistingCorpus = []string{d.Title + "\n" + d.Body}
	issues := Check(d, o)
	if !hasReason(issues, "重複") {
		t.Fatalf("近似重複が検出されなかった: %v", issues)
	}
}

func hasField(issues []Issue, field string) bool {
	for _, i := range issues {
		if i.Field == field {
			return true
		}
	}
	return false
}

func hasReason(issues []Issue, sub string) bool {
	for _, i := range issues {
		if strings.Contains(i.Reason, sub) {
			return true
		}
	}
	return false
}
