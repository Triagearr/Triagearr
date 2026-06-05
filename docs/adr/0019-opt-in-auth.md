# ADR-0019 — Opt-in built-in authentication

**Status:** Accepted (2026-05-21)
**Supersedes:** the `http.auth: none|apikey` config field introduced for v0.7.0 (rolled back before any tag carried the breaking matrix).
**Related:** [ADR-0018](0018-m6-frontend-stack.md) (UI surface), the v0.7.0 deployment that surfaced the regression.

## Context

v0.7.0 shipped a Sonarr-style auth toggle on the daemon (`http.auth: none` on loopback, `auth: apikey` for non-loopback). Two problems surfaced during the first homelab deploy:

1. **Detection bug.** `isLoopbackBind(":9494")` returned `true` because `net.SplitHostPort` yields `host=""`. The daemon ran with `auth: none` on all interfaces. With TinyAuth set to bypass `/api/*` for programmatic clients, the new M6 endpoints (`/api/v1/torrents`, `/scores`, `/runs`, …) were silently world-readable.
2. **UX friction.** Once the bug was patched (`auth: apikey` forced), every operator session inside the dashboard required pasting the X-API-Key into `ApiKeyGate`, even after TinyAuth had already authenticated them. Single-user dashboards (Sonarr, qBittorrent, Maintainerr) don't ask for a key in addition to their session — Triagearr shouldn't either.

Both issues come from the same root cause: tying the auth model to a static config flag that operators rarely revisit.

## Decision

Authentication is **opt-in, managed from the dashboard, persisted in the SQLite store** — not a config field.

### Daemon

- New tables (`auth_users`, `auth_sessions`) carry a single operator account when enabled. `auth_users` empty ⇒ open mode.
- Middleware behaviour:
  - 0 users in DB → pass-through (operator relies on upstream protection: TinyAuth, Authelia, private LAN, nothing).
  - ≥ 1 user → require either a valid session cookie OR a matching `X-API-Key` header. `X-API-Key` is independent of the cookie path and always available for programmatic clients.
- Session cookie: opaque 32-byte random token, `HttpOnly`, `SameSite=Lax`, `Secure` when the original request is HTTPS (TLS or `X-Forwarded-Proto: https`). Server stores `sha256(token)` only; a DB read doesn't leak live tokens.
- Sliding TTL: 7 days, refreshed on every authenticated hit. Periodic sweep on startup.
- HTTP surface:
  - `GET /api/v1/session` (unauthenticated) → `{auth_enabled, authenticated, username?}`
  - `POST /api/v1/session` `{username, password}` → cookie
  - `DELETE /api/v1/session` → clears cookie + deletes the row
  - `POST /api/v1/auth/enable` `{username, password?}` → registers the user (auto-generates a password if omitted, returns it once) and immediately issues a session
  - `POST /api/v1/auth/disable` `{password}` — requires a UI session, not just X-API-Key
  - `POST /api/v1/auth/password` `{current, new?}` — rotates the password (auto-generated when `new` is omitted)
- Per-IP rate limit on auth-mutating endpoints (5/min); the existing 1/min limit on `POST /api/v1/runs` is unchanged.

### UI

- `LoginGate` replaces `ApiKeyGate`. Shows the login form only when `auth_enabled && !authenticated`. The form posts to `POST /api/v1/session` with `credentials: 'include'`; the cookie does the rest.
- Settings → **Security** card:
  - When disabled: "Enable authentication" form (username + optional password). Auto-generated password is rendered once with a Copy button.
  - When enabled: "Change password" (auto-generate or supply own) and "Disable authentication" (requires current password, must run from a cookie session — not from a key-only client).
  - "Signed in as …" + "Sign out" affordance.
- `apiFetch` always sends `credentials: 'include'`. The localStorage-based API key (`triagearr.apiKey`) is removed.

### Config

- `http.auth` and `Config.HTTPConfig.Auth` are removed entirely. `isLoopbackBind` and `/api/v1/auth-mode` go with them.
- Configs that still carry `http.auth: …` log a one-time warning at load and otherwise behave normally — koanf ignores the unknown key.
- `http.api_key` keeps its meaning: when set, it's the `X-API-Key` accepted by the middleware (independent of cookie auth). When the operator never sets one, programmatic access requires a cookie.

## Trade-offs / consequences

- **Bigger surface, smaller blast radius.** Six new endpoints + two tables + bcrypt dep, but the daemon stops mixing "deployment topology" (loopback / non-loopback) with "is auth on" — those are now orthogonal, which they always should have been.
- **Open by default.** A fresh install exposes everything until the operator clicks "Enable authentication" — same posture as qBittorrent, *arr, Maintainerr. The operator owns that choice; the daemon documents it in `README.md` and via the dashboard banner.
- **No more "you forgot to set api_key and now everything is unprotected" footgun.** The dashboard banner makes "auth disabled" visible. Programmatic clients still get `X-API-Key`, never lose the ability to operate.
- **bcrypt** lives in `golang.org/x/crypto/bcrypt` (already a transitive dep of much of the Go ecosystem; we now require it directly). Cost 10 — ~70ms per hash on the homelab CPU, fast enough for a single-user dashboard, slow enough to make offline brute-force impractical against the leaked hash.
- **Single user, by design.** Multi-user / RBAC is out of scope. If we ever need it, we'd grow `auth_users` into a real table; the migration is straightforward.
- **Lockout recovery is out-of-band.** Both in-app password operations require the current password, so a lost password needs host access to recover: `triagearr auth set-password` (rotate, keep auth on) or `triagearr auth disable` (drop to open mode). See [docs/CONFIGURATION.md](../CONFIGURATION.md#lockout-recovery).

## Alternatives considered

1. **HTML injection of the API key** into `index.html` (so the SPA reads `window.__TRIAGEARR_API_KEY__`). Simpler — ~30 lines — but the key lives in the DOM, every browser extension can read it, and there's no way to revoke a leaked key short of regenerating it server-side and re-deploying.
2. **Forward-auth header trust** (Triagearr accepts requests carrying a `Remote-User` header set by a trusted upstream). Works only when the operator already has TinyAuth/Authelia + a path matcher that lets `/api/*` go through forward-auth too. Forces a deployment topology on the operator.
3. **Built-in user from config** (`http.admin_password` env var, bootstrap on first run). Adds an env var per deployment, doesn't solve the "operator never sets it and now everything is open" footgun.

The session-cookie + opt-in path keeps the daemon usable on a fresh `docker run` (no creds to provision), gives the operator the canonical "Sonarr-style" UX once they enable it, and stays compatible with TinyAuth/Authelia in front as defense-in-depth.
