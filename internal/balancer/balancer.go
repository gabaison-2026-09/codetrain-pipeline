// Package balancer は「現状の問題バンクの分布」と「ポリシーの目標分布」から、
// この生成バッチで作るべき問題の割当リストを決める。
//
// 純関数として実装する（DB アクセスは呼び出し側が済ませ、集計結果を渡す）。
// これにより balancer_test.go で「分布 in → 割当 out」を決定的に検証できる。
package balancer

import (
	"sort"

	"github.com/gabaison-2026-09/codetrain-pipeline/internal/policy"
)

// DistRow は question を (type, difficulty) で GROUP BY した 1 行。
type DistRow struct {
	Type       string
	Difficulty int
	Count      int
}

// SkillNodeRef は生成時にトピック文脈として渡す skill_node の最小情報。
type SkillNodeRef struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Assignment は「1 問生成するための条件」。generate がこれを 1 つずつ処理する。
type Assignment struct {
	Type       string       `json:"type"`
	Difficulty int          `json:"difficulty"`
	Language   string       `json:"language"`
	SkillNode  SkillNodeRef `json:"skill_node"` // ゼロ値なら未マッピング（skill_node_id は NULL）
}

// Plan は割当リストを返す。要素数は最大 policy.Batch.MaxNewPerRun。
//
// アルゴリズム:
//  1. 目標カウント T[type][diff] = (現状総数 + MaxNewPerRun) * byType[type] * byDifficulty[diff]
//  2. 不足 deficit = max(0, T - 現状カウント)
//  3. deficit を重みに、MaxNewPerRun 個のスロットを「残り不足が最大のセルから 1 つずつ」割り当てる
//     （largest-remainder 方式。決定的でバランスが良い）
//  4. deficit が全て 0 なら、目標比率そのものを重みに同じ手順で割り当てる
//  5. 各スロットに language / skill_node をラウンドロビンで付与する
func Plan(dist []DistRow, nodes []SkillNodeRef, p policy.Policy) []Assignment {
	byType := p.TargetDistribution.ByType
	byDiff := p.TargetDistribution.ByDifficulty

	current := map[cell]int{}
	total := 0
	for _, r := range dist {
		if !policy.KnownType(r.Type) || r.Difficulty < 1 || r.Difficulty > 5 {
			continue
		}
		current[cell{r.Type, r.Difficulty}] += r.Count
		total += r.Count
	}

	n := p.Batch.MaxNewPerRun
	assumed := float64(total + n)

	// セル一覧（ポリシーに現れる type × difficulty の組み合わせ）を決定的な順序で作る。
	types := sortedKeys(byType)
	diffs := sortedDiffKeys(byDiff)

	weights := map[cell]float64{}
	var deficitSum float64
	for _, t := range types {
		for _, d := range diffs {
			target := assumed * byType[t] * byDiff[itoa(d)]
			deficit := target - float64(current[cell{t, d}])
			if deficit < 0 {
				deficit = 0
			}
			weights[cell{t, d}] = deficit
			deficitSum += deficit
		}
	}
	// 不足がどこにも無い（バンクが飽和 or 空でない均衡）場合は目標比率で埋める。
	if deficitSum == 0 {
		for _, t := range types {
			for _, d := range diffs {
				weights[cell{t, d}] = byType[t] * byDiff[itoa(d)]
			}
		}
	}

	// largest-remainder: 残り重みが最大のセルから 1 スロットずつ取る。
	remaining := map[cell]float64{}
	for k, v := range weights {
		remaining[k] = v
	}
	order := make([]cell, 0, len(types)*len(diffs))
	for _, t := range types {
		for _, d := range diffs {
			order = append(order, cell{t, d})
		}
	}

	langs := p.Languages
	out := make([]Assignment, 0, n)
	for i := 0; i < n; i++ {
		best := -1.0
		var bestCell cell
		for _, c := range order { // order は固定なので同点は先勝ち＝決定的
			if remaining[c] > best {
				best = remaining[c]
				bestCell = c
			}
		}
		if best <= 0 {
			break
		}
		remaining[bestCell]--

		a := Assignment{
			Type:       bestCell.t,
			Difficulty: bestCell.d,
			Language:   langs[i%len(langs)],
		}
		if len(nodes) > 0 {
			a.SkillNode = nodes[i%len(nodes)]
		}
		out = append(out, a)
	}
	return out
}

type cell struct {
	t string
	d int
}

func sortedKeys(m map[string]float64) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedDiffKeys(m map[string]float64) []int {
	ks := make([]int, 0, len(m))
	for k := range m {
		ks = append(ks, atoi(k))
	}
	sort.Ints(ks)
	return ks
}

func itoa(i int) string { return string(rune('0' + i)) }

func atoi(s string) int {
	if len(s) == 1 && s[0] >= '0' && s[0] <= '9' {
		return int(s[0] - '0')
	}
	return 0
}
