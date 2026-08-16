#!/usr/bin/env node
// End-to-end check of `ctx-optimize serve`, driving a REAL browser.
//
// Why this exists: every claim made about the dashboard so far — that the
// viewers render, that drilling works at three grains, that the URL round
// trips, that the overview is fast — was verified with throwaway scripts in a
// scratchpad. That is the same failure the benchmark arena already taught this
// repo once: a number produced by a script nobody committed cannot be checked
// by anyone, including its author a week later.
//
// No dependency: Chrome is driven over the DevTools protocol using Node's
// built-in WebSocket (Node >= 22). Nothing to install, nothing to download,
// nothing that can drift from what CI has.
//
//   node scripts/e2e/dashboard.mjs [--url http://127.0.0.1:4747] [--json]
//
// It asserts BEHAVIOUR (what renders, what a click does, what the URL becomes)
// and BUDGETS (how long each screen takes). A budget is a real assertion here:
// the overview was 27.5s and nothing failed, because nothing was watching.
import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import net from 'node:net'

const args = process.argv.slice(2)
const flag = (name, fb) => {
  const i = args.indexOf('--' + name)
  return i >= 0 && args[i + 1] ? args[i + 1] : fb
}
const BASE = flag('url', 'http://127.0.0.1:4747')
const AS_JSON = args.includes('--json')

const CHROME = [
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/Applications/Brave Browser.app/Contents/MacOS/Brave Browser',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
  process.env.CHROME_PATH,
].filter(Boolean).find((p) => existsSync(p))

const results = []
const record = (name, ok, detail) => {
  results.push({ name, ok, detail })
  if (!AS_JSON) console.log(`${ok ? '  ok  ' : '  FAIL'} ${name}${detail ? '  — ' + detail : ''}`)
}
const check = (name, cond, detail) => record(name, !!cond, detail)

const freePort = () => new Promise((res) => {
  const srv = net.createServer()
  srv.listen(0, '127.0.0.1', () => { const p = srv.address().port; srv.close(() => res(p)) })
})

async function main() {
  if (!CHROME) {
    console.error('e2e: no Chrome/Chromium found. Set CHROME_PATH, or skip this check.')
    process.exit(2)
  }
  // the dashboard must already be running: this checks the real server, not a
  // stub, and starting one here would hide a broken `serve`
  try {
    const r = await fetch(BASE + '/api/modules')
    if (!r.ok) throw new Error('status ' + r.status)
  } catch (e) {
    console.error(`e2e: no dashboard at ${BASE} (${e.message}). Run \`ctx-optimize serve\` first.`)
    process.exit(2)
  }

  const port = await freePort()
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, '--no-first-run',
    '--no-default-browser-check', '--disable-extensions',
    `--user-data-dir=${process.env.TMPDIR || '/tmp'}/ctx-e2e-${port}`,
    'about:blank',
  ], { stdio: 'ignore' })

  const page = await connect(port)
  try {
    await run(page)
  } finally {
    page.close()
    chrome.kill()
  }

  const failed = results.filter((r) => !r.ok)
  if (AS_JSON) console.log(JSON.stringify({ results, failed: failed.length }, null, 1))
  else console.log(`\n${results.length - failed.length}/${results.length} checks passed`)
  process.exit(failed.length === 0 ? 0 : 1)
}

async function connect(port) {
  let target = null
  for (let i = 0; i < 100 && !target; i++) {
    await sleep(100)
    try {
      target = await (await fetch(`http://127.0.0.1:${port}/json/new?about:blank`, { method: 'PUT' })).json()
    } catch { /* chrome still coming up */ }
  }
  if (!target) throw new Error('chrome never answered on the devtools port')

  const ws = new WebSocket(target.webSocketDebuggerUrl)
  let id = 0
  const pending = new Map()
  const consoleErrors = []
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data)
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); return }
    if (msg.method === 'Runtime.exceptionThrown') {
      consoleErrors.push(msg.params?.exceptionDetails?.exception?.description || 'exception')
    }
    if (msg.method === 'Runtime.consoleAPICalled' && msg.params?.type === 'error') {
      consoleErrors.push((msg.params.args || []).map((a) => a.value || a.description).join(' '))
    }
  }
  await new Promise((r) => { ws.onopen = r })
  const send = (method, params = {}) => new Promise((res) => {
    const n = ++id
    pending.set(n, res)
    ws.send(JSON.stringify({ id: n, method, params }))
  })
  await send('Runtime.enable')
  await send('Page.enable')
  await send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 1000, deviceScaleFactor: 1, mobile: false })

  const ev = async (expression, awaitPromise = false) => {
    const r = await send('Runtime.evaluate', { expression, awaitPromise, returnByValue: true })
    if (r.result?.exceptionDetails) throw new Error(JSON.stringify(r.result.exceptionDetails).slice(0, 300))
    return r.result?.result?.value
  }
  const install = () => ev(`
    window.painted = () => {
      const cv = document.querySelector('canvas')
      if (!cv) return false
      // a canvas that exists but has drawn nothing is not a rendered screen
      const g = cv.getContext('2d')
      if (!g) return false
      const w = Math.min(cv.width, 400), h = Math.min(cv.height, 400)
      const d = g.getImageData(0, 0, w, h).data
      let first = -1
      for (let i = 0; i < d.length; i += 4) {
        const v = (d[i] << 16) | (d[i + 1] << 8) | d[i + 2]
        if (first < 0) first = v
        else if (v !== first) return true   // more than one colour: something was drawn
      }
      return false
    }
    true
  `)

  return {
    send, ev, consoleErrors, install,
    close: () => ws.close(),
    blank: async () => { await send('Page.navigate', { url: 'about:blank' }); await sleep(200) },
    /**
     * findPointers sweeps the canvas and returns EVERY point where the viewer
     * sets a pointer cursor — i.e. where IT says something is clickable,
     * rather than where this script calculates one should be.
     *
     * Every, not the first: the first pointer found in scan order is RESET
     * VIEW, which is clickable and deliberately does not navigate. A check
     * that clicked only the first target reported drill-down broken when it
     * was not.
     */
    findPointers: async (limit = 8) => {
      // The whole sweep runs INSIDE the page. Dispatching each pointermove over
      // CDP meant ~13,000 round trips for one canvas and the check never
      // finished; synthesised in-page it is a single call. The viewer reads
      // clientX/clientY off the event and sets canvas.style.cursor, so a
      // synthetic PointerEvent exercises exactly the real hit-testing path.
      return (await ev(`(() => {
        const cv = document.querySelector('canvas')
        if (!cv) return []
        const r = cv.getBoundingClientRect()
        const out = []
        const STEP = 7
        for (let y = r.top + 6; y < r.bottom - 6 && out.length < ${limit}; y += STEP) {
          for (let x = r.left + 6; x < r.right - 6 && out.length < ${limit}; x += STEP) {
            cv.dispatchEvent(new PointerEvent('pointermove', {
              clientX: x, clientY: y, bubbles: true, pointerId: 1, pointerType: 'mouse',
            }))
            if (cv.style.cursor !== 'pointer') continue
            if (out.some(p => Math.abs(p.x - x) < 100 && Math.abs(p.y - y) < 26)) continue
            out.push({ x, y })
          }
        }
        return out
      })()`)) || []
    },
    goto: async (hash) => {
      await send('Page.navigate', { url: BASE + '/' + hash })
      await install()
    },
    click: async (x, y) => {
      await send('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y, buttons: 0 })
      await sleep(120)
      await send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', buttons: 1, clickCount: 1 })
      await sleep(80)
      await send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', buttons: 0, clickCount: 1 })
      await sleep(700)
    },
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** waitFor polls an in-page expression until truthy, and returns how long it took. */
async function waitFor(page, expr, budgetMs, since) {
  const t0 = since || Date.now()
  while (Date.now() - t0 < budgetMs) {
    try {
      if (await page.ev(expr)) return Date.now() - t0
    } catch { /* navigating: the expression is not installed yet */ }
    await sleep(100)
  }
  return -1
}

async function run(page) {
  const modules = await (await fetch(BASE + '/api/modules')).json()
  const small = modules.slice().sort((a, b) => a.nodes - b.nodes).find((m) => m.nodes > 200) || modules[0]

  // ---- API budgets. These are the numbers a user waits on, and they are the
  // ones that regressed silently before anything was watching them.
  const timed = async (path) => {
    const t0 = Date.now()
    const r = await fetch(BASE + path)
    await r.arrayBuffer()
    return Date.now() - t0
  }
  const cheap = await timed('/api/stores?cheap=1')
  check('/api/stores?cheap=1 under 3s', cheap < 3000, `${cheap}ms`)
  await timed('/api/stores')                       // warm the caches
  const warm = await timed('/api/stores')
  check('/api/stores warm under 2s', warm < 2000, `${warm}ms`)

  const sceneUrl = `/api/scene?module=${encodeURIComponent(small.key)}`
  await timed(sceneUrl)
  const sceneWarm = await timed(sceneUrl)
  check('/api/scene warm under 500ms', sceneWarm < 500, `${sceneWarm}ms (${small.key})`)

  // ---- the scene payload is a contract the canvas depends on
  const sc = await (await fetch(BASE + sceneUrl)).json()
  const arrays = ['cards', 'links', 'world', 'stats', 'chips', 'notes', 'crumbs', 'questions']
  check('scene never sends null where the client expects an array',
    arrays.every((k) => Array.isArray(sc[k])),
    arrays.filter((k) => !Array.isArray(sc[k])).join(',') || 'all arrays')
  check('scene declares its grain', typeof sc.level === 'string' && sc.level !== '', sc.level)

  // ---- screens render.
  //
  // Each navigation goes via about:blank first. Without it the canvas from the
  // PREVIOUS screen is still in the DOM when the next check runs, every marker
  // is true immediately, and the suite reports 1ms renders that prove nothing —
  // which is exactly what the first run of this file did.
  for (const [hash, marker, budget] of [
    ['#/overview', 'document.querySelectorAll(".screenwrap").length > 0', 25000],
    [`#/viewer/${encodeURIComponent(small.key)}?view=flow`, 'painted()', 20000],
    [`#/viewer/${encodeURIComponent(small.key)}?view=house`, 'painted()', 20000],
    [`#/viewer/${encodeURIComponent(small.key)}?view=graph`, 'painted()', 20000],
  ]) {
    await page.blank()
    const t0 = Date.now()
    await page.goto(hash)
    const ms = await waitFor(page, marker, budget, t0)
    check(`renders ${hash}`, ms >= 0, ms >= 0 ? `${ms}ms` : `nothing after ${budget}ms`)
  }

  // ---- the overview paints BEFORE the slow half arrives
  await page.blank()
  await page.goto('#/overview')
  const painted = await waitFor(page, 'document.body.innerText.includes("NODES")', 20000)
  check('overview paints store cards under 5s', painted >= 0 && painted < 5000,
    painted >= 0 ? `${painted}ms` : 'never painted')

  // ---- drill-down, by hunting for the affordance the way a user does.
  //
  // The first version of this computed the name's position from the same
  // layout maths the canvas uses — which is a gate mirroring the code it
  // gates: change the layout and BOTH move, and the check keeps passing while
  // the link lands somewhere else. So it probes instead: sweep the pointer
  // across the canvas and look for the cursor the viewer sets on a link.
  const sceneFor = await (await fetch(`${BASE}/api/scene?module=${encodeURIComponent(small.key)}`)).json()
  const enterable = (sceneFor.cards || []).filter((c) => c.children > 0)
  check('drill-down: the store has a card with something inside',
    enterable.length > 0, enterable.map((c) => c.dir).join(', ') || 'none')

  if (enterable.length > 0) {
    await page.blank()
    await page.goto(`#/viewer/${encodeURIComponent(small.key)}?view=flow`)
    await waitFor(page, 'painted()', 20000)
    await sleep(900)
    const before = await page.ev('location.hash')
    const spots = await page.findPointers()
    check('links advertise themselves with a pointer cursor', spots.length > 0,
      `${spots.length} clickable regions`)

    let drilled = ''
    let tried = 0
    for (const spot of spots) {
      tried++
      await page.click(spot.x, spot.y)
      const after = await page.ev('location.hash')
      if (after !== before && after.includes('root=')) { drilled = after; break }
      // that target did something else (RESET VIEW, a crumb): put the view
      // back and try the next one
      await page.goto(`#/viewer/${encodeURIComponent(small.key)}?view=flow`)
      await waitFor(page, 'painted()', 20000)
      await sleep(500)
    }
    check('clicking a name drills in', drilled !== '',
      drilled || `no navigating target among ${spots.length} clickable regions`)
    check('the drilled level is a shareable URL',
      drilled.includes('view=flow') && drilled.includes('root='), drilled || 'n/a')

    if (drilled) {
      // a level you can enter and not leave is worse than no drill-down
      const outSpots = await page.findPointers()
      let left = ''
      for (const spot of outSpots) {
        await page.click(spot.x, spot.y)
        const out = await page.ev('location.hash')
        if (out !== drilled) { left = out; break }
      }
      check('the drilled level leads somewhere else', left !== '',
        left || `stuck: ${outSpots.length} regions, none navigated`)
    }
  }

  // ---- the address is honoured on a cold load, which is what a shared link is
  const deep = enterable[0]?.dir
  if (deep) {
    await page.blank()
    await page.goto(`#/viewer/${encodeURIComponent(small.key)}?view=house&root=${encodeURIComponent(deep)}`)
    await waitFor(page, 'painted()', 20000)
    const h = await page.ev('location.hash')
    check('a shared URL opens the right store, view AND level',
      h.includes('view=house') && h.includes('root='), h)
  }

  // ---- MODULE GRAIN (ADR 22 D4): a repo scene whose cards are its modules,
  // and a click that opens a DIFFERENT STORE rather than a directory of this
  // one. Picked from live data: the first repo whose module index yields a
  // `depends` arrow, so the check exercises the join and not just the level.
  // A missing route and a repo that is not a monorepo both answer 404, and
  // reporting "none" for either is how a STALE SERVER reads as a broken
  // feature. Asking with no repo separates them: the route that exists rejects
  // the empty name with a 400.
  const probe = await fetch(`${BASE}/api/repo/scene?repo=`)
  if (probe.status === 404) {
    check('the running server has the module-grain route', false,
      'GET /api/repo/scene is not routed — restart `ctx-optimize serve` from this build')
  }
  const repos = [...new Set(modules.map((m) => m.root || m.key))]
  let repoScene = null
  for (const r of repos) {
    const res = await fetch(`${BASE}/api/repo/scene?repo=${encodeURIComponent(r)}`)
    if (!res.ok) continue
    const rs = await res.json()
    if ((rs.links || []).some((l) => l.relation === 'depends')) { repoScene = { repo: r, sc: rs }; break }
    if (!repoScene && (rs.cards || []).length > 1) repoScene = { repo: r, sc: rs }
  }
  check('a repo answers at module grain', !!repoScene,
    repoScene ? `${repoScene.repo}: ${repoScene.sc.cards.length} modules, ${repoScene.sc.links.length} links` : 'none')

  if (repoScene) {
    // The way back UP, on the canvas. The chooser above the picture is not
    // where a reader who has drilled three levels is looking, and a level you
    // can enter and not leave is the failure ADR 21 named.
    const inner = repoScene.sc.cards.find((c) => c.id !== repoScene.repo)
    if (inner) {
      const ms = await (await fetch(`${BASE}/api/scene?module=${encodeURIComponent(inner.id)}`)).json()
      const up = (ms.crumbs || []).find((c) => c.module)
      check('a module scene carries a crumb back to its repo',
        !!up && up.module === repoScene.repo,
        up ? `${up.label} -> ${up.module}` : `crumbs: ${JSON.stringify(ms.crumbs)}`)
    }

    check('module-grain cards are store keys the viewer can open',
      repoScene.sc.cards.every((c) => modules.some((m) => m.key === c.id)),
      repoScene.sc.cards.map((c) => c.id).slice(0, 3).join(', '))
    // `shares` is symmetric — it must never be reported as a directed call.
    const between = (repoScene.sc.links || []).filter((l) => !l.to.startsWith('world:'))
    const bad = between.filter((l) => !['depends', 'calls', 'shares'].includes(l.relation))
    check('module grain draws only relations it can defend', bad.length === 0,
      bad.map((l) => l.relation).join(',') || 'depends/calls/shares only')
    // A count is not an explanation: a line between a ui and an api that says
    // only "12" reads as "the ui calls the api" when it is twelve third
    // parties they both call. Every port-derived link has to name them.
    const unnamed = between.filter(
      (l) => (l.relation === 'shares' || l.relation === 'calls') && !l.detail)
    check('port-derived links name what they stand for', unnamed.length === 0,
      unnamed.length ? `${unnamed.length} unnamed` : 'all named')
    // The module grain is the level that answers "what does this repo touch".
    // It drew no outer world at all, so a service every module calls and one
    // that exactly one module calls were equally invisible.
    const worldLinks = (repoScene.sc.links || []).filter((l) => l.to.startsWith('world:'))
    check('module grain shows the outer world', (repoScene.sc.world || []).length > 0,
      (repoScene.sc.world || []).map((w) => `${w.total} ${w.transport}`).join(', ') || 'none')
    check('every line carries the transport that colours it',
      [...between, ...worldLinks].every((l) => l.relation === 'depends' || l.transport),
      `${worldLinks.length} world links, ${between.length} between modules`)

    await page.blank()
    await page.goto(`#/viewer/${encodeURIComponent(repoScene.repo)}?view=flow`)
    const rendered = await waitFor(page, 'painted()', 20000)
    check(`renders the repo scene for ${repoScene.repo}`, rendered >= 0,
      rendered >= 0 ? `${rendered}ms` : 'never painted')
    await sleep(900)
    const beforeRepo = await page.ev('location.hash')
    const repoSpots = await page.findPointers()
    let entered = ''
    for (const spot of repoSpots) {
      await page.click(spot.x, spot.y)
      const after = await page.ev('location.hash')
      // entering a MODULE changes the store in the address and carries no
      // drill: it is a different graph, not a subdirectory of this one.
      if (after !== beforeRepo && !after.includes('root=') &&
          repoScene.sc.cards.some((c) => after.includes(encodeURIComponent(c.id)))) {
        entered = after
        break
      }
      await page.goto(`#/viewer/${encodeURIComponent(repoScene.repo)}?view=flow`)
      await waitFor(page, 'painted()', 20000)
      await sleep(500)
    }
    check('clicking a module opens that module\'s own store', entered !== '',
      entered || `no module-opening target among ${repoSpots.length} regions`)
  }

  // ---- nothing threw along the way
  check('no uncaught errors in the console', page.consoleErrors.length === 0,
    page.consoleErrors.slice(0, 2).join(' | ') || 'clean')
}

main().catch((e) => { console.error('e2e crashed:', e); process.exit(3) })
