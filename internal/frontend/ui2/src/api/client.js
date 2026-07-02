// API client for the UI2 frontend.
//
// The app is served from the same Go binary/origin as the REST API, so
// authentication relies on the same-origin HttpOnly session cookie — no token
// storage here. `fetch` defaults to `credentials: 'same-origin'`, which sends
// that cookie automatically. Do NOT move UI2 to a different origin/port without
// revisiting this assumption (the cookie would no longer be sent).

/**
 * Resolve the API base path from the current location pathname.
 *
 * UI2 is mounted at `<prefix?>/ui2/...`, where `<prefix>` is an optional
 * reverse-proxy segment injected by Caddy (the hidden random admin path in the
 * production deploy). The API lives at `<prefix?>/api`, a sibling of `ui2`.
 *
 * Examples:
 *   /ui2/                    -> /api
 *   /ui2/interfaces          -> /api
 *   /a3c8ac/ui2/             -> /a3c8ac/api
 *   /a3c8ac/ui2/interfaces   -> /a3c8ac/api
 *
 * The rule: take everything up to (and excluding) the `ui2` segment as the
 * prefix, then append `/api`.
 *
 * @param {string} pathname - typically window.location.pathname
 * @returns {string} API base path with no trailing slash, e.g. "/api" or "/a3c8ac/api"
 */
export function resolveApiBase(pathname) {
  const segments = (pathname || '/').split('/').filter(Boolean)
  const ui2Index = segments.indexOf('ui2')
  const prefix = ui2Index > 0 ? '/' + segments.slice(0, ui2Index).join('/') : ''
  return prefix + '/api'
}

/**
 * Thin fetch wrapper. Throws Error(message) on non-2xx responses, mirroring the
 * legacy client's error contract.
 */
async function request(method, path, body) {
  const base = resolveApiBase(window.location.pathname)
  const opts = {
    method,
    headers: {},
    credentials: 'same-origin',
  }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }

  const res = await fetch(base + path, opts)

  // 401 → the single product login flow lives in the legacy UI at the root.
  // UI2 is a logged-in-only surface for now; bounce there rather than building
  // a second login form.
  if (res.status === 401) {
    const base401 = resolveApiBase(window.location.pathname).replace(/\/api$/, '/')
    window.location.href = base401
    throw new Error('unauthenticated')
  }

  if (!res.ok) {
    let msg = res.statusText
    try {
      const j = await res.json()
      msg = j.message || j.error || msg
    } catch (_) {
      /* non-JSON error body — keep statusText */
    }
    throw new Error(msg)
  }

  const ct = res.headers.get('Content-Type') || ''
  if (ct.includes('application/json')) return res.json()
  return res.text()
}

export const api = {
  get: (path) => request('GET', path),
  post: (path, body) => request('POST', path, body),
  patch: (path, body) => request('PATCH', path, body),
  put: (path, body) => request('PUT', path, body),
  delete: (path) => request('DELETE', path),
}

// Root-relative URL for a resource served by the API (e.g. QR image, config
// download). Includes any Caddy prefix so <img src> / <a href> resolve
// correctly; the session cookie is sent automatically (same origin).
export function apiUrl(path) {
  return resolveApiBase(window.location.pathname) + path
}
