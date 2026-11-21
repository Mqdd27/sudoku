import { LEVELS, type Level } from './config'
import { conflicts, PEERS } from './logic/sudoku'
import { erase, isFull, newGame, setValue, toggleNote, undo, type CheckMode, type Game } from './logic/game'
import { LocalGameSource, type GameSource } from './source'
import { load, store } from './storage'

// The only place that picks a source; a server-backed GameSource drops in here.
const source: GameSource = new LocalGameSource()

type Stats = Partial<Record<Level, { solved: number; best: number }>>
const settings = load<{ check: CheckMode; theme: 'auto' | 'light' | 'dark' }>('settings', { check: 'rules', theme: 'auto' })
const stats = load<Stats>('stats', {})
let game: Game | null = load<Game | null>('game', null)
if (!game || game.done || !game.id || game.values?.length !== 81 || !(game.level in LEVELS)) game = null

let sel = -1
let notesMode = false
let paused = false
let tick = 0

// ---------- DOM ----------
const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T
const board = $('board'), pad = $('pad'), levelSel = $<HTMLSelectElement>('level'), newBtn = $<HTMLButtonElement>('new')
const timerBtn = $('timer'), resumeBtn = $('resume'), notesBtn = $('notes')
const winDlg = $<HTMLDialogElement>('win'), setDlg = $<HTMLDialogElement>('settings')
const setForm = setDlg.querySelector('form')!

for (const [k, v] of Object.entries(LEVELS)) levelSel.add(new Option(v.label, k))

const cells: HTMLElement[] = []
const keys: string[] = new Array(81).fill('') // last rendered content per cell
for (let i = 0; i < 81; i++) {
  const c = document.createElement('div')
  c.className = 'c'
  c.dataset.i = String(i)
  c.setAttribute('role', 'gridcell')
  cells.push(c)
}
board.append(...cells)

const padBtns: HTMLButtonElement[] = []
for (let d = 1; d <= 9; d++) {
  const b = document.createElement('button')
  b.dataset.d = String(d)
  b.innerHTML = `${d}<small></small>`
  padBtns.push(b)
}
pad.append(...padBtns)

// ---------- render ----------
const fmt = (s: number) => {
  const h = (s / 3600) | 0, m = ((s / 60) | 0) % 60, ss = s % 60
  const p = (n: number) => String(n).padStart(2, '0')
  return (h ? h + ':' : '') + p(m) + ':' + p(ss)
}
const showTime = () => {
  timerBtn.textContent = `${fmt(game?.elapsed ?? 0)} ${paused ? '▶' : '⏸'}`
  timerBtn.setAttribute('aria-label', paused ? 'Resume' : 'Pause')
}

function paint() {
  if (!game) return
  const g = game
  const wrong = settings.check === 'rules' ? conflicts(g.values) : new Set(g.wrong)
  const peers = new Set(sel >= 0 ? PEERS[sel] : [])
  const sv = sel >= 0 ? g.values[sel] : 0
  const counts = new Array(10).fill(0)

  for (let i = 0; i < 81; i++) {
    const v = g.values[i], n = g.notes[i], c = cells[i]
    counts[v]++
    const key = v ? 'v' + v : 'n' + n
    if (key !== keys[i]) {
      keys[i] = key
      c.innerHTML = v ? String(v)
        : n ? '<div class="notes">' + [1, 2, 3, 4, 5, 6, 7, 8, 9].map((d) => `<span>${n & (1 << d) ? d : ''}</span>`).join('') + '</div>'
        : ''
      c.setAttribute('aria-label', `Row ${((i / 9) | 0) + 1}, column ${(i % 9) + 1}, ${v || 'empty'}`)
    }
    let cls = 'c'
    if (g.puzzle[i]) cls += ' given'
    if (peers.has(i)) cls += ' peer'
    if (v && v === sv) cls += ' same'
    if (wrong.has(i)) cls += ' wrong'
    if (i === sel) cls += ' sel'
    if (c.className !== cls) c.className = cls
  }
  for (let d = 1; d <= 9; d++) {
    const left = 9 - counts[d]
    padBtns[d - 1].lastElementChild!.textContent = String(Math.max(left, 0))
    padBtns[d - 1].classList.toggle('full', left <= 0)
  }
  $('mistakes').textContent = String(g.mistakes)
  $('level-label').textContent = LEVELS[g.level].label
  board.classList.toggle('paused', paused)
  resumeBtn.hidden = !paused
  showTime()
}

// ---------- timer ----------
function syncTimer() {
  const run = !!game && !game.done && !paused && !document.hidden
  if (run && !tick) tick = window.setInterval(() => { game!.elapsed++; showTime() }, 1000)
  else if (!run && tick) { clearInterval(tick); tick = 0 }
}
function setPaused(p: boolean) {
  if (!game || game.done) p = false
  paused = p
  syncTimer()
  paint()
  if (game) store('game', game)
}
document.addEventListener('visibilitychange', () => {
  if (document.hidden) setPaused(true)
  else syncTimer()
})
addEventListener('pagehide', () => game && store('game', game))

// ---------- actions ----------
const save = () => { paint(); store('game', game) }

/** Ask the source whether cell i is right (solution mode only). Ignores stale answers. */
async function verify(i: number, countMistake: boolean) {
  const g = game!, v = g.values[i]
  if (settings.check !== 'solution' || !v || g.puzzle[i]) return
  const ok = await source.checkCell(g.id, i, v)
  if (game !== g || g.values[i] !== v || ok) return
  if (!g.wrong.includes(i)) g.wrong.push(i)
  if (countMistake) g.mistakes++
  save()
}

/** Board full and rule-clean → let the source decide if it's solved. */
async function trySubmit() {
  const g = game!
  if (!isFull(g) || conflicts(g.values).size) return
  const { solved } = await source.submit(g.id, g.values, g.elapsed)
  if (solved && game === g && !g.done) { g.done = true; save(); win() }
}

async function input(d: number) {
  if (!game || paused || sel < 0) return
  const i = sel
  if (notesMode) { if (toggleNote(game, i, d)) save(); return }
  if (!setValue(game, i, d)) return
  if (settings.check === 'rules' && conflicts(game.values).has(i)) game.mistakes++
  save()
  await verify(i, true)
  await trySubmit()
}
function doErase() { if (game && !paused && sel >= 0 && erase(game, sel)) save() }
async function doUndo() {
  if (!game || paused) return
  const i = undo(game)
  if (i < 0) return
  sel = i
  save()
  await verify(i, false)
  await trySubmit()
}
async function doHint() {
  if (!game || paused || game.done) return
  const g = game
  const h = await source.getHint(g.id, g.values, sel)
  if (!h || game !== g) return
  sel = h.cell
  if (setValue(g, h.cell, h.value)) save()
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

function win() {
  syncTimer()
  const g = game!
  const s = (stats[g.level] ??= { solved: 0, best: 0 })
  s.solved++
  const record = !s.best || g.elapsed < s.best
  if (record) s.best = g.elapsed
  store('stats', stats)
  $('win-text').innerHTML =
    `Level: <b>${LEVELS[g.level].label}</b><br>Time: <b>${fmt(g.elapsed)}</b>${record ? ' (new best!)' : ''}<br>Mistakes: <b>${g.mistakes}</b>`
  winDlg.showModal()
}

async function startNew() {
  if (game && !game.done && game.history.length && !confirm('Abandon the current game?')) return
  newBtn.disabled = true
  newBtn.textContent = 'Generating…'
  game = newGame(await source.start(levelSel.value as Level))
  newBtn.disabled = false
  newBtn.textContent = 'New Game'
  sel = -1
  paused = false
  syncTimer()
  save()
}

// ---------- events ----------
board.addEventListener('click', (e) => {
  const c = (e.target as HTMLElement).closest<HTMLElement>('.c')
  if (c) select(+c.dataset.i!)
})
pad.addEventListener('click', (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>('[data-d]')
  if (b) input(+b.dataset.d!)
})
$('undo').onclick = doUndo
$('erase').onclick = doErase
$('hint').onclick = doHint
notesBtn.onclick = toggleNotes
timerBtn.onclick = () => setPaused(!paused)
resumeBtn.onclick = () => setPaused(false)
newBtn.onclick = startNew
$('win-new').onclick = () => { game = null; startNew() }

document.addEventListener('keydown', (e) => {
  if (document.querySelector('dialog[open]') || (e.target as Element).closest?.('select, input')) return
  const k = e.key
  if ((e.ctrlKey || e.metaKey) && k.toLowerCase() === 'z') doUndo()
  else if (e.ctrlKey || e.metaKey || e.altKey) return
  else if (k >= '1' && k <= '9' && k.length === 1) input(+k)
  else if (k === 'Backspace' || k === 'Delete' || k === '0') doErase()
  else if (k === 'n' || k === 'N') toggleNotes()
  else if (k.startsWith('Arrow')) {
    const i = sel < 0 ? 40 : sel
    const r = (i / 9) | 0, c = i % 9
    const [dr, dc] = { ArrowUp: [-1, 0], ArrowDown: [1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1] }[k] ?? [0, 0]
    select(((r + dr + 9) % 9) * 9 + ((c + dc + 9) % 9))
  } else return
  e.preventDefault()
})

// ---------- settings ----------
const applyTheme = () => {
  if (settings.theme === 'auto') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = settings.theme
}
$('settings-btn').onclick = () => {
  ;(setForm.elements.namedItem('check') as RadioNodeList).value = settings.check
  ;(setForm.elements.namedItem('theme') as HTMLSelectElement).value = settings.theme
  $('stats').innerHTML = '<tr><th>Level</th><th>Solved</th><th>Best</th></tr>' +
    Object.entries(LEVELS).map(([k, v]) => {
      const s = stats[k as Level]
      return `<tr><td>${v.label}</td><td>${s?.solved ?? 0}</td><td>${s?.best ? fmt(s.best) : '–'}</td></tr>`
    }).join('')
  setDlg.showModal()
}
setForm.addEventListener('change', () => {
  const f = new FormData(setForm)
  const switched = settings.check !== f.get('check')
  settings.check = f.get('check') as CheckMode
  settings.theme = f.get('theme') as typeof settings.theme
  store('settings', settings)
  applyTheme()
  paint()
  // entering solution mode: check what's already on the board (not counted as new mistakes)
  if (switched && game) for (let i = 0; i < 81; i++) verify(i, false)
})

// ---------- boot ----------
applyTheme()
;(async () => {
  if (game && (await source.resume(game.id))) {
    levelSel.value = game.level
    save()
    syncTimer()
  } else {
    game = null
    startNew()
  }
})()

if ('serviceWorker' in navigator && import.meta.env.PROD) navigator.serviceWorker.register('/sw.js')
