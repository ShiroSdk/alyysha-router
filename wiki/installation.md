---
title: Installation
order: 2
section: Getting Started
---

# Installation

## npm (primary)

```bash
npm install -g alyysha
```

The launcher installs the right binary for your OS automatically (Windows, macOS Intel/Apple Silicon, Linux x64/ARM64). Requires Node.js **>= 18**; the gateway binary itself is a single Go executable with no runtime dependencies.

## Alternative — script installer (same binaries)

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/ShiroSdk/alyysha-router/main/install.sh | bash
```

```powershell
# Windows (PowerShell) — writes to %LOCALAPPDATA% + user PATH, no admin needed
irm https://raw.githubusercontent.com/ShiroSdk/alyysha-router/main/install.ps1 | iex
```

## Defaults

No flags required:

| | Default |
|---|---|
| Port | `20128` |
| Listener | `127.0.0.1` (loopback); explicit `HOST=0.0.0.0` or `HOST=*` enables all interfaces |
| Data | `~/.alyysha` (`%APPDATA%/alyysha` on Windows) |
| Dashboard | `http://localhost:20128` |
| Initial password | `123456` (localhost only) |

## Running

```bash
alyysha              # run in the foreground
curl http://localhost:20128/health
alyysha version
```

Override only when you need something different (`PORT`, `DATA_DIR`/`DB_PATH` — there are no `--port` flags):

```bash
PORT=20129 alyysha
DATA_DIR=/srv/alyysha alyysha
DB_PATH=/srv/alyysha/data.sqlite alyysha
HOST=127.0.0.1 alyysha
```

## Background mode

```bash
alyysha start          # same as: Alyysha Router --background   (or -d)
alyysha status         # pid, dashboard, log
alyysha restart
alyysha logs -n 100
alyysha stop
```

The detached process records itself in `DATA_DIR/run/gateway.pid` and writes its
output to `DATA_DIR/run/gateway.log`. A second `start` while one is running
refuses instead of fighting over the port.

> Windows stops the process with `TerminateProcess` instead of `SIGTERM`, so
> `stop` skips the graceful drain; use the dashboard's Shutdown button or
> `alyysha restart` when you want in-flight streams to finish first.

## Start on boot

```bash
alyysha service install     # systemd user unit, launchd agent, OpenRC/runit service, cron @reboot, or Windows logon task
alyysha service status
alyysha service start
alyysha service restart
alyysha service stop
alyysha service uninstall
```

`install` picks the native backend for the host. systemd, launchd, and cron all
install into the **user** scope and need no root; runit's `~/service` path too.
Two exceptions: OpenRC is a system service and needs root (run with `sudo`, or
pick `--backend cron`), and on Windows install registers a scheduled task via
`schtasks`, which requires an Administrator console. Force a backend with
`--backend systemd|launchd|openrc|runit|cron`.

## Docker

For container port publishing, explicitly set `HOST=0.0.0.0` inside the
container; the default loopback listener is not reachable through a published
port. Require strong dashboard authentication before allowing network access.

```bash
docker run -d --name alyysha --restart unless-stopped \
  -p 20128:20128 -v "$HOME/.alyysha:/data" \
  -e PORT=20128 -e DATA_DIR=/data \
  -e INITIAL_PASSWORD=your-secure-password \
  ShiroSdk/alyysha-router:latest
```

Experimental builds are marked **Pre-release** and never offered to a normal
install; Docker Hub tags them `exp` (`ShiroSdk/alyysha-router:exp`), never `latest`.

## Building from source

Prerequisites: Go 1.27 and Bun 1.x (the dashboard is embedded into the binary, so build web first).

```bash
git clone https://github.com/ShiroSdk/alyysha-router.git
cd Alyysha Router
make web-build   # bun install --frozen-lockfile && bun run build
make build       # embeds VERSION into the Go binary
```

## First login

- **Localhost (`localhost` / `127.0.0.1`)**: the initial compatibility password is
  `123456`. Once logged in, change it in **Settings → Profile**.
- **Remote / VPS / Docker / LAN**: for security (preventing public takeover of
  fresh installs with known defaults), remote access blocks the default `123456`
  password. Either:
  1. Set `INITIAL_PASSWORD` on launch (recommended):
     ```bash
     INITIAL_PASSWORD="your-secure-password" alyysha
     ```
  2. Or access via SSH port-forwarding first:
     ```bash
     ssh -L 20128:127.0.0.1:20128 user@remote-host
     # open http://localhost:20128, log in with 123456, then change the password
     ```
