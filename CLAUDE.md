# CLAUDE.md

Triagearr — Go daemon, disk-pressure media reaper for Plex/*arr/qBit hardlink setups.

Pitch + niche: [`README.md`](README.md). Architecture: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Roadmap: [`docs/ROADMAP.md`](docs/ROADMAP.md). Sibling repo (deployment target): `~/Github/homelab`.

## Working here

- Read [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) and the relevant `docs/adr/` before changing core flows.
- New external dep → ADR required + entry in [`docs/STACK.md`](docs/STACK.md).
- New scoring factor → update [`docs/SCORING.md`](docs/SCORING.md) first, implement second.
- Touching `internal/actor/` → integration test required before merge.

## Stack pinned

Go 1.26+ · urfave/cli/v3 · koanf/v2 · modernc.org/sqlite · sqlx · net/http ServeMux (Go 1.22 routing) · log/slog · React 19 + Vite + shadcn/ui. Full pins in [`docs/STACK.md`](docs/STACK.md).

## Conventions

- stdlib first (ADR-0001) — every external dep is justified
- errors: `fmt.Errorf("doing X: %w", err)`, never bare returns
- logging: `slog` only, structured key/value pairs
- tests: table-driven, real subjects (`t.TempDir()`, `httptest`), light on mocks
- comments: WHY only, never WHAT; no PR/issue refs in code
- frontend (`web/`): use **bun**, not npm — `bun install`, `bun run lint`, `bun run build`, `bun run dev`
- i18n (paraglide/inlang): **no hardcoded user-facing strings** — every visible label/text goes through `m.*` and must be added to **all four** locale files (`web/messages/{en,fr,de,es}.json`) in the same change. Don't ship translatable copy as text in Go/API payloads (it bypasses i18n); keep such strings in the frontend message files.

## Non-negotiable safety rules

- Default `mode: dry-run`. Live deletion requires explicit opt-in.
- Per-*arr-instance `act: false` by default — acting requires explicit per-instance flag.
- Deletion order: *arr API first, qBit second (ADR-0003). Never invert.
- HnR window is a hard veto (`-10000` weight), non-configurable.

## Layout shortcuts

- `cmd/triagearr/main.go` — CLI entry
- `internal/store/` — SQLite + migrations
- `internal/clients/arr/{sonarr,radarr,lidarr,readarr,whisparr_v2,whisparr_v3}/` — one per *arr provider
- `internal/clients/torrent/{qbit,...}/` — one per torrent-client provider
- `internal/scorer/` — DeleteScore
- `internal/actor/` — destructive ops only
- `web/` — Vite + React UI, embedded via `embed.FS`
- `docs/adr/` — every accepted architectural decision (read before changing)

## Out of scope (don't propose)

- Download client management (`qbit_manage`'s job)
- Malware / stalled / blocked download cleanup (`Cleanuparr`'s job)
- Watch-history-driven library cleanup (`Maintainerr`'s job)

These projects cohabit with Triagearr. They don't compete.
