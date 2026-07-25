# Spike — P1: XML config via bundled manifest packs

Status: **MEASURED** — 2026-07-25. Answers the two open questions in
`proposal.md` §P1. No product code was touched; everything below was run
against the installed HEAD build (`ctx-optimize 0.0.0-dev`) in scratch repos
with `CTX_OPTIMIZE_STORE` pointed at a temp dir.

---

## VERDICT

**P1 is real but LOW priority — and the mechanism is P1a (bundled packs) only,
with three defects to fix first. P1b (adding `.xml` to `configExts`) must be
REJECTED outright: measured, it produces 25,091 nodes of which 97.2% are XML
markup fragments.**

Three separate verdicts:

| item | verdict | basis |
|---|---|---|
| **P1a** bundled XML packs | **works mechanically; ship only with the 3 fixes below** | 63 nodes from 4 real XML files that produce 0 today; queries hit |
| **P1b** `.xml` → `configExts` | **REJECT — actively harmful** | 25,415 keys over 506 real XML files, 97.2% are `<w` / `</a` / `xmlns` |
| **P1** overall priority | **DOWN-rank below P2/P3** | in this user's ~/muthu/gitworkspace, the P1-target files number **2 logback + 1 non-vendored build.xml + 0 spring beans**; every other hit is inside the vendored `chromium/third_party` checkout |

The selector language **can** express most of the real shapes — including
element text, which was the open worry. It **cannot** express three things that
matter, and the `emit` vocabulary has no valid kind for any of the facts P1
wants to emit. So P1a is not "pure data": it needs a **new `emit` kind** plus a
**Location field** plus a **namespace-aware node ID**. That is a small binary
change, not zero.

### The three fixes P1a requires

1. **New `emit` kind.** `validEmits` (`packs.go:60`) is `{dependency, task}`. A
   logback appender, a spring bean, a servlet, an Ant `<property>` are **none of
   these**. Shipping packs today means labelling all of them `task`, which is a
   lie in the graph — measured output includes `ant-property:build.dir [task]`
   and `lb-appender-name:CONSOLE [task]`. Needs `config_key` (or `resource`,
   which the proposal shows already exists in this repo's own store).
2. **Pack nodes carry no `Location`.** `applyPackRule` (`packs.go:216-222`) sets
   no `Location`, so every pack node cites a file with no line. Measured node:
   `{"id":"logback-spring.xml::task:CONSOLE", ... "source":"logback-spring.xml"}`
   — no `location` key. This breaks the product's "exact file:line" promise for
   the entire pack surface.
3. **Node ID is namespace-blind → silent cross-rule dedup.** ID is
   `rel + "::task:" + name` (`packs.go:217`), ignoring `namespace`. Two rules in
   the same pack that yield the same string collapse to one node and the second
   rule's namespace is **silently lost**. Measured below (§3.2).

### Doc bug found (independent of whether P1 ships)

The package-comment example at **`internal/extract/manifests/packs.go:17-18`**
is wrong:

```
{"file": "*.build.xml", "format": "xml", "path": "target/@name", "emit": "task"}
```

`xmlSelect` is **root-anchored and exact-depth** (`matches()` requires
`len(stack) == len(segs)`, `packs.go:405`). Measured against a real Ant
`build.xml` with 19 `<target>` elements, `"path": "target/@name"` yields
**0 nodes**; `"project/target/@name"` yields **19**. Also `*.build.xml` does not
`path.Match` the basename `build.xml`. Anyone copying the shipped example gets
silence. Worth fixing in the doc regardless of the P1 decision.

---

## 1. The gap is confirmed: 0 nodes

Scratch git repo with four **real** files copied out of
`~/muthu/gitworkspace` (plus one hand-written canonical spring beans file,
because zero exist in the user's world — see §2):

- `logback-spring.xml` — from `enterprisewebagent/apps/server/src/main/resources/`
- `web.xml` — from `chromium/third_party/libphonenumber/src/java/demo/.../WEB-INF/`
- `build.xml` — from `chromium/third_party/libphonenumber/src/java/` (19 targets, 22 properties)
- `applicationContext.xml` — hand-written canonical Spring shape (3 beans, 5 `<property>`)

```
$ CTX_OPTIMIZE_STORE=<scratch>/store ctx-optimize add .
added 0 nodes → .../store/repo
wiki: 1 pages
$ ctx-optimize nodes   → (0 nodes)
$ ctx-optimize edges   → (0 edges)
```

**Measured: 4 real XML config files, 378 lines, → 0 nodes, 0 edges.** Not even
file nodes. Gap as described in the proposal.

## 2. Prevalence — this is the finding that down-ranks P1

`find ~/muthu/gitworkspace` excluding `node_modules`, `.git`, `.venv`:

| kind | total | excl. `./chromium` | status |
|---|---|---|---|
| `pom.xml` | 45 | 20 | **already handled** (`manifestNames`) |
| `*.csproj` | 29 | 29 | **already handled** |
| `build.xml` (Ant) | 9 | **1** | P1 target |
| `web.xml` | 3 | **0** | P1 target |
| `logback*.xml` | 2 | 2 (same file, two copies of one project) | P1 target |
| `applicationContext*.xml` / `spring*.xml` | 0 | 0 | P1 target |
| `ivy.xml` | 0 | 0 | P1 target |
| `persistence.xml` | 0 | 0 | P1 target |
| `faces-config.xml` | 0 | 0 | P1 target |

Cross-check for spring beans by content, not filename:
`grep -rl --include='*.xml' -m1 '<beans' ~/muthu/gitworkspace` → **0 files.**

Total `*.xml`: **5,928** including chromium, **538** excluding it. So XML is
abundant *as data* and near-absent *as the config kinds P1 targets*.

**Honest read:** 8 of 9 Ant builds and 3 of 3 `web.xml` live inside the vendored
`chromium/third_party` tree — third-party code the user does not maintain. The
genuine P1-addressable surface in this user's world is **one Spring Boot
project's `logback-spring.xml`**. That is a legitimate reason to rank P1 below
P2 (python/rust deps) and P3 (yaml paths), both of which touch files this user
has by the dozen. It is *not* a reason to call the mechanism broken — a Java
consultancy repo would look completely different.

## 3. Capability matrix — what the tiny selector can actually express

Read of `xmlSelect` (`packs.go:389-452`) establishes the contract:

- Path is **absolute from the document root** and **exact-depth**
  (`len(stack) == len(segs)`). No `//` descendant axis.
- `*` matches **any one element name at that one level**, never a variable depth.
- Trailing `/@attr` yields that attribute's value; **no** `/@attr` yields the
  matched element's trimmed character content — **including nested elements**.
- One match yields **one string**. There is no way to correlate two attributes
  of the same element, and `pair.version` is never populated for xml
  ("No version channel — xml rules yield names only", `packs.go:387`).

### 3.1 Tested for real

Pack dropped at `<scratch-repo>/.ctxoptimize/manifests/xmlprobe.json`, 15 rules,
then `ctx-optimize add .` → `manifests: 63 nodes, 63 edges`.

| shape wanted | selector | expressible | what I actually got |
|---|---|---|---|
| logback `<appender name>` (spring-profile nested) | `configuration/springProfile/appender/@name` | **yes** | 2 nodes: `CONSOLE`, `JSON` |
| logback `<appender class>` | `configuration/springProfile/appender/@class` | **yes, but** | **1** node, not 2 — both appenders share `ch.qos.logback.core.ConsoleAppender`, IDs collided |
| appender **name AND class on one node** | — | **NO** | one rule = one string; two rules = two unrelated nodes, no edge between them |
| logback appender at *any* depth (`logback.xml` top-level **and** `logback-spring.xml` nested) | — | **NO** | needs one rule per depth: `configuration/appender/@name` (2 nodes on plain `logback.xml`) and `configuration/springProfile/appender/@name` (2 on the spring one). No descendant axis |
| logback `<logger name>` | `configuration/logger/@name` | **yes** | 1 node: `com.acme.orders` |
| logback `<appender-ref ref>` | `configuration/root/appender-ref/@ref` | **yes** | 1 node in isolation; **0** when co-shipped — see §3.2 |
| logback nested element text `<file>` | `configuration/appender/file` | **yes** | 1 node: `logs/app.log` |
| logback deep element text `<encoder><pattern>` | `configuration/appender/encoder/pattern` | **yes** | 1 node: `%d %msg%n` |
| spring `<bean id>` | `beans/bean/@id` | **yes** | 3 nodes: `dataSource`, `orderRepository`, `orderService` |
| spring `<bean class>` | `beans/bean/@class` | **yes** | 3 `dependency` nodes (`dep:bean-class/com.acme.orders.OrderServiceImpl`, …) + 3 `declares` edges |
| spring nested `<property name>` | `beans/bean/property/@name` | **yes** | 4 nodes (5 `<property>`, `dataSource` name collided with the bean id) |
| spring `<property ref>` → **edge bean→bean** | — | **NO** | `@ref` yields a name; `applyPackRule` can only emit file→node `contains` / `declares`. No node→node edge exists in the mechanism |
| **web.xml value in a CHILD ELEMENT** `<servlet-name>x</servlet-name>` | `web-app/servlet/servlet-name` | **YES** | 2 nodes: `PhoneNumberParser`, `Input`. **The open worry is resolved — element text is reachable** |
| web.xml `<servlet-class>` | `web-app/servlet/servlet-class` | **yes** | 2 `dependency` nodes (`dep:servlet-class/com.google.phonenumbers.demo.ResultServlet`, …) |
| web.xml `<url-pattern>` | `web-app/servlet-mapping/url-pattern` | **yes** | 3 nodes: `/`, `/inputform`, `/phonenumberparser` |
| web.xml servlet-name ↔ its mapping (**an edge**) | — | **NO** | same reason: no node→node edges |
| Ant `<target name>` | `project/target/@name` | **yes** | 19 nodes |
| Ant `<target name>` per the **shipped doc example** | `target/@name` | **NO** | **0 nodes** — root-anchored (doc bug, see verdict) |
| Ant `depends="compile"` → **depends_on edge** | — | **NO** | `project/target/@depends` yields the raw attribute string. Got 3 garbage nodes whose labels are comma lists: `ant-depends:test-jar, testname`, `ant-depends:clean,jar`, `ant-depends:download-jars,build-phone-metadata,…,build-timezones-data`. **No list splitting, no edge.** |
| Ant `<property name>` | `project/property/@name` | yes, but **wrong kind** | 22 nodes emitted as `[task]` — e.g. `ant-property:build.dir [task]`. They are config keys, not tasks |

### 3.2 Measured ID-collision bug

In the multi-rule run, `configuration/root/appender-ref/@ref` produced
**0 nodes**. Re-run in isolation in a second scratch repo: the *same* selector
produced **1 node** (`APPENDER-REF:STDOUT`). Cause: the node ID
`logback.xml::task:STDOUT` was already claimed by the `@name` rule, and
`namespace` is label-only. Likewise `*/appender/@name` produced 0 in the
co-shipped run and 2 in isolation.

**Consequence for shipping packs:** any pack with more than one rule over the
same file silently loses whichever rule runs second on a shared value, and the
loss is invisible — no warning, no count. A shipped multi-rule logback pack
would be quietly lossy today.

### 3.3 Findability does work

Against the packed store (63 nodes), queries hit:

```
$ ctx-optimize query "logback appender CONSOLE"
lb-appender-name:CONSOLE  [task]  logback-spring.xml
$ ctx-optimize query "servlet PhoneNumberParser"
servlet-name:PhoneNumberParser  [task]  web.xml
$ ctx-optimize query "ant build target compile"
ant-target:compile  [task]  build.xml
```

Consistent with the proposal's "findable vs connected" framing: P1a buys
findability, and — because packs cannot emit node→node edges at all — it buys
**zero** connectedness. Every one of the 63 edges is `file --contains-->` or
`file --declares-->`.

## 4. P1b floor check — 0 useful nodes, 25,091 noise nodes

Prediction from reading `extractConfig` (`markdown.go:146-192`): it looks for
top-level `key: value` / `key = value` / `[section]`; XML has none of those
shapes, so yield should be ~0 real keys but **non-zero garbage**, because
`strings.ContainsAny(t, ":=")` fires on any line containing `=` — i.e. every XML
attribute — and on any `:` such as a namespace prefix.

Confirmed with a verbatim Go reimplementation of the loop (including `slug`),
run on real files:

| real file | config_key nodes | keys produced |
|---|---|---|
| `logback-spring.xml` (real) | **0** | — |
| `logback.xml` (canonical) | **0** | — |
| `web.xml` (real) | 3 | `version`, `xmlns`, `http` |
| `build.xml` (real Ant, 19 targets) | 8 | `value`, `depends`, `destdir`, `destdir`, `includeAntRuntime`, `depends`, `failureProperty`, `if` |
| `applicationContext.xml` | 3 | `xmlns`, `xsi`, `http` |

**Not one meaningful key in any file.** No appender, no bean id, no target name,
no servlet — because those all live in *indented* lines or in attributes on the
same line as the element name, and the guard skips indented lines with trailing
whitespace while the `:=` cut grabs whatever attribute happens to be first.

Aggregate over **every** real non-chromium, non-`pom.xml` `*.xml` in
`~/muthu/gitworkspace` that passes the existing 256 KB `maxConfigBytes` cap
(506 files):

```
files_under_256KB=506  total_config_key=25091  zero_yield=139
mean=49.6  median=5  p90=200  max=350
total_keys=25415  markup_or_ns_junk=24721  pct_junk=97.2%
```

Top keys across the whole corpus: `<a` (9,435), `<w` (6,495), `</a` (3,750),
`</w` (1,290), `<p` (1,065), `xmlns` (952), `</p` (780) — Office Open XML and
SVG markup fragments, from single-line XML where the `:`/`=` cut lands mid-tag.
The 2.8% residue is no better: `or`, `such`, `open`, `no`, `directory`,
`plt.figure(figsize`.

Without the size cap the number is **379,741** config_key nodes over 537 files.

**P1b is not a "floor" — it is a 25,000-node poisoning of query ranking with
zero semantic gain. Reject it, and reject P1c with it.** Only P1a is on the
table.

---

## Answers to the proposal's open questions

> Open: does the tiny selector language cover spring `<bean class=…>` and
> `<property name=… ref=…>`?

**Partly.** `<bean class>` and `<property name>` and `<property ref>` are each
individually expressible and each yields nodes (measured). What is **not**
expressible is anything relational: `name`+`ref` on the same element cannot
become one fact, and `ref` cannot become an edge to the bean it points at. Same
for `web.xml` servlet↔mapping and Ant `target depends`. Per the standing
doctrine quoted in the proposal ("if it can't express it, the answer is an
adapter, not a bigger language"), **the relational half of every P1 shape is
adapter territory, not pack territory** — and an adapter is not "zero new
mechanism, pure data".

## Recommendation

1. **Reject P1b / P1c** on the measured 97.2% junk rate. This is the strongest
   result in the spike.
2. **Fix the `packs.go:17-18` doc example** now, independent of P1 — it teaches
   a selector that measurably returns nothing.
3. **Fix the namespace-blind node ID** (§3.2) before any multi-rule pack ships
   anywhere; today it is a silent-loss bug in a shipped mechanism, not a P1
   issue.
4. **P1a: park behind P2 and P3** for this user, on the prevalence table. If it
   does ship, scope it to what packs can honestly do — `logback`, Ant `target`,
   `bean id`/`class`, `servlet-name`/`servlet-class` as **findability-only**
   nodes — and budget the new `emit` kind + `Location` as part of the work.
   Do not describe it as "pure data".

## Commands used

```bash
# gap + capability tests (scratch repos, hermetic store)
CTX_OPTIMIZE_STORE=<scratch>/store ctx-optimize add .
CTX_OPTIMIZE_STORE=<scratch>/store ctx-optimize nodes|edges|query|card
# packs dropped at <scratch-repo>/.ctxoptimize/manifests/<name>.json

# prevalence
find ~/muthu/gitworkspace \( -name node_modules -o -name .git -o -name .venv \) -prune \
  -o -type f \( -name 'logback*.xml' -o -name web.xml -o -name 'applicationContext*.xml' \
  -o -name 'spring*.xml' -o -name build.xml -o -name ivy.xml -o -name '*.csproj' \
  -o -name pom.xml -o -name persistence.xml -o -name 'faces-config.xml' \) -print \
  | sed 's|.*/||' | sort | uniq -c | sort -rn
grep -rl --include='*.xml' -m1 '<beans' ~/muthu/gitworkspace   # → 0

# P1b simulation: verbatim Go port of extractConfig's loop (scratch, not in repo)
go run . $(cat xmlist_capped.txt)
```

Scratch dir: `/private/tmp/claude-501/.../scratchpad/spike-p1/`. Zero edits
under `internal/`, `cmd/`, `npm/`.
