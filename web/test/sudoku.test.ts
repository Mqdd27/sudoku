import { describe, expect, it } from 'vitest'
import { givens, LEVELS, shapeOf, SIZES, type Level } from '../src/config'
import { conflicts, countSolutions, generate, search, solve, type Shape } from '../src/logic/sudoku'
import { isFull, newGame, setValue, toggleNote, undo } from '../src/logic/game'
import f4 from '../../shared/fixtures/sudoku-4.json'
import f6 from '../../shared/fixtures/sudoku-6.json'
import f9 from '../../shared/fixtures/sudoku-9.json'

const parse = (s: string) => [...s].map(Number)
const isValidSolution = (g: number[], s: Shape) =>
  g.length === s.size ** 2 && g.every((v) => v >= 1 && v <= s.size) && conflicts(g, s).size === 0
const S9 = shapeOf(9)
const P = parse(f9.puzzles[0].puzzle), S = parse(f9.puzzles[0].solution)

describe('shared fixtures', () => {
  for (const fx of [f4, f6, f9]) {
    const shape = { size: fx.size, boxRows: fx.boxRows, boxCols: fx.boxCols }
    for (const f of fx.puzzles) {
      it(`${fx.size}x${fx.size} ${f.level}: unique and solves to the stored solution`, () => {
        const p = parse(f.puzzle)
        expect(p.filter(Boolean).length).toBe(f.givens)
        expect(countSolutions(p, shape)).toBe(1)
        expect(solve(p, shape)).toEqual(parse(f.solution))
        if (f.level in LEVELS) {
          const [min, max] = givens(fx.size as 4 | 6 | 9, f.level as Level)
          expect(f.givens).toBeGreaterThanOrEqual(min)
          expect(f.givens).toBeLessThanOrEqual(max)
        }
      })
    }
  }
})

describe('solver', () => {
  it('detects multiple solutions', () => {
    for (const size of SIZES) expect(countSolutions(new Array(size * size).fill(0), shapeOf(size))).toBe(2)
  })
  it('returns 0 for clashing givens', () => {
    const g = P.slice(); g[1] = 5 // two 5s in row 0
    expect(countSolutions(g, S9)).toBe(0)
  })
  it('returns 0 for out-of-range digits', () => {
    const g = new Array(16).fill(0); g[0] = 5
    expect(countSolutions(g, shapeOf(4))).toBe(0)
  })
  it('returns 0 for unsolvable grid', () => {
    const g = S.slice(); g[0] = 0; g[1] = 0; g[2] = 3; g[3] = 0
    expect(countSolutions(g, S9)).toBe(0)
  })
  it('6x6 boxes are 2 rows x 3 columns', () => {
    const g = new Array(36).fill(0)
    g[0] = 1; g[8] = 1 // (0,0) and (1,2) share the top-left 2x3 box
    expect(conflicts(g, shapeOf(6))).toEqual(new Set([0, 8]))
    g[8] = 0; g[13] = 1 // (2,1) is in the next box down, other row and column
    expect(conflicts(g, shapeOf(6)).size).toBe(0)
  })
  it('random fill is a valid sudoku for every size', () => {
    for (const size of SIZES) {
      const s = shapeOf(size)
      expect(isValidSolution(search(new Array(size * size).fill(0), s, 1, true).solution!, s)).toBe(true)
    }
  })
})

describe('generator', () => {
  for (const size of SIZES) for (const level of Object.keys(LEVELS) as Level[]) {
    it(`${size}x${size} ${level}: unique, consistent, givens in range`, () => {
      const s = shapeOf(size), [min, max] = givens(size, level)
      for (let n = 0; n < 3; n++) {
        const { puzzle, solution } = generate(s, [min, max])
        const count = puzzle.filter(Boolean).length
        expect(count).toBeGreaterThanOrEqual(min)
        expect(count).toBeLessThanOrEqual(max)
        expect(isValidSolution(solution, s)).toBe(true)
        expect(puzzle.every((v, i) => !v || v === solution[i])).toBe(true)
        expect(countSolutions(puzzle, s)).toBe(1)
      }
    }, 30_000)
  }
})

describe('game state', () => {
  it('edits, protects givens, undoes, detects full board', () => {
    const g = newGame({ id: 't', size: 9, level: 'easy', puzzle: P })
    const i = P.indexOf(0)
    expect(setValue(g, 0, 1)).toBe(false) // given
    expect(toggleNote(g, i, 4)).toBe(true)
    expect(setValue(g, i, 9)).toBe(true)
    g.wrong.push(i)
    expect(undo(g)).toBe(i)
    expect([g.values[i], g.notes[i], g.wrong]).toEqual([0, 1 << 4, []])
    S.forEach((v, j) => setValue(g, j, v))
    expect(isFull(g)).toBe(true)
  })
})

describe('LocalGameSource', () => {
  for (const size of SIZES) {
    it(`${size}x${size}: start → checkCell → hints until submit solves`, async () => {
      const { LocalGameSource } = await import('../src/source') // no Worker in Node: exercises the inline fallback
      const src = new LocalGameSource()
      const { id, puzzle } = await src.start(size, 'easy')
      expect(puzzle.length).toBe(size * size)
      expect(await src.resume(id)).toBe(true)
      expect(await src.resume('other')).toBe(false)
      const values = puzzle.slice()
      const i = values.indexOf(0)
      const h = (await src.getHint(id, values, i))!
      expect(h.cell).toBe(i)
      expect((await src.checkCell(id, i, h.value)).correct).toBe(true)
      expect((await src.checkCell(id, i, (h.value % size) + 1)).correct).toBe(false)
      expect((await src.submit(id, values, 0)).solved).toBe(false)
      for (let n; (n = await src.getHint(id, values, -1)); ) values[n.cell] = n.value
      expect((await src.submit(id, values, 0)).solved).toBe(true)
    })
  }
})
