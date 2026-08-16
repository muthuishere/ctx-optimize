S=/private/tmp/claude-501/-Users-muthuishere-tmp/d5155945-9c3d-4567-b087-9378cb23af6c/scratchpad && python3 - <<'EOF'
import json,collections,os
S='/private/tmp/claude-501/-Users-muthuishere-tmp/d5155945-9c3d-4567-b087-9378cb23af6c/scratchpad'
n=json.load(open(S+'/r-nodes.json'))
n=n if isinstance(n,list) else n.get('nodes',n)
SYM={'function','method','type','interface','class'}
NOISE=('node_modules','/dist/','__tests__','.test.')
# tree: dir -> {files: {file: {sym:[(label,kind,loc)...], k:counts}}, kinds}
tree=collections.defaultdict(lambda:{'f':collections.defaultdict(list),'k':collections.Counter()})
for x in n:
    s=x.get('source','')
    if not s or '://' in s or any(t in s for t in NOISE): continue
    if not s.startswith('apps/'): continue
    d=os.path.dirname(s)
    k=x.get('kind')
    tree[d]['k'][k]+=1
    if k in SYM:
        tree[d]['f'][s].append((x.get('label',''),k,x.get('location','')))
# compact: per dir: {sym total, files:{basename:[count, top5 labels]}}
out={}
for d,v in tree.items():
    files={}
    for f,syms in v['f'].items():
        files[os.path.basename(f)]=[len(syms),[s[0] for s in syms[:6]],[s[2] for s in syms[:6]]]
    kc={k:c for k,c in v['k'].items() if k not in SYM and c}
    out[d]={'s':sum(len(x) for x in v['f'].values()),'k':kc,'f':files}
js=json.dumps(out,separators=(',',':'))
open(S+'/tree.json','w').write(js)
print('dirs:',len(out),'| size:',len(js)//1024,'KB')
# check components subtree
for d in sorted(out):
    if d.startswith('apps/ui/src/components'): print(' ',d,out[d]['s'],'syms',len(out[d]['f']),'files')
EOF