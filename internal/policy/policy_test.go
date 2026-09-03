package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	// 存在しないパス → 内蔵デフォルトにフォールバックし、検証を通ること。
	p, err := Load(filepath.Join(t.TempDir(), "no-such.yaml"))
	if err != nil {
		t.Fatalf("デフォルトの読み込みに失敗: %v", err)
	}
	if p.Batch.MaxNewPerRun < 1 {
		t.Fatalf("max_new_per_run が不正: %d", p.Batch.MaxNewPerRun)
	}
	if len(p.Languages) == 0 {
		t.Fatal("languages が空")
	}
}

func TestLoadRejectsBadSum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	bad := `version: 1
batch:
  max_new_per_run: 5
target_distribution:
  by_type:
    code_reading: 0.5
    output_prediction: 0.2
  by_difficulty:
    "1": 0.5
    "2": 0.5
languages: ["javascript"]
`
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("by_type の合計が 0.7 なのにエラーにならなかった")
	}
}

func TestDiversityDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) (Policy, error) {
		path := filepath.Join(dir, "p.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load(path)
	}
	base := `version: 1
batch: { max_new_per_run: 5 }
target_distribution:
  by_type: { code_reading: 1.0 }
  by_difficulty: { "1": 1.0 }
languages: ["javascript"]
`
	// diversity 無指定 → 無効。
	p, err := write(base)
	if err != nil {
		t.Fatal(err)
	}
	if p.Diversity.Enabled {
		t.Fatal("diversity 無指定なのに有効")
	}

	// enabled のみ指定 → per_prompt が 1/1/1 に補正される。
	p, err = write(base + "diversity: { enabled: true }\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Diversity.PerPrompt.Methods != 1 || p.Diversity.PerPrompt.Patterns != 1 || p.Diversity.PerPrompt.SpecTopics != 1 {
		t.Fatalf("per_prompt が補正されていない: %+v", p.Diversity.PerPrompt)
	}

	// 負値は拒否。
	if _, err := write(base + "diversity: { enabled: true, per_prompt: { methods: -1 } }\n"); err == nil {
		t.Fatal("負の per_prompt を受理した")
	}
}

func TestLoadRejectsUnknownType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	bad := `version: 1
batch:
  max_new_per_run: 5
target_distribution:
  by_type:
    made_up_type: 1.0
  by_difficulty:
    "1": 1.0
languages: ["javascript"]
`
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("未知タイプなのにエラーにならなかった")
	}
}
