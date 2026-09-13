## ADDED Requirements

### Requirement: Direct non-interactive command execution
The system SHALL execute the exact argument vector following `compact run --`
without joining or reparsing it, and SHALL reject terminal-attached stdin before
launching the child.

#### Scenario: Arguments contain shell metacharacters
- **WHEN** an argument contains spaces, quotes, `$`, `*`, semicolons, leading dashes, or newlines
- **THEN** the child receives that argument unchanged and no shell expansion occurs

#### Scenario: Explicit shell execution
- **WHEN** the executable after `--` is an explicit shell with its own arguments
- **THEN** the named shell alone owns parsing and expansion behavior

#### Scenario: Interactive input is attached
- **WHEN** stdin is attached to a terminal
- **THEN** the system rejects the invocation before the child starts and explains that v1 is non-interactive

### Requirement: Execute-once process semantics
The system MUST launch the child at most once, forward supported termination
signals to its process group, preserve non-terminal stdin behavior, and return
the documented child exit outcome.

#### Scenario: Projection fails after child execution
- **WHEN** the child performs a side effect once and projection subsequently fails
- **THEN** the system emits complete captured output without rerunning the child and preserves the child's exit outcome

#### Scenario: Child is cancelled
- **WHEN** the wrapper receives a supported termination signal
- **THEN** the signal reaches the child process group and no managed descendant remains running

### Requirement: Stream-attributed bounded capture
The system SHALL preserve the original bytes and order within stdout and stderr
separately, SHALL NOT claim a reconstructed total order across streams, and MUST
bound raw capture without writing command bodies to disk.

#### Scenario: Output contains arbitrary bytes
- **WHEN** output contains invalid UTF-8, NUL, ANSI control sequences, carriage returns, or a partial final line
- **THEN** every emitted source byte remains attributable to its original stream and byte range

#### Scenario: Capture limit is exceeded
- **WHEN** either capture buffer reaches its configured ceiling
- **THEN** the system emits complete buffered output, passes subsequent output through, discloses that projection was abandoned, and preserves the child exit outcome

### Requirement: Evidence-block segmentation
The system SHALL segment only recognized command families into complete evidence
blocks, retain stable source provenance, and emit original bytes rather than
canonicalized text.

#### Scenario: Complete stack trace
- **WHEN** a recognized failure contains a multiline stack or causal chain
- **THEN** segmentation keeps the trace and its attached cause as one evidence block

#### Scenario: Family or parse is uncertain
- **WHEN** the system does not recognize the family or cannot parse it above the required confidence
- **THEN** the output passes through completely without a compression claim

### Requirement: Safe canonicalization
The system SHALL use canonicalization only for template identity and ranking,
and MUST distinguish blocks whose outcome, location, expected/actual value, or
diagnostic payload differs.

#### Scenario: Volatile identifiers repeat a template
- **WHEN** blocks differ only in recognized timestamps, durations, PIDs, UUIDs, hashes, or generated IDs
- **THEN** the system may rank them as one template while emitting selected blocks from original bytes

#### Scenario: Canonicalization changes diagnostic meaning
- **WHEN** two blocks have different pass/fail outcomes or diagnostic values
- **THEN** they remain separate templates and neither can suppress the other as a duplicate

### Requirement: Mandatory evidence preservation
Each supported family MUST define mandatory evidence gates and SHALL select all
mandatory blocks before applying an output budget.

#### Scenario: Mandatory evidence exceeds budget
- **WHEN** complete failure evidence is larger than the configured budget
- **THEN** the system emits the mandatory evidence over budget and discloses the overflow

#### Scenario: Semantic failure appears on stdout with exit zero
- **WHEN** a supported family reports a recognized failure on stdout despite a zero exit status
- **THEN** the failure and its required context remain mandatory

### Requirement: Deterministic budgeted selection
For the same captured event fixture, configuration, and eligible graph snapshot,
the system MUST produce byte-identical selected output and disclosure independent
of map iteration, concurrency, locale, wall clock, and prior runs.

#### Scenario: Equal-ranked optional blocks
- **WHEN** optional blocks receive equal scores
- **THEN** a documented stable source-order tie break selects them deterministically

#### Scenario: Repeated replay
- **WHEN** the same fixture is projected 100 times under varied supported concurrency settings
- **THEN** every output and disclosure is byte-identical

### Requirement: Explicit omission disclosure
Every projected result SHALL disclose source bytes, emitted bytes, selected and
omitted block counts, family and confidence, budget overflow, and fallback reason,
and SHALL satisfy `source blocks = emitted blocks + omitted blocks`.

#### Scenario: Optional blocks are omitted
- **WHEN** ranking excludes one or more optional blocks
- **THEN** the disclosure identifies their stable block IDs and canonical summaries without retaining or printing their raw bodies

#### Scenario: No durable recall
- **WHEN** projection completes
- **THEN** no command body or secret-bearing argv is persisted for later recall

### Requirement: Optional graph impact cannot weaken evidence
The system MUST keep graph impact disabled unless its independent acceptance gate
passes, and any enabled boost SHALL be capped, exact-resolution-only, and unable
to alter mandatory classification or confidence.

#### Scenario: Reference is ambiguous or store is stale
- **WHEN** an output reference does not resolve exactly or the graph snapshot is partial or stale
- **THEN** that reference receives no graph-impact boost

### Requirement: Family-level quality gate
A command-family projector SHALL remain pass-through until the frozen benchmark
shows perfect critical-evidence recall and meets the accepted downstream quality
and reduction thresholds.

#### Scenario: Held-out mandatory omission
- **WHEN** a candidate family projector omits any held-out critical block
- **THEN** that family is not enabled in product code
