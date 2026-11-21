// PvP over one WebSocket (protocol: docs/protocol.md). The server owns the puzzle, the solution,
// the clock and the result; this file only renders what it says.
import { needLogin } from './account'
import { me } from './api'
import { fmt, mount, stop } from './board'
import { LEVELS, PVP, SIZES, sizeInfo, type Level, type Size } from './config'
import { newGame, type Game } from './logic/game'
import type { Grid } from './logic/sudoku'
import type { GameSource } from './source'
import { $, dialog, esc, go, showGame, showPage } from './ui'

type Msg = Record<string, any> & { t: string }
interface Player { name: string; ready: boolean; rating: number }

// ---------- socket ----------
let ws: WebSocket | null = null
let wanted = false, retry = 0, lastPong = 0, beat = 0
const outbox: Msg[] = []

function connect() {
  wanted = true
  if (ws) return
  ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`)
  ws.onopen = () => {
    retry = 0
    lastPong = Date.now()
    for (const m of outbox.splice(0)) ws!.send(JSON.stringify(m))
    clearInterval(beat)
    // the server pings at the protocol level too; this one lets the browser notice a dead link
    beat = window.setInterval(() => {
      if (Date.now() - lastPong > 45_000) ws?.close()
      else send({ t: 'ping' })
    }, 20_000)
  }
  ws.onmessage = (e) => { lastPong = Date.now(); onMsg(JSON.parse(e.data)) }
  ws.onclose = () => {
    ws = null
    clearInterval(beat)
    if (!wanted) return
    setOpp('Reconnecting…')
    setTimeout(connect, Math.min(1000 * 2 ** retry++, 8000))
  }
}

function send(m: Msg) {
  if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(m))
  else { outbox.push(m); connect() }
}

// ---------- game source ----------
class PvpSource implements GameSource {
  readonly strict = true
  readonly hints = PVP.hints
  readonly pausable = false
  sent: Grid
  waiting = new Map<number, (r: { correct: boolean; mistakes: number }) => void>()
  hintWait: ((h: { cell: number; value: number } | null) => void) | null = null
  constructor(values: Grid) { this.sent = values.slice() }

  /** The server keeps its own copy of the board: send every cell that differs from what it has. */
  save(g: Game) {
    g.values.forEach((v, i) => {
      if (v !== this.sent[i] && !g.puzzle[i]) { this.sent[i] = v; send({ t: 'place', cell: i, value: v }) }
    })
  }
  checkCell(_id: string, cell: number) {
    return new Promise<{ correct: boolean; mistakes: number }>((res) => this.waiting.set(cell, res))
  }
  getHint() {
    send({ t: 'hint' })
    return new Promise<{ cell: number; value: number } | null>((res) => { this.hintWait = res })
  }
  async submit() { return { solved: false } } // the server ends the match itself when the board is right
}

// ---------- state ----------
let src: PvpSource | null = null
let game: Game | null = null
let room: { size: Size; level: Level; code: string; ranked: boolean; players: Player[] } | null = null
let pick: { size: Size; level: Level } = { size: 9, level: 'easy' }
let offset = 0 // server clock − local clock
let startAt = 0
let countTimer = 0

const serverNow = () => Date.now() + offset
const opponent = () => room?.players.find((p) => p.name !== me?.username)

function setOpp(text: string) { const el = document.getElementById('opp-s'); if (el) el.textContent = text }
function showErr(msg: string) { const el = document.getElementById('pvp-err'); if (el) el.textContent = msg }

function onMsg(m: Msg) {
  switch (m.t) {
    case 'pong': case 'hello': break
    case 'queued': queuePage(m.timeoutSec); break
    case 'queue_timeout':
      dialog('No opponent yet', '<p>Nobody else is looking for this board and level right now.</p>', ['Keep waiting', 'Cancel'])
        .then((i) => (i === 0 ? send({ t: 'queue', ...pick }) : go('#/pvp')))
      break
    case 'cancelled': case 'left': if (location.hash.startsWith('#/pvp')) pickPage(); break
    case 'opponent_left':
      room = null
      if (document.querySelector('#win[open]')) $('win-text').insertAdjacentHTML('beforeend', '<p>The other player left.</p>')
      else if (!game || game.done) dialog('Opponent left', '<p>The other player left the room.</p>', ['OK']).then(() => go('#/pvp'))
      break
    case 'lobby': room = m as any; lobbyPage(); break
    case 'countdown': offset = m.now - Date.now(); startAt = m.startAt; countdownPage(); break
    case 'start': offset = m.now - Date.now(); startAt = m.startAt; startGame(m.puzzle, m.puzzle, 0); break
    case 'state':
      room = m as any
      offset = m.now - Date.now()
      if (m.phase === 'lobby') lobbyPage()
      else if (m.phase === 'countdown') { startAt = m.startAt; countdownPage() }
      else if (m.phase === 'playing') {
        startAt = m.startAt
        startGame(m.puzzle, m.values, m.mistakes)
        if (m.opponent) oppUpdate(m.opponent.progress, m.opponent.mistakes)
        if (m.opponent && !m.opponent.connected) setOpp('Opponent disconnected')
      }
      break
    case 'checked': {
      const w = src?.waiting.get(m.cell)
      src?.waiting.delete(m.cell)
      if (w) w(m as any)
      else if (game && m.mistakes > game.mistakes) { game.mistakes = m.mistakes }
      break
    }
    case 'hinted':
      if (src) { src.sent[m.cell] = m.value; src.hintWait?.({ cell: m.cell, value: m.value }); src.hintWait = null }
      break
    case 'opponent': oppUpdate(m.progress, m.mistakes); break
    case 'opponent_away': setOpp(`Opponent disconnected, ${m.graceSec}s to come back`); break
    case 'opponent_back': setOpp(''); break
    case 'result': result(m); break
    case 'rematch':
      if (m.from !== me?.username) {
        const t = document.getElementById('win-text')
        if (t && !t.querySelector('.rq')) t.insertAdjacentHTML('beforeend', `<p class="rq"><b>${esc(m.from)}</b> wants a rematch.</p>`)
      }
      break
    case 'replaced':
      wanted = false
      dialog('Opened elsewhere', '<p>PvP is now open in another tab or device.</p>', ['OK']).then(() => go('#/'))
      break
    case 'error': showErr(m.msg); break
  }
}

// ---------- pages ----------
const backBtn = '<a href="#/" class="btn icon" aria-label="Back">←</a>'

export function pvpPage(arg: string) {
  if (needLogin('#/pvp' + (arg ? '/' + arg : ''))) return
  connect()
  if (game && !game.done && room) return startGame(game.puzzle, game.values, game.mistakes) // back to a running match
  if (room) return lobbyPage()
  if (arg) { send({ t: 'join', code: arg }); return waitPage('Joining room…') }
  pickPage()
}

function pickPage() {
  game = null
  showPage(`
    <div class="top">${backBtn}<h1 tabindex="-1">PvP</h1></div>
    <div class="sizes" role="radiogroup" aria-label="Board size">${SIZES.map((s) => {
      const i = sizeInfo(s)
      return `<button class="tile${s === pick.size ? ' on' : ''}" data-size="${s}" role="radio" aria-checked="${s === pick.size}">
        <span class="mini" style="--m:${s};--bc:${i.boxCols};--br:${i.boxRows}"></span>
        <b>${i.name} ${s}×${s}</b><small>About ${i.minutes} min</small></button>`
    }).join('')}</div>
    <label class="spread">Level <select id="p-level">${Object.entries(LEVELS).map(([k, l]) =>
      `<option value="${k}"${k === pick.level ? ' selected' : ''}>${l}</option>`).join('')}</select></label>
    <div class="row"><button class="primary" id="p-quick">Quick match</button><button id="p-room">Private room</button></div>
    <p class="muted">Quick match is ranked (Elo per board size). Private rooms are just for fun.</p>
    <form class="row" id="p-join"><input type="text" name="code" placeholder="Room code" maxlength="6" autocapitalize="characters" autocomplete="off" spellcheck="false" aria-label="Room code" style="width:8rem"><button>Join</button></form>
    <p class="err" id="pvp-err" role="alert"></p>`)
  document.querySelector('.sizes')!.addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest<HTMLElement>('[data-size]')
    if (!b) return
    pick.size = +b.dataset.size! as Size
    document.querySelectorAll('.sizes .tile').forEach((t) => {
      const on = t === b
      t.classList.toggle('on', on)
      t.setAttribute('aria-checked', String(on))
    })
  })
  $('p-level').onchange = (e) => { pick.level = (e.target as HTMLSelectElement).value as Level }
  $('p-quick').onclick = () => send({ t: 'queue', ...pick })
  $('p-room').onclick = () => send({ t: 'create', ...pick })
  $<HTMLFormElement>('p-join').onsubmit = (e) => {
    e.preventDefault()
    const code = new FormData(e.target as HTMLFormElement).get('code')
    if (code) send({ t: 'join', code: String(code) })
  }
}

function waitPage(text: string) {
  showPage(`<div class="top">${backBtn}<h1 tabindex="-1">PvP</h1></div><p>${text}</p><p class="err" id="pvp-err" role="alert"></p>`)
}

function queuePage(timeoutSec: number) {
  showPage(`
    <div class="top"><h1 tabindex="-1">Finding an opponent…</h1></div>
    <p>${sizeInfo(pick.size).name} ${pick.size}×${pick.size} · ${LEVELS[pick.level]}</p>
    <p class="muted">Waiting up to ${timeoutSec}s.</p>
    <button id="p-cancel">Cancel</button>`)
  $('p-cancel').onclick = () => send({ t: 'cancel' })
}

function lobbyPage() {
  if (!room) return
  const r = room, mine = r.players.find((p) => p.name === me?.username)
  const link = `${location.origin}/#/pvp/${r.code}`
  showPage(`
    <div class="top"><h1 tabindex="-1">${r.ranked ? 'Ranked match' : 'Private room'}</h1></div>
    <p>${sizeInfo(r.size).name} ${r.size}×${r.size} · ${LEVELS[r.level]}</p>
    ${r.ranked ? '' : `<p>Room code <b class="code">${esc(r.code)}</b></p>
      <div class="row"><button id="p-copy">Copy invite link</button></div>`}
    <table class="list">${r.players.map((p) => `<tr><td>${esc(p.name)}${p.name === me?.username ? ' (you)' : ''}</td><td>${p.rating}</td><td>${p.ready ? '✓ Ready' : '…'}</td></tr>`).join('')}
      ${r.players.length < 2 ? '<tr><td colspan="3" class="muted">Waiting for a second player…</td></tr>' : ''}</table>
    <div class="row"><button class="primary" id="p-ready"${mine?.ready ? ' disabled' : ''}>${mine?.ready ? 'Waiting for opponent…' : 'Ready'}</button><button id="p-leave">Leave</button></div>
    <p class="err" id="pvp-err" role="alert"></p>`)
  $('p-ready').onclick = () => send({ t: 'ready' })
  $('p-leave').onclick = () => send({ t: 'leave' })
  const copy = document.getElementById('p-copy')
  if (copy) copy.onclick = () => navigator.clipboard?.writeText(link).then(() => { copy.textContent = 'Copied' }, () => { copy.textContent = link })
}

function countdownPage() {
  clearInterval(countTimer)
  showPage(`<div class="countdown" aria-live="assertive"><h1 tabindex="-1" id="p-count"></h1><p>Get ready</p></div>`)
  const draw = () => {
    const s = Math.ceil((startAt - serverNow()) / 1000)
    const el = document.getElementById('p-count')
    if (!el || s <= 0) return clearInterval(countTimer)
    el.textContent = String(s)
  }
  draw()
  countTimer = window.setInterval(draw, 200)
}

function startGame(puzzle: Grid, values: Grid, mistakes: number) {
  clearInterval(countTimer)
  const r = room!
  game = newGame({ id: 'pvp-' + r.code, size: r.size, level: r.level, puzzle })
  game.values = values.slice()
  game.mistakes = mistakes
  src = new PvpSource(values)
  const o = opponent()
  showGame(`<span class="opp"><b>${esc(o?.name ?? 'Opponent')}</b><meter id="opp-p" min="0" max="100" value="0" aria-label="Opponent progress"></meter><span id="opp-m">0 ✗</span></span>
    <button id="p-resign">Resign</button>`, `${r.size}×${r.size} · ${LEVELS[r.level]}`)
  document.getElementById('game-info')!.insertAdjacentHTML('beforeend', ' <small id="opp-s" class="muted"></small>')
  $('p-resign').onclick = async () => {
    if (await dialog('Resign?', '<p>Your opponent wins this match.</p>', ['Resign', 'Keep playing']) === 0) send({ t: 'resign' })
  }
  mount({ game, source: src, onWin: () => {}, clock: () => Math.max(0, Math.floor((serverNow() - startAt) / 1000)) })
}

function oppUpdate(progress: number, mistakes: number) {
  const p = document.getElementById('opp-p') as HTMLMeterElement | null
  if (p) { p.value = progress; p.title = progress + '% correct' }
  const m = document.getElementById('opp-m')
  if (m) m.textContent = `${mistakes} ✗`
}

async function result(m: Msg) {
  if (game) { game.done = true; stop() }
  const won = m.winner === me?.username
  const why = m.reason === 'resign' ? ' (resigned)' : m.reason === 'disconnect' ? ' (disconnected)' : ''
  const title = !m.winner ? 'Match over' : won ? 'You won! 🎉' : `${m.winner} won`
  const rows = (m.players as any[]).map((p) => `<tr><td>${esc(p.name)}${p.name === m.winner ? ' 🏆' : ''}</td><td>${p.ms == null ? '–' : fmt(Math.round(p.ms / 1000))}</td><td>${p.mistakes}</td>${m.ranked ? `<td>${p.rating} (${p.delta >= 0 ? '+' : ''}${p.delta})</td>` : ''}</tr>`).join('')
  const i = await dialog(title, `<p class="muted">${m.reason === 'solved' ? 'First correct board wins.' : 'Match ended' + why + '.'}</p>
    <table class="list"><tr><th>Player</th><th>Time</th><th>✗</th>${m.ranked ? '<th>Rating</th>' : ''}</tr>${rows}</table>`,
    ['Rematch', 'New match', 'Home'])
  if (i === 0 && room) { send({ t: 'rematch' }); setOpp('Waiting for opponent…') }
  else { send({ t: 'leave' }); room = null; go(i === 1 ? '#/pvp' : '#/') }
}
