/**
 * The access token, held outside React.
 *
 * `api.ts` is a plain module with no hooks, so the token has to live somewhere
 * both it and the component tree can reach. This store is that place; React
 * binds to it through `useSyncExternalStore` in AuthProvider.
 *
 * Deliberately in memory only. The refresh token is an HttpOnly cookie the
 * page cannot read, so a script injected into this app has nothing durable to
 * steal — and a reload recovers the session through /auth/refresh anyway.
 */

export interface UserInfo {
  username: string
  is_admin: boolean
}

export interface TokenResponse {
  access_token: string
  token_type: string
  expires_in: number
  session_expires_at: string
  user: UserInfo
}

export interface SessionState {
  /** False in local development, where nothing is gated. */
  authRequired: boolean
  authenticated: boolean
  user: UserInfo | null
  /** When the *session* ends — the code's 48-hour deadline. */
  sessionExpiresAt: string | null
  /** False until /auth/session has answered, so the UI can hold off. */
  ready: boolean
}

/** Thrown when a request needs a token and no valid one could be obtained. */
export class AuthRequiredError extends Error {
  constructor(message = 'Enter an access code to use this feature.') {
    super(message)
    this.name = 'AuthRequiredError'
  }
}

let token: string | null = null
/** Epoch ms at which `token` stops being accepted. */
let tokenExpiresAt = 0

let state: SessionState = {
  // Assume gated until told otherwise: briefly showing the button in local dev
  // is a much smaller mistake than briefly hiding it in prod.
  authRequired: true,
  authenticated: false,
  user: null,
  sessionExpiresAt: null,
  ready: false,
}

const listeners = new Set<() => void>()

function emit(next: SessionState) {
  state = next
  listeners.forEach((listener) => listener())
}

export function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function getSnapshot(): SessionState {
  return state
}

export function accessToken(): string | null {
  return token
}

function acceptGrant(grant: TokenResponse) {
  token = grant.access_token
  // Renew a little early: a token that expires in flight would surface as a
  // spurious "your session ended" on a request that was really fine.
  tokenExpiresAt = Date.now() + grant.expires_in * 1000 - 30_000
  emit({
    authRequired: state.authRequired,
    authenticated: true,
    user: grant.user,
    sessionExpiresAt: grant.session_expires_at,
    ready: true,
  })
}

function clearSession() {
  token = null
  tokenExpiresAt = 0
  emit({ ...state, authenticated: false, user: null, sessionExpiresAt: null, ready: true })
}

async function readError(response: Response, fallback: string): Promise<string> {
  try {
    const body = await response.json()
    if (typeof body?.detail === 'string') return body.detail
  } catch {
    /* non-JSON error body — keep the fallback */
  }
  return fallback
}

// --- the session lifecycle -------------------------------------------------

/** Ask the server who we are. Called once on load, before anything is gated. */
export async function loadSession(): Promise<void> {
  try {
    const response = await fetch('/auth/session')
    if (!response.ok) throw new Error('unreachable')
    const info = await response.json()
    emit({
      authRequired: Boolean(info.auth_required),
      authenticated: Boolean(info.authenticated),
      user: info.user ?? null,
      sessionExpiresAt: info.session_expires_at ?? null,
      ready: true,
    })
    // A refresh cookie survives a reload, so try to pick the session back up
    // before deciding the visitor is anonymous.
    if (info.auth_required && !info.authenticated) await refreshSession()
  } catch {
    emit({ ...state, ready: true })
  }
}

/** Exchange the refresh cookie for a new access token. Returns success. */
export async function refreshSession(): Promise<boolean> {
  try {
    const response = await fetch('/auth/refresh', { method: 'POST' })
    if (!response.ok) {
      clearSession()
      return false
    }
    acceptGrant((await response.json()) as TokenResponse)
    return true
  } catch {
    clearSession()
    return false
  }
}

export async function redeemCode(code: string): Promise<void> {
  const response = await fetch('/auth/redeem', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  })
  if (!response.ok) {
    throw new AuthRequiredError(
      await readError(response, `Could not check that code (${response.status}).`),
    )
  }
  acceptGrant((await response.json()) as TokenResponse)
}

export async function logout(): Promise<void> {
  try {
    await fetch('/auth/logout', { method: 'POST' })
  } finally {
    clearSession()
  }
}

// --- the fetch every API call goes through ---------------------------------

function withToken(init: RequestInit | undefined): RequestInit {
  if (!token) return { ...init }
  const headers = new Headers(init?.headers)
  headers.set('Authorization', `Bearer ${token}`)
  return { ...init, headers }
}

/**
 * `fetch` that carries the access token and repairs an expired one.
 *
 * A 401 gets exactly one repair attempt through /auth/refresh — one, because a
 * loop here would turn an expired code into an infinite retry storm against
 * the server. If that fails the session is cleared, which is what makes the
 * "Enter Access Code" button reappear mid-visit when a code runs out.
 */
export async function authFetch(input: string, init?: RequestInit): Promise<Response> {
  if (token && Date.now() >= tokenExpiresAt) await refreshSession()

  const response = await fetch(input, withToken(init))
  if (response.status !== 401) return response

  // Never retry the auth routes themselves: their 401 is the answer, not a
  // symptom, and refreshing in response to one would recurse.
  if (input.startsWith('/auth/')) return response

  if (!(await refreshSession())) return response
  return fetch(input, withToken(init))
}
