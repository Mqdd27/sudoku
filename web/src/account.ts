// Sign in / register, profile (stats, devices, delete), Classic stats import, leaderboard.
import { api, ApiError, me, pvpOff, setMe, type User } from './api'
import { fmt } from './board'
import { LEVELS, SIZES, TA, type Level } from './config'
import { load, store } from './storage'
import { $, dialog, esc, go, showPage } from './ui'

let afterLogin = '#/'
/** Send a guest to the sign-in page, then back to `back`. Returns false when already signed in. */
export function needLogin(back: string) {
  if (me) return false
  afterLogin = back
  go('#/login')
  return true
}

const back = (to = '#/') => `<a href="${to}" class="btn icon" aria-label="Back">←</a>`
const date = (ms: number) => new Date(ms).toLocaleDateString()
const msTime = (ms: number | null | undefined) => (ms ? fmt(Math.round(ms / 1000)) : '–')

export function loginPage() {
  let register = false
  const draw = () => {
    showPage(`
      <div class="top">${back()}<h1 tabindex="-1">${register ? 'Create account' : 'Sign in'}</h1></div>
      <form class="stack" id="auth">
        <label>Username <input type="text" name="username" autocomplete="username" required minlength="3" maxlength="20" pattern="[A-Za-z0-9_.\\-]+" autocapitalize="none" spellcheck="false"></label>
        <label>Password <input type="password" name="password" autocomplete="${register ? 'new-password' : 'current-password'}" required minlength="${register ? 8 : 1}"></label>
        ${register ? '<label>Email <span class="muted">(optional)</span> <input type="email" name="email" autocomplete="email"></label>' : ''}
        <p class="err" id="auth-err" role="alert"></p>
        <button class="primary">${register ? 'Create account' : 'Sign in'}</button>
        <button type="button" id="auth-switch">${register ? 'I already have an account' : 'Create an account'}</button>
      </form>`)
    $('auth-switch').onclick = () => { register = !register; draw() }
    const form = $<HTMLFormElement>('auth')
    form.onsubmit = async (e) => {
      e.preventDefault()
      const f = Object.fromEntries(new FormData(form)) as Record<string, string>
      if (!f.email) delete f.email
      const btn = form.querySelector<HTMLButtonElement>('button.primary')!
      btn.disabled = true
      try {
        setMe(await api<User>('POST', register ? '/register' : '/login', f))
        await offerImport()
        go(afterLogin)
        afterLogin = '#/'
      } catch (err) {
        $('auth-err').textContent = (err as Error).message
        btn.disabled = false
      }
    }
  }
  draw()
}

/** First sign-in on a device with local Classic stats: offer to copy them to the account (once). */
async function offerImport() {
  const stats = load<Record<string, Record<string, { solved: number; best: number }>>>('stats', {})
  const games = Object.values(stats).flatMap((l) => Object.values(l)).reduce((n, s) => n + (s?.solved || 0), 0)
  if (!me || me.imported || load('imported', false) || !games) return
  const yes = await dialog('Import your stats?', `<p>This device has <b>${games}</b> solved Classic game${games > 1 ? 's' : ''}. Add them to your account?</p>`, ['Import', 'Not now'])
  store('imported', true) // ask once per device either way
  if (yes !== 0) return
  try { await api('POST', '/classic/import', { stats }); me.imported = true } catch { /* already imported elsewhere */ }
}

interface Profile {
  username: string; createdAt: number
  classic: { size: number; level: Level; solved: number; bestMs: number | null }[]
  story: { completed: number; stars: number; total: number }
  pvp: { mode: string; size: number; rating: number; wins: number; losses: number; draws: number }[]
  timeAttack: { wins: number; losses: number; draws: number; bestBoxes: number; avgBoxes: number }
}
interface Session { id: string; current: boolean; lastSeen: number; userAgent: string; ip: string }

export async function profilePage(name: string) {
  if (!name) { if (needLogin('#/profile')) return; name = me!.username }
  let p: Profile
  try { p = await api<Profile>('GET', '/profile/' + encodeURIComponent(name)) } catch (e) {
    showPage(`<div class="top">${back()}<h1 tabindex="-1">Profile</h1></div><p class="err">${esc((e as Error).message)}</p>`)
    return
  }
  const own = me?.username.toLowerCase() === p.username.toLowerCase()
  const rows = (cells: string[][]) => cells.map((r) => `<tr>${r.map((c) => `<td>${c}</td>`).join('')}</tr>`).join('')
  showPage(`
    <div class="top">${back()}<h1 tabindex="-1">${esc(p.username)}</h1></div>
    <p class="muted">Joined ${date(p.createdAt)}</p>
    <h2>Classic</h2>
    ${p.classic.length ? `<table class="list"><tr><th>Board</th><th>Level</th><th>Solved</th><th>Best</th></tr>${rows(p.classic.map((c) =>
      [`${c.size}×${c.size}`, esc(LEVELS[c.level] ?? c.level), String(c.solved), msTime(c.bestMs)]))}</table>` : '<p class="muted">No games yet.</p>'}
    <h2>Story</h2>
    <p>${p.story.completed} / ${p.story.total} levels · ${p.story.stars} ★</p>
    ${pvpOff ? '' : `<h2>PvP</h2>
    ${p.pvp.length ? `<table class="list"><tr><th>Mode</th><th>Board</th><th>Rating</th><th>W / L / D</th></tr>${rows(p.pvp.map((r) =>
      [r.mode === 'ta' ? 'Time Attack' : 'Race', `${r.size}×${r.size}`, String(r.rating), `${r.wins} / ${r.losses} / ${r.draws}`]))}</table>` : '<p class="muted">No matches yet.</p>'}
    ${p.timeAttack.wins + p.timeAttack.losses + p.timeAttack.draws ? `<p>Time Attack: ${p.timeAttack.wins} won, ${p.timeAttack.losses} lost, ${p.timeAttack.draws} drawn ·
      best ${p.timeAttack.bestBoxes} boxes · average ${p.timeAttack.avgBoxes.toFixed(1)} boxes</p>` : ''}`}
    ${own ? `
      <h2>Devices</h2><div id="devices" class="muted">Loading…</div>
      <div class="row"><button id="logout">Sign out</button><button id="others">Sign out other devices</button><button id="delete">Delete account</button></div>` : ''}`)
  if (!own) return
  const devices = async () => {
    const list = await api<Session[]>('GET', '/sessions')
    $('devices').classList.remove('muted')
    $('devices').innerHTML = `<table class="list">${rows(list.map((s) => [
      `${esc(s.userAgent.replace(/\s*\(.*?\)/g, ' ').slice(0, 60) || 'Unknown device')}<br><small class="muted">${esc(s.ip)} · ${date(s.lastSeen)}</small>`,
      s.current ? '<small>This device</small>' : `<button data-sid="${esc(s.id)}">Sign out</button>`]))}</table>`
    return list
  }
  let list = await devices().catch(() => [] as Session[])
  $('devices').onclick = async (e) => {
    const id = (e.target as HTMLElement).dataset.sid
    if (id) { await api('DELETE', '/sessions/' + id); list = await devices() }
  }
  $('others').onclick = async () => {
    await Promise.all(list.filter((s) => !s.current).map((s) => api('DELETE', '/sessions/' + s.id)))
    list = await devices()
  }
  $('logout').onclick = async () => { await api('POST', '/logout').catch(() => {}); setMe(null); go('#/') }
  $('delete').onclick = async () => {
    const i = await dialog('Delete account?', `<p>This removes your account, stats, Story progress and ratings for good.</p>
      <label class="stack">Password <input type="password" id="del-pw" autocomplete="current-password"></label>`, ['Delete forever', 'Cancel'])
    if (i !== 0) return
    try {
      await api('DELETE', '/account', { password: ($('del-pw') as HTMLInputElement).value })
      setMe(null)
      go('#/')
    } catch (e) { dialog('Not deleted', `<p>${esc((e as ApiError).message)}</p>`, ['OK']) }
  }
}

interface BoardRow { username: string; value: number; wins?: number; losses?: number; cells?: number }

export function leaderboardPage() {
  const s = { kind: pvpOff ? 'classic' : 'race', size: '9', level: 'easy', story: '1', minutes: '3' }
  showPage(`
    <div class="top">${back()}<h1 tabindex="-1">Leaderboard</h1></div>
    <form class="row" id="lb">
      <select name="kind" aria-label="Mode">${pvpOff ? '' : '<option value="race">Race rating</option><option value="ta">Time Attack rating</option><option value="tarec">Time Attack records</option>'}<option value="classic">Classic best time</option><option value="story">Story best time</option></select>
      <select name="size" aria-label="Board size">${SIZES.map((n) => `<option value="${n}"${n === 9 ? ' selected' : ''}>${n}×${n}</option>`).join('')}</select>
      <select name="level" aria-label="Level">${Object.entries(LEVELS).map(([k, l]) => `<option value="${k}">${l}</option>`).join('')}</select>
      <select name="minutes" aria-label="Duration">${[...new Set(Object.values(TA.minutes).flat())].sort((a, b) => a - b).map((n) => `<option value="${n}"${n === 3 ? ' selected' : ''}>${n} min</option>`).join('')}</select>
      <label>Level <input type="number" name="story" min="1" value="1" style="width:5rem;min-height:44px"></label>
    </form>
    <div id="lb-out"></div>`)
  const form = $<HTMLFormElement>('lb')
  const draw = async () => {
    Object.assign(s, Object.fromEntries(new FormData(form)))
    const el = (n: string) => form.elements.namedItem(n) as HTMLElement
    el('size').hidden = s.kind === 'story'
    el('level').hidden = s.kind !== 'classic' && s.kind !== 'tarec'
    el('minutes').hidden = s.kind !== 'tarec'
    el('story').parentElement!.hidden = s.kind !== 'story'
    const rating = s.kind === 'race' || s.kind === 'ta', rec = s.kind === 'tarec'
    const q = rating ? `kind=pvp&mode=${s.kind}&size=${s.size}`
      : rec ? `kind=ta&size=${s.size}&level=${s.level}&minutes=${s.minutes}`
      : s.kind === 'classic' ? `kind=classic&size=${s.size}&level=${s.level}` : `kind=story&level=${s.story}`
    const out = $('lb-out')
    try {
      const rows = await api<BoardRow[]>('GET', `/leaderboard?${q}`)
      const head = rating ? 'Rating</th><th>W / L' : rec ? 'Boxes</th><th>Cells' : 'Time'
      const cells = (r: BoardRow) => rating ? `<td>${r.value}</td><td>${r.wins} / ${r.losses}</td>`
        : rec ? `<td>${r.value}/${s.size}</td><td>${r.cells}</td>` : `<td>${msTime(r.value)}</td>`
      out.innerHTML = rows.length ? `<table class="list"><tr><th>#</th><th>Player</th><th>${head}</th></tr>${rows.map((r, i) =>
        `<tr><td>${i + 1}</td><td><a href="#/profile/${encodeURIComponent(r.username)}">${esc(r.username)}</a></td>${cells(r)}</tr>`).join('')}</table>`
        : '<p class="muted">Nobody here yet.</p>'
    } catch (e) { out.innerHTML = `<p class="err">${esc((e as Error).message)}</p>` }
  }
  form.onchange = draw
  form.onsubmit = (e) => e.preventDefault()
  draw()
}
