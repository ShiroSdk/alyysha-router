// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'bun:test'

// The detail and media surfaces share the masking store; neither should
// describe that same toggle in a different language.
describe('provider email privacy labels', () => {
  for (const file of [
    'connections/ProviderDetailView.svelte',
    'media/MediaProviderDetail.svelte',
  ]) {
    it(`${file} names both toggle states in English`, () => {
      const source = readFileSync(join(import.meta.dirname, '..', 'components', file), 'utf8')
      expect(source).toContain("title={$emailPrivacy ? 'Show full email' : 'Mask / hide email'}")
      expect(source).toContain("{$emailPrivacy ? 'Email Masked' : 'Mask Email'}")
      for (const old of ['Tampilkan email lengkap', 'Sensor / sembunyikan email', 'Email Disensor', 'Sensor Email']) {
        expect(source).not.toContain(`'${old}'`)
      }
    })
  }
})
