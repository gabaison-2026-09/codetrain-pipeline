// Package policy は生成ポリシー（目標分布）の読み込みと検証を行う。
//
// ポリシーは「問題バンクをどういうバランスに保ちたいか」の宣言で、
// balancer がこれと DB の現状分布を突き合わせて生成割当を決める。
package policy

import (
	_ "embed"
	"fmt"
	"math"
	"os"

	"github.com/gabaison-2026-09/codetrain-core/pkg/domain"
	"gopkg.in/yaml.v3"
)

//go:embed default.yaml
var defaultYAML []byte

// Policy は policy.yaml の内容。
type Policy struct {
	Version int `yaml:"version"`
	Batch   struct {
		MaxNewPerRun int `yaml:"max_new_per_run"`
	} `yaml:"batch"`
	TargetDistribution struct {
		ByType       map[string]float64 `yaml:"by_type"`
		ByDifficulty map[string]float64 `yaml:"by_difficulty"`
	} `yaml:"target_distribution"`
	Languages      []string `yaml:"languages"`
	SkillNodeSlugs []string `yaml:"skill_node_slugs"`
}

// Load は path のファイルを読む。path が存在しない場合は内蔵デフォルトを使う。
func Load(path string) (Policy, error) {
	raw := defaultYAML
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			raw = b
		case os.IsNotExist(err):
			// 内蔵デフォルトにフォールバック（本番イメージなど）
		default:
			return Policy{}, fmt.Errorf("ポリシーファイルの読み込みに失敗 (%s): %w", path, err)
		}
	}

	var p Policy
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return Policy{}, fmt.Errorf("ポリシーの YAML パースに失敗: %w", err)
	}
	if err := p.validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (p Policy) validate() error {
	if p.Version != 1 {
		return fmt.Errorf("policy.version は 1 のみ対応です: %d", p.Version)
	}
	if p.Batch.MaxNewPerRun < 1 {
		return fmt.Errorf("batch.max_new_per_run は 1 以上です: %d", p.Batch.MaxNewPerRun)
	}
	if len(p.Languages) == 0 {
		return fmt.Errorf("languages を 1 つ以上指定してください")
	}

	if err := checkTypes(p.TargetDistribution.ByType); err != nil {
		return err
	}
	if err := checkDifficulties(p.TargetDistribution.ByDifficulty); err != nil {
		return err
	}
	if err := checkSum("target_distribution.by_type", p.TargetDistribution.ByType); err != nil {
		return err
	}
	if err := checkSum("target_distribution.by_difficulty", p.TargetDistribution.ByDifficulty); err != nil {
		return err
	}
	return nil
}

// KnownType は生成対象として扱う問題タイプか。
func KnownType(t string) bool {
	switch domain.QuestionType(t) {
	case domain.QuestionTypeCodeReading, domain.QuestionTypeOutputPrediction,
		domain.QuestionTypeBugFinding, domain.QuestionTypeFillInBlank,
		domain.QuestionTypeBestPractice:
		return true
	}
	return false
}

func checkTypes(m map[string]float64) error {
	if len(m) == 0 {
		return fmt.Errorf("target_distribution.by_type が空です")
	}
	for k, v := range m {
		if !KnownType(k) {
			return fmt.Errorf("target_distribution.by_type に未知のタイプ: %q", k)
		}
		if v < 0 {
			return fmt.Errorf("target_distribution.by_type[%s] が負です: %v", k, v)
		}
	}
	return nil
}

func checkDifficulties(m map[string]float64) error {
	if len(m) == 0 {
		return fmt.Errorf("target_distribution.by_difficulty が空です")
	}
	for k, v := range m {
		if k != "1" && k != "2" && k != "3" && k != "4" && k != "5" {
			return fmt.Errorf("target_distribution.by_difficulty のキーは \"1\"〜\"5\" です: %q", k)
		}
		if v < 0 {
			return fmt.Errorf("target_distribution.by_difficulty[%s] が負です: %v", k, v)
		}
	}
	return nil
}

func checkSum(name string, m map[string]float64) error {
	var sum float64
	for _, v := range m {
		sum += v
	}
	if math.Abs(sum-1.0) > 0.01 {
		return fmt.Errorf("%s の合計が 1.0 ではありません: %.3f", name, sum)
	}
	return nil
}
