## ADDED Requirements

### Requirement: Closed deterministic transform language
The system SHALL expose only a versioned, closed set of structured transform
operators and MUST NOT provide eval, arbitrary callbacks, imports, subprocesses,
network access, or a general scripting escape hatch.

#### Scenario: Unsupported expression is supplied
- **WHEN** a transform contains an unknown operator or arbitrary code expression
- **THEN** validation fails before input is consumed or side effects occur

### Requirement: Structured data transforms
The system SHALL support only spike-accepted operations for parsing, projecting,
filtering, sorting, deduplicating, grouping, aggregating, and validating JSON or
NDJSON while preserving documented null, missing, number, Unicode, and ordering
semantics.

#### Scenario: NDJSON contains a malformed record
- **WHEN** a malformed record appears after valid records
- **THEN** the command returns the documented failure without silently dropping or rewriting the malformed input

#### Scenario: Null differs from missing
- **WHEN** a filter or assertion encounters explicit null and an absent field
- **THEN** the two states remain distinguishable according to the operator contract

### Requirement: Text extraction and verification
The system SHALL support only spike-accepted bounded text or regex extraction,
grouping, aggregation, and assertions with deterministic diagnostics.

#### Scenario: Verification fails
- **WHEN** an assertion does not hold
- **THEN** the command exits non-zero and identifies the failed assertion without emitting a partial success claim

### Requirement: Unsupported programs are rejected
The system MUST reject a requested transformation that cannot be represented
exactly by the supported operators rather than approximate its behavior.

#### Scenario: Python behavior requires general computation
- **WHEN** replayed behavior depends on unsupported control flow, libraries, or side effects
- **THEN** the candidate is classified as not replaceable and no transform is generated

### Requirement: Observable replay equivalence
Every transform declared supported MUST match the reference program's documented
exit status, stdout, stderr, structured value, diagnostics, and filesystem diff
for the frozen replay corpus.

#### Scenario: Candidate output differs from reference
- **WHEN** any supported replay has an unexplained observable mismatch
- **THEN** the responsible operator or capability fails its acceptance gate

### Requirement: Filesystem writes are separately gated
Write operators MUST remain disabled until they prove bounded-root enforcement,
symlink-safe resolution, atomic replacement, and failure cleanup independently
of the read-only transform core.

#### Scenario: Target escapes the allowed root
- **WHEN** a path or symlink would resolve outside the configured root
- **THEN** validation fails before any filesystem mutation occurs

#### Scenario: Atomic write fails
- **WHEN** replacement cannot complete successfully
- **THEN** the original target remains intact and no partial output is presented as committed

### Requirement: Transform capability quality gate
Structured transforms SHALL NOT enter product code unless the frozen census and
replay spike meets the accepted replaceability, equivalence, and usability gates.

#### Scenario: Coverage requires a scripting escape hatch
- **WHEN** the measured coverage threshold can be reached only by adding general eval or callbacks
- **THEN** the capability is rejected rather than expanded into a scripting language
