-- M6.1 built-in auth (opt-in via UI).
--
-- Auth is OFF when auth_users is empty — daemon serves open and relies on
-- whatever upstream protection the operator provides (TinyAuth/Authelia/
-- private network/nothing). When the operator enables it via Settings, a
-- single row is inserted here and the HTTP middleware starts requiring a
-- valid session cookie OR an X-API-Key on every /api/v1/* request.
--
-- Sessions are opaque random tokens (32 bytes). The DB stores only the
-- sha256 of the token so a DB leak doesn't grant impersonation; the token
-- itself lives only in the operator's browser cookie.

CREATE TABLE auth_users (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    username              TEXT NOT NULL UNIQUE,
    password_hash         TEXT NOT NULL,           -- bcrypt
    created_at            TIMESTAMP NOT NULL,
    password_changed_at   TIMESTAMP NOT NULL
);

CREATE TABLE auth_sessions (
    token_hash            TEXT PRIMARY KEY,        -- sha256 hex of the cookie token
    user_id               INTEGER NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    created_at            TIMESTAMP NOT NULL,
    expires_at            TIMESTAMP NOT NULL,
    last_seen_at          TIMESTAMP NOT NULL
);

CREATE INDEX auth_sessions_expires_idx ON auth_sessions(expires_at);
CREATE INDEX auth_sessions_user_idx ON auth_sessions(user_id);
