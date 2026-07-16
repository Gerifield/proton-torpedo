# proton-torpedo

A browser-managed ProtonVPN WireGuard switch for Docker, with Tailscale sidecar sharing the tunnel as an exit node.

## Architecture

- **`cmd/torpedo/main.go`** — entry point; creates `logic.Logic`, calls `Restore()` to reconnect last server, then starts HTTP server.
- **`internal/logic/logic.go`** — domain layer: server list (from `gluetun-entrypoint format-servers`), VPN connect/kill, state persistence, log broadcasting.
- **`internal/server/server.go`** — HTTP handlers. All routes are JSON except `/api/logs` (SSE).
- **`static/index.html`** — Bootstrap 5 SPA. No build step, just vanilla JS.
- **`docker/Dockerfile`** — two-stage: Go compiler → qmcgaw/gluetun:v3 base image.
- **`docker-compose.yaml`** — `protonvpn-manager` (torpedo + gluetun) + `tailscale` sidecar sharing the network namespace.

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/list` | WireGuard server list (cached after first call) |
| `GET` | `/api/ip` | Current external IP via ipinfo.io |
| `POST` | `/api/connect` | Start VPN; body `{"server_name":"..."}` |
| `GET` | `/api/status` | Active server name + connected bool |
| `GET` | `/api/logs` | SSE stream of gluetun stdout lines (+ history) |

## State persistence

On each `Connect()` the active server name is written to `STATE_FILE` (default `/data/torpedo-state.json`, backed by `./data` host volume). On startup `Restore()` reads this file and reconnects, so VPN survives container restarts.

## Log streaming

`LogBroadcaster` in `logic.go` keeps a 200-line ring buffer and fans every gluetun stdout line to SSE subscribers. The UI connects to `/api/logs` on load and appends lines to a dark terminal-style box.

## Running locally (Docker)

```bash
cp .env.example .env
# fill WIREGUARD_PRIVATE_KEY and TS_AUTHKEY in .env
docker compose up --build
# open http://localhost:8081
```

## Key env vars

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8081` | HTTP bind address |
| `STATE_FILE` | `/data/torpedo-state.json` | Persistent active-server state |
| `VPN_SERVICE_PROVIDER` | `protonvpn` | gluetun provider |
| `VPN_TYPE` | `wireguard` | gluetun tunnel type |
| `WIREGUARD_PRIVATE_KEY` | — | WireGuard private key (required) |
| `TS_AUTHKEY` | — | Tailscale auth key (required) |

## Notes

- `gluetun-entrypoint` is the binary from the base image; both it and `torpedo` live at `/` in the container.
- `runningProcess` is protected by `processMu`; `serverListCache` is protected by `serverListCacheLock`.
- Zero external Go dependencies — stdlib only.
- The `tailscale` container uses `network_mode: "service:protonvpn-manager"` to share the WireGuard tunnel.
