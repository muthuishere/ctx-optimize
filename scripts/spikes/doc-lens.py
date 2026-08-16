#!/usr/bin/env python3
"""SPIKE: would a DOCS LENS have anything to draw?

    python3 scripts/spikes/doc-lens.py [--root ~/ctxoptimize] [--json] [--repo NAME]

The flow view ranks subsystems by cross-directory edge weight, so documentation
never wins a card: reqsume has 45 cross-directory `references` against ~30,000
code edges. Retrieval is fine — `query` returns sections with file:line and
federates across modules — but the PICTURE has no docs in it.

The proposed answer is a lens: a view scoped to `document`/`section` nodes and
`references` edges, so the doc graph is drawn on its own terms instead of
competing with code. Before building it, this measures whether such a view
would show anything worth looking at.

READS THE STORE, deliberately — unlike doc-graph.py, which had to read source
because it measured links the store does not contain. Here the question is what
a lens could draw FROM WHAT EXISTS, so the store is the honest input.

  docs / sections     the nodes a lens would place
  linked              docs with at least one reference in or out
  orphans             docs with none. A lens over links shows these as dust;
                      if most docs are orphans, the honest lens is a LIST with
                      a few links, not a graph.
  components          connected components over `references` among documents
  largest             biggest component — is there one story, or islands?
  hub                 the most linked-TO document: a repo's real entry point
"""
import argparse, json, os, glob, collections


def load(store):
    docs, sections, refs = {}, collections.Counter(), []
    npath = os.path.join(store, 'graph', 'nodes.ndjson')
    epath = os.path.join(store, 'graph', 'edges.ndjson')
    owner = {}
    try:
        for line in open(npath):
            n = json.loads(line)
            k = n.get('kind')
            if k == 'document':
                docs[n['id']] = n.get('source', n['id'])
                owner[n['id']] = n.get('source', '')
            elif k == 'section':
                sections[n.get('source', '')] += 1
                owner[n['id']] = n.get('source', '')
    except OSError:
        return None
    try:
        for line in open(epath):
            e = json.loads(line)
            if e.get('relation') == 'references':
                refs.append((e['source'], e['target']))
    except OSError:
        pass
    return docs, sections, refs, owner


def doc_of(node_id, owner, docs):
    """The DOCUMENT a node belongs to — a link hangs off a section, and for the
    lens what matters is which file it came from."""
    src = owner.get(node_id)
    if src is None:
        return None
    return src if src in docs.values() or src.endswith(('.md', '.markdown', '.mdx')) else None


def scan(repo, repo_store):
    stores = [os.path.dirname(os.path.dirname(p))
              for p in glob.glob(os.path.join(repo_store, '**', 'graph', 'nodes.ndjson'), recursive=True)]
    docs, sections, refs, owner = {}, collections.Counter(), [], {}
    for st in stores:
        got = load(st)
        if not got:
            continue
        d, s, r, o = got
        # namespace by store so two modules' docs/README.md stay distinct
        key = os.path.relpath(st, repo_store)
        for k, v in d.items():
            docs[f'{key}::{v}'] = v
        for k, v in s.items():
            sections[f'{key}::{k}'] += v
        for a, b in r:
            refs.append((f'{key}::{o.get(a, a)}', f'{key}::{b}'))
        for k, v in o.items():
            owner[f'{key}::{k}'] = f'{key}::{v}'

    # link graph over DOCUMENTS
    adj = collections.defaultdict(set)
    indeg = collections.Counter()
    kept = 0
    for a, b in refs:
        if a in docs and b in docs and a != b:
            adj[a].add(b)
            adj[b].add(a)
            indeg[b] += 1
            kept += 1

    linked = {d for d in docs if adj[d]}
    orphans = len(docs) - len(linked)

    seen, comps = set(), []
    for d in docs:
        if d in seen or not adj[d]:
            continue
        stack, comp = [d], []
        seen.add(d)
        while stack:
            x = stack.pop()
            comp.append(x)
            for y in adj[x]:
                if y not in seen:
                    seen.add(y)
                    stack.append(y)
        comps.append(len(comp))
    comps.sort(reverse=True)

    hub, hub_n = ('', 0)
    if indeg:
        hub, hub_n = indeg.most_common(1)[0]
    return {
        'repo': repo, 'stores': len(stores),
        'docs': len(docs), 'sections': sum(sections.values()),
        'refs_between_docs': kept,
        'linked': len(linked), 'orphans': orphans,
        'components': len(comps), 'largest': comps[0] if comps else 0,
        'hub': hub.split('::')[-1] if hub else '', 'hub_in': hub_n,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--root', default=os.path.expanduser('~/ctxoptimize'))
    ap.add_argument('--repo')
    ap.add_argument('--json', action='store_true')
    a = ap.parse_args()

    rows = []
    for st in sorted(glob.glob(os.path.join(a.root, '*'))):
        if not os.path.isdir(st):
            continue
        repo = os.path.basename(st)
        if a.repo and repo != a.repo:
            continue
        r = scan(repo, st)
        if r and r['docs'] > 0:
            rows.append(r)
    rows.sort(key=lambda r: -r['docs'])

    if a.json:
        print(json.dumps(rows, indent=1))
        return

    print(f"{'repo':24} {'docs':>6} {'sects':>7} {'links':>6} {'linked':>7} {'orphan%':>8} {'comps':>6} {'largest':>8}  hub")
    print('-' * 108)
    tot = collections.Counter()
    for r in rows:
        pct = 100 * r['orphans'] // max(1, r['docs'])
        for k in ('docs', 'sections', 'refs_between_docs', 'linked', 'orphans'):
            tot[k] += r[k]
        print(f"{r['repo'][:24]:24} {r['docs']:>6} {r['sections']:>7} {r['refs_between_docs']:>6} "
              f"{r['linked']:>7} {pct:>7}% {r['components']:>6} {r['largest']:>8}  "
              f"{r['hub'][:34]}{(' (' + str(r['hub_in']) + ')') if r['hub'] else ''}")
    print()
    pct = 100 * tot['orphans'] // max(1, tot['docs'])
    print(f"TOTAL  docs {tot['docs']}  sections {tot['sections']}  "
          f"doc-to-doc links {tot['refs_between_docs']}  linked {tot['linked']}  orphans {tot['orphans']} ({pct}%)")
    print(f"repos with documents: {len(rows)}")


if __name__ == '__main__':
    main()
