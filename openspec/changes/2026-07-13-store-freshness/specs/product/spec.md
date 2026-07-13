# Spec delta — store freshness

## ADDED Requirements

### Requirement: Record source provenance on add
When `add` gathers from a path that is a git working tree, the store SHALL record that
source root's current git HEAD (commit sha + committer unix time) and the add time, keyed
by absolute source path, in `source.json`. Re-adding the same root SHALL update its entry.
If the path is not a git repo or git is unavailable, `add` SHALL proceed unchanged and
record nothing (no error).

#### Scenario: add in a git repo records HEAD
- **GIVEN** a git repo at `/r` with HEAD `abc123`
- **WHEN** `ctx-optimize add .` runs in `/r`
- **THEN** the store's `source.json` has an entry for `/r` with head `abc123`

#### Scenario: add in a non-git dir records nothing
- **GIVEN** a plain directory (no `.git`)
- **WHEN** `add .` runs
- **THEN** it succeeds and writes no source entry (freshness later reports `unknown`)

### Requirement: Freshness comparison is pure and deterministic
`internal/freshness.Evaluate(recorded, currentHead, currentHeadUnix, now)` SHALL be a pure
function returning a report with a `State` of `fresh` (recorded head == current head),
`stale` (both present and differ), or `unknown` (either head empty), plus both SHAs and
the store's age in seconds. It SHALL NOT call git, read files, or use wall-clock time.

#### Scenario: equal heads → fresh
- **GIVEN** recorded head `abc` and current head `abc`
- **THEN** State == `fresh`

#### Scenario: differing heads → stale
- **GIVEN** recorded head `abc` and current head `def`
- **THEN** State == `stale` with both SHAs in the report

#### Scenario: missing current head → unknown
- **GIVEN** current head `""` (not a git repo / git absent)
- **THEN** State == `unknown`

### Requirement: status surfaces freshness
`status` SHALL show a `fresh:` line summarising each tracked source's state, and
`status --json` SHALL include a `freshness` array. Absence of any tracked source SHALL
render as `fresh: (unknown — no git provenance)`, never an error.

#### Scenario: stale store is visible in status
- **GIVEN** a store whose recorded head differs from the repo's current head
- **WHEN** `status` runs
- **THEN** the output marks the source STALE and names the current-vs-store SHAs

### Requirement: fresh verb with exit codes
`ctx-optimize fresh` SHALL print a one-line verdict and exit `0` when every tracked source
is fresh, `1` when any is stale, and `2` when freshness is unknown (no provenance). It
SHALL support `--json`. It SHALL open the store read-only — reading `source.json` and the
graph but writing NO graph/wiki artifacts — matching the sibling `status` command (opening
an absent store may scaffold its empty layout, exactly as `status`/`query` do; the
never-materialize rule is scoped to the long-running dashboard serve path).

#### Scenario: agent gate on staleness
- **GIVEN** a stale store
- **WHEN** a hook runs `ctx-optimize fresh`
- **THEN** exit code is `1` and the line advises `ctx-optimize add .`
