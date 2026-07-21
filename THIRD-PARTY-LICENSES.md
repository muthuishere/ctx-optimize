# Third-Party Licenses — ctx-optimize

`ctx-optimize` ships as a single static Go binary. That binary **contains** the
third-party code listed below, so this file reproduces the attribution those
licences require. It is generated from `go-licenses report ./...` plus the
grammar provenance in `scripts/wasm/grammars.lock`.

ctx-optimize itself is MIT (see `LICENSE`).

## Summary

| Licence | Components |
|---|---|
| BSD-3-Clause | 15 |
| MIT | 9 |
| Apache-2.0 | 9 |
| MPL-2.0 | 1 |
| BSD-2-Clause | 1 |

> **MPL-2.0 notice.** `github.com/go-sql-driver/mysql` is licensed under the
> Mozilla Public License 2.0. MPL-2.0 is a file-level copyleft: this project may
> be distributed under the MIT License as a whole, but the MPL-covered source
> remains under MPL-2.0 and its source must stay available to recipients. It is
> obtainable at the URL below and via `go mod download`. No modifications have
> been made to that dependency.

## Go module dependencies

| Module | Licence | Licence text |
|---|---|---|
| `github.com/golang-sql/civil` | Apache-2.0 | https://github.com/golang-sql/civil/blob/b832511892a9/LICENSE |
| `github.com/klauspost/compress` | Apache-2.0 | https://github.com/klauspost/compress/blob/v1.18.6/LICENSE |
| `github.com/nats-io/nats.go` | Apache-2.0 | https://github.com/nats-io/nats.go/blob/v1.52.0/LICENSE |
| `github.com/nats-io/nkeys` | Apache-2.0 | https://github.com/nats-io/nkeys/blob/v0.4.15/LICENSE |
| `github.com/nats-io/nuid` | Apache-2.0 | https://github.com/nats-io/nuid/blob/v1.0.1/LICENSE |
| `github.com/tetratelabs/wazero` | Apache-2.0 | https://github.com/tetratelabs/wazero/blob/v1.12.0/LICENSE |
| `github.com/xdg-go/scram` | Apache-2.0 | https://github.com/xdg-go/scram/blob/v1.2.0/LICENSE |
| `github.com/xdg-go/stringprep` | Apache-2.0 | https://github.com/xdg-go/stringprep/blob/v1.0.4/LICENSE |
| `go.mongodb.org/mongo-driver/v2` | Apache-2.0 | https://github.com/mongodb/mongo-go-driver/blob/v2.8.0/LICENSE |
| `github.com/redis/go-redis/v9` | BSD-2-Clause | https://github.com/redis/go-redis/blob/v9.21.0/LICENSE |
| `filippo.io/edwards25519` | BSD-3-Clause | https://github.com/FiloSottile/edwards25519/blob/v1.2.0/LICENSE |
| `github.com/golang-sql/sqlexp` | BSD-3-Clause | https://github.com/golang-sql/sqlexp/blob/v0.1.0/LICENSE |
| `github.com/google/uuid` | BSD-3-Clause | https://github.com/google/uuid/blob/v1.6.0/LICENSE |
| `github.com/klauspost/compress/internal/snapref` | BSD-3-Clause | https://github.com/klauspost/compress/blob/v1.18.6/internal/snapref/LICENSE |
| `github.com/klauspost/compress/s2` | BSD-3-Clause | https://github.com/klauspost/compress/blob/v1.18.6/s2/LICENSE |
| `github.com/klauspost/compress/snappy` | BSD-3-Clause | https://github.com/klauspost/compress/blob/v1.18.6/snappy/LICENSE |
| `github.com/microsoft/go-mssqldb` | BSD-3-Clause | https://github.com/microsoft/go-mssqldb/blob/v1.10.0/LICENSE.txt |
| `github.com/pierrec/lz4/v4` | BSD-3-Clause | https://github.com/pierrec/lz4/blob/v4.1.26/LICENSE |
| `github.com/twmb/franz-go/pkg` | BSD-3-Clause | https://github.com/twmb/franz-go/blob/v1.21.5/LICENSE |
| `github.com/twmb/franz-go/pkg/kmsg` | BSD-3-Clause | https://github.com/twmb/franz-go/blob/pkg/kmsg/v1.13.1/pkg/kmsg/LICENSE |
| `github.com/ulikunitz/xz` | BSD-3-Clause | https://github.com/ulikunitz/xz/blob/v0.5.15/LICENSE |
| `golang.org/x/crypto` | BSD-3-Clause | https://cs.opensource.google/go/x/crypto/+/v0.51.0:LICENSE |
| `golang.org/x/sync` | BSD-3-Clause | https://cs.opensource.google/go/x/sync/+/v0.20.0:LICENSE |
| `golang.org/x/sys` | BSD-3-Clause | https://cs.opensource.google/go/x/sys/+/v0.44.0:LICENSE |
| `golang.org/x/text` | BSD-3-Clause | https://cs.opensource.google/go/x/text/+/v0.37.0:LICENSE |
| `github.com/cespare/xxhash/v2` | MIT | https://github.com/cespare/xxhash/blob/v2.3.0/LICENSE.txt |
| `github.com/jackc/pgpassfile` | MIT | https://github.com/jackc/pgpassfile/blob/v1.0.0/LICENSE |
| `github.com/jackc/pgservicefile` | MIT | https://github.com/jackc/pgservicefile/blob/5a60cdf6a761/LICENSE |
| `github.com/jackc/pgx/v5` | MIT | https://github.com/jackc/pgx/blob/v5.10.0/LICENSE |
| `github.com/klauspost/compress/zstd/internal/xxhash` | MIT | https://github.com/klauspost/compress/blob/v1.18.6/zstd/internal/xxhash/LICENSE.txt |
| `github.com/microsoft/go-mssqldb/internal/github.com/swisscom/mssql-always-encrypted/pkg` | MIT | https://github.com/microsoft/go-mssqldb/blob/v1.10.0/internal/github.com/swisscom/mssql-always-encrypted/LICENSE.txt |
| `github.com/shopspring/decimal` | MIT | https://github.com/shopspring/decimal/blob/v1.4.0/LICENSE |
| `github.com/youmark/pkcs8` | MIT | https://github.com/youmark/pkcs8/blob/a2c0da244d78/LICENSE |
| `go.uber.org/atomic` | MIT | https://github.com/uber-go/atomic/blob/v1.11.0/LICENSE.txt |
| `github.com/go-sql-driver/mysql` | MPL-2.0 | https://github.com/go-sql-driver/mysql/blob/v1.10.0/LICENSE |

## Bundled tree-sitter runtime and grammars

`internal/extract/code/treesitter.wasm` is a WASI module compiled by
`scripts/wasm/build.sh` from the tree-sitter runtime and the grammars below, and
is **committed and embedded** via `go:embed` — it is redistributed with every
binary. Commit SHAs are the exact revisions built, recorded in
`scripts/wasm/grammars.lock`. All are MIT.

| Grammar | Upstream | Licence | Commit |
|---|---|---|---|
| tree-sitter | https://github.com/tree-sitter/tree-sitter | MIT | `ee0847d605e1` |
| tree-sitter-go | https://github.com/tree-sitter/tree-sitter-go | MIT | `2346a3ab1bb3` |
| tree-sitter-python | https://github.com/tree-sitter/tree-sitter-python | MIT | `26855eabccb1` |
| tree-sitter-javascript | https://github.com/tree-sitter/tree-sitter-javascript | MIT | `58404d8cf191` |
| tree-sitter-typescript | https://github.com/tree-sitter/tree-sitter-typescript | MIT | `75b3874edb2d` |
| tree-sitter-java | https://github.com/tree-sitter/tree-sitter-java | MIT | `e10607b45ff7` |
| tree-sitter-c | https://github.com/tree-sitter/tree-sitter-c | MIT | `b780e47fc780` |
| tree-sitter-cpp | https://github.com/tree-sitter/tree-sitter-cpp | MIT | `8b5b49eb196b` |
| tree-sitter-c-sharp | https://github.com/tree-sitter/tree-sitter-c-sharp | MIT | `af29416d729b` |
| tree-sitter-rust | https://github.com/tree-sitter/tree-sitter-rust | MIT | `77a3747266f4` |
| tree-sitter-zig | https://github.com/tree-sitter-grammars/tree-sitter-zig | MIT | `6479aa13f32f` |
| tree-sitter-sql | https://github.com/DerekStride/tree-sitter-sql | MIT | `c2e1e08db1ea` |

### Grammar packs (`grammars/`) — provenance to confirm

`grammars/kotlin.wasm`, `swift.wasm` and `dart.wasm` are redistributed with this
repository but their upstream source and revision are **not currently recorded**
in-tree. They must be traced to their upstream grammars and attributed here
before this file can be considered complete.

