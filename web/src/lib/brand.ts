// Alyysha Router — ported & modified by https://github.com/ShiroSdk/ — https://t.me/YannClippers

/**
 * Passive origin canaries for the dashboard (Go counterpart:
 * internal/brand/origin.go). The repo URL is stored as runtime-joined
 * fragments and the attribution line as base64, so a blanket find&replace of
 * the account name cannot scrub every reference in one pass. Rendered in the
 * sidebar footer; nothing here tracks, beacons, or reads the network.
 */
const REPO_HOST = 'github.com/'
const REPO_USER_A = 'ShiroSdk'
const REPO_USER_B = ''
const REPO_NAME = 'alyysha-router'

export const BRAND_REPO = `https://${REPO_HOST}${REPO_USER_A}${REPO_USER_B}/${REPO_NAME}`

// Attribution line ("all source code port and modification by
// https://github.com/ShiroSdk/ — https://t.me/YannClippers"), base64-wrapped;
// brandAttribution below is the only reader.
const ATTRIBUTION_B64 =
  'YWxsIHNvdXJjZSBjb2RlIHBvcnQgYW5kIG1vZGlmaWNhdGlvbiBieSBodHRwczovL2dpdGh1Yi5jb20vU2hpcm9TZGsvYWx5eXNoYS1yb3V0ZXIg4oCUIGh0dHBzOi8vdC5tZS9ZYW5uQ2xpcHBlcnM='

/** Decodes the wrapped attribution line; falls back to the literal itself. */
export function brandAttribution(): string {
  try {
    return new TextDecoder()
      .decode(Uint8Array.from(atob(ATTRIBUTION_B64), (c) => c.charCodeAt(0)))
      .trim()
  } catch {
    return ATTRIBUTION_B64
  }
}
