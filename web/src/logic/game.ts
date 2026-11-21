// Game state + undo history. Pure data, no DOM, and no solution: answers come from a GameSource.
import type { Level, Size } from '../config'
import type { Grid } from './sudoku'

export type CheckMode = 'rules' | 'solution'
type Change = [cell: number, value: number, notes: number] // previous state of one cell

export interface Game {
  id: string // GameSource game id
  size: Size
  level: Level
  puzzle: Grid
  values: Grid
  notes: number[] // bitmask per cell, bit d = candidate d
  wrong: number[] // cells the source said are wrong (solution check mode)
  mistakes: number
  hints: number
  elapsed: number // seconds
  history: Change[][]
  done: boolean
}

export const newGame = (s: { id: string; size: Size; level: Level; puzzle: Grid }): Game => ({
  ...s,
  values: s.puzzle.slice(),
  notes: new Array(s.puzzle.length).fill(0),
  wrong: [],
  mistakes: 0,
  hints: 0,
  elapsed: 0,
  history: [],
  done: false,
})

const editable = (g: Game, i: number) => !g.done && !g.puzzle[i]

function apply(g: Game, i: number, value: number, notes: number) {
  g.history.push([[i, g.values[i], g.notes[i]]])
  g.values[i] = value
  g.notes[i] = notes
  g.wrong = g.wrong.filter((j) => j !== i)
}

/** Each returns true if the board changed. */
export function setValue(g: Game, i: number, d: number) {
  if (!editable(g, i) || g.values[i] === d) return false
  apply(g, i, d, 0)
  return true
}

export function toggleNote(g: Game, i: number, d: number) {
  if (!editable(g, i) || g.values[i]) return false
  apply(g, i, 0, g.notes[i] ^ (1 << d))
  return true
}

export function erase(g: Game, i: number) {
  if (!editable(g, i) || (!g.values[i] && !g.notes[i])) return false
  apply(g, i, 0, 0)
  return true
}

/** Reverts the last change; returns the affected cell or -1. */
export function undo(g: Game): number {
  const step = g.done ? undefined : g.history.pop()
  if (!step) return -1
  for (const [i, v, n] of step) { g.values[i] = v; g.notes[i] = n; g.wrong = g.wrong.filter((j) => j !== i) }
  return step[0][0]
}

export const isFull = (g: Game) => g.values.every(Boolean)
