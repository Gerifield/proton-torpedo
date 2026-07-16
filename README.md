# proton-torpedo
Tailscale with ProtonVPN exit node

This small docker package contains a manager which allows you to easily switch between ProtonVPN exit nodes while using Tailscale.

**_Note: This is still in alpha, it works, but not stable enough for continuous usage._**

## Usage

Copy the .env.example file to .env and fill in your credentials.

Then run:
```bash
docker-compose up -d
```

This should connect to tailscale and proton.

You can then access the web interface at http://<hostname>:8081 to switch between exit nodes.
The connection will be built based on the `SERVER_HOSTNAMES` env variable, this will be overwritten by manager, the rest of the variables will be simply copied from the .env file.

## Features

- **Web UI** — browse the list of ProtonVPN WireGuard servers and switch with a single click.
- **Current IP panel** — see your external IP, country, city, and org (via ipwho.is) with a refresh button.
- **Live connection log** — SSE-streamed gluetun output in a terminal-style box, so you can watch handshakes and reconnects in real time. Recent history is buffered so refreshing the page still shows the last ~200 lines.
- **Live status badge & disconnect** — the UI shows which server is currently active and highlights its row in the table. A disconnect button next to the badge cleanly stops the VPN (flushes gluetun's kill-switch iptables rules and resets `/etc/resolv.conf` so local networking keeps working).
- **Persistent active server** — the last chosen server is written to `STATE_FILE` (on the `./data` host volume). On container restart the manager automatically reconnects to it, so the VPN survives reboots without manual intervention. Disconnecting clears this state so a restart does not reconnect.
- **Tailscale sidecar** — the `tailscale` container shares the manager's network namespace, so all its traffic exits through the ProtonVPN tunnel and it can advertise itself as an exit node.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8081` | HTTP bind address for the web UI |
| `STATE_FILE` | `/data/torpedo-state.json` | Path used to persist the active server across restarts |
| `VPN_SERVICE_PROVIDER` | `protonvpn` | gluetun provider |
| `VPN_TYPE` | `wireguard` | gluetun tunnel type |
| `WIREGUARD_PRIVATE_KEY` | — | Your WireGuard private key (required) |
| `TS_AUTHKEY` | — | Tailscale auth key (required) |
| `PUBLICIP_ENABLED` | `yes` | Set to `no` to disable gluetun's built-in `[ip getter]` log line and periodic third-party IP checks (purely informational, safe to disable) |

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/list` | WireGuard server list |
| `GET` | `/api/ip` | Current external IP |
| `POST` | `/api/connect` | Switch VPN; body `{"server_name":"..."}` |
| `POST` | `/api/disconnect` | Stop the VPN, flush kill-switch rules, reset DNS, clear persisted state |
| `GET` | `/api/status` | Active server name + connected bool |
| `GET` | `/api/logs` | SSE stream of gluetun stdout (with recent history) |

## Future plans

- Add OpenVPN support (gluetun supports this and part of the image already)
- Add more providers supported by gluetun

Or a different path:

- Switch to native wireguard instead of gluetun
  - For this we can load in the informations from https://github.com/tn3w/ProtonVPN-IPs repo
  - The frontend should get data from here and create the WG config on the fly
