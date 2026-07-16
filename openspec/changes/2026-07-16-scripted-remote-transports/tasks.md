# Tasks — scripted remote transports (v0.4.0)

Gate for every step: `task ci` green; `task golden` for anything touching
extract/query/analyze (not expected here); hermetic tests only.

## Binary

- [ ] `internal/project`: `Remote` becomes `{Push, Pull string}` with a
      tolerant `UnmarshalJSON` (legacy string / {type,url,credentials}
      object → empty Remote, never a Load error); `RemoteCommand(dir)`
      helper. Delete `Resolve()` + `${VAR}` placeholder machinery (shell
      expands env in commands naturally).
- [ ] `internal/app`: `remote push|pull` = load config at scope root → run
      declared command via shell (cwd repo root, `cmd /c` on windows) with
      CTX_STORE_DIR / CTX_STORE_KEY / CTX_SCOPE_PREFIX / CTX_DIRECTION;
      pull pre-creates the store dir. Missing declaration → migration
      error. `remote init` (any form) → migration error. Legacy config →
      "legacy remote config" error on push/pull.
- [ ] `init` auto-pull-on-clone: trigger = committed `remote.pull` + empty
      local store; runs the command, falls back to printed hint.
- [ ] `status`: remote line reports declared commands (push/pull/both/none).
- [ ] DELETE `internal/remote` (+ tests); drop store-local config.json
      (`Store.Config/SaveConfig` — remote was its only field); drop
      `scopeStoreRels` and the `remote` import from multimodule.go.
- [ ] Dashboard: `/api/remote/push|pull` keep working through the new cmd
      funcs; `/api/setup` reports declared commands instead of a URL.
- [ ] Dashboard UI (found in review 2026-07-16): `Settings.tsx` renders
      `setup.remote.url` and its empty-state says "run remote init <url>";
      `types.ts` types the old shape. Update the React source to the
      {push, pull, from} shape + new empty-state wording, then
      `task dashboard-build` (dist is committed — go:embed precedent).
- [ ] Legacy-save edge (found in review 2026-07-16): a v0.3 config with
      `"remote": "s3://…"` re-saved by an UNRELATED write (`config X
      --project`, init) must not silently emit `"remote": {}` — drop the
      key entirely when both commands are empty (nil out before Save) and
      pin it with a test.
- [ ] Help text: remote section rewritten (config shape + env contract);
      footer's remote/credentials example replaced.

## Scaffold + skill (first-class, same change)

- [ ] Templates: `push.js.sample` + `pull.js.sample` (git lane, zero-dep
      node, header comment = how to arm) via go:embed; rewrite
      `remote.example.md` around authoring (git lane / s3 lane / custom,
      env contract, secrets rule).
- [ ] `Scaffold`: write the two samples (never overwrite existing).
- [ ] Skill: rewrite `references/push-pull.md` (agent AUTHORS scripts);
      SKILL.md share row + frontmatter; activation-routing.xml
      (`remote-init` route → `remote-author`, `remote-sync` updated,
      defaults rule "no network except your configured remote" reworded).
- [ ] CLAUDE.md layout line for internal/remote replaced; repo pointer
      blocks' `<no-local-store>` wording (project.go pointerBlock) updated.

## Tests

- [ ] project: config round-trip with commands; legacy string/object forms
      load inert; RemoteCommand.
- [ ] app: push/pull run the declared command with correct env (script
      writes env to a file); exit!=0 fails; missing declaration errors;
      module scope sets CTX_SCOPE_PREFIX; init auto-pull via declared
      command; `remote init` migration error; legacy config error.
- [ ] app: replace v0.3 remote round-trip tests (app_test.go,
      remote_scope_test.go) with script-transport equivalents (a file://
      "host" implemented BY the test's script — same coverage, new lane).
- [ ] dashboard: remote endpoints against a script-backed repo; setup
      payload shape.
- [ ] Scaffold test: samples land, inert until renamed + declared.

## Release

- [ ] CHANGELOG 0.4.0 with migration table.
- [ ] `task ci` + full smoke (init → add → declare → push → wipe → pull →
      query) against a git-lane script in a temp "host".
- [ ] Tag v0.4.0 on maintainer's "publish".
