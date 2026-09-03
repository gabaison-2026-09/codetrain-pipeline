package seeds

import (
	"math/rand"
	"testing"
)

func TestLoad(t *testing.T) {
	cat, err := Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	if _, ok := cat.byLang["javascript"]; !ok {
		t.Fatal("javascript のプールが無い")
	}
	if len(cat.common.Patterns) == 0 {
		t.Fatal("common.patterns が空")
	}
}

func TestPickDeterministic(t *testing.T) {
	cat, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	n := Counts{Methods: 1, Patterns: 2, SpecTopics: 1}

	a := cat.Pick("javascript", n, rand.New(rand.NewSource(42)))
	b := cat.Pick("javascript", n, rand.New(rand.NewSource(42)))

	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("同じシードで結果が違う: %v vs %v", a, b)
	}
	if len(a.Methods) != 1 || len(a.Patterns) != 2 || len(a.SpecTopics) != 1 {
		t.Fatalf("件数が Counts と一致しない: %+v", a)
	}
	if a.Empty() || a.PromptBlock() == "" {
		t.Fatal("条件が空になっている")
	}

	c := cat.Pick("javascript", n, rand.New(rand.NewSource(43)))
	if a.Fingerprint() == c.Fingerprint() {
		t.Fatal("別シードでも同じ結果になった（偶然の可能性はあるが要確認）")
	}
}

func TestPickNilRNGAndUnknownLang(t *testing.T) {
	cat, _ := Load()
	if !cat.Pick("javascript", Counts{Methods: 1}, nil).Empty() {
		t.Fatal("rng=nil なら空 Condition のはず")
	}
	// 未知言語でも common から patterns/spec_topics は引ける。methods は空。
	got := cat.Pick("cobol", Counts{Methods: 1, Patterns: 1}, rand.New(rand.NewSource(1)))
	if len(got.Methods) != 0 {
		t.Fatalf("未知言語の methods は空のはず: %v", got.Methods)
	}
	if len(got.Patterns) != 1 {
		t.Fatalf("common から patterns を引けるはず: %v", got.Patterns)
	}
}

func TestDisabledConditionUnchangedKey(t *testing.T) {
	// Empty な Condition は Fingerprint も PromptBlock も空 → CacheKey/プロンプト不変。
	var c Condition
	if c.Fingerprint() != "" || c.PromptBlock() != "" {
		t.Fatal("空 Condition が痕跡を残している")
	}
}
