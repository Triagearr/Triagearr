# Technology Stack

This document captures every dependency Triagearr commits to, the version pin, and the reason it was chosen over its alternatives. Versions are valid as of **2026-05-17**.

## Runtime

| | Choice | Version | Why |
|---|---|---|---|
| Language | Go | **1.26.3** | Mature stdlib, `log/slog` mature, easy cross-compile, single static binary |

## CLI framework

| | Choice | Version | Alternatives considered |
|---|---|---|---|
| CLI | `github.com/urfave/cli/v3` | **v3.9.0** | `spf13/cobra` v1.10.2 |

**Why urfave/cli v3 over Cobra:** Cobra is the de-facto standard for tools with deep subcommand hierarchies (kubectl, gh, helm). Triagearr has a handful of top-level commands (`serve`, `inspect`, `score`, `migrate`, `version`). urfave/cli v3 has a more modern functional-options API, a much higher release cadence (monthly vs annual for Cobra), and a smaller dep footprint. See [ADR-0007](adr/0007-urfave-cli-v3.md).

## Configuration

| | Choice | Version | Alternatives considered |
|---|---|---|---|
| Layered config | `github.com/knadh/koanf/v2` | **v2.3.4** | `spf13/viper` v1.21.0 |
| YAML parser | `gopkg.in/yaml.v3` | latest | `goccy/go-yaml` |

**Why koanf:** lighter than Viper, fewer transitive deps, modular by design (only the providers you need: yaml, env, file). Native env-var overlay with prefix support.

## Storage

| | Choice | Version | Alternatives considered |
|---|---|---|---|
| Embedded SQL DB | `modernc.org/sqlite` | **v1.50.1** (SQLite 3.53.1) | `ncruces/go-sqlite3` v0.34.1, `mattn/go-sqlite3` (dormant since 2022), DuckDB, BadgerDB, bbolt |
| SQL ergonomics | `github.com/jmoiron/sqlx` | latest | std `database/sql` only |
| Migrations | `embedded /store/migrations/*.sql` + runner | n/a | `goose`, `migrate` |

**Why modernc/sqlite:** see [ADR-0002](adr/0002-sqlite-for-storage.md). Pure Go (no CGO), allowing `go install` to work universally and trivial cross-compile for QNAP. ncruces is a credible WASM-based alternative kept as fallback.

**Why SQLite over KV stores or DuckDB:** our workload is hybrid (time-series + relational + audit). KV stores would force us to reimplement SQL on top. DuckDB excels at analytics but requires CGO and its tooling is less universally available on user hosts. Our scale (50-700 MB/year) sits 3 orders of magnitude below SQLite's pain threshold.

## Logging

| | Choice | Version | Alternatives considered |
|---|---|---|---|
| Structured logging | `log/slog` (stdlib) | Go 1.26 native | `rs/zerolog`, `uber-go/zap`, `sirupsen/logrus` (deprecated) |

**Why slog:** part of the stdlib since 1.21, mature, structured (JSON + text handlers), no external dep. Sufficient for our throughput. Future-proof.

## HTTP

| | Choice | Version | Alternatives considered |
|---|---|---|---|
| Router | `github.com/go-chi/chi/v5` | **v5.2.5** | std `net/http` + `http.ServeMux` (Go 1.22+), `labstack/echo` v5.1.1, `gin-gonic/gin` v1.12.0 |
| HTTP client | `net/http` (stdlib) | n/a | resty, hashicorp/go-cleanhttp |

**Why chi:** light, idiomatic, sub-routers, middleware composition close to net/http. Echo/Gin add their own context type which makes integration with `slog` and `context.Context` slightly awkward.

## Web UI

| | Choice | Version | Alternatives considered |
|---|---|---|---|
| Bundler | Vite | **8.0.x** | webpack, parcel |
| Runtime / pm | Bun | **1.3.x** | npm, pnpm, yarn |
| Framework | React | **19.2.6** | Svelte 5, Preact, Solid.js, HTMX + Templ |
| Language | TypeScript | **6.0.x** | plain JavaScript |
| Component library | shadcn/ui primitives (handcrafted equiv.) | **4.7.0** | Radix UI, Headless UI, Mantine |
| Icons | lucide-react | **1.16.x** | Heroicons, Phosphor |
| Routing | TanStack Router | **1.170.x** | React Router |
| Data fetching | TanStack Query | **5.100.x** | SWR, plain fetch |
| Styling | Tailwind CSS | **4.3.x** (Vite-native plugin) | vanilla CSS, CSS modules |
| Charts | Recharts | **3.8.x** | Tremor, visx, uPlot |
| Schema validation | zod | **4.4.x** | yup, valibot |
| Embedding | `embed.FS` (stdlib) | n/a | http.FileSystem from disk |

**Why React 19 + shadcn:** ecosystem alignment (Sonarr, Radarr, Maintainerr, Bazarr are all React-based), the highest visual ceiling (shadcn produces "this looks professional" UIs with minimal effort), and the largest contributor pool. See [ADR-0008](adr/0008-react-shadcn-ui.md) and [ADR-0017](adr/0017-frontend-stack.md) for the M6-era tightening.

**Why bundled inside the Go binary:** zero second deployment, no CORS, single image to ship. The Vite build outputs to `web/dist/`, which `embed.FS` slurps into the binary.

## Scheduling

| | Choice | Version | Alternatives considered |
|---|---|---|---|
| Cron scheduler | `github.com/robfig/cron/v3` | **v3.0.1** | hand-rolled 5-field parser, `time.Ticker` 24 h |

**Why robfig/cron/v3:** the downsampler (M2) and any future cron-driven jobs (M4 disk-pressure scan candidate) need time-of-day predictability. A 24 h `Ticker` drifts on every restart; a hand-rolled parser is ~200 LOC for a solved problem. robfig/cron/v3 is the de-facto Go cron lib (Caddy, Loki, k6). API is trivial, ship surface is small. See [ADR-0011](adr/0011-cron-library.md).

## Testing

| | Choice | Version |
|---|---|---|
| Assertions | `stretchr/testify` | latest |
| Integration (optional) | `testcontainers-go` | latest |
| HTTP mocking | `httptest` (stdlib) | n/a |

## Observability (V2)

| | Choice | Version |
|---|---|---|
| Metrics | `prometheus/client_golang` | latest |
| Tracing (optional) | OpenTelemetry SDK | latest |

## Build & release

| | Choice | Version |
|---|---|---|
| Cross-compile + release | `goreleaser` | v2.x |
| Container build | Docker buildx (multi-arch) | n/a |
| Container registry | GHCR (`ghcr.io/triagearr/triagearr`) | n/a |
| CI | GitHub Actions | n/a |

## Versioning policy

- Triagearr follows **SemVer 2.0**.
- `v0.x.y` while in alpha (M0 → M5). Breaking changes allowed without major bump but documented in CHANGELOG.
- `v1.0.0` requires: full M8 completion, ≥70% test coverage on `scorer`/`linker`/`decider`/`actor`, two real-world users in production for ≥4 weeks.

## Dependency hygiene

- Every direct dependency must be justified in this document.
- Indirect deps reviewed quarterly with `go mod why` + `govulncheck`.
- Any new direct dep requires an ADR.
- Goal: < 30 direct deps total in `go.mod` (excluding test).
