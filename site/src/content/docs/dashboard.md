---
title: The dashboard — `serve`
description: Loopback, per-process token on writes, audited, no CDN.
---

```bash
ctx-optimize serve          # http://127.0.0.1:4747/  — alias: dashboard
```

`--port`, `--host`. Same cmd functions as the CLI.

**Screens:** Overview · Repos · Onboard · Query · Viewer ([Flow](/ctx-optimize/see/) / House / Graph) · Settings · Changes (`ctx-optimize log`).

## Lock

- Binds `127.0.0.1:4747`.
- Writes need `X-Ctx-Token` from loopback `GET /api/token`. `--host` does not lift the `RemoteAddr` check.
- Mutations → `<store-root>/audit.ndjson`.
- UI is `go:embed`. No CDN.

`/api/graph` is budgeted (`truncated: true`). Flow/House send a 4.4 KB scene. Complete blast: CLI.

| GET (loopback) | |
|---|---|
| `/api/stores` `/api/modules` `/api/graph` `/api/scene` `/api/query` `/api/usage` `/api/setup` `/api/audit` `/api/token` | reads |
| POST `/api/repo/add` `/api/onboard` `/api/store` `/api/config` `/api/remote/…` | writes + token |

Reads never create store dirs.
