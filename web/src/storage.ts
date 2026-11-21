// localStorage, namespaced by schema version so a later version (or an account import) can migrate it.
//
// Schema v2 — every value is JSON:
//   sudoku:v2:settings  { check: "rules" | "solution", theme: "auto" | "light" | "dark", size: 4|6|9, level }
//   sudoku:v2:stats     { [size]: { [level]: { solved: number, best: number /* seconds */ } } }
//   sudoku:v2:game      Game (see logic/game.ts), the in-progress Classic board; never holds the solution
//   sudoku:v2:local     { id: string, solution: number[] }, LocalGameSource's answer key
//   sudoku:v2:imported  { [userId]: true }, accounts already offered the stats import
const NS = 'sudoku:v2:'

export type Key = 'settings' | 'stats' | 'game' | 'local' | 'imported'

export function load<T>(key: Key, fallback: T): T {
  try { return JSON.parse(localStorage.getItem(NS + key) ?? 'null') ?? fallback } catch { return fallback }
}

export function store(key: Key, value: unknown) {
  try { localStorage.setItem(NS + key, JSON.stringify(value)) } catch { /* private mode / quota: keep playing unsaved */ }
}

// One-time upgrade from v1 (9x9 only) and from the unversioned keys before it.
try {
  const get = (k: string) => { const v = localStorage.getItem(k); return v && JSON.parse(v) }
  if (!localStorage.getItem(NS + 'stats')) {
    const old = get('sudoku:v1:stats') ?? get('sudoku.stats')
    if (old) store('stats', { 9: old })
  }
  if (!localStorage.getItem(NS + 'settings')) {
    const old = get('sudoku:v1:settings') ?? get('sudoku.settings')
    if (old) store('settings', old)
  }
  const game = get('sudoku:v1:game'), local = get('sudoku:v1:local')
  if (game && local && !localStorage.getItem(NS + 'game')) {
    store('game', { ...game, size: 9, hints: 0 })
    store('local', local)
  }
  for (const k of ['settings', 'stats', 'game', 'local']) {
    localStorage.removeItem('sudoku:v1:' + k)
    localStorage.removeItem('sudoku.' + k)
  }
} catch { /* storage unavailable or corrupt: start fresh */ }
