#!/bin/sh
# ローカル開発イメージ（Dockerfile.dev）のエントリポイント。
#
# 目的: 2 つの使い方を両立させる。
#   docker compose ... run --rm pipeline generate          -> go run ./cmd/pipeline generate
#   docker compose ... run --rm --no-deps pipeline go test ./...  -> go test ./...
#
# 第 1 引数が pipeline のサブコマンド（または空）なら CLI として起動し、
# それ以外（go / sh / gofmt など）はそのまま実行する。
set -e

case "$1" in
	generate|regenerate|questions|approve|reject|verify|eval|help|-h|--help|"")
		exec go run ./cmd/pipeline "$@"
		;;
	*)
		exec "$@"
		;;
esac
