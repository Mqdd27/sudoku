// Solver, generator, validator. Grid = number[81], 0 = empty.
import { LEVELS, GENERATE_BUDGET_MS, type Level } from '../config'

export type Grid = number[]

const box = (i: number) => (((i / 27) | 0) * 3) + (((i % 9) / 3) | 0)

/** Indices sharing a row, column or box with i (excluding i). */
export const PEERS: number[][] = Array.from({ length: 81 }, (_, i) => {
  const r = (i / 9) | 0, c = i % 9, b = box(i)
  const out: number[] = []
  for (let j = 0; j < 81; j++)
    if (j !== i && (((j / 9) | 0) === r || j % 9 === c || box(j) === b)) out.push(j)
  return out
})

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
export function search(grid: Grid, limit = 2, random = false): { count: number; solution: Grid | null } {
  const g = grid.slice()
  const rows = new Array(9).fill(0), cols = new Array(9).fill(0), boxes = new Array(9).fill(0)
  for (let i = 0; i < 81; i++) {
    const v = g[i]
    if (!v) continue
    const bit = 1 << v, r = (i / 9) | 0, c = i % 9, b = box(i)
    if ((rows[r] | cols[c] | boxes[b]) & bit) return { count: 0, solution: null } // givens clash
    rows[r] |= bit; cols[c] |= bit; boxes[b] |= bit
  }
  let count = 0
  let solution: Grid | null = null
  const digits = [1, 2, 3, 4, 5, 6, 7, 8, 9]

  const rec = (): boolean => {
    let best = -1, bestMask = 0, bestN = 10
    for (let i = 0; i < 81; i++) {
      if (g[i]) continue
      const m = ~(rows[(i / 9) | 0] | cols[i % 9] | boxes[box(i)]) & 0x3fe
      if (!m) return false
      let n = 0
      for (let x = m; x; x &= x - 1) n++
      if (n < bestN) { best = i; bestMask = m; bestN = n; if (n === 1) break }
    }
    if (best < 0) {
      if (!count++) solution = g.slice()
      return count >= limit
    }
    const r = (best / 9) | 0, c = best % 9, b = box(best)
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

export const countSolutions = (g: Grid, limit = 2) => search(g, limit).count
export const solve = (g: Grid) => search(g, 1).solution

export interface Puzzle { level: Level; puzzle: Grid; solution: Grid }

/** Random full grid, then dig holes while the solution stays unique, until givens land in the level range. */
export function generate(level: Level, budgetMs = GENERATE_BUDGET_MS): Puzzle {
  const { min, max } = LEVELS[level]
  const deadline = Date.now() + budgetMs
  let best: Puzzle | null = null
  do {
    const solution = search(new Array(81).fill(0), 1, true).solution!
    const puzzle = solution.slice()
    const target = min + ((Math.random() * (max - min + 1)) | 0)
    let givens = 81
    for (const i of shuffle([...Array(81).keys()])) {
      if (givens <= target) break
      const v = puzzle[i]
      puzzle[i] = 0
      if (countSolutions(puzzle) === 1) givens--
      else puzzle[i] = v
    }
    const p = { level, puzzle, solution }
    if (givens <= max) return p
    if (!best || givens < best.puzzle.filter(Boolean).length) best = p
  } while (Date.now() < deadline)
  return best! // ponytail: may have a few givens above max if the budget runs out; still unique
}

/** Indices of filled cells that duplicate a peer's value. */
export function conflicts(g: Grid): Set<number> {
  const out = new Set<number>()
  for (let i = 0; i < 81; i++)
    if (g[i] && PEERS[i].some((j) => g[j] === g[i])) out.add(i)
  return out
}
