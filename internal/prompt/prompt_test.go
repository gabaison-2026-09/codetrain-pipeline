package prompt

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/balancer"
	"github.com/gabaison-2026-09/codetrain-pipeline/internal/seeds"
)

func sampleAssignment() balancer.Assignment {
	return balancer.Assignment{
		Type:       "output_prediction",
		Difficulty: 2,
		Language:   "javascript",
		SkillNode:  balancer.SkillNodeRef{Slug: "values-and-types", Name: "値と型"},
	}
}

func TestBuildGenerationEmptyConditionKeepsLegacyCacheKey(t *testing.T) {
	req := Generation().BuildGeneration("m", sampleAssignment(), seeds.Condition{}, nil)
	want := "gen-question_gen.v1-output_prediction-d2-javascript-values-and-types"
	if req.CacheKey != want {
		t.Fatalf("空 Condition で CacheKey が変わった\n got: %s\nwant: %s", req.CacheKey, want)
	}
	if strings.Contains(req.User, "作問の追加条件") {
		t.Fatal("空 Condition なのにプロンプトへ条件ブロックが入っている")
	}
}

func TestBuildGenerationWithConditionAppendsFingerprintAndBlock(t *testing.T) {
	cat, err := seeds.Load()
	if err != nil {
		t.Fatal(err)
	}
	cond := cat.Pick("javascript", seeds.Counts{Methods: 1, Patterns: 1, SpecTopics: 1}, rand.New(rand.NewSource(7)))
	req := Generation().BuildGeneration("m", sampleAssignment(), cond, nil)

	if !strings.HasSuffix(req.CacheKey, "-"+cond.Fingerprint()) {
		t.Fatalf("CacheKey に条件フィンガープリントが付いていない: %s", req.CacheKey)
	}
	if !strings.Contains(req.User, "作問の追加条件") {
		t.Fatal("プロンプトに条件ブロックが無い")
	}
}

func TestSystemIncludesTypeGuide(t *testing.T) {
	req := Generation().BuildGeneration("m", sampleAssignment(), seeds.Condition{}, nil)
	if !strings.Contains(req.System, "## この問題タイプについて") {
		t.Fatal("System に種別ガイドが連結されていない")
	}
	if !strings.Contains(req.System, "出力予測（output_prediction）") {
		t.Fatal("System に output_prediction のガイド本文が入っていない")
	}
	// 別種別では別ガイドが入る。
	a := sampleAssignment()
	a.Type = "fill_in_blank"
	req2 := Generation().BuildGeneration("m", a, seeds.Condition{}, nil)
	if !strings.Contains(req2.System, "穴埋め（fill_in_blank）") {
		t.Fatal("fill_in_blank のガイドが入っていない")
	}
}

func TestRegenerationSystemHasTypeGuide(t *testing.T) {
	req := Regeneration().BuildRegeneration("m", "key123", "bug_finding", "{}", "", nil)
	if !strings.Contains(req.System, "バグ発見（bug_finding）") {
		t.Fatal("再生成 System に種別ガイドが無い")
	}
	if req.CacheKey != "regen-regenerate.v1-key123" {
		t.Fatalf("再生成 CacheKey が変わった: %s", req.CacheKey)
	}
}
