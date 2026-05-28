# Contributing to Triagearr

Thanks for considering a contribution. Triagearr is a small project with a focused scope; the bar for additions is "does it fit the niche, not just the codebase."

## Before you start

1. **Open an issue first** for any feature larger than ~50 LOC. We'll discuss scope and fit. Unsolicited large PRs are likely to be declined.
2. Read [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) and [`docs/ROADMAP.md`](docs/ROADMAP.md) — they cover what's in scope and what isn't.
3. Check the [non-goals section of the roadmap](docs/ROADMAP.md#what-will-not-be-done) before proposing a feature.

## Development setup

### Prerequisites

- Go 1.26+ (`go version`)
- Node.js 22 LTS + pnpm 9+ (for the React UI under `web/`)
- A running qBittorrent + Sonarr + Radarr (a docker-compose stack for local dev is provided in `scripts/dev-stack/`)
- `golangci-lint` (`brew install golangci-lint` or equivalent)
- `goreleaser` (optional, for local release rehearsal)

### Bootstrap

```bash
git clone https://github.com/Triagearr/Triagearr
cd triagearr
make dev-setup        # downloads deps, sets up pre-commit hooks
make test             # runs unit tests
make run              # runs against dev-stack config
```

### Project layout

See `docs/ARCHITECTURE.md` § Repo structure. Briefly:

- `cmd/triagearr/` — CLI entry point (small)
- `internal/` — all business logic (private to this module)
- `pkg/` — types exported for consumers of the module (kept small)
- `web/` — Vite + React UI
- `docs/` — Markdown documentation
- `docs/adr/` — Architectural Decision Records

## Conventions

### Go style

- Standard `gofmt` + `goimports` (enforced by `golangci-lint`)
- Errors: wrap with `fmt.Errorf("doing X: %w", err)` (the verb-then-noun pattern)
- Logging: `slog` only, always with structured key/value pairs
- Comments: explain WHY, not WHAT. The code already shows what.
- Tests: table-driven where applicable, real subjects (`testing.T`, `httptest.Server`) over heavy mocks
- No init() except for package-level registry registration

### Commit style

[Conventional Commits](https://www.conventionalcommits.org/):

```
feat(scorer): add swarm_health_bonus factor
fix(qbit): handle 401 on auth bypass scenarios
docs(scoring): clarify rare_content_threshold semantics
chore(deps): bump koanf to v2.3.5
```

Types we use: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `build`, `ci`.

### PR style

- One feature/fix per PR
- Reference the issue (`Closes #123`)
- Update `docs/` if the change touches user-facing behavior
- Update `CHANGELOG.md` under `## [Unreleased]`
- Add an ADR under `docs/adr/` if the change introduces a new dependency, alters a core flow, or reverses a previous decision

### Tests required

Any change touching:
- `internal/scorer` — needs unit tests for new factors
- `internal/actor` — needs an integration test (testcontainers) for the new path
- `internal/mapper` — needs a real-filesystem test using `os.TempDir`
- HTTP routes — needs an `httptest` round-trip test
- Config schema — needs a validation test

UI changes: visual review in PR comments (screenshots), no automated UI tests required at this stage.

## Architectural Decision Records (ADRs)

When you make a non-trivial design choice, write an ADR. Format:

```markdown
# ADR-NNNN: Short title

## Status
Proposed | Accepted | Superseded by ADR-XYZ

## Context
What is the issue we're seeing that motivates this decision?

## Decision
What did we decide?

## Consequences
What becomes easier? What becomes harder? What did we trade away?
```

Number sequentially. Don't edit accepted ADRs — write a new one that supersedes the old.

## Releasing

(For maintainers.)

### Stable release

```bash
git tag -s vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

GitHub Actions builds the multi-arch binary + container, signs everything
keyless with cosign (OIDC), generates a CycloneDX SBOM per archive, and
publishes SLSA build provenance attestations. Verification commands are in
the project README under "Verify a release".

### Release candidates

Use a `-rc.N` suffix to validate the pipeline or stage breaking changes
without affecting the stable channel:

```bash
git tag -s vX.Y.Z-rc.0 -m "vX.Y.Z-rc.0"
git push origin vX.Y.Z-rc.0
```

Goreleaser detects the pre-release suffix automatically (`prerelease: auto`)
and:

- marks the GitHub release as **Pre-release** (hidden from "Latest release");
- pushes the container as `ghcr.io/triagearr/triagearr:rc` instead of `:latest`,
  while the explicit `:vX.Y.Z-rc.0` tag stays addressable.

`:latest` always points to the last stable; `docker pull …:rc` is the opt-in
channel. Use rc tags whenever a release reshapes the release pipeline
itself (cosign, SBOM, attestation changes), to catch regressions on real
OIDC + GHCR before cutting a stable tag.

## Code of conduct

Be kind, be specific, be useful. Disagreements about technical decisions are welcome; ad hominem isn't.

## License

By contributing you agree your contributions are licensed under the MIT license, same as the project.
