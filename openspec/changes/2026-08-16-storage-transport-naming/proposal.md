# ADR 24 — `storage.local` names a coverage we do not have

Status: ACCEPTED — implemented 2026-08-16
Date: 2026-08-16

## The ask

> "storage.local doesn't look good — storage.file, storage.bucket can be better
> right … or storage.directory"

## What the store says

The transport has exactly ONE rule:

```
storage.local   1 rule:  webstorage
  matches (localStorage|sessionStorage)\.(getItem|setItem|removeItem)\(
  ext .ts .tsx .js .jsx   tier INFERRED
```

There is no file rule, no bucket rule and no directory rule anywhere in
`defaults.json`. The full shipped taxonomy is:

```
config.env      3   env-go, env-js, env-py
network.http    8   http-url-literal, routes-express, routes-go, routes-java …
network.ws      1   websocket-js
process.exec    3   process-go, process-js, process-py
storage.local   1   webstorage
```

So the name is wrong, and the proposed replacements are wrong in the other
direction: `storage.file` and `storage.bucket` would each promise a rule that
does not exist, which is worse than a vague name. `localStorage` is not the
local disk — `local` there is the browser's word for "not session-scoped", and
reading it as a filesystem is exactly the misreading that prompted the
question.

## Decision

**`storage.local` → `storage.browser`.** It names what the one rule actually
records, and nothing more.

The family is kept open and the names are RESERVED in the viewer's key, so a
future rule lands on a sentence rather than the fallback:

| transport | what it would mean | shipped |
|---|---|---|
| `storage.browser` | the browser's own storage | **yes** |
| `storage.file` | files on disk | no rule yet |
| `storage.bucket` | object storage (S3 and friends) | no rule yet |

A `storage.*` nobody has written a rule for is described as "stored data" —
deliberately vague, rather than borrowing a sibling's description.

`storage.bucket` is the one worth writing next: the sources lane already dials
S3 with stdlib SigV4, so the vocabulary and the connector would agree.

## Amendment, same day — one browser storage is three

> "so storage.browser.local and storage.browser.session … storage.browser.cookie"

Right, and for a reason the flat name hid: the three differ in **lifetime** and
in **who else can see them**, which is the part a reader actually needs.

| transport | lifetime | who else sees it |
|---|---|---|
| `storage.browser.local` | outlives the tab | the page's own origin |
| `storage.browser.session` | dies with the tab | the page's own origin |
| `storage.browser.cookie` | until it expires | **the server, on every request** |

A token in a cookie and a token in sessionStorage are not the same fact about a
system, and `storage.browser` said they were.

`webstorage` splits into `webstorage-local`, `webstorage-session` and
`webstorage-cookie`. The cookie rule matches CALL forms only — `Cookies.*`
(js-cookie) and `cookieStore.*` (the Cookie Store API). A bare `document.cookie`
is a two-element member chain with no trailing property to name, and the
`member` shape takes the property AFTER its path (`process.env.X` names X), so
it is **deliberately not claimed** rather than shipped as a rule that matches
nothing — the same discipline that rejected `storage.file` above.

### The before/after diff the tier gate demanded

Adding rules trips `TestShippedRulesDeclareTierAndEvidence` ("shipped rule
count moved: 18 (was 16) — re-run the before/after port diff"). Run with
`--force` on two real repos:

```
agentic-nexus    69 → 69 ports     1 storage.browser → 1 …browser.session
volentis       1066 → 1067 ports  47 storage.browser → 40 local + 7 session + 1 cookie
```

Every existing port was RECLASSIFIED; none was lost. The single new one is a
real site the old rule could not see:

```
Cookies.set('lang', userLang, { expires: 365 });
  apps/librechat/client/src/components/Nav/SettingsTabs/General/General.tsx:148
```

No localStorage/sessionStorage shape matches that line, so a cookie this app
sets on every visitor was outside the boundary picture entirely.

### Curve labels

`shortTransport` now takes the LAST segment, not everything after the first
dot: `storage.browser.local` reads `LOCAL`, not `BROWSER.LOCAL`. The full name
is in the key — one row per mode — so the label on a curve only has to be
recognisable.

## Migration

Port ids carry the transport (`port:storage.browser:>session_token`), so a
store gathered before this change keeps the old id until it is re-gathered.

**A plain `ctx-optimize add .` is NOT enough, and this was measured rather than
assumed.** Running it on volentis left all 14 ports on the old name: nothing in
the REPO changed, the tree signature is identical, and incremental gather
correctly skipped every module. A rule-set change is invisible to a freshness
check that looks at the source tree — which is right, and is also why the
upgrade needs saying out loud.

`ctx-optimize add . --force` rewrites them; verified, 14/14 now
`storage.browser`.

The alternative — an alias table so two names mean one thing forever — is how a
taxonomy stops being a taxonomy, so it is refused. But the honest consequence
is that a store gathered before this build reports the old transport until
someone forces a re-gather, and `boundaries --transport storage.browser` will
find nothing there. Worth knowing before reading a stale store's answer as a
statement about the code.
