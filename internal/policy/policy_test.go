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
