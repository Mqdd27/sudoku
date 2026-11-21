import { describe, expect, it } from 'vitest'
import { LEVELS, type Level } from '../config'
import { conflicts, countSolutions, generate, search, solve } from './sudoku'
import { hint, newGame, setValue, undo } from './game'

const parse = (s: string) => [...s].map((c) => (c === '.' ? 0 : +c))
// Classic puzzle with a known unique solution.
const P = parse('53..7....6..195....98....6.8...6...34..8.3..17...2...6.6....28....419..5....8..79')
const S = parse('534678912672195348198342567859761423426853791713924856961537284287419635345286179')

const isValidSolution = (g: number[]) => g.every((v) => v >= 1 && v <= 9) && conflicts(g).size === 0

describe('solver', () => {
  it('solves a known puzzle', () => expect(solve(P)).toEqual(S))
  it('detects unique solution', () => expect(countSolutions(P)).toBe(1))
  it('detects multiple solutions', () => expect(countSolutions(new Array(81).fill(0))).toBe(2))
  it('returns 0 for clashing givens', () => {
    const g = P.slice(); g[1] = 5 // two 5s in row 0
    expect(countSolutions(g)).toBe(0)
  })
  it('returns 0 for unsolvable grid', () => {
    const g = S.slice(); g[0] = 0; g[1] = 0; g[2] = 3; g[3] = 0 // 3 duplicated → no fill
    expect(countSolutions(g)).toBe(0)
  })
  it('random fill of empty grid is a valid sudoku', () =>
    expect(isValidSolution(search(new Array(81).fill(0), 1, true).solution!)).toBe(true))
})

describe('generator', () => {
  for (const level of Object.keys(LEVELS) as Level[]) {
    it(`${level}: unique, consistent, givens in range`, () => {
      for (let n = 0; n < 3; n++) {
        const { puzzle, solution } = generate(level)
        const givens = puzzle.filter(Boolean).length
        expect(givens).toBeGreaterThanOrEqual(LEVELS[level].min)
        expect(givens).toBeLessThanOrEqual(LEVELS[level].max)
        expect(isValidSolution(solution)).toBe(true)
        expect(puzzle.every((v, i) => !v || v === solution[i])).toBe(true)
        expect(countSolutions(puzzle)).toBe(1)
        expect(solve(puzzle)).toEqual(solution)
      }
    }, 30_000)
  }
})

describe('game state', () => {
  it('counts mistakes, undoes, finishes', () => {
    const g = newGame({ level: 'easy', puzzle: P, solution: S })
    const i = P.indexOf(0)
    setValue(g, i, S[i] === 9 ? 1 : 9, 'solution')
    expect(g.mistakes).toBe(1)
    undo(g)
    expect(g.values[i]).toBe(0)
    expect(g.mistakes).toBe(1) // info only, undo doesn't erase it
    setValue(g, 0, 1, 'solution') // given cell, ignored
    expect(g.values[0]).toBe(5)
    while (hint(g, -1) >= 0);
    expect(g.done).toBe(true)
  })
})
