-- All timestamps are unix milliseconds.

CREATE TABLE users (
  id          INTEGER PRIMARY KEY,
  username    TEXT NOT NULL,
  username_lc TEXT NOT NULL UNIQUE,
  email       TEXT,
  pw_hash     TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  imported_at INTEGER -- set once the browser's Classic stats were imported
);

CREATE TABLE sessions (
  token_hash BLOB PRIMARY KEY, -- sha256 of the cookie token; the token itself is never stored
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  ip         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);

CREATE TABLE classic_stats (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  size    INTEGER NOT NULL,
  level   TEXT NOT NULL,
  solved  INTEGER NOT NULL DEFAULT 0,
  best_ms INTEGER,
  PRIMARY KEY (user_id, size, level)
);

CREATE TABLE story_progress (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  level        INTEGER NOT NULL, -- 1-based story level number
  stars        INTEGER NOT NULL,
  best_ms      INTEGER NOT NULL,
  completed_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, level)
);

CREATE TABLE story_saves (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  level      INTEGER NOT NULL,
  board      TEXT NOT NULL, -- JSON {"values":[...],"notes":[...]}
  elapsed_ms INTEGER NOT NULL DEFAULT 0,
  mistakes   INTEGER NOT NULL DEFAULT 0,
  hints      INTEGER NOT NULL DEFAULT 0,
  started_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, level)
);

CREATE TABLE ratings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  size    INTEGER NOT NULL,
  rating  INTEGER NOT NULL DEFAULT 1000, -- Quick match only
  wins    INTEGER NOT NULL DEFAULT 0,
  losses  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, size)
);
CREATE INDEX ratings_board ON ratings(size, rating DESC);

CREATE TABLE matches (
  id          INTEGER PRIMARY KEY,
  size        INTEGER NOT NULL,
  level       TEXT NOT NULL,
  ranked      INTEGER NOT NULL,
  p1          INTEGER REFERENCES users(id) ON DELETE SET NULL,
  p2          INTEGER REFERENCES users(id) ON DELETE SET NULL,
  winner      INTEGER REFERENCES users(id) ON DELETE SET NULL,
  p1_ms       INTEGER,
  p2_ms       INTEGER,
  p1_mistakes INTEGER NOT NULL,
  p2_mistakes INTEGER NOT NULL,
  reason      TEXT NOT NULL, -- solved | resign | disconnect
  finished_at INTEGER NOT NULL
);
