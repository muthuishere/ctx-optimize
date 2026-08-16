import json,sys,os,subprocess,re
F="/Users/muthuishere/.claude/projects/-Users-muthuishere-muthu-gitworkspace-ctx-optimize/d5155945-9c3d-4567-b087-9378cb23af6c.jsonl"
DIR="/private/tmp/claude-501/-Users-muthuishere-tmp/d5155945-9c3d-4567-b087-9378cb23af6c/scratchpad"
lines=open(F).readlines()
ops=[]
for i in range(930,1290):
    try: d=json.loads(lines[i])
    except: continue
    m=d.get("message") or {}
    for b in (m.get("content") or []):
        if not (isinstance(b,dict) and b.get("type")=="tool_use"): continue
        n,inp=b["name"],b.get("input") or {}
        p=inp.get("file_path","")
        if n in ("Write","Edit") and p.endswith("spike7-realm.mjs"): ops.append((i,n,inp))
        elif n=="Bash" and DIR in str(inp.get("command","")): ops.append((i,"Bash",inp))
if sys.argv[1]=="scan":
    for i,n,inp in ops:
        if n=="Bash":
            for pat in ("rm ","rm -","curl","ssh ","git ","mv /"):
                if pat in inp["command"]: print("DANGER?",i,pat)
    print("ops",len(ops),[ (i,n) for i,n,_ in ops ])
    sys.exit()
path=os.path.join(DIR,"spike7-realm.mjs")
for i,n,inp in ops:
    if n=="Write":
        open(path,"w").write(inp["content"]); print(i,"write",len(inp["content"]))
    elif n=="Edit":
        s=open(path).read(); o,w=inp["old_string"],inp["new_string"]
        c=s.count(o)
        if c==0: print(i,"EDIT-MISS"); continue
        s=s.replace(o,w) if inp.get("replace_all") else s.replace(o,w,1)
        open(path,"w").write(s); print(i,"edit ok",c)
    else:
        r=subprocess.run(["zsh","-c",inp["command"]],capture_output=True,text=True,cwd=DIR)
        print(i,"bash rc=",r.returncode,(r.stdout+r.stderr).strip()[-300:])
