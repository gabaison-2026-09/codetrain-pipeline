# codetrain-pipeline — ローカル実行コマンド集
#
# Go は必ずコンテナで動かす（LOCAL_DEV.md §1 / §6）。pipeline 単体に compose は無く、
# 隣の codetrain-devenv が束ねているので、その compose を -f で参照して run する。
#
# 前提（初回のみ）:
#   cd ../codetrain-devenv && cp -n .env.example .env && docker compose up -d
#     → Track B（postgres / マイグレーション / シード）が立ち上がる
#
# プロンプトの中身だけ見たいとき:
#   make dump-prompts        # reports/<ts>/prompts/*.md に System+User を書き出す（LLM 呼ばない）
#   make dump-prompts SEED=42 POLICY=policy/policy.yaml
#
# LLM_MODE=manual の使い方:
#   make manual-generate     # 1) manual/<key>.prompt.md を書き出す（DB は無変更）
#   #  2) 各 prompt.md の「--- SYSTEM ---」以降をブラウザの LLM に貼る
#   #  3) 返ってきた JSON を manual/<同じ key>.response.txt に保存する
#   make manual-generate     # 4) 返答を読んで検証 → DB 登録
#   make questions            #    登録を確認
#
#   make manual-smoke        # ブラウザ無し。カセットを流用して 1)〜4) を一括実行

SHELL := /bin/bash
.DEFAULT_GOAL := help

DEVENV     ?= ../codetrain-devenv
COMPOSE     = docker compose -f $(DEVENV)/compose.yaml -f $(DEVENV)/compose.lab.yaml
RUN         = $(COMPOSE) run --rm
POLICY     ?= policy/policy.demo.yaml
MANUAL_DIR ?= manual
SEED       ?= 0
MANUAL_ENV  = -e LLM_MODE=manual -e MANUAL_DIR=$(MANUAL_DIR)

.PHONY: help
help: ## このヘルプを表示
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@echo
	@echo "  前提: cd ../codetrain-devenv && cp -n .env.example .env && docker compose up -d"
	@echo "  変数: POLICY=$(POLICY)  MANUAL_DIR=$(MANUAL_DIR)  SEED=$(SEED)"

.PHONY: dump-prompts
dump-prompts: ## LLM へ投げる直前のプロンプト（中間生成物）を reports/<ts>/prompts/ に書き出す（generate --dry-run。LLM・DB書込なし）
	$(RUN) -e POLICY_PATH=$(POLICY) pipeline generate --dry-run --seed $(SEED)
	@echo "→ reports/<最新のタイムスタンプ>/prompts/*.md を確認してください"

.PHONY: dump-regen-prompts
dump-regen-prompts: ## needs_edit の再生成プロンプトを reports/<ts>/prompts/ に書き出す（regenerate --dry-run）
	$(RUN) -e POLICY_PATH=$(POLICY) pipeline regenerate --dry-run

.PHONY: manual-generate
manual-generate: ## LLM_MODE=manual で generate。1回目=プロンプト書き出し / 返答を置いて2回目=検証・DB登録
	@mkdir -p $(MANUAL_DIR)
	$(RUN) $(MANUAL_ENV) -e POLICY_PATH=$(POLICY) pipeline generate

.PHONY: manual-regenerate
manual-regenerate: ## LLM_MODE=manual で regenerate（needs_edit の問題を手動返答で作り直す）
	@mkdir -p $(MANUAL_DIR)
	$(RUN) $(MANUAL_ENV) pipeline regenerate

.PHONY: manual-seed
manual-seed: ## manual/*.prompt.md に対応するカセットの返答を .response.txt へ流用（ブラウザ無しの確認用）
	@shopt -s nullglob; \
	found=0; \
	for f in $(MANUAL_DIR)/*.prompt.md; do \
		found=1; \
		key=$$(basename "$$f" .prompt.md); \
		resp="$(MANUAL_DIR)/$$key.response.txt"; \
		cas="testdata/cassettes/$$key.json"; \
		if [ -f "$$resp" ]; then echo "skip  (既存): $$resp"; continue; fi; \
		if [ -f "$$cas" ]; then \
			jq -r '.response.text' "$$cas" > "$$resp" && echo "seed        : $$resp  <- $$cas"; \
		else \
			echo "手動対応が必要: $$cas が無い。$$f をブラウザに貼り $$resp に保存してください"; \
		fi; \
	done; \
	if [ $$found -eq 0 ]; then echo "$(MANUAL_DIR)/*.prompt.md がありません。先に make manual-generate を実行してください"; fi

.PHONY: manual-smoke
manual-smoke: ## manual-generate → manual-seed → manual-generate → questions を通しで実行（完全オフライン）
	@$(MAKE) manual-generate
	@$(MAKE) manual-seed
	@$(MAKE) manual-generate
	@$(MAKE) questions

.PHONY: questions
questions: ## needs_review の問題を一覧表示
	$(RUN) pipeline questions --status needs_review

.PHONY: manual-clean
manual-clean: ## manual/ を削除
	rm -rf $(MANUAL_DIR)

.PHONY: test
test: ## コンテナ内で go test ./...
	$(RUN) --no-deps pipeline go test ./...

.PHONY: vet
vet: ## コンテナ内で go vet ./...
	$(RUN) --no-deps pipeline go vet ./...
