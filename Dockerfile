# codetrain-pipeline 本番イメージ。
#
# REPOSITORIES.md §2: 成果物はコンテナイメージ（ECR → EKS の Job / CronJob）。
# DESIGN.md §7: 生成 / 検証は Kubernetes CronJob + Job として起動する。
#
# codetrain-core は本番では Go module（semver タグ固定）として参照する想定。
# タグ公開までは go.mod の replace が効くため、ビルドコンテキストに core を含める
# 必要がある（CI では横並びチェックアウト or GOPRIVATE 認証に切り替える。
# REPOSITORIES.md §5 / OPEN_ISSUES D-13）。
FROM golang:1.27 AS build

WORKDIR /src

# 依存の解決（キャッシュを効かせるため先に）
COPY codetrain-pipeline/go.mod codetrain-pipeline/go.sum ./codetrain-pipeline/
COPY codetrain-core/ ./codetrain-core/
WORKDIR /src/codetrain-pipeline
RUN go mod download

COPY codetrain-pipeline/ ./
# dev_auth のようなビルドタグは付けない（pipeline に dev 専用パスは無い）。
RUN CGO_ENABLED=0 go build -trimpath -o /out/pipeline ./cmd/pipeline

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pipeline /pipeline
# プロンプトテンプレートとポリシーは埋め込み（go:embed）されるため同梱不要。
ENTRYPOINT ["/pipeline"]
CMD ["generate"]
