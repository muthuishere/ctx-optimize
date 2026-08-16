#!/usr/bin/env python3
"""SPIKE: is there a DOCUMENT graph across a monorepo, and what shape is it?

    python3 scripts/spikes/doc-graph.py [--root ~/ctxoptimize] [--json]

The store already extracts markdown links as `references` edges, and they work
INSIDE a module — `[other](./other.md)` becomes an edge. A link that escapes the
module is silently dropped: measured on a two-module fixture, `[UI notes](../..
/ui/docs/notes.md)` produced no edge at all, while both documents existed as
nodes in their own stores. Not dangling, not AMBIGUOUS — gone.

So before designing a repo-level document graph, the question this measures is
whether one would have anything in it:

  intra      a link resolving inside its own module. Already an edge today.
  cross      a link resolving to a file in a SIBLING module. Dropped today —
             this is the number that decides whether the level is worth having.
  outside    resolves to a real file that is not in any declared module
             (repo-root docs, a stray folder). Would need the residual store.
  dangling   resolves to nothing on disk. A broken link, which is worth knowing
             on its own and is currently invisible.
  external   http(s):// — a URL, not a document relationship.
  anchor     #section only — intra-document, already covered by `contains`.

WHY IT READS THE SOURCE, NOT THE STORE: the interesting links are precisely the
ones the store does not contain. Reading the store would measure the gap as
zero by construction. A repo whose sources are gone is skipped and said so,
never counted as zero.
"""
import argparse, json, os, glob, re, collections

# [text](target) — the only form that becomes an edge today, plus the
# reference-style definition, which resolves the same way.
INLINE = re.compile(r'\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)')
REFDEF = re.compile(r'^\s{0,3}\[[^\]]+\]:\s*<?([^\s>]+)>?', re.M)

DOC_EXT = {'.md', '.markdown', '.mdx'}


def load_modules(repo_store):
    """Module paths, repo-relative, from the navigator index."""
    p = os.path.join(repo_store, 'modules.json')
    try:
        idx = json.load(open(p))
    except (OSError, ValueError):
        return None
    return [m['path'].strip('/') for m in idx.get('modules', []) if m.get('path')]


def source_of(repo_store):
    try:
        d = json.load(open(os.path.join(repo_store, 'source.json')))
        d = d if isinstance(d, list) else d.get('sources', [d])
        return d[0].get('path') if d else None
    except (OSError, ValueError, IndexError):
        return None


def module_of(rel, modules):
    """Longest module path that is a prefix of rel; None = outside them all."""
    best = None
    for m in modules:
        if rel == m or rel.startswith(m + '/'):
            if best is None or len(m) > len(best):
                best = m
    return best


def classify(src_root, modules, doc_rel, target):
    t = target.strip()
    if not t:
        return 'dangling', None
    if t.startswith('#'):
        return 'anchor', None
    if re.match(r'^[a-zA-Z][a-zA-Z0-9+.-]*://', t) or t.startswith('mailto:'):
        return 'external', None
    t = t.split('#', 1)[0].split('?', 1)[0]
    if not t:
        return 'anchor', None
    base = os.path.dirname(doc_rel)
    rel = os.path.normpath(os.path.join('/', base, t) if not t.startswith('/')
                           else os.path.join('/', t.lstrip('/'))).lstrip('/')
    if not os.path.exists(os.path.join(src_root, rel)):
        return 'dangling', rel
    here, there = module_of(doc_rel, modules), module_of(rel, modules)
    # A doc at the repo ROOT linking into a module is NOT a module-to-module
    # link, and counting it as one inflated the headline number twelvefold on
    # the first run of this spike: deepseek's 6,004 "cross" links were almost
    # all THIRD_PARTY_NOTICES.md and friends pointing at packages/*. Real, and
    # a different fact — it belongs to the residual root store.
    if here is None and there is None:
        return 'outside', rel
    if here is None:
        return 'root_into', rel
    if there is None:
        return 'outside', rel
    if here == there:
        return 'intra', rel
    return 'cross', rel


def scan_repo(repo, repo_store):
    modules = load_modules(repo_store)
    if not modules or len(modules) < 2:
        return None  # not a monorepo: nothing to cross
    src = source_of(repo_store)
    if not src or not os.path.isdir(src):
        return {'repo': repo, 'modules': len(modules), 'source_gone': True}

    counts = collections.Counter()
    pairs = collections.Counter()
    examples = []
    docs = 0
    for dirpath, dirnames, filenames in os.walk(src):
        dirnames[:] = [d for d in dirnames
                       if d not in ('node_modules', 'vendor', '.git', 'dist', 'build', 'target')
                       and not d.startswith('.')]
        for fn in filenames:
            if os.path.splitext(fn)[1].lower() not in DOC_EXT:
                continue
            full = os.path.join(dirpath, fn)
            doc_rel = os.path.relpath(full, src).replace(os.sep, '/')
            docs += 1
            try:
                body = open(full, encoding='utf-8', errors='replace').read()
            except OSError:
                continue
            for m in list(INLINE.finditer(body)) + list(REFDEF.finditer(body)):
                kind, rel = classify(src, modules, doc_rel, m.group(1))
                counts[kind] += 1
                if kind == 'cross':
                    a, b = module_of(doc_rel, modules), module_of(rel, modules)
                    pairs[(a, b)] += 1
                    if len(examples) < 3:
                        examples.append(f'{doc_rel} -> {rel}')
    return {'repo': repo, 'modules': len(modules), 'docs': docs,
            'counts': dict(counts), 'module_pairs': len(pairs),
            'top_pairs': [f'{a} -> {b} ({n})' for (a, b), n in pairs.most_common(3)],
            'examples': examples}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--root', default=os.path.expanduser('~/ctxoptimize'))
    ap.add_argument('--json', action='store_true')
    a = ap.parse_args()

    rows, gone = [], []
    for mj in sorted(glob.glob(os.path.join(a.root, '*', 'modules.json'))):
        store = os.path.dirname(mj)
        r = scan_repo(os.path.basename(store), store)
        if r is None:
            continue
        if r.get('source_gone'):
            gone.append(r['repo'])
            continue
        rows.append(r)

    if a.json:
        print(json.dumps({'repos': rows, 'source_gone': gone}, indent=1))
        return

    kinds = ['intra', 'cross', 'root_into', 'outside', 'dangling', 'external', 'anchor']
    print(f"{'repo':26} {'mods':>4} {'docs':>5} " + ' '.join(f'{k:>8}' for k in kinds) + f" {'pairs':>6}")
    print('-' * 104)
    tot = collections.Counter()
    for r in rows:
        c = r['counts']
        for k in kinds:
            tot[k] += c.get(k, 0)
        print(f"{r['repo'][:26]:26} {r['modules']:>4} {r['docs']:>5} "
              + ' '.join(f"{c.get(k, 0):>8}" for k in kinds) + f" {r['module_pairs']:>6}")
    print()
    print('TOTAL links by kind:', dict(tot))
    print(f'multi-module repos scanned: {len(rows)}')
    if gone:
        print(f'skipped, sources gone (never counted as zero): {", ".join(gone)}')
    print()
    for r in rows:
        if r['examples']:
            print(f"== {r['repo']}  ({r['module_pairs']} module pairs linked)")
            for p in r['top_pairs']:
                print(f'   pair  {p}')
            for e in r['examples']:
                print(f'   e.g.  {e}')


if __name__ == '__main__':
    main()
