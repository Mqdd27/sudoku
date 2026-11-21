import { describe, expect, it } from 'vitest'
import { LEVELS, type Level } from '../src/config'
import { conflicts, countSolutions, generate, search, solve } from '../src/logic/sudoku'
import { isFull, newGame, setValue, toggleNote, undo } from '../src/logic/game'
import fixtures from './fixtures/puzzles.json'

const parse = (s: string) => [...s].map(Number)
const isValidSolution = (g: number[]) => g.every((v) => v >= 1 && v <= 9) && conflicts(g).size === 0
const [classic] = fixtures.puzzles
const P = parse(classic.puzzle), S = parse(classic.solution)

describe('fixtures (shared test vectors)', () => {
  for (const f of fixtures.puzzles) {
    it(`${f.level}: unique and solves to the stored solution`, () => {
      const p = parse(f.puzzle)
      expect(p.filter(Boolean).length).toBe(f.givens)
      expect(countSolutions(p)).toBe(1)
      expect(solve(p)).toEqual(parse(f.solution))
      if (f.level in LEVELS) {
        expect(f.givens).toBeGreaterThanOrEqual(LEVELS[f.level as Level].min)
        expect(f.givens).toBeLessThanOrEqual(LEVELS[f.level as Level].max)
      }
    })
  }
})

describe('solver', () => {
  it('detects multiple solutions', () => expect(countSolutions(new Array(81).fill(0))).toBe(2))
  it('returns 0 for clashing givens', () => {
    const g = P.slice(); g[1] = 5 // two 5s in row 0
    expect(countSolutions(g)).toBe(0)
  })
  it('returns 0 for unsolvable grid', () => {
    const g = S.slice(); g[0] = 0; g[1] = 0; g[2] = 3; g[3] = 0
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
  it('edits, protects givens, undoes, detects full board', () => {
    const g = newGame({ id: 't', level: 'easy', puzzle: P })
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
  it('start → checkCell → hints until submit solves', async () => {
    const { LocalGameSource } = await import('../src/source') // no Worker in Node: exercises the inline fallback
    const src = new LocalGameSource()
    const { id, puzzle } = await src.start('easy')
    expect(await src.resume(id)).toBe(true)
    expect(await src.resume('other')).toBe(false)
    const values = puzzle.slice()
    const i = values.indexOf(0)
    const h = (await src.getHint(id, values, i))!
    expect(h.cell).toBe(i)
    expect(await src.checkCell(id, i, h.value)).toBe(true)
    expect(await src.checkCell(id, i, (h.value % 9) + 1)).toBe(false)
    expect((await src.submit(id, values, 0)).solved).toBe(false)
    for (let n; (n = await src.getHint(id, values, -1)); ) values[n.cell] = n.value
    expect((await src.submit(id, values, 0)).solved).toBe(true)
  })
})
