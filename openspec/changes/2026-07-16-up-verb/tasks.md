# Tasks — `up` verb

- [ ] `cmdUp` (internal/app): decision matrix per proposal — empty store +
      declared pull → run it (verify nodes landed; fall back to gather on
      failure or empty result, saying so); empty store, no pull → full
      gather; store stale vs HEAD → sync lane (`add . --no-adapters`);
      fresh → no-op; unknown freshness → report present, touch nothing.
      Flags passed through: --path/--store/--force/--jobs.
- [ ] `init` clone courtesy → redirect: pull-declaring config + empty
      store prints "run: ctx-optimize up" (no scaffold, no pull).
- [ ] Help text: `up` listed first in commands; fresh-clone wording.
- [ ] Pointer blocks (single + multi `<no-local-store>`) → `ctx-optimize up`.
- [ ] Skill: SKILL.md clone row + frontmatter; activation-routing.xml
      (`onboard-clone` → up, new `up` route); onboarding.md; push-pull.md
      teammate flow.
- [ ] Tests: up matrix (gather when no remote; pull on clone; fallback on
      broken pull; no-op when fresh; re-gather when stale — git-backed);
      init redirect assertion replaces the old auto-pull test.
- [ ] CHANGELOG under 0.4.0 (ships with scripted transports).
- [ ] task ci + golden; refresh this repo's own pointer block via init.
