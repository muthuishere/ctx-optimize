# boundary fixture

Every boundary CLASS the port model claims, in the smallest repo that exercises
them. Pinned by `boundary_test.go`; the point is that a rule going quiet fails
a named class rather than passing silently.

| class | where | expected |
|---|---|---|
| config (plain) | `api/main.go` | `SERVICE_TIER`, not flagged |
| config (secret) | `api/main.go`, `ai/agent.py` | `PAYMENTS_API_KEY`, `OPENAI_API_KEY`, `sensitive=true` |
| egress (literal) | `worker/worker.go` | `api.weather.example` |
| egress (SDK) | `ai/agent.py` + `pyproject.toml` | `api.openai.com`, two tiers |
| process | `api/main.go` | `git` |
| api surface (go) | `api/main.go` | `/healthz`, `/orders` |
| api surface (express) | `web/app.ts` | `GET /status`, `POST /upload` |
| storage | `web/app.ts` | `session_token` |

Do not "tidy" this repo: every literal is load-bearing. `SERVICE_TIER` exists
precisely because it must NOT be flagged sensitive, and `git` is a literal
because a variable binary is the known miss the AMBIGUOUS tier records.
