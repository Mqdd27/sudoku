// Where answers come from. The board only talks to a GameSource and never sees a solution, so
// Classic (LocalGameSource), Story and PvP (server-backed, see net.ts) share the same board code.
import { givens, shapeOf, WORKER_TIMEOUT_MS, type Level, type Size } from './config'
import { generate, type Grid, type Puzzle } from './logic/sudoku'
import type { Game } from './logic/game'
import { load, store } from './storage'

export interface StartedGame { id: string; size: Size; level: Level; puzzle: Grid }

export interface GameSource {
  /** Server-judged: every placed digit is checked and the returned mistake count is the real one. */
  readonly strict: boolean
  readonly hints: boolean
  readonly pausable: boolean
  checkCell(id: string, cell: number, value: number): Promise<{ correct: boolean; mistakes?: number }>
  /** A correct value for `preferred` if it's still open, otherwise any open cell. Null when nothing is left. */
  getHint(id: string, values: Grid, preferred: number): Promise<{ cell: number; value: number } | null>
  submit(id: string, values: Grid, elapsedSec: number): Promise<{ solved: boolean }>
  /** Board changed or paused; the source decides when (and whether) to persist. */
  save(g: Game, urgent: boolean): void
}

function makePuzzle(size: Size, level: Level): Promise<Puzzle> {
  return new Promise((resolve) => {
    let w: Worker | undefined, settled = false
    const finish = (p?: Puzzle) => {
      if (settled) return
      settled = true
      clearTimeout(t)
      w?.terminate()
      resolve(p ?? generate(shapeOf(size), givens(size, level), 1000)) // fallback: inline, short budget
    }
    const t = setTimeout(() => finish(), WORKER_TIMEOUT_MS)
    try {
      w = new Worker(new URL('./worker.ts', import.meta.url), { type: 'module' })
      w.onmessage = (e: MessageEvent<Puzzle>) => finish(e.data)
      w.onerror = () => finish()
      w.postMessage([size, level])
    } catch { finish() }
  })
}

export class LocalGameSource implements GameSource {
  readonly strict = false
  readonly hints = true
  readonly pausable = true
  private cur = load<{ id: string; solution: Grid } | null>('local', null)

  private solution(id: string): Grid {
    if (this.cur?.id !== id) throw new Error('unknown game ' + id)
    return this.cur.solution
  }

  async start(size: Size, level: Level): Promise<StartedGame> {
    const { puzzle, solution } = await makePuzzle(size, level)
    const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
    this.cur = { id, solution }
    store('local', this.cur)
    return { id, size, level, puzzle }
  }

  async resume(id: string) { return this.cur?.id === id }

  async checkCell(id: string, cell: number, value: number) { return { correct: this.solution(id)[cell] === value } }

  async getHint(id: string, values: Grid, preferred: number) {
    const s = this.solution(id)
    const open = (i: number) => values[i] !== s[i]
    let cell = preferred
    if (cell < 0 || !open(cell)) {
      const cands = [...s.keys()].filter(open)
      if (!cands.length) return null
      cell = cands[(Math.random() * cands.length) | 0]
    }
    return { cell, value: s[cell] }
  }

  async submit(id: string, values: Grid, _elapsedSec?: number) {
    const s = this.solution(id)
    return { solved: values.every((v, i) => v === s[i]) }
  }

  save(g: Game) { store('game', g) }
}
