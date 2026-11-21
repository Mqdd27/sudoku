// JSON calls to the Go server (same origin; nginx proxies /api and /ws). Never cached by the SW.
export interface User { id: number; username: string; createdAt: number; imported: boolean }

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

export async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  let r: Response
  try {
    r = await fetch('/api' + path, {
      method,
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'Needs a connection. Check your network and try again.')
  }
  if (r.status === 204) return null as T
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new ApiError(r.status, j.error || 'Something went wrong.')
  return j as T
}

/** The signed-in user, or null. Refreshed by whoAmI() at start and after login/logout. */
export let me: User | null = null
export const setMe = (u: User | null) => { me = u }
/** False on a static-only host (e.g. Vercel): there is no game server, so only Classic is offered. */
export let hasServer = true
export async function whoAmI() {
  try { me = await api<User>('GET', '/me') } catch (e) {
    const s = (e as ApiError).status
    if (s === 401) me = null
    else if (s === 404 || s === 405) hasServer = false // the server always answers /me with 200 or 401
  }
  return me
}
