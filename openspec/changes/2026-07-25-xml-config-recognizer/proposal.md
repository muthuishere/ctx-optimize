# ADR — XML config as a NATIVE recognizer (Spring beans, logback, web.xml, …)

Status: **CLOSED — NOT SHIPPING** (2026-07-25). Kept as the reasoning trail; the
scope collapsed to legacy-only under the owner's own rules, and the effort is
better spent elsewhere (see "Where the effort should go instead").

Decision sequence, all owner: *"add xml ensure all these config spring beans
whatever"* → *"dont ship spring as it will be issue"* → (asked whether `web.xml`
was worth keeping) → **"its burden then no"**.

## Why this closes rather than shipping a reduced version

Removing Spring took out the only high-traffic, still-current shape. Removing
`web.xml` took out the rest of the servlet-era surface. What remained was:

| shape | honest value |
|---|---|
| logback / log4j2 | appenders + loggers — modest; rarely the question an agent is asked |
| `web.config` / `app.config` | **legacy .NET** — modern .NET config is `appsettings.json` (see below) |
| `persistence.xml` | JPA units — thin, and annotation-driven config is the norm now |
| Ant `build.xml` | legacy build tool; measured 8 of 9 local files vendored inside chromium |

Every survivor is a *legacy* format. Shipping a producer for them means carrying a
new lane, new node kinds, a secret-refusal surface, fixtures and golden
snapshots — permanently — for facts few users will ask about. That is the
"burden" judgement, and it is correct: the cost is ongoing, the payoff is a
declining slice.

This is NOT a reversal of the literal-only rule — every dropped item *would* have
been literal and safe. It fails a different bar: **worth maintaining.**

## Where the effort should go instead

Same literal-only discipline, current formats, high traffic:

1. **Docker + Compose** (measured: `Dockerfile` → 1 node; `compose.yaml` → 17
   flat keys, image VALUES lost, `depends_on` not an edge) — and it reuses the
   existing `service`/`image` kinds and `uses_image`/`depends_on` relations, so no
   new vocabulary.
2. **Build-file gaps**: gradle map-notation deps silently missed, gradle `task`
   never extracted, CMake/sbt/Rake/Bazel/meson at zero.
3. **`appsettings.json` and JSON config generally — the gap this ADR accidentally
   exposed.** `configFile()` matches `manifestNames[name] || configExts[ext]`
   (`internal/extract/markdown/markdown.go`); `.json` is NOT in `configExts` and
   `manifestNames` lists only `package.json`. So **`appsettings.json`,
   `tsconfig.json`, `launch.json`, `*.deps.json` produce nothing today** —
   derived from those two lookup tables, not measured. That is the MODERN
   replacement for `web.config`, it is plain literal key/value, and it is a
   bigger real gap than everything this ADR proposed.

   **MEASURED, not derived:** `appsettings.json` → **NO NODES**;
   `tsconfig.json` → **NO NODES**; `package.json` → recognized (config +
   `dep:npm/react`).

   Two constraints for whoever picks this up:
   - `.json` cannot simply join `configExts` — it must be proven not to repeat
     the `.xml` junk result, and data-shaped JSON (`package-lock`, fixtures,
     `*.min.json`, large arrays) must stay excluded.
   - 🔴 **It inherits the secret-refusal requirement in full.** The probe's
     `appsettings.json` held
     `"ConnectionStrings": {"Default": "…User=sa;Password=hunter2"}`, and the
     store contains no trace of it — verified. But that safety is *accidental*:
     it holds only because we never read the file. The moment JSON config is
     indexed, `ConnectionStrings` becomes a live credential-leak path into a
     graph that gets committed and pushed. Ship the value-refusal test FIRST,
     exactly as specified in the "must never leak a secret" section above.

## Decision history (recorded plainly, including the flips)

1. `2026-07-25-structured-formats` **rejected** XML config packs (P1) on
   prevalence measured in the owner's own workspace, and rejected `.xml` in
   `configExts` (P1b) on measured junk.
2. Owner then required *"any enterprise can adapt without adapters"*; I flagged
   that the P1 prevalence argument was workspace-local and does not hold for
   enterprise Java/.NET.
3. Owner: *"remove xml"* → dropped.
4. Owner: **"add xml ensure all these config spring beans whatever"** → **XML
   config is IN**, this ADR. Final.

## What must be true (from the measured spike, `spike-p1.md`)

The pack path **cannot** serve this, so this is a **native recognizer** in
`internal/extract/manifests` (the `dotnet.go`/`k8s.go` shape), NOT a manifest
pack:

- a selector yields ONE value per match, so it cannot pair two attributes of one
  element — but a Spring bean IS `id` + `class` together, and a logback appender
  IS `name` + `class` together. Unpairable ⇒ unexpressible.
- `emit` is only `{dependency, task}`; a bean/appender/servlet is neither. The
  spike measured the wrong-kind fallout: `ant-property:build.dir [task]`.
- selectors are root-anchored and exact-depth with no descendant operator, so
  `logback.xml` and profile-nested variants need separate rules.
- no node→node edges are possible, so bean→bean `ref` wiring and servlet↔mapping
  cannot be expressed at all.

**P1b stays dead**: `.xml` must NOT go into `configExts`. Measured 506 real XML
files → 25,091 `config_key` nodes, **97.2% markup junk** (`<a` 9,435, `xmlns`
952). Recognition is by SHAPE (root element / namespace), never by extension.

## 🔴 Non-negotiable: this lane must never leak a secret

XML config is where enterprise credentials live. `web.config` has
`<connectionStrings>`; Spring beans have
`<property name="password" value="…"/>`; `context.xml` has DataSource
`password=`. The repo rule is absolute — secrets must never be stored, printed,
or logged, and must never enter model context.

Requirements:

1. **Emit identity, never values.** Same doctrine the k8s lane already applies to
   `kind: Secret` ("identity only, data never read"). A bean gets its `id` and
   `class`; a `<property>` gets its `name` and, only when it is a `ref`, the
   target — **never a `value=`**.
2. **Hard refusal list on attribute/element names**: anything matching
   `password`, `passwd`, `pwd`, `secret`, `credential`, `token`, `apikey`,
   `api-key`, `accesskey`, `privatekey`, `connectionstring`, `sas`. Refuse the
   VALUE; the name may be recorded.
3. `secretName()` filename refusal already applies (`manifests.go`) and stays.
4. A test must assert that a fixture containing a real-looking password and a
   full JDBC connection string with credentials produces **no node, label, or
   metadata containing any part of the value**. This is the test that keeps the
   promise honest — write it first.

## Shapes to recognize (by root element / namespace, not filename)

| shape | detect | emit |
|---|---|---|
| ~~**Spring beans**~~ | — | **DROPPED — owner 2026-07-25: "dont ship spring as it will be issue".** See below. |
| **logback / log4j2** | root `<configuration>` with `<appender>` | `appender` node (`name` + `class` metadata), `logger` node per `<logger name=>`, `<root>`; `<appender-ref ref=>` → `logger --uses--> appender` |
| **web.xml** | root `<web-app>` | `servlet` node (`servlet-name` + `servlet-class`), `filter` node; `<servlet-mapping>` `<url-pattern>` → **`route` node** joined to its servlet |
| **.NET web.config / app.config** | root `<configuration>` with `<appSettings>` | `config_key` per `<add key=>` (**key only, never value**); `<connectionStrings>` → name only, **value refused** |
| **persistence.xml** | root `<persistence>` | `module` node per `<persistence-unit name=>` |
| **Ant build.xml** | root `<project>` | `task` node per `<target name=>`; `depends=` → `task --depends_on--> task` |

All parsing via stdlib `encoding/xml` (the `dotnet.go` precedent). Every node
carries a real `Location` (`Decoder.InputOffset()` → line, the S3 technique).

## Spring is OUT — and why that is the right call

Owner, 2026-07-25: **"dont ship spring as it will be issue."** Agreed, and the
reasons are concrete rather than a matter of taste:

- Spring XML is the least literal member of this family. `<context:component-scan>`
  declares beans that **do not appear in the file at all**; `<import resource=>`
  splits a context across files; `${…}` placeholders mean a bean's `class` may not
  be a literal class name; `<beans profile=>` makes the same file describe
  different graphs per environment; and `@Configuration`/annotation-driven beans
  (the modern default) are invisible to any XML reader.
- Under the governing rule — *literal only, if there is a 1% chance of being
  wrong, skip it* — a Spring recognizer would routinely be wrong: it would show a
  partial bean graph while implying completeness, which is the worst failure mode
  we have (a confident partial answer).
- Modern Spring is annotation-based, so XML would describe a shrinking legacy
  slice while looking authoritative.

**Consequence: the cross-lane code link is dropped from this change too.** Its
value came mostly from beans; `web.xml`'s `<servlet-class>` is the same mechanism
but web.xml is legacy and declining, so the remaining payoff does not justify
introducing a synthesized-edge lane. This change ships **pure literal
recognition, zero inferred edges** — which also means zero risk to the existing
`calls` graph (no new names enter any resolver) and no need for the
edge-loss spike.

If the code link is ever wanted, it returns as its own ADR with the FQN-ambiguity
spike, not smuggled in here.

## Final scope

**IN** (all pure literal reads): logback/log4j2 appenders + loggers · `web.xml`
servlets/filters/url-patterns · `web.config`/`app.config` appSettings keys and
connectionString **names** · `persistence.xml` persistence-units · Ant
`build.xml` targets + `depends`.

**OUT**: Spring beans (above) · any code-link edge · `.xml` in `configExts`
(P1b, 97.2% junk) · anything requiring placeholder/scan resolution.

## Spike required before building

1. **Fixtures**: logback (plain + nested `<springProfile>` — the FILE shape is
   still worth reading even though Spring beans are out), `web.xml`,
   `web.config`, `persistence.xml`, Ant `build.xml`. Only 2 real `logback*.xml`
   exist locally, so hand-write canonical shapes and label them synthetic.
2. **Secret refusal**: prove the value-refusal requirement holds (the
   password + JDBC-connection-string fixture test).
3. **Junk check**: confirm shape detection does not fire on arbitrary XML — run
   over the 506 real XML files from `spike-p1.md` and report how many produce
   nodes. Anything resembling the 97.2% junk result is a design failure.
4. **Perf**: gather wall-time delta on a Java repo (owner constraint: no
   performance regression). XML files are already walked by the manifests
   producer, so cost should be parse-only on matched shapes.

*(The "no edge loss" spike is no longer needed — with the code link dropped, this
change adds no names to any resolver.)*

## Questions for the owner

1. New node kinds `appender` / `logger` / `servlet` / `filter` — or fold into
   existing kinds? (Recommend new kinds: `--kind servlet` is the query an agent
   would actually write, and kinds are matched exactly.)
2. Is `web.xml` worth it at all, given it is legacy? Dropping it would leave this
   change as: logback + web.config + persistence.xml + Ant.
3. Anything else in the family you want (`struts.xml`, `faces-config.xml`,
   `ivy.xml`)? Default answer under the literal-only rule: only if their facts are
   plain attributes.
