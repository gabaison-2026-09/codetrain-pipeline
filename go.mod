module github.com/gabaison-2026-09/codetrain-pipeline

go 1.27

require (
	github.com/gabaison-2026-09/codetrain-core v0.0.0
	github.com/jackc/pgx/v5 v5.7.6
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

// codetrain-core はまだ GitHub に push されていないため、隣のチェックアウトを指す。
// LOCAL_DEV.md §10.1 のとおり、core にタグを打って公開した時点でこの replace を外し、
// GOPRIVATE 経由のバージョン固定参照に切り替える。
replace github.com/gabaison-2026-09/codetrain-core => ../codetrain-core
