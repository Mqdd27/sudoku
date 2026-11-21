-- Time Attack: ratings become per (mode, size); matches record mode, duration and box/cell scores.

CREATE TABLE pvp_ratings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mode    TEXT NOT NULL, -- race | ta
  size    INTEGER NOT NULL,
  rating  INTEGER NOT NULL DEFAULT 1000, -- Quick match only
  wins    INTEGER NOT NULL DEFAULT 0,
  losses  INTEGER NOT NULL DEFAULT 0,
  draws   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, mode, size)
);
CREATE INDEX pvp_ratings_board ON pvp_ratings(mode, size, rating DESC);

INSERT INTO pvp_ratings(user_id, mode, size, rating, wins, losses)
  SELECT user_id, 'race', size, rating, wins, losses FROM ratings;
DROP TABLE ratings;

ALTER TABLE matches ADD COLUMN mode TEXT NOT NULL DEFAULT 'race';
ALTER TABLE matches ADD COLUMN duration_sec INTEGER; -- Time Attack only
ALTER TABLE matches ADD COLUMN p1_boxes INTEGER;
ALTER TABLE matches ADD COLUMN p2_boxes INTEGER;
ALTER TABLE matches ADD COLUMN p1_cells INTEGER;
ALTER TABLE matches ADD COLUMN p2_cells INTEGER;
ALTER TABLE matches ADD COLUMN early INTEGER NOT NULL DEFAULT 0; -- ended before time ran out
CREATE INDEX matches_ta ON matches(mode, size, level, duration_sec);
