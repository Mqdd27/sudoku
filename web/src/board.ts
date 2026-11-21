// The game screen: board, number pad, actions, timer. Works for every size and every GameSource.
import { shapeOf } from './config'
import { conflicts, geo, type Shape } from './logic/sudoku'
import { erase, isFull, setValue, toggleNote, undo, type CheckMode, type Game } from './logic/game'
import type { GameSource } from './source'
import { load, store } from './storage'

export interface Settings { check: CheckMode; theme: 'auto' | 'light' | 'dark'; size?: number; level?: string }
export const settings = load<Settings>('settings', { check: 'rules', theme: 'auto' })
export const saveSettings = () => store('settings', settings)

export interface Play {
  game: Game
  source: GameSource
  onWin(g: Game): void
  /** Elapsed seconds from an outside clock (PvP: server start time). Default: the game's own counter. */
  clock?: () => number
  /** Time Attack: the clock counts down (warn near zero), no undo, bigger timer. */
  timed?: boolean
}

/** Server-driven cell states (Time Attack). The owner edits the sets and calls paint(). */
export const marks = { locked: new Set<number>(), cooling: new Set<number>(), boxes: new Set<number>(), fresh: -1 }

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T
export const fmt = (s: number) => {
  const h = (s / 3600) | 0, m = ((s / 60) | 0) % 60, ss = s % 60
  const p = (n: number) => String(n).padStart(2, '0')
  return (h ? h + ':' : '') + p(m) + ':' + p(ss)
}

const app = $('game'), board = $('board'), pad = $('pad')
const timerBtn = $('timer'), resumeBtn = $('resume'), notesBtn = $('notes'), hintBtn = $('hint')

let play: Play | null = null
let shape: Shape = shapeOf(9)
let cells: HTMLElement[] = []
let padBtns: HTMLButtonElement[] = []
let keys: string[] = []
let sel = -1
let notesMode = false
let paused = false
let tick = 0

/** Builds the cells and pad once per game (only when the size changes), then paints. */
export function mount(p: Play) {
  play = p
  const s = shapeOf(p.game.size)
  if (s.size !== shape.size || !cells.length) {
    shape = s
    const n = s.size
    app.dataset.n = String(n)
    app.style.setProperty('--n', String(n))
    app.style.setProperty('--bc', String(s.boxCols))
    app.style.setProperty('--br', String(s.boxRows))
    cells = Array.from({ length: n * n }, (_, i) => {
      const c = document.createElement('div')
      c.dataset.i = String(i)
      c.setAttribute('role', 'gridcell')
      const r = (i / n) | 0, col = i % n
      // r/b: thick right/bottom edge at a box boundary; x/y: last column/row, the board border draws it
      c.dataset.b = (col === n - 1 ? 'x' : (col + 1) % s.boxCols ? '' : 'r') + (r === n - 1 ? 'y' : (r + 1) % s.boxRows ? '' : 'b')
      return c
    })
    board.replaceChildren(...cells)
    padBtns = Array.from({ length: n }, (_, k) => {
      const b = document.createElement('button')
      b.dataset.d = String(k + 1)
      b.innerHTML = `${k + 1}<small></small>`
      return b
    })
    pad.replaceChildren(...padBtns)
  }
  keys = new Array(cells.length).fill('')
  sel = -1
  paused = false
  marks.locked.clear(); marks.cooling.clear(); marks.boxes.clear(); marks.fresh = -1
  if (p.timed) app.dataset.mode = 'timed'
  else delete app.dataset.mode
  $('undo').hidden = !!p.timed
  hintBtn.hidden = !p.source.hints
  timerBtn.toggleAttribute('disabled', !p.source.pausable)
  syncTimer()
  paint()
}

/** Stop input + timer (game over from outside, e.g. PvP result). */
export function stop() {
  if (play) play.game.done = true
  syncTimer()
  paint()
}

export const current = () => play?.game ?? null

// ---------- render ----------
const elapsed = () => (play?.clock ? play.clock() : play?.game.elapsed ?? 0)
const showTime = () => {
  const pausable = play?.source.pausable, t = elapsed()
  timerBtn.textContent = fmt(t) + (pausable ? (paused ? ' ▶' : ' ⏸') : '')
  timerBtn.classList.toggle('urgent', !!play?.timed && t <= 10 && !play.game.done)
  timerBtn.setAttribute('aria-label', paused ? 'Resume' : 'Pause')
}

export function paint() {
  if (!play) return
  const g = play.game
  const { peers } = geo(shape)
  const wrong = settings.check === 'rules' ? conflicts(g.values, shape) : new Set(g.wrong)
  if (play.source.strict) g.wrong.forEach((i) => g.values[i] && wrong.add(i))
  const near = new Set(sel >= 0 ? peers[sel] : [])
  const sv = sel >= 0 ? g.values[sel] : 0
  const counts = new Array(shape.size + 1).fill(0)
  const n = shape.size
  const { box } = geo(shape)

  for (let i = 0; i < cells.length; i++) {
    const v = g.values[i], m = g.notes[i], c = cells[i]
    counts[v]++
    const key = v ? 'v' + v : 'n' + m
    if (key !== keys[i]) {
      keys[i] = key
      c.innerHTML = v ? String(v)
        : m ? '<div class="notes">' + Array.from({ length: n }, (_, k) => `<span>${m & (1 << (k + 1)) ? k + 1 : ''}</span>`).join('') + '</div>'
        : ''
      c.setAttribute('aria-label', `Row ${((i / n) | 0) + 1}, column ${(i % n) + 1}, ${v || 'empty'}`)
    }
    let cls = 'c'
    if (g.puzzle[i]) cls += ' given'
    if (near.has(i)) cls += ' peer'
    if (v && v === sv) cls += ' same'
    if (wrong.has(i)) cls += ' wrong'
    if (i === sel) cls += ' sel'
    if (marks.locked.has(i)) cls += ' lock'
    if (marks.cooling.has(i)) cls += ' cool'
    if (marks.boxes.has(box[i])) cls += box[i] === marks.fresh ? ' boxdone boxnew' : ' boxdone'
    if (c.className !== cls) c.className = cls
  }
  padBtns.forEach((b, k) => {
    const left = n - counts[k + 1]
    b.lastElementChild!.textContent = String(Math.max(left, 0))
    b.classList.toggle('full', left <= 0)
  })
  $('mistakes').textContent = String(g.mistakes)
  board.classList.toggle('paused', paused)
  resumeBtn.hidden = !paused
  showTime()
}

// ---------- timer ----------
function syncTimer() {
  const g = play?.game
  const run = !!g && !g.done && !paused && !(document.hidden && play!.source.pausable) && !app.hidden
  if (run && !tick) tick = window.setInterval(() => { if (!play!.clock) play!.game.elapsed++; showTime() }, 1000)
  else if (!run && tick) { clearInterval(tick); tick = 0 }
}
export function setPaused(p: boolean) {
  if (!play || play.game.done || !play.source.pausable) p = false
  paused = p
  syncTimer()
  paint()
  if (play) play.source.save(play.game, true)
}
export function onShow() {
  syncTimer()
  if (app.hidden && play) play.source.save(play.game, true)
}
document.addEventListener('visibilitychange', () => {
  if (document.hidden && play?.source.pausable && !app.hidden) setPaused(true)
  else syncTimer()
})
addEventListener('pagehide', () => play && play.source.save(play.game, true))

// ---------- actions ----------
const changed = () => { paint(); play!.source.save(play!.game, false) }

/** Ask the source whether cell i is right. Strict sources: always (they count mistakes). Otherwise only in solution mode. */
async function verify(i: number, countMistake: boolean) {
  const p = play!, g = p.game, v = g.values[i]
  if ((!p.source.strict && settings.check !== 'solution') || !v || g.puzzle[i]) return
  const r = await p.source.checkCell(g.id, i, v)
  if (play !== p || g.values[i] !== v) return
  if (r.mistakes !== undefined) g.mistakes = r.mistakes
  if (!r.correct) {
    if (!g.wrong.includes(i)) g.wrong.push(i)
    if (countMistake && r.mistakes === undefined) g.mistakes++
  }
  changed()
}

/** Board full and rule-clean → let the source decide if it's solved. */
async function trySubmit() {
  const p = play!, g = p.game
  if (!isFull(g) || conflicts(g.values, shape).size || g.done) return
  const { solved } = await p.source.submit(g.id, g.values, elapsed())
  if (solved && play === p && !g.done) { g.done = true; syncTimer(); changed(); p.onWin(g) }
}

async function input(d: number) {
  const g = play?.game
  if (!g || paused || sel < 0 || marks.locked.has(sel) || marks.cooling.has(sel)) return
  const i = sel
  if (notesMode) { if (toggleNote(g, i, d)) changed(); return }
  if (!setValue(g, i, d)) return
  if (!play!.source.strict && settings.check === 'rules' && conflicts(g.values, shape).has(i)) g.mistakes++
  changed()
  await verify(i, true)
  await trySubmit()
}
function doErase() { const g = play?.game; if (g && !paused && sel >= 0 && !marks.locked.has(sel) && erase(g, sel)) changed() }
async function doUndo() {
  const g = play?.game
  if (!g || paused || play!.timed) return
  const i = undo(g)
  if (i < 0) return
  sel = i
  changed()
  await verify(i, false)
  await trySubmit()
}
async function doHint() {
  const p = play
  if (!p || paused || p.game.done || !p.source.hints) return
  const g = p.game
  const h = await p.source.getHint(g.id, g.values, sel)
  if (!h || play !== p) return
  sel = h.cell
  g.hints++
  if (setValue(g, h.cell, h.value)) changed()
  await trySubmit()
}
function toggleNotes() {
  notesMode = !notesMode
  notesBtn.setAttribute('aria-pressed', String(notesMode))
}
function select(i: number) {
  if (paused) return
  sel = i
  paint()
}

// ---------- events ----------
board.addEventListener('click', (e) => {
  const c = (e.target as HTMLElement).closest<HTMLElement>('[data-i]')
  if (c) select(+c.dataset.i!)
})
pad.addEventListener('click', (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>('[data-d]')
  if (b) input(+b.dataset.d!)
})
$('undo').onclick = doUndo
$('erase').onclick = doErase
hintBtn.onclick = doHint
notesBtn.onclick = toggleNotes
timerBtn.onclick = () => setPaused(!paused)
resumeBtn.onclick = () => setPaused(false)

document.addEventListener('keydown', (e) => {
  if (app.hidden || document.querySelector('dialog[open]') || (e.target as Element).closest?.('select, input')) return
  const k = e.key, n = shape.size
  if ((e.ctrlKey || e.metaKey) && k.toLowerCase() === 'z') doUndo()
  else if (e.ctrlKey || e.metaKey || e.altKey) return
  else if (k >= '1' && k <= String(n) && k.length === 1) input(+k)
  else if (k === 'Backspace' || k === 'Delete' || k === '0') doErase()
  else if (k === 'n' || k === 'N') toggleNotes()
  else if (k.startsWith('Arrow')) {
    const i = sel < 0 ? (cells.length / 2) | 0 : sel
    const r = (i / n) | 0, c = i % n
    const [dr, dc] = { ArrowUp: [-1, 0], ArrowDown: [1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1] }[k] ?? [0, 0]
    select(((r + dr + n) % n) * n + ((c + dc + n) % n))
  } else return
  e.preventDefault()
})

// ---------- settings dialog ----------
const setDlg = $<HTMLDialogElement>('settings'), setForm = setDlg.querySelector('form')!
export const applyTheme = () => {
  if (settings.theme === 'auto') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = settings.theme
}
export function openSettings() {
  ;(setForm.elements.namedItem('check') as RadioNodeList).value = settings.check
  ;(setForm.elements.namedItem('theme') as HTMLSelectElement).value = settings.theme
  setDlg.showModal()
}
setForm.addEventListener('change', () => {
  const f = new FormData(setForm)
  const switched = settings.check !== f.get('check')
  settings.check = f.get('check') as CheckMode
  settings.theme = f.get('theme') as Settings['theme']
  saveSettings()
  applyTheme()
  paint()
  // entering solution mode: check what's already on the board (not counted as new mistakes)
  if (switched && play) for (let i = 0; i < cells.length; i++) verify(i, false)
})
