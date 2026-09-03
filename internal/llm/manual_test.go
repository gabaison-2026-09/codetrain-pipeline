package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manualReq() Request {
	return Request{
		Model:         "claude-haiku-4-5",
		PromptVersion: "question_gen.v1",
		System:        "SYSTEM-INSTRUCTIONS",
		User:          "USER-CONDITIONS",
		Temperature:   0.7,
		CacheKey:      "gen-question_gen.v1-code_reading-d2-javascript-none",
	}
}

func TestManualClient_PromptWrittenWhenNoResponse(t *testing.T) {
	dir := t.TempDir()
	c := &manualClient{dir: dir}
	req := manualReq()

	_, err := c.Generate(context.Background(), req)
	if err == nil {
		t.Fatal("返答ファイルが無いのでエラーになるはず")
	}
	if !IsManualPending(err) {
		t.Fatalf("errManualPending であるべき: %v", err)
	}

	key := cassetteKey(req)
	b, rerr := os.ReadFile(filepath.Join(dir, key+".prompt.md"))
	if rerr != nil {
		t.Fatalf("prompt.md が書き出されているべき: %v", rerr)
	}
	got := string(b)
	if !strings.Contains(got, req.System) || !strings.Contains(got, req.User) {
		t.Fatalf("プロンプトに System と User が含まれるべき:\n%s", got)
	}
}

func TestManualClient_ReadsResponseFile(t *testing.T) {
	dir := t.TempDir()
	c := &manualClient{dir: dir}
	req := manualReq()
	key := cassetteKey(req)

	want := `{"type":"code_reading"}`
	if err := os.WriteFile(filepath.Join(dir, key+".response.txt"), []byte("\n"+want+"\n  "), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := c.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("返答ファイルがあれば成功するべき: %v", err)
	}
	if resp.Text != want {
		t.Fatalf("Text は trim されたファイル内容であるべき: got %q", resp.Text)
	}
}

func TestManualClient_EmptyResponseFileIsError(t *testing.T) {
	dir := t.TempDir()
	c := &manualClient{dir: dir}
	req := manualReq()
	key := cassetteKey(req)

	if err := os.WriteFile(filepath.Join(dir, key+".response.txt"), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Generate(context.Background(), req); err == nil {
		t.Fatal("空の返答ファイルはエラーになるべき")
	} else if IsManualPending(err) {
		t.Fatalf("空ファイルは pending ではなく通常エラー: %v", err)
	}
}
