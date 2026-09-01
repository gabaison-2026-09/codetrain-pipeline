# LLM 記録再生カセット

`LLM_MODE=replay`（既定）のとき、pipeline はここの JSON からのみ応答を得る。
ミスヒットすると**エラーで停止**する（黙って実 API に落ちない。LOCAL_DEV.md §7.1）。

## キー

ファイル名 = リクエストの `CacheKey` を英数字・`-`・`_` に正規化したもの。
`CacheKey` はプロンプトの文面ではなく「何を生成させたいか」で決まる（`internal/prompt`）:

- 新規生成: `gen-<promptVersion>-<type>-d<difficulty>-<language>-<skillNodeSlug>`
- 再生成:   `regen-<promptVersion>-<questionUUID>`

`.` は `_` に正規化される（例: `question_gen.v1` → `question_gen_v1`）。

文面を微修正してもカセットは無駄にミスヒットしない。プロンプト版（`v1` → `v2`）を
上げたときだけ作り直す。

## 追加・更新のしかた

実 API から記録する:

```bash
docker compose -f compose.yaml -f compose.lab.yaml run --rm \
  -e LLM_MODE=record -e ANTHROPIC_API_KEY=sk-... \
  pipeline generate --allow-llm-calls
```

`response.text` は問題 1 問の JSON 文字列（`internal/schema` の QuestionDraft 形）。
手で用意してもよい（このディレクトリの既存ファイルが例）。

## 同梱物

| ファイル | 用途 |
| --- | --- |
| `gen-question_gen_v1-code_reading-d2-javascript-values-and-types.json` | デモ（`policy/policy.demo.yaml`） |
| `gen-question_gen_v1-output_prediction-d2-javascript-values-and-types.json` | デモ |
| `gen-question_gen_v1-bug_finding-d3-javascript-values-and-types.json` | 自動検証の不合格→再試行の確認用（正解キーが choices に無い不正データ） |
