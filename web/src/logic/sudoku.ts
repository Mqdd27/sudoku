// Solver, generator, validator for any box-shaped Sudoku up to 9x9. Grid = number[size*size], 0 = empty.
import { GENERATE_BUDGET_MS } from '../config'

export type Grid = number[]
export interface Shape { size: number; boxRows: number; boxCols: number }
interface Geo { n: number; cells: number; box: number[]; peers: number[][]; full: number }

const geos = new Map<string, Geo>()

/** Cached geometry for a shape: box index per cell, peers per cell, bitmask of all digits. */
export function geo(s: Shape): Geo {
  const key = `${s.size}x${s.boxRows}x${s.boxCols}`
  let g = geos.get(key)
  if (g) return g
  const n = s.size, cells = n * n, perRow = n / s.boxCols
  const box = Array.from({ length: cells }, (_, i) =>
    Math.floor(Math.floor(i / n) / s.boxRows) * perRow + Math.floor((i % n) / s.boxCols))
  const peers = box.map((b, i) => {
    const out: number[] = []
    for (let j = 0; j < cells; j++)
      if (j !== i && (Math.floor(j / n) === Math.floor(i / n) || j % n === i % n || box[j] === b)) out.push(j)
    return out
  })
  g = { n, cells, box, peers, full: (1 << (n + 1)) - 2 }
  geos.set(key, g)
  return g
}

function shuffle<T>(a: T[]): T[] {
  for (let i = a.length - 1; i > 0; i--) {
    const j = (Math.random() * (i + 1)) | 0
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}

/**
 * Backtracking with bitmasks + most-constrained-cell first.
 * Stops after `limit` solutions. Returns the count found and the first solution.
 */
export function search(grid: Grid, s: Shape, limit = 2, random = false): { count: number; solution: Grid | null } {
  const { n, cells, box, full } = geo(s)
  const g = grid.slice()
  const rows = new Array(n).fill(0), cols = new Array(n).fill(0), boxes = new Array(n).fill(0)
  for (let i = 0; i < cells; i++) {
    const v = g[i]
    if (!v) continue
    const bit = 1 << v, r = (i / n) | 0, c = i % n, b = box[i]
    if (v > n || (rows[r] | cols[c] | boxes[b]) & bit) return { count: 0, solution: null } // givens clash
    rows[r] |= bit; cols[c] |= bit; boxes[b] |= bit
  }
  let count = 0
  let solution: Grid | null = null
  const digits = Array.from({ length: n }, (_, i) => i + 1)

  const rec = (): boolean => {
    let best = -1, bestMask = 0, bestN = n + 1
    for (let i = 0; i < cells; i++) {
      if (g[i]) continue
      const m = ~(rows[(i / n) | 0] | cols[i % n] | boxes[box[i]]) & full
      if (!m) return false
      let k = 0
      for (let x = m; x; x &= x - 1) k++
      if (k < bestN) { best = i; bestMask = m; bestN = k; if (k === 1) break }
    }
    if (best < 0) {
      if (!count++) solution = g.slice()
      return count >= limit
    }
    const r = (best / n) | 0, c = best % n, b = box[best]
    for (const d of random ? shuffle(digits.slice()) : digits) {
      const bit = 1 << d
      if (!(bestMask & bit)) continue
      g[best] = d; rows[r] |= bit; cols[c] |= bit; boxes[b] |= bit
      if (rec()) return true
      g[best] = 0; rows[r] &= ~bit; cols[c] &= ~bit; boxes[b] &= ~bit
    }
    return false
  }
  rec()
  return { count, solution }
}

export const countSolutions = (g: Grid, s: Shape, limit = 2) => search(g, s, limit).count
export const solve = (g: Grid, s: Shape) => search(g, s, 1).solution

export interface Puzzle { puzzle: Grid; solution: Grid }

/** Random full grid, then dig holes while the solution stays unique, until givens land in [min, max]. */
export function generate(s: Shape, [min, max]: [number, number], budgetMs = GENERATE_BUDGET_MS): Puzzle {
  const { cells } = geo(s)
  const deadline = Date.now() + budgetMs
  let best: Puzzle | null = null
  let bestGivens = Infinity
  do {
    const solution = search(new Array(cells).fill(0), s, 1, true).solution!
    const puzzle = solution.slice()
    const target = min + ((Math.random() * (max - min + 1)) | 0)
    let givens = cells
    for (const i of shuffle([...Array(cells).keys()])) {
      if (givens <= target) break
      const v = puzzle[i]
      puzzle[i] = 0
      if (countSolutions(puzzle, s) === 1) givens--
      else puzzle[i] = v
    }
    if (givens <= max) return { puzzle, solution }
    if (givens < bestGivens) { best = { puzzle, solution }; bestGivens = givens }
  } while (Date.now() < deadline)
  return best! // Note: may have a few givens above max if the budget runs out; still unique
}

/** Indices of filled cells that duplicate a peer's value. */
export function conflicts(g: Grid, s: Shape): Set<number> {
  const { peers } = geo(s)
  const out = new Set<number>()
  for (let i = 0; i < g.length; i++)
    if (g[i] && peers[i].some((j) => g[j] === g[i])) out.add(i)
  return out
}
