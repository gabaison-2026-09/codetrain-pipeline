package balancer

import (
	"testing"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/policy"
)

func testPolicy(maxNew int) policy.Policy {
	var p policy.Policy
	p.Version = 1
	p.Batch.MaxNewPerRun = maxNew
	p.TargetDistribution.ByType = map[string]float64{
		"code_reading":      0.5,
		"output_prediction": 0.5,
	}
	p.TargetDistribution.ByDifficulty = map[string]float64{
		"1": 0.5,
		"2": 0.5,
	}
	p.Languages = []string{"javascript", "typescript"}
	return p
}

func TestPlanFillsEmptyBankToTargetShape(t *testing.T) {
	p := testPolicy(8)
	got := Plan(nil, nil, p)
	if len(got) != 8 {
		t.Fatalf("割当数が想定と違う: got %d, want 8", len(got))
	}
	// 空バンク + 均等な目標 → 4 セルに 2 件ずつ。
	counts := map[cell]int{}
	for _, a := range got {
		counts[cell{a.Type, a.Difficulty}]++
	}
	for c, n := range counts {
		if n != 2 {
			t.Fatalf("セル %v の割当数が偏っている: %d（want 2）", c, n)
		}
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	p := testPolicy(7)
	dist := []DistRow{{Type: "code_reading", Difficulty: 1, Count: 3}}
	a := Plan(dist, nil, p)
	b := Plan(dist, nil, p)
	if len(a) != len(b) {
		t.Fatalf("実行ごとに件数が変わる: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("実行ごとに割当が変わる（決定的でない）: index %d", i)
		}
	}
}

func TestPlanPrioritizesUnderfilledCells(t *testing.T) {
	p := testPolicy(4)
	// code_reading/1 が過剰、他は空 → 生成は他セルに寄る。
	dist := []DistRow{{Type: "code_reading", Difficulty: 1, Count: 100}}
	got := Plan(dist, nil, p)
	for _, a := range got {
		if a.Type == "code_reading" && a.Difficulty == 1 {
			t.Fatalf("過剰なセルに割り当てられた: %+v", a)
		}
	}
}

func TestPlanRoundRobinsLanguage(t *testing.T) {
	p := testPolicy(4)
	got := Plan(nil, nil, p)
	seen := map[string]bool{}
	for _, a := range got {
		seen[a.Language] = true
	}
	if !seen["javascript"] || !seen["typescript"] {
		t.Fatalf("言語がラウンドロビンされていない: %v", seen)
	}
}
