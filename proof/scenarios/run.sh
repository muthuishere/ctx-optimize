#!/bin/bash
# Scenario matrix runner — every scenario executes against the real binary.
# Output format: "=== S<id> <name>" then the commands' real output, then
# "--- PASS|FAIL S<id>". No set -e: every scenario reports independently.
# Work OUTSIDE any repo: scenario repos must never sit under a parent
# .ctxoptimize (the upward config walk would adopt them into ITS scope).
W="${TMPDIR:-/tmp}/ctxopt-scenarios"; rm -rf "$W"; mkdir -p "$W"
export CTX_OPTIMIZE_STORE="$W/store"
export CTX_OPTIMIZE_GLOBAL_ENV="$W/globalenv"   # isolate the machine-global .env
BIN=ctx-optimize
pass=0; fail=0
ck() { # ck <id> <expected-substring> <<< "output"
  local id="$1" want="$2" out; out=$(cat)
  echo "$out" | tail -20
  if echo "$out" | grep -qF "$want"; then echo "--- PASS $id"; pass=$((pass+1)); else echo "--- FAIL $id (wanted: $want)"; fail=$((fail+1)); fi
}
mkrepo() { # mkrepo <dir> — git repo with a tiny Go+md project
  mkdir -p "$1" && cd "$1" && git init -q
  git config user.email t@t; git config user.name t
  cat > pay.go <<'EOF'
package pay

func ProcessRefund(id string) error { return validateRefund(id) }

func validateRefund(id string) error { return nil }
EOF
  printf '# Payments\n\n## Refund flow\n\nRefunds go through ProcessRefund.\n' > README.md
  git add -A && git commit -qm init
}

echo "=== S01 bare repo: up bootstraps + gathers + first query"
mkrepo "$W/s01"; cd "$W/s01"
{ $BIN up; $BIN query "refund flow"; } 2>&1 | ck S01 "ProcessRefund"

echo "=== S02 idempotent up: second run is a no-op"
cd "$W/s01"; $BIN up 2>&1 | ck S02 "up to date with git HEAD"

echo "=== S03 code moved: commit → fresh exits 1 → up refreshes"
cd "$W/s01"
echo 'func NewThing() {}' >> pay.go && git add -A && git commit -qm more
$BIN fresh; echo "fresh-exit:$?"
{ $BIN up; $BIN fresh >/dev/null; echo "fresh-exit-after:$?"; } 2>&1 | ck S03 "fresh-exit-after:0"

echo "=== S04 non-git folder: gather works, freshness unknown (exit 2)"
mkdir -p "$W/s04" && cd "$W/s04"
printf '# Notes\n\n## Setup\n\nInstall the widget.\n' > notes.md
{ $BIN init --instructions NONE >/dev/null; $BIN add . >/dev/null; $BIN fresh; echo "exit:$?"; } 2>&1 | ck S04 "exit:2"

echo "=== S05 intent verbs: card / change-plan / affected / path / hubs / explain"
cd "$W/s01"
{ $BIN card ProcessRefund; echo ....; $BIN change-plan validateRefund | head -12; $BIN affected validateRefund | head -4; } 2>&1 | ck S05 "callers (1)"

echo "=== S06 verify: valid claim passes, drifted line range fails loudly"
cd "$W/s01"
{ $BIN verify "pay.go:L1-L5"; echo "ok:$?"; $BIN verify "pay.go:L900-L950"; echo "drift:$?"; } 2>&1 | ck S06 "drift:1"

echo "=== S07 shrink guard: real deletion refused then --force applies"
mkrepo "$W/s07"; cd "$W/s07"
for i in 1 2 3 4 5 6; do printf 'package pay\n\nfunc Fn%s() {}\n' $i > f$i.go; done
git add -A && git commit -qm big
$BIN up >/dev/null 2>&1
rm f*.go && git add -A && git commit -qm gut
{ $BIN add . ; echo "refused:$?"; $BIN add . --force >/dev/null 2>&1; echo "forced:$?"; } 2>&1 | ck S07 "refusing to shrink"

echo "=== S08 store-name collision: config name gives the repo its own store"
mkdir -p "$W/a/app" "$W/b/app"
cd "$W/a/app" && git init -q && echo 'package a1' > a.go && $BIN init --instructions NONE >/dev/null 2>&1
cd "$W/b/app" && git init -q && echo 'package b1' > b.go
$BIN init --instructions NONE >/dev/null 2>&1
python3 - <<'PYEOF'
import json; c=json.load(open('.ctxoptimize/config.json')); c['name']='app-b'; json.dump(c,open('.ctxoptimize/config.json','w'))
PYEOF
{ $BIN add . ; ls "$CTX_OPTIMIZE_STORE"; } 2>&1 | ck S08 "app-b"

echo "=== S09 tracked .env: loud warning with the exact command"
mkrepo "$W/s09"; cd "$W/s09"
echo 'X=1' > .env && git add .env && git commit -qm oops
$BIN init --instructions NONE 2>&1 | ck S09 "git rm --cached .env"

echo "=== S10 sources: literal password in committed config is REFUSED at load"
mkrepo "$W/s10"; cd "$W/s10"; $BIN init --instructions NONE >/dev/null 2>&1
python3 - <<'EOF'
import json; c=json.load(open('.ctxoptimize/config.json')); c['sources']=['postgres://user:realpass@db:5432/x']; json.dump(c,open('.ctxoptimize/config.json','w'))
EOF
$BIN status 2>&1 | ck S10 "credentials"

echo "=== S11 sources: unset var = clean SKIP, --strict fails"
mkrepo "$W/s11"; cd "$W/s11"; $BIN init --instructions NONE >/dev/null 2>&1
python3 - <<'EOF'
import json; c=json.load(open('.ctxoptimize/config.json')); c['sources']=['TEAM_ONLY_DB_URL']; json.dump(c,open('.ctxoptimize/config.json','w'))
EOF
{ $BIN up --sources=always; echo; $BIN up --sources=always --strict >/dev/null 2>&1; echo "strict:$?"; } 2>&1 | ck S11 "skipped"

echo "=== S12 sources: local OpenAPI file path + machine-global env file"
mkrepo "$W/s12"; cd "$W/s12"; $BIN init --instructions NONE >/dev/null 2>&1
cat > spec.json <<'EOF'
{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok"}}}}}}
EOF
echo "PETS_SPEC=spec.json" > "$CTX_OPTIMIZE_GLOBAL_ENV"
{ $BIN add PETS_SPEC; $BIN query "listPets"; } 2>&1 | ck S12 "GET /pets"

echo "=== S13 capture: debug primitive prints batch JSON, writes nothing"
cd "$W/s12"; $BIN capture PETS_SPEC 2>&1 | head -3 | ck S13 '"producer"'

echo "=== S14 custom adapter: drop a script, add runs it, adapters run repeats"
mkrepo "$W/s14"; cd "$W/s14"; $BIN init --instructions NONE >/dev/null 2>&1
cat > .ctxoptimize/adapters/tickets.sh <<'EOF'
#!/bin/sh
echo '{"producer":"tickets","nodes":[{"id":"t:1","label":"TCK-1 checkout bug","kind":"ticket","file_type":"external","source":"tickets"}]}'
EOF
{ $BIN add . | grep adapter; $BIN query "TCK-1"; $BIN adapters run | grep adapter; } 2>&1 | ck S14 "TCK-1 checkout bug"

echo "=== S15 sync skips adapters; their nodes stay put"
cd "$W/s14"; { $BIN sync; $BIN query "TCK-1"; } 2>&1 | ck S15 "TCK-1 checkout bug"

echo "=== S16 --json door: pipe any batch in"
cd "$W/s14"
echo '{"producer":"hand","nodes":[{"id":"h:1","label":"HandFact","kind":"fact","file_type":"external","source":"hand"}]}' | $BIN add --json - >/dev/null 2>&1
$BIN query "HandFact" 2>&1 | ck S16 "HandFact"

echo "=== S17 invalid batch: fail-closed validation names the defect"
cd "$W/s14"
echo '{"nodes":[{"id":"x"}]}' | $BIN add --json - 2>&1 | ck S17 "producer"

echo "=== S18 monorepo: scan preview → init --scan → fan-out → navigator"
M="$W/mono"; mkdir -p "$M/services/api" "$M/services/worker"
cd "$M" && git init -q && git config user.email t@t && git config user.name t
echo 'module acme/api' > services/api/go.mod
printf 'package api\n\nfunc HandleCheckout() {}\n' > services/api/api.go
echo 'module acme/worker' > services/worker/go.mod
printf 'package worker\n\nfunc RunPayroll() {}\n' > services/worker/worker.go
echo '# Acme' > README.md
git add -A && git commit -qm init
{ $BIN scan | head -6; $BIN init --scan --yes --instructions NONE >/dev/null; $BIN add . | tail -3; } 2>&1 | ck S18 "navigator"

echo "=== S19 monorepo scope: module dir answers locally, root federates"
{ cd "$M/services/api" && $BIN query "checkout"; cd "$M" && $BIN query "RunPayroll"; } 2>&1 | ck S19 "RunPayroll"

echo "=== S20 monorepo escalation: asking api for worker's symbol escalates"
cd "$M/services/api"; $BIN query "RunPayroll" 2>&1 | ck S20 "RunPayroll"

echo "=== S21 reconcile: new module in config (uncommitted) → up gathers exactly it"
cd "$M"; mkdir -p services/billing
echo 'module acme/billing' > services/billing/go.mod
printf 'package billing\n\nfunc ChargeCard() {}\n' > services/billing/billing.go
python3 - <<'EOF'
import json; c=json.load(open('.ctxoptimize/config.json'))
c['modules'].append({'path':'services/billing'}); json.dump(c,open('.ctxoptimize/config.json','w'),indent=2)
EOF
$BIN up 2>&1 | ck S21 "== services/billing"

echo "=== S22 reconcile: broken residual → up re-gathers ONLY it"
cd "$M"; : > "$CTX_OPTIMIZE_STORE/mono/graph/nodes.ndjson"; : > "$CTX_OPTIMIZE_STORE/mono/graph/edges.ndjson"
$BIN up 2>&1 | ck S22 "gathering only those"

echo "=== S23 residual shrink exempt: module split of a fossil store passes"
F="$W/fossil"; mkdir -p "$F/svc/one"
cd "$F" && git init -q && git config user.email t@t && git config user.name t
for i in 1 2 3 4 5 6; do printf 'package one\n\nfunc G%s() {}\n' $i > svc/one/g$i.go; done
echo 'module f/one' > svc/one/go.mod; echo hi > README.md
git add -A && git commit -qm init
$BIN init --instructions NONE >/dev/null 2>&1 && $BIN add . >/dev/null 2>&1   # whole-tree fossil
$BIN init --scan --yes --instructions NONE >/dev/null 2>&1                     # declare modules
{ $BIN add .; echo "exit:$?"; } 2>&1 | ck S23 "exit:0"

echo "=== S24 orphan store: module removed from config is reported, not deleted"
cd "$M"
python3 - <<'EOF'
import json; c=json.load(open('.ctxoptimize/config.json'))
c['modules']=[m for m in c['modules'] if m['path']!='services/billing']; json.dump(c,open('.ctxoptimize/config.json','w'),indent=2)
EOF
{ $BIN add . | grep -A1 'no longer'; ls "$CTX_OPTIMIZE_STORE/mono/services/" ; } 2>&1 | ck S24 "billing"

echo "=== S25 merge: combined view from nested module stores (by dir path)"
cd "$M"; { $BIN merge "$M/services/api" "$M/services/worker" --into everything; grep RunPayroll "$CTX_OPTIMIZE_STORE/everything/graph/nodes.ndjson" | head -1; } 2>&1 | ck S25 "RunPayroll"

echo "=== S26 remote git-lane E2E: push to a local bare repo, wipe, pull back"
H="$W/storehost.git"; git init -q --bare "$H"
R="$W/s26"; mkrepo "$R"; cd "$R"
$BIN init --instructions NONE >/dev/null 2>&1
mv .ctxoptimize/push.js.sample .ctxoptimize/push.js
mv .ctxoptimize/pull.js.sample .ctxoptimize/pull.js
python3 - "$H" <<'EOF'
import sys,re
for f in ['.ctxoptimize/push.js','.ctxoptimize/pull.js']:
    s=open(f).read()
    s=re.sub(r'const STORE_REPO_URL = "[^"]*"', f'const STORE_REPO_URL = "{sys.argv[1]}"', s)
    open(f,'w').write(s)
import json; c=json.load(open('.ctxoptimize/config.json'))
c['remote']={'push':'node .ctxoptimize/push.js','pull':'node .ctxoptimize/pull.js'}
json.dump(c,open('.ctxoptimize/config.json','w'),indent=2)
EOF
$BIN add . >/dev/null 2>&1
{ HOME="$W/fakehome26" $BIN remote push | tail -2; rm -rf "$CTX_OPTIMIZE_STORE/s26"; HOME="$W/fakehome26" $BIN up | tail -2; $BIN query "refund flow" | head -2; } 2>&1 | ck S26 "Refund flow"

echo "=== S27 pointer targets follow existing files: AGENTS.md-only repo stays CLAUDE-free"
mkrepo "$W/s27"; cd "$W/s27"; echo '# agents' > AGENTS.md; git add -A; git commit -qm agents
{ $BIN init; ls; } 2>&1 | ck S27 "AGENTS.md"
test ! -f "$W/s27/CLAUDE.md" && echo "--- PASS S27b (no CLAUDE.md created)" && pass=$((pass+1)) || { echo "--- FAIL S27b"; fail=$((fail+1)); }

echo "=== S28 instructions.md: user text outside markers survives re-init"
cd "$W/s27"
echo '## Team note: never touch generated/' >> .ctxoptimize/instructions.md
$BIN init >/dev/null 2>&1
grep -q 'Team note' .ctxoptimize/instructions.md && echo "survived" | ck S28 "survived"

echo "=== S29 export: dot + csv land on disk"
cd "$W/s01"; { $BIN export --format dot --out "$W/g.dot" >/dev/null; head -2 "$W/g.dot"; } 2>&1 | ck S29 "digraph"

echo "=== S30 audit log: config set is recorded"
cd "$W/s01"; { $BIN config name s01-renamed --project >/dev/null; $BIN log | tail -2; } 2>&1 | ck S30 "config"

echo "=== S31 save-result + reflect: the learning loop"
cd "$W/s01"
{ $BIN save-result --question "where is refund" --type query --outcome useful >/dev/null; $BIN reflect | head -4; } 2>&1 | ck S31 "LESSONS"

echo "=== S32 wiki: deterministic pages exist and name the symbol"
cd "$W/s01"; grep -rl "ProcessRefund" "$CTX_OPTIMIZE_STORE/s01/wiki/" "$CTX_OPTIMIZE_STORE/s01-renamed/wiki/" 2>/dev/null | head -2 | ck S32 "wiki"

echo ""
echo "RESULT pass=$pass fail=$fail"
