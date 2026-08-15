# ADR 19 — the skill is the product, so stop making people install it

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: first-run behaviour of the binary, and the `install` verb's argument
handling. No schema change, no store change, no npm postinstall (see D0).
Raised by the owner: *"installing skill is the core, no one uses it manually,
so install that as part of default."*

## The problem, in one line

The documented path is two commands:

```
npm install -g @muthuishere/ctx-optimize
ctx-optimize install --skills
```

A user who runs only the first gets a CLI that their agent never calls. The
graph is built, correct, and invisible — which is the same failure mode as
every other "the capability exists but nothing routes to it" defect this
project has hit in the last week, only at the point of adoption.

## D0 — NOT an npm postinstall

The obvious mechanism is the one we should not take, and the npm wrapper's
`package.json` has no `scripts` block today on purpose:

- Postinstall runs arbitrary code at install time. That is the supply-chain
  shape security teams block, and `npm ci --ignore-scripts` is common enough in
  CI that the feature would silently not happen for exactly the teams most
  likely to notice.
- It would write into `$HOME` before the user has run our binary even once.
- It only covers the npm channel. `go install`, the release tarballs and the
  curl installer would still need a different answer, so we would ship two
  behaviours and document neither well.

## D1 — install on FIRST RUN, once, and say so

The first time any verb runs and no skill is installed, install it, print what
was written, and record that we did it so it never happens twice.

Why first run rather than install time: the user has now *executed our binary*,
which is a clearer act of consent than unpacking a tarball; it works on every
distribution channel including a bare downloaded binary; and it survives
`--ignore-scripts`.

Design constraints:

- **One line of output, naming every path written**, plus how to undo it
  (`ctx-optimize uninstall`). A tool that writes into `$HOME` silently is not
  something this project should ship, whatever the convenience.
- **Idempotent and recorded**, so the second run is silent.
- **Suppressed when it would be wrong**: `CI=1`, a non-TTY stdout, or
  `CTX_OPTIMIZE_NO_AUTO_INSTALL=1`. A container that runs one `query` in a
  pipeline must not accrete files in its home directory.
- **Never touches a repo.** The per-repo `CLAUDE.md`/`AGENTS.md` pointer block
  stays with `init`, because that is a committed, reviewable change to someone
  else's repository and must remain an explicit act.

Open question for the owner: today `install` fans out to claude, codex, copilot
and devin, and writes a **global rule** into `~/.claude/CLAUDE.md` and
`~/.codex/AGENTS.md`. Auto-installing the skill for a detected agent is
defensible. Auto-editing a user's global instructions file on first run is a
larger claim on their machine, and I would default it OFF and leave it to the
explicit verb. Owner decides.

## D2 — `install --help` currently installs

Measured today: `ctx-optimize install --help` performed the install — skills
written for four agents, hooks checked, global rule reported — instead of
printing usage. `--help` is the one flag a user types when they are NOT sure
they want the effect, and it is in the common-flag allowlist for every verb.

This is the same class as the unknown-flag defect fixed in `1c2e9bf`: a flag
that reads as a question is answered with an action. It should be fixed
regardless of D1, and it becomes more important with D1, because auto-install
makes the blast radius of "install ran when I did not mean it" larger.

## Gates

- A test that runs a verb in a temp `$HOME` with no skill present, asserts the
  skill is installed and the paths are named in stdout; then runs again and
  asserts the second run is silent. Prove it red first.
- A test that the same first run under `CI=1` writes nothing.
- `install --help` prints usage and writes nothing — asserted by comparing a
  `$HOME` snapshot before and after.

## Kill criterion

If auto-install cannot be made silent-on-second-run and fully suppressed in CI,
it should not ship. An agent tool that surprises people by writing to their home
directory would cost more trust than the adoption it buys.
