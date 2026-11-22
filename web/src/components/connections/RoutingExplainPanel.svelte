<!-- Alyysha Router — derived from the work of https://github.com/ShiroSdk/ — port and modifications by the maintainers — https://t.me/YannClippers -->
<script lang="ts">
  import { onMount } from 'svelte'
  import Card from '../../lib/ui/Card.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import { api } from '../../api/client'
  import { RefreshCw } from 'lucide-svelte'

  // Routing explain (G1.4): the newest fallback-chain decisions the gateway
  // recorded, one row per candidate considered — attempted, skipped (with
  // the reason the chain gave), or the success the client finally saw.
  interface RoutingExplainEntry {
    timestamp: string
    provider: string
    model: string
    connectionId?: string
    statusCode?: number
    reason: string
    outcome: 'attempted' | 'success' | 'skipped'
    latencyMs?: number
  }

  let entries = $state<RoutingExplainEntry[]>([])
  let loading = $state(false)
  let failed = $state(false)

  async function load() {
    loading = true
    try {
      const res = (await api.getRoutingExplain()) as { entries: RoutingExplainEntry[] }
      entries = res.entries ?? []
      failed = false
    } catch {
      failed = true
    } finally {
      loading = false
    }
  }

  onMount(load)

  const outcomeClass = (o: string) =>
    o === 'success' ? 'text-emerald-500' : o === 'skipped' ? 'text-amber-500' : 'text-text-muted'
</script>

<Card padding="md" class="space-y-3">
  <div class="flex items-center justify-between pb-2 border-b border-border">
    <div class="flex items-center gap-2">
      <h3 class="text-sm font-semibold text-text-main">Routing Explain</h3>
      <span class="text-[11px] text-text-subtle">why the chain picked (or skipped) each candidate</span>
    </div>
    <Button variant="ghost" size="sm" onclick={load} loading={loading}>
      <RefreshCw class="w-3.5 h-3.5 text-text-muted" />
    </Button>
  </div>

  {#if failed}
    <p class="text-xs text-text-muted">Routing explain unavailable right now.</p>
  {:else if entries.length === 0}
    <p class="py-4 text-center text-xs text-text-muted">No routing decisions recorded yet — they appear as requests walk the fallback chain.</p>
  {:else}
    <div class="overflow-x-auto">
      <table class="glyph-table w-full text-left text-xs min-w-[560px]">
        <thead class="text-text-muted">
          <tr class="border-b border-border">
            <th class="py-2 px-3 font-semibold">Time</th>
            <th class="py-2 px-3 font-semibold">Provider</th>
            <th class="py-2 px-3 font-semibold">Model</th>
            <th class="py-2 px-3 font-semibold">Outcome</th>
            <th class="py-2 px-3 font-semibold">Reason</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-border-subtle">
          {#each entries as e (e.timestamp + e.provider + e.model + e.reason)}
            <tr class="hover:bg-surface-2/60 transition-colors">
              <td class="py-2 px-3 font-mono text-[11px] text-text-muted">{e.timestamp.slice(11, 19)}</td>
              <td class="py-2 px-3 font-semibold text-text-main">{e.provider}</td>
              <td class="py-2 px-3 font-mono text-[11px] text-text-muted">{e.model}</td>
              <td class="py-2 px-3 font-semibold {outcomeClass(e.outcome)}">{e.outcome}</td>
              <td class="py-2 px-3 text-text-muted">{e.reason}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</Card>
