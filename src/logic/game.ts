// Game state + undo history. Pure data, no DOM.
import type { Level } from '../config'
import { conflicts, type Grid, type Puzzle } from './sudoku'

export type CheckMode = 'rules' | 'solution'
type Change = [cell: number, value: number, notes: number] // previous state of one cell

export interface Game {
  level: Level
  puzzle: Grid
  solution: Grid
  values: Grid
  notes: number[] // bitmask per cell, bit d = candidate d
  mistakes: number
  elapsed: number // seconds
  history: Change[][]
  done: boolean
}

export const newGame = (p: Puzzle): Game => ({
  ...p,
  values: p.puzzle.slice(),
  notes: new Array(81).fill(0),
  mistakes: 0,
  elapsed: 0,
  history: [],
  done: false,
})

const editable = (g: Game, i: number) => !g.done && !g.puzzle[i]

function apply(g: Game, i: number, value: number, notes: number) {
  g.history.push([[i, g.values[i], g.notes[i]]])
  g.values[i] = value
  g.notes[i] = notes
}

/** Cells shown as wrong under the given check mode. */
export function wrongCells(g: Game, mode: CheckMode): Set<number> {
  if (mode === 'rules') return conflicts(g.values)
  const out = new Set<number>()
  for (let i = 0; i < 81; i++) if (g.values[i] && g.values[i] !== g.solution[i]) out.add(i)
  return out
}

export function setValue(g: Game, i: number, d: number, mode: CheckMode) {
  if (!editable(g, i) || g.values[i] === d) return
  apply(g, i, d, 0)
  if (wrongCells(g, mode).has(i)) g.mistakes++
  g.done = g.values.every((v, j) => v === g.solution[j])
}

export function toggleNote(g: Game, i: number, d: number) {
  if (!editable(g, i) || g.values[i]) return
  apply(g, i, 0, g.notes[i] ^ (1 << d))
}

export function erase(g: Game, i: number) {
  if (!editable(g, i) || (!g.values[i] && !g.notes[i])) return
  apply(g, i, 0, 0)
}

/** Fills the selected cell if it's empty/wrong, otherwise a random one. Returns the filled index or -1. */
export function hint(g: Game, selected: number): number {
  const open = (i: number) => !g.puzzle[i] && g.values[i] !== g.solution[i]
  let i = selected
  if (i < 0 || !open(i)) {
    const cands = [...Array(81).keys()].filter(open)
    if (!cands.length || g.done) return -1
    i = cands[(Math.random() * cands.length) | 0]
  }
  apply(g, i, g.solution[i], 0)
  g.done = g.values.every((v, j) => v === g.solution[j])
  return i
}

export function undo(g: Game): number {
  const step = g.done ? undefined : g.history.pop()
  if (!step) return -1
  for (const [i, v, n] of step) { g.values[i] = v; g.notes[i] = n }
  return step[0][0]
}
