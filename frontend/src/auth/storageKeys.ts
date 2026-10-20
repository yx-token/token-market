/** Engine SPA shares origin with idle-cat; keep tokens on separate keys. */
const ENGINE = import.meta.env.VITE_ENGINE_HOST === '1'
const PREFIX = ENGINE ? 'engine_' : ''

export const AUTH_TOKEN_KEY = `${PREFIX}auth_token`
export const AUTH_USER_KEY = `${PREFIX}auth_user`
export const REFRESH_TOKEN_KEY = `${PREFIX}refresh_token`
export const TOKEN_EXPIRES_AT_KEY = `${PREFIX}token_expires_at`
export const PENDING_AUTH_SESSION_KEY = `${PREFIX}pending_auth_session`

/** Absolute path to the SPA login page for this build. */
export function authLoginPath(): string {
  if (ENGINE) {
    const base = String(import.meta.env.BASE_URL || '/engine/').replace(/\/?$/, '/')
    return `${base}login`
  }
  if (import.meta.env.VITE_MARKET_HOST === '1') {
    return '/admin/login'
  }
  return '/login'
}
