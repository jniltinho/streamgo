// Portions adapted from MeetHub (MIT): frontend/tests/unit/i18n.test.ts
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { errorMessage, locale, t } from '@/i18n'
import en from '@/i18n/en'
import ptBR from '@/i18n/pt-BR'

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) return sources(p)
    return /\.(ts|vue)$/.test(f) && !f.endsWith('.test.ts') ? [p] : []
  })
}

// every literal passed to t('…') in the SPA
function usedKeys(): Set<string> {
  const keys = new Set<string>()
  const re = /\bt\(\s*(['"`])((?:\\.|(?!\1).)*)\1/g
  for (const file of sources(join(__dirname, '../../src'))) {
    for (const m of readFileSync(file, 'utf8').matchAll(re)) keys.add(m[2]!.replace(/\\(['"`])/g, '$1'))
  }
  return keys
}

describe('catalogs', () => {
  it('en and pt-BR have the same keys (catalog parity)', () => {
    const a = Object.keys(en)
    const b = Object.keys(ptBR)
    expect(
      a.filter((k) => !b.includes(k)),
      'in en, missing from pt-BR',
    ).toEqual([])
    expect(
      b.filter((k) => !a.includes(k)),
      'in pt-BR, missing from en',
    ).toEqual([])
  })
  it('en lists every string used in the SPA', () => {
    expect([...usedKeys()].filter((k) => !(k in en))).toEqual([])
  })
  it('en has no stale entries', () => {
    const keys = usedKeys()
    expect(Object.keys(en).filter((k) => !keys.has(k) && !k.startsWith('error.'))).toEqual([])
  })
})

describe('t', () => {
  afterEach(() => {
    locale.value = 'en'
  })
  it('is English by default', () => {
    expect(locale.value).toBe('en')
    expect(t('Broadcasts')).toBe('Broadcasts')
  })
  it('switches language at once', () => {
    locale.value = 'pt-BR'
    expect(t('Broadcasts')).toBe('Transmissões')
  })
  it('falls back to English for a missing entry', () => {
    locale.value = 'pt-BR'
    expect(t('Untranslated source string')).toBe('Untranslated source string')
  })
  it('fills placeholders', () => {
    expect(t('Hello {name}', { name: 'Ana' })).toBe('Hello Ana')
  })
  it('translates error codes, falling back to the server message', () => {
    locale.value = 'pt-BR'
    expect(errorMessage('invalid_credentials', 'x')).toBe('E-mail ou senha inválidos.')
    expect(errorMessage('some_new_code', 'Server text.')).toBe('Server text.')
  })
})
