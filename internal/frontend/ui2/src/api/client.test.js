import { describe, it, expect } from 'vitest'
import { resolveApiBase } from './client.js'

describe('resolveApiBase', () => {
  it('resolves bare /ui2/ to /api', () => {
    expect(resolveApiBase('/ui2/')).toBe('/api')
  })

  it('resolves /ui2 (no trailing slash) to /api', () => {
    expect(resolveApiBase('/ui2')).toBe('/api')
  })

  it('resolves a deep UI2 route to /api', () => {
    expect(resolveApiBase('/ui2/interfaces')).toBe('/api')
  })

  // The critical reverse-proxy case: behind Caddy's hidden admin path, the app
  // is served at /<random>/ui2/... and the API is a sibling at /<random>/api.
  // This is the case that works in local dev (no prefix) but silently breaks in
  // production if the base-path logic naively assumes "first segment = prefix".
  it('resolves a proxy-prefixed /ui2/ to /<prefix>/api', () => {
    expect(resolveApiBase('/a3c8ac/ui2/')).toBe('/a3c8ac/api')
  })

  it('resolves a proxy-prefixed deep route to /<prefix>/api', () => {
    expect(resolveApiBase('/a3c8ac/ui2/interfaces')).toBe('/a3c8ac/api')
  })

  it('handles a multi-segment proxy prefix', () => {
    expect(resolveApiBase('/x/y/ui2/interfaces')).toBe('/x/y/api')
  })

  it('falls back to /api when ui2 segment is absent', () => {
    expect(resolveApiBase('/')).toBe('/api')
    expect(resolveApiBase('')).toBe('/api')
  })
})
