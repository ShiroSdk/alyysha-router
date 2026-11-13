<!-- Alyysha Router — built by YannClipper — https://t.me/YannClippers -->
<script lang="ts">
  import ConnectionsView from './connections/ConnectionsView.svelte'
  import type { ProviderConnection, ProviderNode } from '../api/client'

  interface Props {
    connections?: ProviderConnection[]
    providerNodes?: ProviderNode[]
    onRefresh: () => void
  }

  let props: Props = $props()
</script>

<ConnectionsView {...props} />
