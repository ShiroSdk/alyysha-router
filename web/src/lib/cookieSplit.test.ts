// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
import { describe, expect, test } from 'vitest'
import { combineCookieFields, splitCookieHeader } from './cookieSplit'

describe('splitCookieHeader', () => {
	test('gemini-web: full header with both cookies', () => {
		const raw = '__Secure-1PSID=abc123; __Secure-1PSIDTS=sid-ts-1'
		const r = splitCookieHeader(raw, 'gemini-web')
		expect(r).toEqual({ primary: 'abc123', secondary: 'sid-ts-1' })
	})

	test('gemini-web: primary only (no secondary)', () => {
		const r = splitCookieHeader('__Secure-1PSID=abc123', 'gemini-web')
		expect(r).toEqual({ primary: 'abc123', secondary: '' })
	})

	test('gemini-web: primary missing → null', () => {
		expect(splitCookieHeader('__Secure-1PSIDTS=sid-ts-1', 'gemini-web')).toBeNull()
	})

	test('grok-web: full header with sso and sso-rw', () => {
		const raw = 'sso=grok-sso-val; sso-rw=grok-rw-val'
		const r = splitCookieHeader(raw, 'grok-web')
		expect(r).toEqual({ primary: 'grok-sso-val', secondary: 'grok-rw-val' })
	})

	test('grok-web: sso + sso-rw + cf_clearance (extra appended to secondary)', () => {
		const raw = 'sso=grok-sso-val; sso-rw=grok-rw-val; cf_clearance=cf-tok'
		const r = splitCookieHeader(raw, 'grok-web')
		expect(r?.primary).toBe('grok-sso-val')
		expect(r?.secondary).toContain('grok-rw-val')
		expect(r?.secondary).toContain('cf_clearance=cf-tok')
	})

	test('grok-web: bare value (no key=value) treated as primary', () => {
		const r = splitCookieHeader('just-a-raw-cookie-value', 'grok-web')
		expect(r).toEqual({ primary: 'just-a-raw-cookie-value', secondary: '' })
	})

	test('empty input → null', () => {
		expect(splitCookieHeader('', 'gemini-web')).toBeNull()
		expect(splitCookieHeader('  ', 'grok-web')).toBeNull()
	})

	test('unrelated cookies → null (primary not found)', () => {
		expect(splitCookieHeader('other=value; more=stuff', 'gemini-web')).toBeNull()
	})
})

describe('combineCookieFields', () => {
	for (const provider of ['gemini-web', 'grok-web'] as const) {
		test(`${provider}: split values round-trip with exact cookie names`, () => {
			const header = provider === 'gemini-web'
				? '__Secure-1PSID=primary==; __Secure-1PSIDTS=timestamp='
				: 'sso=primary==; sso-rw=timestamp='
			const fields = splitCookieHeader(header, provider)!
			expect(combineCookieFields(fields.primary, fields.secondary, provider)).toBe(header)
			expect(combineCookieFields(header.split(';')[0], header.split(';')[1], provider)).toBe(header)
		})
	}
	test('primary only retains its label', () => {
		expect(combineCookieFields('abc123', '', 'gemini-web')).toBe('__Secure-1PSID=abc123')
	})
	test('grok extras retain their original name without duplicating labels', () => {
		const header = 'sso=primary; sso-rw=secondary; cf_clearance=clearance'
		const fields = splitCookieHeader(header, 'grok-web')!
		expect(combineCookieFields(fields.primary, fields.secondary, 'grok-web')).toBe(header)
	})
	test('no primary cannot form a credential', () => {
		expect(combineCookieFields('', 'secondary', 'grok-web')).toBe('')
	})
})
