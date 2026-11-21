// localStorage, namespaced by schema version so a later version (or an account import) can migrate it.
//
// Schema v1 — every value is JSON:
//   sudoku:v1:settings  { check: "rules" | "solution", theme: "auto" | "light" | "dark" }
//   sudoku:v1:stats     { [level]: { solved: number, best: number /* seconds */ } }
//   sudoku:v1:game      Game (see logic/game.ts) — in-progress board, no solution in it
//   sudoku:v1:local     { id: string, solution: number[81] } — LocalGameSource's answer key
const NS = 'sudoku:v1:'

export type Key = 'settings' | 'stats' | 'game' | 'local'

export function load<T>(key: Key, fallback: T): T {
  try { return JSON.parse(localStorage.getItem(NS + key) ?? 'null') ?? fallback } catch { return fallback }
}

export function store(key: Key, value: unknown) {
  try { localStorage.setItem(NS + key, JSON.stringify(value)) } catch { /* private mode / quota: keep playing unsaved */ }
}

// Pre-v1 keys (`sudoku.settings`, `sudoku.stats`) had the same shape: carry them over once.
// Old in-progress games are dropped, their format held the solution inline.
try {
  for (const k of ['settings', 'stats'] as const) {
    const old = localStorage.getItem('sudoku.' + k)
    if (old && !localStorage.getItem(NS + k)) localStorage.setItem(NS + k, old)
  }
  for (const k of ['settings', 'stats', 'game']) localStorage.removeItem('sudoku.' + k)
} catch { /* storage unavailable */ }
