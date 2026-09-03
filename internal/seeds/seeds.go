// Package seeds は「作問条件」のカタログを保持する。
//
// プロンプト生成時、balancer が決めた割当（種別 / 難易度 / 言語 / トピック）に加えて、
// ここからランダムに「絡める API・メソッド」「題材にする処理」「解説で触れる観点」を
// 1〜数件選び、プロンプトに添える。これにより温度と重複検出だけに頼らず、
// 出題の題材レベルで多様性を能動的に確保する（DESIGN.md §4 の重複排除を補う）。
//
// データは seeds/*.yaml に置き go:embed で焼き込む。<lang>.yaml が言語固有、
// common.yaml が言語非依存で全言語に合成される。
//
// 抽選は呼び出し側が渡す *rand.Rand で行うため、シードを固定すれば結果は決定的。
// generate はこのシードを実行レポートに記録し、replay で再現できるようにする。
package seeds

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

//go:embed seeds/*.yaml
var seedsFS embed.FS

// langSeeds は 1 ファイル分の作問条件プール。
type langSeeds struct {
	Language   string   `yaml:"language"` // common.yaml では空
	Methods    []string `yaml:"methods"`
	Patterns   []string `yaml:"patterns"`
	SpecTopics []string `yaml:"spec_topics"`
}

// Catalog は全言語の作問条件プール。Load で組み立てる。
type Catalog struct {
	byLang map[string]langSeeds
	common langSeeds
}

// Counts は 1 プロンプトあたり各カテゴリから何件抽選するか。
type Counts struct {
	Methods    int
	Patterns   int
	SpecTopics int
}

// Condition は 1 プロンプト分の抽選結果。
type Condition struct {
	Methods    []string
	Patterns   []string
	SpecTopics []string
}

// Empty は 1 件も条件が無いか。
func (c Condition) Empty() bool {
	return len(c.Methods) == 0 && len(c.Patterns) == 0 && len(c.SpecTopics) == 0
}

// PromptBlock はプロンプトへ添える日本語ブロックを返す。Empty なら空文字。
func (c Condition) PromptBlock() string {
	if c.Empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 作問の追加条件（不自然にならない範囲で取り入れる）\n")
	if len(c.Methods) > 0 {
		fmt.Fprintf(&b, "- 題材に絡める API / メソッド: %s\n", strings.Join(c.Methods, " / "))
	}
	if len(c.Patterns) > 0 {
		fmt.Fprintf(&b, "- 題材にする処理: %s\n", strings.Join(c.Patterns, " / "))
	}
	if len(c.SpecTopics) > 0 {
		fmt.Fprintf(&b, "- 解説で軽く触れる観点: %s\n", strings.Join(c.SpecTopics, " / "))
	}
	return b.String()
}

// Labels は付与した条件を "category:value" 形式のフラットなリストで返す（レポート用）。
func (c Condition) Labels() []string {
	out := make([]string, 0, len(c.Methods)+len(c.Patterns)+len(c.SpecTopics))
	for _, v := range c.Methods {
		out = append(out, "method:"+v)
	}
	for _, v := range c.Patterns {
		out = append(out, "pattern:"+v)
	}
	for _, v := range c.SpecTopics {
		out = append(out, "spec_topic:"+v)
	}
	return out
}

// Fingerprint は CacheKey に混ぜるための決定的な短い識別子を返す（Empty なら空文字）。
func (c Condition) Fingerprint() string {
	if c.Empty() {
		return ""
	}
	joined := strings.Join(c.Methods, ",") + "|" +
		strings.Join(c.Patterns, ",") + "|" +
		strings.Join(c.SpecTopics, ",")
	return shortHash(joined)
}

// Load は埋め込み YAML を読み Catalog を組み立てる。
func Load() (Catalog, error) {
	entries, err := seedsFS.ReadDir("seeds")
	if err != nil {
		return Catalog{}, fmt.Errorf("seeds: ディレクトリの読み込みに失敗: %w", err)
	}

	cat := Catalog{byLang: map[string]langSeeds{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := seedsFS.ReadFile("seeds/" + e.Name())
		if err != nil {
			return Catalog{}, fmt.Errorf("seeds: %s の読み込みに失敗: %w", e.Name(), err)
		}
		var s langSeeds
		if err := yaml.Unmarshal(b, &s); err != nil {
			return Catalog{}, fmt.Errorf("seeds: %s の YAML パースに失敗: %w", e.Name(), err)
		}
		if e.Name() == "common.yaml" {
			cat.common = s
			continue
		}
		if s.Language == "" {
			return Catalog{}, fmt.Errorf("seeds: %s に language がありません", e.Name())
		}
		cat.byLang[s.Language] = s
	}
	return cat, nil
}

// Pick は lang 向けに Counts 件ずつ抽選する。
// lang 固有プールが無い場合は common だけから選ぶ。rng が nil なら空 Condition。
func (c Catalog) Pick(lang string, n Counts, rng *rand.Rand) Condition {
	if rng == nil {
		return Condition{}
	}
	ls := c.byLang[lang]
	return Condition{
		Methods:    sample(ls.Methods, n.Methods, rng),
		Patterns:   sample(union(ls.Patterns, c.common.Patterns), n.Patterns, rng),
		SpecTopics: sample(union(ls.SpecTopics, c.common.SpecTopics), n.SpecTopics, rng),
	}
}

// union は 2 つのリストを重複なく連結し、決定的な順序（ソート）で返す。
func union(a, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, v := range append(append([]string{}, a...), b...) {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// sample は pool から重複なく k 件を rng で選ぶ（pool が k 未満なら全件）。
func sample(pool []string, k int, rng *rand.Rand) []string {
	if k <= 0 || len(pool) == 0 {
		return nil
	}
	if k > len(pool) {
		k = len(pool)
	}
	idx := rng.Perm(len(pool))[:k]
	sort.Ints(idx)
	out := make([]string, 0, k)
	for _, i := range idx {
		out = append(out, pool[i])
	}
	return out
}
