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
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX users_email_unique ON users (email);

DROP TABLE IF EXISTS remember_tokens;

CREATE TABLE remember_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    remember_token TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX remember_tokens_token_index ON remember_tokens (remember_token);
CREATE INDEX remember_tokens_user_id_index ON remember_tokens (user_id);

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
