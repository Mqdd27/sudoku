import { leaderboardPage, loginPage, profilePage } from './account'
import { api, hasServer, me, pvpOff, whoAmI } from './api'
import { LEVELS, SIZES, type Level, type Size } from './config'
import { applyTheme, fmt, mount, openSettings, settings, saveSettings } from './board'
import { newGame, type Game } from './logic/game'
import { LocalGameSource } from './source'
import { load, store } from './storage'
import { pvpPage } from './pvp'
import { storyPage } from './story'
import { $, dialog, esc, go, offline, showGame, showPage } from './ui'

type Stats = Record<string, Partial<Record<Level, { solved: number; best: number }>>>
const local = new LocalGameSource()
let classic: Game | null = null

// ---------- pages ----------
function home() {
  if (!hasServer) {
    showPage(`<div class="top"><h1 tabindex="-1">Sudoku</h1></div>
      <nav class="tiles"><a class="tile" href="#/classic"><b>Classic</b><small>4×4, 6×6 or 9×9, plays offline</small></a></nav>`)
    return
  }
  const net = offline() ? ' disabled aria-disabled="true"' : ''
  const note = offline() ? '<small>Needs a connection</small>' : ''
  const acct = offline() ? '' : me
    ? `<a class="btn" href="#/profile">${esc(me.username)}</a>`
    : '<a class="btn" href="#/login">Sign in</a>'
  showPage(`
    <div class="top"><h1 tabindex="-1">Sudoku</h1>${acct}</div>
    <nav class="tiles">
      <a class="tile" href="#/classic"><b>Classic</b><small>4×4, 6×6 or 9×9, plays offline</small></a>
      <a class="tile" href="#/story"${net}><b>Story</b>${note || '<small>40 hand-picked levels</small>'}</a>
      ${pvpOff ? '' : `<a class="tile" href="#/pvp"${net}><b>PvP</b>${note || '<small>Race a friend or a stranger</small>'}</a>`}
      <a class="tile" href="#/leaderboard"${net}><b>Leaderboard</b>${note}</a>
    </nav>`)
}

const sizeSel = (v: Size) => `<select id="c-size" aria-label="Board size">${SIZES.map((s) =>
  `<option value="${s}"${s === v ? ' selected' : ''}>${s}×${s}</option>`).join('')}</select>`
const levelSel = (v: Level) => `<select id="c-level" aria-label="Level">${Object.entries(LEVELS).map(([k, l]) =>
  `<option value="${k}"${k === v ? ' selected' : ''}>${l}</option>`).join('')}</select>`

async function classicPage() {
  if (!classic) {
    const saved = load<Game | null>('game', null)
    if (saved && !saved.done && saved.id && saved.values?.length === saved.size ** 2 && await local.resume(saved.id)) classic = saved
  }
  if (!classic) return newClassic(true)
  openClassic(classic)
}

function openClassic(g: Game) {
  classic = g
  showGame(`${sizeSel(g.size)}${levelSel(g.level)}<button id="c-new" class="primary">New</button>`,
    `${g.size}×${g.size} · ${LEVELS[g.level]}`)
  $('c-new').onclick = () => newClassic(false)
  mount({ game: g, source: local, onWin: classicWon })
}

async function newClassic(force: boolean) {
  const size = +(($('c-size') as HTMLSelectElement | null)?.value ?? settings.size ?? 9) as Size
  const level = (($('c-level') as HTMLSelectElement | null)?.value ?? settings.level ?? 'easy') as Level
  if (!force && classic && !classic.done && classic.history.length &&
    await dialog('Start a new game?', '<p>The current board will be lost.</p>', ['New game', 'Keep playing']) !== 0) return
  settings.size = size; settings.level = level; saveSettings()
  const btn = $('c-new') as HTMLButtonElement | null
  if (btn) { btn.disabled = true; btn.textContent = '…' }
  const g = newGame(await local.start(size, level))
  local.save(g)
  openClassic(g)
}

async function classicWon(g: Game) {
  const stats = load<Stats>('stats', {})
  const s = ((stats[g.size] ??= {})[g.level] ??= { solved: 0, best: 0 })
  s.solved++
  const record = !s.best || g.elapsed < s.best
  if (record) s.best = g.elapsed
  store('stats', stats)
  if (me) api('POST', '/classic/result', { size: g.size, level: g.level, ms: g.elapsed * 1000 }).catch(() => {})
  const i = await dialog('Solved! 🎉',
    `<p>${g.size}×${g.size} · <b>${esc(LEVELS[g.level])}</b><br>Time: <b>${fmt(g.elapsed)}</b>${record ? ' (new best!)' : ''}<br>Mistakes: <b>${g.mistakes}</b></p>`,
    ['New game', 'Home'])
  if (i === 0) newClassic(true)
  else if (i === 1) go('#/')
}

// ---------- router ----------
const routes: Record<string, (arg: string) => void> = {
  '': home,
  classic: classicPage,
  story: storyPage,
  pvp: pvpPage,
  leaderboard: leaderboardPage,
  login: loginPage,
  profile: profilePage,
}
function route() {
  const [, name = '', arg = ''] = location.hash.split('/')
  const ok = name === 'classic' || (hasServer && !(pvpOff && name === 'pvp'))
  ;((ok ? routes[name] : home) ?? home)(decodeURIComponent(arg))
}
addEventListener('hashchange', route)
// connection changes only matter on menu pages (they grey out Story/PvP); never reset a running game
const refresh = () => { if (!$('page').hidden) route() }
addEventListener('online', refresh)
addEventListener('offline', refresh)
$('settings-btn').onclick = openSettings

applyTheme()
route()
// who is signed in only changes the Home header; Classic never waits for the network
whoAmI().then((u) => {
  const h = location.hash.replace(/^#\/?/, '')
  if ($('game').hidden && (!hasServer || (pvpOff && h.startsWith('pvp')) || ((u || pvpOff) && !h))) route()
})

if ('serviceWorker' in navigator && import.meta.env.PROD) navigator.serviceWorker.register('/sw.js')

