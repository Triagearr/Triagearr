# ADR-0001: Prefer the Go standard library over third-party packages

## Status

Accepted — 2026-05-17

## Context

Triagearr is a small daemon with a single maintainer at launch. Every external dependency adds:
- Supply-chain risk (one less actor I trust between malware and my users)
- Maintenance overhead (transitive updates, occasional breaking changes)
- Compile-time and binary size cost
- Onboarding cost for contributors (one more API to learn)

The Go standard library in 1.26 is *vast* and *good*: `log/slog`, `net/http` with `http.ServeMux` patterns, `context`, `errors`, `encoding/json`, `database/sql`, `embed`, `sync/atomic`, `slices`, `maps` — most of what a daemon like Triagearr needs, the stdlib already does.

## Decision

Default to stdlib. Reach for a third-party package only when:
1. The stdlib has no equivalent, OR
2. The stdlib equivalent is significantly worse (DX, performance, correctness) for our specific use case, AND
3. The package is well-maintained, has clear semver, and minimal transitive deps.

Every direct dependency added requires:
- A line item in `docs/STACK.md` with version pin and rationale
- A passing `govulncheck`
- A reviewed transitive dep tree (`go mod why <dep>`)

## Consequences

**Easier:**
- Cross-compilation (pure Go path stays open)
- `go install github.com/Triagearr/Triagearr@latest` works for end users
- Dependency audits stay manageable
- Binary stays small (target: < 30 MB compressed)

**Harder:**
- Some ergonomic luxuries (e.g. `lo` or `samber/mo`) are off-limits — we write the loop
- We sometimes write 20 lines of stdlib code where 1 line of a 3rd-party would do

**Traded away:**
- "Slight" productivity gains from omnibus libraries
- Easier copy-paste from blog posts that assume those libs are present

## Specifically out-of-scope third-party packages

(Useful but not justified for our scope.)

- `samber/lo`, `samber/mo` — slice/option utilities
- `gocql`, `mongo-go-driver`, any non-SQLite DB driver
- `cobra` (we chose `urfave/cli/v3` instead — see ADR-0007, but the principle stands: prefer the lighter alternative)
- `viper` (replaced by koanf which is itself a concession to "no good stdlib equivalent for layered config")
- `gorm`, `ent`, `bun` — too heavy for our schema; sqlx is enough
- `logrus`, `zap`, `zerolog` — slog is sufficient

## Specifically in-scope third-party packages

(Justified, see docs/STACK.md for rationale.)

- `urfave/cli/v3` — CLI framework
- `knadh/koanf/v2` — layered config (env + yaml)
- `modernc.org/sqlite` — embedded DB
- `jmoiron/sqlx` — SQL ergonomics
- `go-chi/chi/v5` — HTTP router
- `prometheus/client_golang` — metrics (V2)
- `stretchr/testify` — test assertions

## References

- Go standard library docs: https://pkg.go.dev/std
- `govulncheck`: https://go.dev/blog/govulncheck
