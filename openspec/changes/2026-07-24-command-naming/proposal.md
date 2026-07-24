# ADR — field-standard command aliases (discoverability)

Status: DRAFT — 2026-07-24. For DISCUSSION. Motivated by the command-comparison
(`benchmarks/multilang/command-agent-comparison.md`): our verbs do the same jobs
as the field but use non-obvious names, raising the adoption barrier for users
coming from CodeGraph/GitNexus/graphify.

## Principle: promote the new name, keep the old one working silently

Register field-standard names as the taught primaries. Old names keep working as
**silent transition aliases** (nothing breaks — global CLAUDE.md, committed skills,
muscle memory), for some time. Owner decisions: **do NOT label the old names
"deprecated" in docs, and do NOT print a stderr note** — docs simply teach the new
names; the old ones quietly still resolve during the transition.

## The changes

| Purpose | Taught primary (field-aligned) | Legacy name (silently still works) | Why |
|---|---|---|---|
| Inspect a symbol | **`node`** | `card` | CodeGraph uses `node`; universally understood |
| Blast radius | **`impact`** | `affected` | CodeGraph AND GitNexus both use `impact` for exactly this |
| "About to edit X" (composed: callers + blast radius + tests) | **`change-plan`** (primary) + **`plan`** (alias) | — | No field equivalent — our differentiator; keep `change-plan`, add short `plan`. Not `compose` (too vague). |

**Trap avoided:** `impact` aliases `affected` (pure blast radius), NOT
`change-plan`. In the field `impact` = blast radius; `change-plan` is a richer,
composed verb nobody else has. Pointing `impact` at `change-plan` would bury our
one unique verb under a name that means something narrower everywhere else.

## Scope of the change

1. **CLI**: register `node`, `impact`, and `plan` as aliases in the verb
   dispatcher — one-line each, same handler. **Silent** — no stderr note, no
   "deprecated" wording (owner). Old names just resolve.
2. **Skill + instructions**: update `internal/skills/bundled/.../SKILL.md`,
   `internal/project/templates/instructions.md`, and the global CLAUDE.md/AGENTS.md
   managed block to teach `node`/`impact`/`change-plan` as the primaries. Do NOT
   list `card`/`affected` as "deprecated aliases" — just stop teaching them; they
   keep resolving silently. `install`/`init` refresh these (upgrade-only block).
3. **Site/docs**: cookbook + concepts + compare use the new primaries only. No
   "old name" clutter in docs (owner).
4. **Golden**: golden verb-output snapshots must assert BOTH names resolve to
   identical output (alias correctness), so a future refactor can't silently drop
   the legacy name during the transition window.

## Open questions (mostly resolved)

1. ~~Deprecation loudness~~ RESOLVED: silent, no stderr note, no "deprecated" label
   anywhere. Old names quietly work for a transition window.
2. ~~`plan` alias~~ RESOLVED: yes — `change-plan` primary, `plan` alias.
3. Any OTHER verb worth field-aligning? `query` (universal, keep), `add`/`up`
   (keep), `nodes`/`edges`/`deps` (our filter surface, no field equivalent, keep).
   Lean: only `card→node` and `affected→impact` this change.

## Success check

- `ctx-optimize node <sym>` and `ctx-optimize card <sym>` produce identical output;
  same for `impact`/`affected`. Golden asserts it.
- `--help` shows `node`/`impact` as primary with "(alias: card/affected)".
- Skills + committed instructions teach the new names; old names still work so no
  existing hook or committed pointer breaks.
- Comparison page can show verb parity using the names the field already knows.
