package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// manualClient は実 API を一切叩かない「人手で埋める」モード（config.LLMModeManual）。
//
//   - 返答ファイル <dir>/<key>.response.txt があれば、その中身を LLM 出力として返す。
//   - なければ、プロンプト全文を <dir>/<key>.prompt.md へ書き出し、errManualPending を返す。
//     呼び出し側（generate / regenerate）はこれを「却下」ではなく「保留」として扱う。
//
// key は cassetteClient と同じ cassetteKey（req.CacheKey ベース）。カセットと同じ
// 命名なので、良い返答が得られたら手で testdata/cassettes/<key>.json へ移せば
// replay モードでそのまま再生できる。
type manualClient struct {
	dir string
}

func (c *manualClient) Mode() string { return "manual" }

// errManualPending はプロンプト待ち（返答ファイル未作成）を表す。
type errManualPending struct {
	key        string
	promptPath string
	respPath   string
}

func (e errManualPending) Error() string {
	return fmt.Sprintf("manual: プロンプトを書き出しました (%s)。"+
		"内容をブラウザの LLM に貼り、返答 JSON を %s に保存して再実行してください",
		e.promptPath, e.respPath)
}

// IsManualPending は err が manual モードの「プロンプト待ち」かを判定する。
func IsManualPending(err error) bool {
	var e errManualPending
	return errors.As(err, &e)
}

func (c *manualClient) Generate(ctx context.Context, req Request) (Response, error) {
	key := cassetteKey(req)
	respPath := filepath.Join(c.dir, key+".response.txt")

	if b, err := os.ReadFile(respPath); err == nil {
		text := strings.TrimSpace(string(b))
		if text == "" {
			return Response{}, fmt.Errorf("manual: 返答ファイルが空です (%s)", respPath)
		}
		return Response{Text: text, ModelID: req.Model}, nil
	} else if !os.IsNotExist(err) {
		return Response{}, fmt.Errorf("manual: 返答ファイルの読み込みに失敗 (%s): %w", respPath, err)
	}

	promptPath := filepath.Join(c.dir, key+".prompt.md")
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return Response{}, fmt.Errorf("manual: 出力先の作成に失敗 (%s): %w", c.dir, err)
	}
	if err := os.WriteFile(promptPath, []byte(buildPromptFile(req, key)), 0o644); err != nil {
		return Response{}, fmt.Errorf("manual: プロンプトの書き出しに失敗 (%s): %w", promptPath, err)
	}
	return Response{}, errManualPending{key: key, promptPath: promptPath, respPath: respPath}
}

// buildPromptFile はブラウザの LLM にそのまま貼れる形にプロンプトを整形する。
func buildPromptFile(req Request, key string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!--\n")
	fmt.Fprintf(&b, "key            : %s\n", key)
	fmt.Fprintf(&b, "prompt_version : %s\n", req.PromptVersion)
	fmt.Fprintf(&b, "model          : %s\n", req.Model)
	fmt.Fprintf(&b, "temperature    : %g\n", req.Temperature)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "手順:\n")
	fmt.Fprintf(&b, "  1. この --- 以下（System と User）をブラウザの LLM にそのまま貼る\n")
	fmt.Fprintf(&b, "  2. 返ってきた JSON を manual/%s.response.txt に保存する\n", key)
	fmt.Fprintf(&b, "  3. LLM_MODE=manual で再実行する\n")
	fmt.Fprintf(&b, "-->\n\n")
	b.WriteString("--- SYSTEM ---\n\n")
	b.WriteString(req.System)
	b.WriteString("\n\n--- USER ---\n\n")
	b.WriteString(req.User)
	b.WriteString("\n")
	return b.String()
}
