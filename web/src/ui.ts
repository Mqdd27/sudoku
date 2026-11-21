// Tiny DOM helpers shared by the pages.
import { onShow } from './board'

export const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T

const ESC: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }
/** Escape anything that came from a user or the server before putting it in innerHTML. */
export const esc = (v: unknown) => String(v ?? '').replace(/[&<>"']/g, (c) => ESC[c])

const page = $('page'), game = $('game')

export function showPage(html: string) {
  page.innerHTML = html
  page.hidden = false
  game.hidden = true
  page.querySelector<HTMLElement>('h1')?.focus?.()
  scrollTo(0, 0)
}

export function showGame(bar: string, info: string) {
  $('game-bar').innerHTML = bar
  $('game-info').textContent = info
  page.hidden = true
  game.hidden = false
  onShow()
}

const winDlg = $<HTMLDialogElement>('win')
/** Modal with a title, body html and buttons. Returns when a button is chosen (its index), -1 on Esc. */
export function dialog(title: string, html: string, buttons: string[]): Promise<number> {
  $('win-title').textContent = title
  $('win-text').innerHTML = html
  const box = $('win-actions')
  box.replaceChildren(...buttons.map((b, i) => {
    const el = document.createElement('button')
    el.textContent = b
    if (i === 0) el.className = 'primary'
    el.onclick = () => { winDlg.close(String(i)) }
    return el
  }))
  return new Promise((resolve) => {
    winDlg.onclose = () => resolve(winDlg.returnValue === '' ? -1 : +winDlg.returnValue)
    winDlg.returnValue = ''
    winDlg.showModal()
  })
}

export const go = (hash: string) => { location.hash = hash }
export const offline = () => !navigator.onLine
