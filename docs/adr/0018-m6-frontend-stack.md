# ADR-0018 — M6 frontend stack

**Status:** Accepted (2026-05-21)
**Supersedes:** none
**Related:** [ADR-0008](0008-react-shadcn-ui.md) (original UI direction)

## Context

ADR-0008 set the broad UI direction (React + shadcn) without pinning every dependency. M6 is the milestone that actually scaffolds the dashboard, so we need to lock the surrounding choices before writing code:

- Which router (the React ecosystem has shipped/dropped/forked several this cycle).
- Which charting library (we need ratio/seeders/pressure over time, not a full BI suite).
- How Tailwind v4 is wired (PostCSS or native Vite plugin).
- Which JS runtime/package manager (Bun is aqua-managed in this repo, npm is not).

Decisions taken in adjacent ADRs were the API contract (ADR-0012 stays), the actor pipeline (ADR-0016), and the auth-bind interplay (this ADR pins it for the dashboard surface).

## Decision

| Concern | Choice | Reason |
|---|---|---|
| Runtime / package manager | **Bun 1.3.x** | already aqua-pinned; faster `install` + `vitest` than npm; single binary; lockfile is stable |
| Bundler | **Vite 8** | the only mainstream bundler keeping pace with React 19 / Tailwind 4; SSG-free, ESM-first |
| Framework | **React 19.2** | aligned with the *arr ecosystem; concurrent features actually matter for live-refresh tables |
| Language | **TypeScript 6** | end-to-end types from zod schemas at the API boundary down to the UI |
| Router | **TanStack Router 1.x** | file-based routes + first-class TypeScript inference; in-memory drawer/sheet routes work without URL ceremony |
| Data layer | **TanStack Query 5** | sibling to the router; cache-key ergonomics, polling intervals, mutation invalidation |
| Styling | **Tailwind v4** with `@tailwindcss/vite` (native plugin, no PostCSS) | matches Tailwind's own v4 guidance; one fewer build dep |
| Components | **shadcn/ui patterns**, hand-rolled in `web/src/components/ui/` | avoids the shadcn CLI's per-component install ceremony for a small surface; we copied the relevant primitives (Button, Card, Badge, Input, Modal, Drawer, Tabs, Table) and keep them on the CVA pattern so future shadcn updates stay drop-in |
| Icons | **lucide-react** | the icon set shadcn ships with; tree-shakeable |
| Charts | **Recharts 3** | declarative, SSR-friendly, < 100 kB gzipped; Tremor was attractive but heavier and less customisable |
| Schema validation | **zod 4** | parse-don't-validate at the network boundary; keeps `unknown` responses from leaking into components |
| Test runner | **Vitest 4 + @testing-library/react 16** | vitest reuses the Vite config; faster than jest |
| Embedding | **`embed.FS` (Go stdlib)** | per [STACK.md](../STACK.md); `web/web.go` exposes `Handler()` with SPA fallback |

## Alternatives considered and rejected

- **React Router (v7)**: still solid, but no type-safe params unless you opt into the new framework mode, which couples routing to a server we don't need. TanStack Router's inference wins for a small project.
- **Tremor / visx**: bigger deps, bigger learning curve for the 4 charts we draw.
- **Mantine / NextUI / radix-ui without shadcn**: bigger surface, less "shadcn-clone-and-edit" ergonomics, more visual decisions to make up-front.
- **shadcn CLI driven**: would have added a `components.json`, a registry config, and a `bunx shadcn add` step in the bootstrapping docs for an install dance we run once. We chose to copy the ~8 primitives we actually need by hand.
- **npm/pnpm/yarn**: would have required adding a separate package manager to aqua/CI. Bun is already in.
- **Tailwind v3**: v4 stabilised in 2025; sticking with v3 means missing the Vite-native plugin and the `@theme inline` ergonomics.

## Consequences

- The `web/` directory has its own `package.json`, lockfile (`bun.lock`), and tsconfig — it is a sibling of the Go module, not a child. `make build` runs `cd web && bun run build` before `go build`.
- `web/dist/` lives in git as a committed placeholder so `embed.FS` in `web/web.go` succeeds on a fresh clone. Real bundles overwrite that placeholder; `web/dist/assets/` is in `.gitignore`.
- Future shadcn updates should be incorporated by re-copying the upstream primitive — diffs stay manageable because our copies follow the CVA + Tailwind class pattern verbatim.
- Recharts is large-ish (~340 kB minified for the dynamic chunk). If we ever need to shrink the bundle, the candidate is to replace it with uPlot for the time-series views only — but we are not paying for that yet.

## Pinned versions at acceptance (2026-05-21)

See [`docs/STACK.md` § Web UI](../STACK.md#web-ui) for the canonical pin table; the versions in `web/package.json` are the source of truth for the lockfile.
