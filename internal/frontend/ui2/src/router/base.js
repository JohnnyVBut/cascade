// Resolve the Vue Router history base from the current location pathname.
//
// The app is mounted at `<prefix?>/ui2/`, where `<prefix>` is an optional Caddy
// reverse-proxy segment (the hidden admin path in production). Vue Router needs
// the full base including that prefix, otherwise links resolve to `/ui2/...`
// and drop the prefix (404 behind Caddy).
//
// Examples:
//   /ui2/                  -> /ui2/
//   /ui2/interfaces        -> /ui2/
//   /a3c8ac/ui2/           -> /a3c8ac/ui2/
//   /a3c8ac/ui2/interfaces -> /a3c8ac/ui2/
//
// @param {string} pathname - typically window.location.pathname
// @returns {string} router base with leading and trailing slash
export function resolveRouterBase(pathname) {
  const segments = (pathname || '/').split('/').filter(Boolean)
  const ui2Index = segments.indexOf('ui2')
  if (ui2Index < 0) return '/ui2/'
  return '/' + segments.slice(0, ui2Index + 1).join('/') + '/'
}
