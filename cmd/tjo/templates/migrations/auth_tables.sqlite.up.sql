-- SQLite schema for the authentication tables.
--
-- Differences from the MySQL and PostgreSQL versions are dialect, not intent:
-- INTEGER PRIMARY KEY AUTOINCREMENT rather than AUTO_INCREMENT or SERIAL, no
-- CASCADE on DROP (SQLite has no such clause), and foreign keys need
-- PRAGMA foreign_keys = ON to be enforced at all.

PRAGMA foreign_keys = ON;

DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    user_active INTEGER NOT NULL DEFAULT 0,
    email TEXT NOT NULL,
    password TEXT NOT NULL,
    totp_secret TEXT DEFAULT '',
    totp_enabled INTEGER NOT NULL DEFAULT 0,
    -- The last TOTP time step this account authenticated with. RFC 6238 §5.2
    -- requires that a code be accepted only once, and this is what remembers.
    totp_last_step INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX users_email_unique ON users (email);

-- No remember_tokens table.
--
-- "Remember me" now uses the framework's single-use token store
-- (tjo_reset_tokens), which the auth package creates and which keeps only a
-- hash. The table this replaces held the cookie's value verbatim and had no
-- expiry column, so reading it was a working login for every user who had
-- ticked the box.
DROP TABLE IF EXISTS remember_tokens;

DROP TABLE IF EXISTS tokens;

-- No plaintext token column: tokens are looked up by token_hash.
CREATE TABLE tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    token_hash BLOB NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expiry DATETIME NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX tokens_user_id_index ON tokens (user_id);
