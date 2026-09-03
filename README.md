# codetrain-pipeline

CodeTrain の **LLM 問題生成パイプライン**（Go / CLI バッチ）。

設計の根拠は [Document/DESIGN.md](../Document/DESIGN.md) §4・§7 と
[Document/LOCAL_DEV.md](../Document/LOCAL_DEV.md) §6・§7。**先にそちらを読むこと。**

- ステータス: PoC 実装（LLM 直生成 + 自動検証 + レビュー反映ループ）
- 成果物: コンテナイメージ（ECR → EKS の Job / CronJob。REPOSITORIES.md §2）

## PoC 方針

DESIGN.md §4 は「GitHub 収集 → チャンク化 → LLM 生成 → 検証 → レビュー」だが、
PoC では **GitHub 収集をせず LLM に直接問題作成を依頼する**（OPEN_ISSUES C-10）。

```
cron（K8s CronJob）→ pipeline のサブコマンドを 1 回起動
  → policy/policy.yaml（目標分布）を読む
  → DB の現存 question の傾向を集計し、難易度・タイプ別の不足を重み付けで割当（balancer）
  → プロンプト成型（版付きテンプレ + JSON Schema、日本語指示）
  → LLM 依頼（llm.Client。既定は記録再生 replay。差し替え可能）
  → 返答の吟味（スキーマ / 選択肢 / 正解キー整合 / 日本語 / 近似重複）
  → 不合格なら理由を添えて再生成（GEN_MAX_RETRIES 回まで）
  → 合格を status=needs_review で登録 + review_queue に未レビュー行
admin（codetrain-admin → codetrain-api /v1/admin/*）でレビュー
  → decision=needs_edit は `pipeline regenerate` が拾って自動で作り直し、再レビューへ
```

**DB スキーマは変更しない。** マイグレーションを打てるのは codetrain-core だけ
（REPOSITORIES.md §2.1）。pipeline は既存の `question` / `review_queue` / `raw_source`
（ダミー行）を読み書きするだけ。

## サブコマンド

| コマンド | 用途 |
| --- | --- |
| `generate` | ポリシー駆動でバッチ生成し `status=needs_review` で登録 |
| `regenerate` | レビューで `needs_edit` になった問題を自動で作り直す |
| `questions --status <s>` | 問題の一覧表示（デバッグ） |
| `approve --id <uuid>` / `reject --id <uuid>` | admin 未起動時の CLI レビュー |
| `verify` | （未実装）サンドボックス実コード照合の受け皿 |
| `eval` | （未実装）プロンプト / モデル比較 |

`generate` / `regenerate` に `--allow-llm-calls` を付け `LLM_MODE=record|live` の
ときだけ実 API を呼ぶ（環境変数にキーがあるだけでは課金しない。LOCAL_DEV.md §7.1）。

## 手動 LLM モード（`LLM_MODE=manual`）

Anthropic の API リソースを用意する前に、**LLM 呼び出しだけを人手で埋めて**
「割当 → プロンプト成型 → JSON パース → 検証 → DB 登録 → review_queue」まで
E2E で確認するためのモード。実 API は一切叩かない。API キー不要。

```bash
# 1. プロンプトを書き出す（DB は無変更、割当ごとに保留として記録される）
LLM_MODE=manual POLICY_PATH=policy/policy.demo.yaml pipeline generate
#   → manual/<key>.prompt.md が割当の数だけ生成される

# 2. 各 manual/<key>.prompt.md の「--- SYSTEM ---」以降をブラウザの LLM に貼る

# 3. 返ってきた JSON を、同じ key で manual/<key>.response.txt に保存する

# 4. もう一度実行すると、返答ファイルがある割当だけ検証 → DB 登録まで進む
LLM_MODE=manual POLICY_PATH=policy/policy.demo.yaml pipeline generate

# 5. 登録を確認
pipeline questions --status needs_review
pipeline approve --id <uuid>
```

`MANUAL_DIR`（既定 `manual/`、`.gitignore` 対象）で出力先を変えられる。
key はカセットと同じ命名なので、良い返答が得られたら
`testdata/cassettes/<key>.json` に整形して置けば `replay` で再利用できる。
`regenerate` も同じ手順で動く。

### Makefile で動かす（ローカル）

Go はコンテナで動かすため、`Makefile` が `codetrain-devenv` の compose を呼ぶ。

```bash
# 前提（初回のみ）
cd ../codetrain-devenv && cp -n .env.example .env && docker compose up -d
cd ../codetrain-pipeline

make manual-generate   # 1) manual/<key>.prompt.md を書き出す（DB 無変更）
#  2) 各 prompt.md をブラウザの LLM に貼り、返答 JSON を manual/<key>.response.txt に保存
make manual-generate   # 3) 検証 → DB 登録
make questions          # 4) needs_review を確認

make manual-smoke      # 2) をカセット流用で自動化し 1)〜4) を一括実行（完全オフライン）
make manual-clean      # manual/ を掃除
```

ポリシーは `POLICY=policy/policy.demo-bad.yaml make manual-generate` のように差し替え可能。
`make help` で全ターゲットを表示。

## ローカルでの動かし方

`codetrain-devenv` が束ねる。横並びチェックアウト前提（`../codetrain-core` を参照）。

```bash
cd ~/codetrain/codetrain-devenv
docker compose up -d                     # Track B（postgres / db-init など）

# 既定は replay。testdata/cassettes/ から再生するので API キーもネットも不要。
docker compose -f compose.yaml -f compose.lab.yaml run --rm \
  -e POLICY_PATH=policy/policy.demo.yaml pipeline generate

docker compose -f compose.yaml -f compose.lab.yaml run --rm --no-deps pipeline go test ./...
```

## 構成

```
policy/policy.yaml        目標分布ポリシー（将来は admin UI から編集）
policy/policy.demo.yaml   E2E 確認用の小さいポリシー
internal/
  config/                 環境変数
  policy/                 ポリシーの読み込み・検証（内蔵デフォルトあり）
  balancer/               現状分布 + ポリシー → 生成割当（純関数・テスト可能）
  prompt/                 版付きテンプレート（templates/*.md を go:embed）
  schema/                 LLM 構造化出力の型 + JSON Schema + domain 変換
  llm/                    Client インタフェース / Anthropic 直 / 記録再生 / 手動 / factory
  validate/               返答の吟味（構造化した不合格理由）
  generate/               生成ループ
  regenerate/             needs_edit の自動再生成ループ
  reviewcli/              CLI レビュー
  repository/             pgx。question 分布集計 / INSERT・UPDATE / review_queue
  report/                 実行レポート（reports/、Git 管理外）
testdata/cassettes/       記録再生カセット（Git 管理。README 参照）
```

## 本番（TODO）

- `codetrain-deploy`（helmfile）に `generate` / `regenerate` の CronJob を追加する。
- codetrain-core のタグ公開後、`go.mod` の `replace` を外し GOPRIVATE 参照に切り替える。
- `.go-version` / `Dockerfile` の Go バージョンは同じ PR で上げる（REPOSITORIES.md §9.2）。
