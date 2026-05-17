# ADR-0008: Use React 19 + Vite + shadcn/ui for the web UI

## Status

Accepted — 2026-05-17

## Context

Triagearr ships an HTTP API and a web UI. Choices for the UI fall into three categories:

1. **Server-side rendered, Go-native**: HTMX 4 + Templ + Alpine.js + Tailwind. No JS toolchain, type-safe templates, single Go binary.
2. **Modern SPA**: React 19 / Svelte 5 / Solid / Preact + a component lib + bundler. JS toolchain, build step, richer interactivity.
3. **Bleeding edge**: Datastar (SSE-driven hypermedia). Promising but young.

Project context to inform the choice:
- Single maintainer, time is constrained
- Ambition: be a credible OSS project in the *arr ecosystem, not just a personal tool
- Ecosystem conventions: every major *arr (Sonarr, Radarr, Bazarr, Maintainerr, Overseerr) ships a React SPA. Cleanuparr is the only outlier (Angular).
- The UI is read-mostly (lists, charts, score breakdowns) with a few interactive moments (manual run trigger, override creation)

## Considered alternatives

### HTMX + Templ + Alpine
- Pro: pure Go, no Node toolchain, smaller repo, type-safe via Templ
- Pro: aligns with "boring tech" ethos
- Con: visual ceiling lower than shadcn — looks fine but not "wow"
- Con: ecosystem expects React; contributors familiar with React must learn HTMX patterns
- Con: charts (score history, pressure timelines) are harder without React's ecosystem of viz libs

### Svelte 5
- Pro: smaller bundles than React, modern runes API
- Pro: technically more elegant
- Con: smaller component ecosystem (shadcn-svelte is good but trails shadcn-react)
- Con: diverges from *arr convention — contributors are less likely to know it
- Con: less mature TanStack Query / Router story

### Datastar
- Pro: novel SSE-based reactivity, no client state to sync
- Con: too young for a project that wants OSS adoption — risk of becoming an unmaintained dep
- Con: not suitable for a 1-person project that needs ecosystem support

## Decision

Use **React 19** with **Vite 7** as the bundler, **shadcn/ui** for components, **Tailwind CSS v4** for styling, **TanStack Query 5** for data fetching, **TanStack Router 1** for routing.

The build output (`web/dist/`) is embedded into the Go binary via `embed.FS` and served by chi handlers. No separate frontend deployment.

Concrete pinned versions (M0):
- react@19.2.6
- vite@7.x (latest at scaffold time)
- shadcn@4.7.0
- tailwindcss@4.x
- @tanstack/react-query@5.x
- @tanstack/react-router@1.x

## Consequences

**Easier:**
- Visual quality starts high (shadcn components are excellent out of the box)
- Familiar stack for the broadest pool of contributors
- Charts (Recharts, visx) and tables (TanStack Table) have rich ecosystems
- Pattern-matching with sibling *arr projects increases trust for adopters

**Harder:**
- Repo now has a Node toolchain (pnpm, vite, eslint, etc.)
- Build pipeline has two stages (UI build → Go embed → Go build)
- Binary size grows by ~1-2 MB (acceptable for what we get)
- Maintaining two languages (Go + TS) doubles the surface of "what version is supported"

**Traded away:**
- The "pure Go single binary, no Node anywhere" appeal
- The smaller mental model of "HTMX returns HTML fragments and that's the whole story"

## Implementation notes

- The Vite project lives in `web/`. `npm`-style scripts via `make ui` invoke pnpm internally.
- `web/src/` follows shadcn conventions (`components/ui/`, `lib/`, `routes/`).
- The Go server reads `web/dist/index.html` (and assets) via `embed.FS` defined in `internal/httpserver/ui.go`. The build step `make build` runs `make ui` first.
- For local dev: the Vite dev server runs on `:5173` with a proxy to the Go server on `:9494/api/*`. Hot reload works.
- The `:edge` Docker tag includes a freshly-built UI; tagged releases lock the UI bundle to the binary's commit.

## Re-evaluation triggers

- If React 19's runtime size or SSR story degrades, consider Preact migration (drop-in for most use cases).
- If shadcn becomes unmaintained or pivots, reconsider component lib (Mantine, Radix UI direct).
- If a Triagearr V2 ever needs to be installable without Node (e.g., for air-gapped users compiling from source), revisit Templ alternative.

## References

- React 19: https://react.dev
- Vite 7: https://vite.dev
- shadcn/ui: https://ui.shadcn.com
- TanStack: https://tanstack.com
- Maintainerr's UI migration to Vite (PR #2053) — precedent
