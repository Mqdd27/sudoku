// Story Mode: fixed 9×9 levels judged by the server. The client never receives a solution.
import { needLogin } from './account'
import { api, ApiError } from './api'
import { fmt, mount } from './board'
import { LEVELS, type Level } from './config'
import { newGame, type Game } from './logic/game'
import type { Grid } from './logic/sudoku'
import type { GameSource } from './source'
import { dialog, esc, go, showGame, showPage } from './ui'

const SAVE_EVERY_MS = 10_000

interface Result { solved: boolean; stars?: number; ms?: number; mistakes?: number; hints?: number; next?: number }

class StorySource implements GameSource {
  readonly strict = true
  readonly hints = true
  readonly pausable = true
  last: Result | null = null
  private timer = 0
  private pending: Game | null = null
  constructor(private n: number) {}

  async checkCell(_id: string, cell: number, value: number) {
    try { return await api<{ correct: boolean; mistakes: number }>('POST', `/story/${this.n}/check`, { cell, value }) } catch { return { correct: true } }
  }
  async getHint(_id: string, values: Grid, preferred: number) {
    try { return await api<{ cell: number; value: number } | null>('POST', `/story/${this.n}/hint`, { values, cell: preferred }) } catch { return null }
  }
  async submit(_id: string, values: Grid, elapsedSec: number) {
    this.flush()
    try { this.last = await api<Result>('POST', `/story/${this.n}/submit`, { values, elapsedMs: elapsedSec * 1000 }) } catch { this.last = { solved: false } }
    return this.last
  }
  /** Debounced: at most one save per 10s while playing; right away on pause, leaving or closing the tab. */
  save(g: Game, urgent: boolean) {
    if (g.done) return
    this.pending = g
    if (urgent) this.flush()
    else if (!this.timer) this.timer = window.setTimeout(() => this.flush(), SAVE_EVERY_MS)
  }
  private flush() {
    clearTimeout(this.timer)
    this.timer = 0
    const g = this.pending
    if (!g) return
    this.pending = null
    fetch(`/api/story/${this.n}/save`, {
      method: 'PUT', keepalive: true, headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ values: g.values, notes: g.notes, elapsedMs: g.elapsed * 1000 }),
    }).catch(() => {})
  }
}

interface MapLevel { n: number; chapter: number; difficulty: Level; stars: number; bestMs?: number; unlocked: boolean; inProgress: boolean }
interface StoryMap { chapters: { n: number; title: string; difficulty: Level }[]; levels: MapLevel[]; continue: number }

const starStr = (n: number) => '★'.repeat(n) + '☆'.repeat(3 - n)

export async function storyPage(arg: string) {
  if (arg) return playLevel(+arg)
  if (needLogin('#/story')) return
  let m: StoryMap
  try { m = await api<StoryMap>('GET', '/story') } catch (e) {
    showPage(`<div class="top"><a href="#/" class="btn icon" aria-label="Back">←</a><h1 tabindex="-1">Story</h1></div><p class="err">${esc((e as Error).message)}</p>`)
    return
  }
  const total = m.levels.reduce((s, l) => s + l.stars, 0)
  showPage(`
    <div class="top"><a href="#/" class="btn icon" aria-label="Back">←</a><h1 tabindex="-1">Story</h1><span class="muted">${total} ★</span></div>
    <a class="btn primary" href="#/story/${m.continue}">Continue · Level ${m.continue}</a>
    ${m.chapters.map((c) => `
      <h2>Chapter ${c.n}: ${esc(c.title)} <small class="muted">${LEVELS[c.difficulty]}</small></h2>
      <div class="levels">${m.levels.filter((l) => l.chapter === c.n).map((l) => l.unlocked
        ? `<a href="#/story/${l.n}" aria-label="Level ${l.n}${l.stars ? ', ' + l.stars + ' stars' : ''}">${l.n}<small>${l.stars ? starStr(l.stars) : l.inProgress ? '…' : ''}</small></a>`
        : `<span aria-label="Level ${l.n}, locked">🔒<small>${l.n}</small></span>`).join('')}</div>`).join('')}`)
}

async function playLevel(n: number) {
  if (needLogin('#/story/' + n)) return
  let s: { n: number; chapter: number; difficulty: Level; puzzle: Grid; values: Grid; notes: number[]; elapsedMs: number; mistakes: number; hints: number }
  try { s = await api('POST', `/story/${n}/start`) } catch (e) {
    await dialog('Can\'t open this level', `<p>${esc((e as ApiError).message)}</p>`, ['Level map'])
    return go('#/story')
  }
  const g = newGame({ id: 'story-' + n, size: 9, level: s.difficulty, puzzle: s.puzzle })
  Object.assign(g, { values: s.values, notes: s.notes, elapsed: Math.round(s.elapsedMs / 1000), mistakes: s.mistakes, hints: s.hints })
  const src = new StorySource(n)
  showGame(`<b>Level ${n}</b>`, `Chapter ${s.chapter} · ${LEVELS[s.difficulty]}`)
  mount({ game: g, source: src, onWin: () => won(n, src.last!) })
}

async function won(n: number, r: Result) {
  const i = await dialog(`Level ${n} complete!`, `
    <p class="stars" aria-label="${r.stars} stars">${starStr(r.stars ?? 1)}</p>
    <p>Time: <b>${fmt(Math.round((r.ms ?? 0) / 1000))}</b><br>Mistakes: <b>${r.mistakes}</b> · Hints: <b>${r.hints}</b></p>
    ${(r.stars ?? 3) < 3 ? '<p class="muted">3 stars: no hints, at most 2 mistakes, under par time.</p>' : ''}`,
    r.next ? ['Next level', 'Level map'] : ['Level map'])
  go(r.next && i === 0 ? '#/story/' + r.next : '#/story')
}
