# Alyysha Router — Agent Skills

Drop-in skills for any AI agent (Claude, Cursor, ChatGPT, custom SDK). Just **copy a link** below and paste it to your AI — it will fetch the skill and use Alyysha Router for you.

> Tip: start with the **alyysha** entry skill — it covers setup and links to all capability skills.

## Skills

| Capability | Copy link below and paste to your AI |
|---|---|
| **Entry / Setup** (start here) | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha/SKILL.md |
| Chat / code-gen | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-chat/SKILL.md |
| Image generation | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-image/SKILL.md |
| Video generation (xAI Grok Imagine) | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-video/SKILL.md |
| Text-to-speech | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-tts/SKILL.md |
| Speech-to-text | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-stt/SKILL.md |
| Embeddings | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-embeddings/SKILL.md |
| Web search | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-web-search/SKILL.md |
| Web fetch (URL → markdown) | https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha-web-fetch/SKILL.md |

## How to use

Paste to your AI (Claude, Cursor, ChatGPT, …):

```
Read this skill and use it: https://raw.githubusercontent.com/ShiroSdk/alyysha-router/refs/heads/main/skills/alyysha/SKILL.md
```

Then ask normally — *"generate an image of a cat"*, *"transcribe this URL"*, etc.

## Configure your shell once

```bash
export ALYYSHA_URL="http://localhost:20128"   # local default, or your VPS / tunnel URL
export ALYYSHA_KEY="sk-..."                   # from Dashboard → Keys (only if requireApiKey=true)
```

Verify: `curl $ALYYSHA_URL/api/health` → `{"ok":true}`.

## Links

- Source: https://github.com/ShiroSdk/alyysha-router
- Dashboard: https://alyysha.com
