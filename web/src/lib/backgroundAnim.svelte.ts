// Alyysha Router — ported & modified by https://github.com/ShiroSdk/ — https://t.me/YannClippers

/**
 * The routing background's motion mode, in one place: App.svelte pushes the
 * stored setting in after every settings load, the settings panel writes it
 * back, and AsciiRoutingBg reads it at call time, so a flip reaches the
 * canvas without a reload.
 *
 * `static` freezes the map into a single painted frame — the same path
 * prefers-reduced-motion already takes — to save CPU and battery on phones.
 */
export type BackgroundAnimation = 'auto' | 'static'

let current = $state<BackgroundAnimation>('auto')

/** The motion mode, read at call time (reactive). */
export function backgroundAnimation(): BackgroundAnimation {
  return current
}

/** Applies whatever the server settings carried (or the last panel edit). */
export function setBackgroundAnimation(value: unknown): void {
  current = value === 'static' ? 'static' : 'auto'
}
