# PvP WebSocket protocol

One connection per signed-in user at `GET /ws` (session cookie required, same-origin `Origin`).
Every frame is a small JSON object with a type field `t`. Opening a second connection replaces the first
(the old one gets `replaced`).

The server owns the puzzle, the solution, the clock and the result. The solution is never sent.
Each placed digit is judged on the server; the first player whose board on the server is fully correct wins.

## Heartbeat and limits

- The server sends a WebSocket ping every 25 s and drops the connection if no pong arrives within 10 s.
- Clients may also send `{"t":"ping"}` and get `{"t":"pong"}` (browsers can't see protocol pings).
- Max 4 KB per frame. Rate limit: 10 messages/s with bursts of 20; extra messages get `error` and are
  dropped, and a client that keeps flooding is disconnected.

## Client → server

| `t` | Fields | Meaning |
|---|---|---|
| `queue` | `size` (4, 6, 9), `level` | Quick match (ranked). One queue per size + level. |
| `cancel` | | Leave the queue, or leave a lobby. |
| `create` | `size`, `level` | Private room (unranked). Reply is `lobby` with a 6-char `code`. |
| `join` | `code` | Join a private room (case-insensitive). |
| `ready` | | Ready in the lobby. Both ready → countdown. |
| `place` | `cell`, `value` | Set a cell; `value: 0` clears it. |
| `submit` | | Ask the server to judge the board it holds. Optional: a correct board finishes on its own. |
| `hint` | | Only when `pvp.hints` is on in `shared/config.json`; adds `hintPenaltySec` to your time. |
| `resign` | | Opponent wins. |
| `rematch` | | After a result. Both ask → new countdown, new puzzle, same room. |
| `leave` | | Leave the room (resigns a running match). |
| `ping` | | Heartbeat. |

## Server → client

| `t` | Fields | Meaning |
|---|---|---|
| `hello` | `user` | Connected, not in a room. |
| `queued` | `size`, `level`, `timeoutSec` | Waiting for an opponent. |
| `queue_timeout` | | Nobody found within `timeoutSec` (60). Send `queue` again to keep waiting. |
| `cancelled`, `left` | | Acknowledgements. |
| `lobby` | `code`, `size`, `level`, `ranked`, `players: [{name, ready, rating}]` | Lobby changed. |
| `countdown` | `startAt`, `now` | Match starts at `startAt` (server ms; use `now` to correct for clock skew). |
| `start` | `size`, `boxRows`, `boxCols`, `level`, `puzzle`, `startAt`, `now` | Puzzle as a flat array, 0 = empty. |
| `checked` | `cell`, `value`, `correct`, `mistakes` | Verdict on your `place`. Repeating the same wrong digit in a cell isn't counted twice. |
| `hinted` | `cell`, `value`, `penaltySec` | Hint result. |
| `opponent` | `progress` (0–100, share of open cells correct), `mistakes` | Opponent's progress. Their digits are never sent. |
| `opponent_away` | `graceSec` | Opponent disconnected; they forfeit if not back within `graceSec` (60). |
| `opponent_back` | | Opponent reconnected. |
| `state` | lobby fields + `phase`, `now`, and while playing: `puzzle`, `startAt`, `values`, `mistakes`, `opponent: {progress, mistakes, connected}` | Sent instead of `hello` when you reconnect into a room. |
| `result` | `winner` (name or null), `reason` (`solved`, `resign`, `disconnect`), `ranked`, `players: [{name, ms, mistakes, rating, delta}]` | Match over. `ms` is null for a player who didn't finish. |
| `rematch` | `from` | A player asked for a rematch. |
| `opponent_left` | | The other player left; the room is closed. |
| `rejected` | `msg` | `submit` on a board that isn't solved. |
| `replaced` | | This connection was replaced by a newer one. |
| `error` | `msg` | Something was refused. |

## Rating

Elo, K = 32, starting at 1000, separate per board size. Only Quick match changes ratings;
wins and losses are counted for every finished match.
