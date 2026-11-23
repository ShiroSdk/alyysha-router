<!-- Alyysha Router — source port and modifications by https://github.com/ShiroSdk/ — https://t.me/YannClippers -->
<script lang="ts">
  // Unified provider logo: official brand mark -> baseUrl keyword -> domain
  // favicon -> modern fallback icon. Never a letters-initials badge.
  //
  // Priority 1: catalog id / alias / display name resolves to the official SVG
  // brand mark (OpenAI, Anthropic/Claude, Gemini, DeepSeek, Qwen, Kimi, MiniMax,
  // Zhipu/Z.ai, Baidu, Xiaomi, Cursor, Cline, OpenRouter, Ollama, NVIDIA,
  // Cloudflare, AWS, Cerebras, Cohere, Perplexity, Nous, Featherless, Poolside,
  // Grok/xAI, Mistral, Groq, Windsurf, GitHub Copilot, Kilo, Trae, ...).
  //
  // Priority 2: a custom node's baseUrl hostname names the vendor (openai,
  // anthropic, gemini/googleapis, deepseek, openrouter, groq, mistral, x.ai,
  // moonshot/kimi, minimax, qwen/dashscope/alibaba, ...) -> the vendor mark.
  //
  // Priority 2b: a custom/private domain with no known keyword -> load the
  // domain's favicon (Google s2 or the origin /favicon.ico). Private IPs and
  // localhost are skipped — they can never resolve a favicon.
  //
  // Priority 3: a modern SVG icon by apiType (OpenAI-compatible, Anthropic-
  // compatible, or a generic server node), never a box of letters.

  import { resolveProviderLogo } from '../../lib/providerLogos'

  interface Props {
    /** Provider id or alias from the catalog. */
    id?: string | null
    /** Provider display name (used for the id/name resolution and aria label). */
    name?: string | null
    /** Base URL of a custom provider node; hostname drives the vendor/favicon path. */
    baseUrl?: string | null
    /** apiType picks between the OpenAI chat and Responses glyphs on fallback. */
    apiType?: string | null
    /** Alt text; the provider's display name when omitted. */
    alt?: string | null
    /** Size of the logo box in pixels (default 40). */
    size?: number
    /** Rounded corner radius (default 10). */
    radius?: number
    /** Optional extra classes on the box. */
    class?: string
    /** Inline style forwarded to the box. */
    style?: string
  }

  let {
    id,
    name,
    baseUrl,
    apiType,
    alt,
    size = 40,
    radius = 10,
    class: klass = '',
    style,
  }: Props = $props()

  const resolution = $derived(resolveProviderLogo(id, name, baseUrl, apiType))
  const label = $derived(alt ?? name ?? id ?? 'Provider')

  // The favicon <img> degrades to the fallback icon on error; the {#key}
  // block re-creates the <img> when the provider changes so a failure on one
  // provider never sticks to the next.
  let faviconFailed = $state(false)

  const box = $derived(
    `flex shrink-0 items-center justify-center rounded-[${radius}px] bg-white/[0.04] border border-white/10 overflow-hidden ${klass}`
  )
  const boxStyle = $derived(`${style ?? ''}width:${size}px;height:${size}px;`)
</script>

{#if resolution.kind === 'svg'}
  <!-- Official brand mark: inline SVG, currentColor so dark mode stays crisp. -->
  <div class={box} style={boxStyle} role="img" aria-label={label} title={label}>
    <svg
      viewBox="0 0 24 24"
      width={size * 0.62}
      height={size * 0.62}
      fill="currentColor"
      fill-rule="evenodd"
      xmlns="http://www.w3.org/2000/svg"
      class="text-zinc-100"
      aria-hidden="true"
    >
      {#each resolution.paths as path}
        <path d={path} />
      {/each}
    </svg>
  </div>
{:else if resolution.kind === 'favicon'}
  <!-- Custom domain favicon; keyed so a failed load on one provider never
       sticks to the next, and the onerror degrades to the fallback icon. -->
  {#key resolution.url}
    <div class={box} style={boxStyle} role="img" aria-label={label} title={label}>
      {#if !faviconFailed}
        <img
          src={resolution.url}
          alt={label}
          width={size * 0.6}
          height={size * 0.6}
          loading="lazy"
          decoding="async"
          class="object-contain rounded"
          onerror={() => (faviconFailed = true)}
        />
      {:else}
        <svg viewBox="0 0 24 24" width={size * 0.62} height={size * 0.62} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="text-zinc-100" aria-hidden="true">
          <rect x="2" y="2" width="20" height="8" rx="2" />
          <rect x="2" y="14" width="20" height="8" rx="2" />
          <line x1="6" y1="6" x2="6.01" y2="6" />
          <line x1="6" y1="18" x2="6.01" y2="18" />
        </svg>
      {/if}
    </div>
  {/key}
{:else}
  <!-- Modern fallback icon (OpenAI-compatible / Anthropic / server node). -->
  <div class={box} style={boxStyle} role="img" aria-label={label} title={label}>
    {#if resolution.kind === 'fallback' && resolution.icon === 'anthropic'}
      <svg viewBox="0 0 24 24" width={size * 0.62} height={size * 0.62} fill="currentColor" class="text-zinc-100" aria-hidden="true">
        <path d="M13.827 3.52h3.603L24 20h-3.603l-6.57-16.48zm-7.258 0h3.767L16.906 20h-3.674l-1.343-3.461H5.017l-1.344 3.46H0L6.57 3.522zm4.132 9.959L8.453 7.687 6.205 13.48H10.7z" />
      </svg>
    {:else if resolution.kind === 'fallback' && resolution.icon === 'server'}
      <svg viewBox="0 0 24 24" width={size * 0.62} height={size * 0.62} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="text-zinc-100" aria-hidden="true">
        <rect x="2" y="2" width="20" height="8" rx="2" />
        <rect x="2" y="14" width="20" height="8" rx="2" />
        <line x1="6" y1="6" x2="6.01" y2="6" />
        <line x1="6" y1="18" x2="6.01" y2="18" />
      </svg>
    {:else}
      <svg viewBox="0 0 24 24" width={size * 0.62} height={size * 0.62} fill="currentColor" class="text-zinc-100" aria-hidden="true">
        <path d="M9.205 8.658v-2.26c0-.19.072-.333.238-.428l4.543-2.616c.619-.357 1.356-.523 2.117-.523 2.854 0 4.662 2.212 4.662 4.566 0 .167 0 .357-.024.547l-4.71-2.759a.797.797 0 00-.856 0l-5.97 3.473zm10.609 8.8V12.06c0-.333-.143-.57-.429-.737l-5.97-3.473 1.95-1.118a.433.433 0 01.476 0l4.543 2.617c1.309.76 2.189 2.378 2.189 3.948 0 1.808-1.07 3.473-2.76 4.163zM7.802 12.703l-1.95-1.142c-.167-.095-.239-.238-.239-.428V5.899c0-2.545 1.95-4.472 4.591-4.472 1 0 1.927.333 2.712.928L8.23 5.067c-.285.166-.428.404-.428.737v6.898zM12 15.128l-2.795-1.57v-3.33L12 8.658l2.795 1.57v3.33L12 15.128zm1.796 7.23c-1 0-1.927-.332-2.712-.927l4.686-2.712c.285-.166.428-.404.428-.737v-6.898l1.974 1.142c.167.095.238.238.238.428v5.233c0 2.545-1.974 4.472-4.614 4.472zm-5.637-5.303l-4.544-2.617c-1.308-.761-2.188-2.378-2.188-3.948A4.482 4.482 0 014.21 6.327v5.423c0 .333.143.571.428.738l5.947 3.449-1.95 1.118a.432.432 0 01-.476 0zm-.262 3.9c-2.688 0-4.662-2.021-4.662-4.519 0-.19.024-.38.047-.57l4.686 2.71c.286.167.571.167.856 0l5.97-3.448v2.26c0 .19-.07.333-.237.428l-4.543 2.616c-.619.357-1.356.523-2.117.523zm5.899 2.83a5.947 5.947 0 005.827-4.756C22.287 18.339 24 15.84 24 13.296c0-1.665-.713-3.282-1.998-4.448.119-.5.19-.999.19-1.498 0-3.401-2.759-5.947-5.946-5.947-.642 0-1.26.095-1.88.31A5.962 5.962 0 0010.205 0a5.947 5.947 0 00-5.827 4.757C1.713 5.447 0 7.945 0 10.49c0 1.666.713 3.283 1.998 4.448-.119.5-.19 1-.19 1.499 0 3.401 2.759 5.946 5.946 5.946.642 0 1.26-.095 1.88-.309a5.96 5.96 0 004.162 1.713z" />
      </svg>
    {/if}
  </div>
{/if}
