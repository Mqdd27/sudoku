// Where puzzles and answers come from. The UI only talks to GameSource and never sees a solution,
// so a server-backed source can replace LocalGameSource without touching the board.
import { WORKER_TIMEOUT_MS, type Level } from './config'
import { generate, type Grid, type Puzzle } from './logic/sudoku'
import { load, store } from './storage'

export interface StartedGame { id: string; level: Level; puzzle: Grid }

export interface GameSource {
  start(level: Level): Promise<StartedGame>
  /** Can this source still answer for game `id` (e.g. after a reload)? */
  resume(id: string): Promise<boolean>
  checkCell(id: string, cell: number, value: number): Promise<boolean>
  /** A correct value for `preferred` if it's still open, otherwise any open cell. Null when nothing is left. */
  getHint(id: string, values: Grid, preferred: number): Promise<{ cell: number; value: number } | null>
  submit(id: string, values: Grid, elapsedSec: number): Promise<{ solved: boolean }>
}

function makePuzzle(level: Level): Promise<Puzzle> {
  return new Promise((resolve) => {
    let w: Worker | undefined, settled = false
    const finish = (p?: Puzzle) => {
      if (settled) return
      settled = true
      clearTimeout(t)
      w?.terminate()
      resolve(p ?? generate(level, 1000)) // fallback: inline, short budget
    }
    const t = setTimeout(() => finish(), WORKER_TIMEOUT_MS)
    try {
      w = new Worker(new URL('./worker.ts', import.meta.url), { type: 'module' })
      w.onmessage = (e: MessageEvent<Puzzle>) => finish(e.data)
      w.onerror = () => finish()
      w.postMessage(level)
    } catch { finish() }
  })
}

export class LocalGameSource implements GameSource {
  private cur = load<{ id: string; solution: Grid } | null>('local', null)

  private solution(id: string): Grid {
    if (this.cur?.id !== id) throw new Error('unknown game ' + id)
    return this.cur.solution
  }

  async start(level: Level): Promise<StartedGame> {
    const { puzzle, solution } = await makePuzzle(level)
    const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
    this.cur = { id, solution }
    store('local', this.cur)
    return { id, level, puzzle }
  }

  async resume(id: string) { return this.cur?.id === id }

  async checkCell(id: string, cell: number, value: number) { return this.solution(id)[cell] === value }

  async getHint(id: string, values: Grid, preferred: number) {
    const s = this.solution(id)
    const open = (i: number) => values[i] !== s[i]
    let cell = preferred
    if (cell < 0 || !open(cell)) {
      const cands = [...Array(81).keys()].filter(open)
      if (!cands.length) return null
      cell = cands[(Math.random() * cands.length) | 0]
    }
    return { cell, value: s[cell] }
  }

  async submit(id: string, values: Grid, _elapsedSec?: number) { // elapsed matters only to a server
    const s = this.solution(id)
    return { solved: values.every((v, i) => v === s[i]) }
  }
}
