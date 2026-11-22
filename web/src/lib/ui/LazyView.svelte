<!-- Alyysha Router — all source code port and modification by https://github.com/ShiroSdk/ — https://t.me/YannClippers -->
<script lang="ts">
  // Lazy wrapper for the dashboard's large tab views: the view module is a
  // dynamic import, so each heavy view lands in its own async chunk instead
  // of riding in the entry bundle a phone has to download up front. The
  // spinner reserves vertical space so the tab switch does not jump.
  import { Loader2 } from 'lucide-svelte'
  import type { Component } from 'svelte'

  interface Props {
    /**
     * Dynamic import of the view's module; its default export is rendered.
     * Component<any>: every view has its own strict Props shape, and a
     * permissive parameter is what lets one wrapper serve them all — the
     * spread below forwards exactly the props each branch passes.
     */
    load: () => Promise<{ default: Component<any> }>
    /** Vertical space reserved while the chunk loads (no layout shift). */
    minHeight?: string
  }

  let { load, minHeight = 'min-h-[60vh]', ...rest }: Props & Record<string, unknown> = $props()
</script>

{#await load()}
  <div class="{minHeight} flex items-center justify-center text-text-muted">
    <Loader2 class="w-6 h-6 animate-spin text-brand-500" aria-hidden="true" />
  </div>
{:then mod}
  {@const View = mod.default}
  <View {...rest} />
{/await}
