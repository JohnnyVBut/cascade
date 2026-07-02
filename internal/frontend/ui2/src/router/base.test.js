import { describe, it, expect } from 'vitest'
import { resolveRouterBase } from './base.js'

describe('resolveRouterBase', () => {
  it('resolves bare /ui2/ to /ui2/', () => {
    expect(resolveRouterBase('/ui2/')).toBe('/ui2/')
  })

  it('resolves a deep UI2 route to /ui2/', () => {
    expect(resolveRouterBase('/ui2/interfaces')).toBe('/ui2/')
  })

  // Critical reverse-proxy case: behind Caddy's hidden admin path the base must
  // include the prefix, or router links drop it and 404.
  it('resolves a proxy-prefixed /ui2/ to /<prefix>/ui2/', () => {
    expect(resolveRouterBase('/a3c8ac/ui2/')).toBe('/a3c8ac/ui2/')
  })

  it('resolves a proxy-prefixed deep route to /<prefix>/ui2/', () => {
    expect(resolveRouterBase('/a3c8ac/ui2/interfaces')).toBe('/a3c8ac/ui2/')
  })

  it('handles a multi-segment proxy prefix', () => {
    expect(resolveRouterBase('/x/y/ui2/interfaces')).toBe('/x/y/ui2/')
  })

  it('falls back to /ui2/ when ui2 segment is absent', () => {
    expect(resolveRouterBase('/')).toBe('/ui2/')
  })
})
