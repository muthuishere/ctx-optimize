// Build the recursive realm: embeds the REAL directory tree (tree.json, from the store)
// so every level down to a single function is a real fact.
import { readFileSync, writeFileSync } from 'node:fs';
const dir = new URL('.', import.meta.url).pathname;
const TREE = readFileSync(dir + 'tree.json', 'utf8');
const ALLF = readFileSync(dir + 'allflows.json', 'utf8');
const IMP = readFileSync(dir + 'imports.json', 'utf8');

const html = `<title>The Realm of Reqsume</title>
<style>
:root{--bg:#0c0f14;--ink:#e9e4d8;--mid:#a89f8c;--dim:#6b6557;--line:#242a33;
  --gold:#d4a94e;--teal:#4fa39a;--red:#c05b47;--parch:#171a20}
*{box-sizing:border-box}html,body{margin:0;height:100%;overflow:hidden}
body{background:var(--bg);color:var(--ink);
  font:14px/1.55 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-variant-numeric:tabular-nums}
canvas{display:block;position:fixed;inset:0}
#title{position:fixed;top:20px;left:0;right:0;text-align:center;pointer-events:none}
#title h1{margin:0;font-size:14px;letter-spacing:.3em;text-transform:uppercase;font-weight:600;color:var(--mid)}
#title .d{font-size:11px;color:var(--dim);margin-top:4px;letter-spacing:.05em}
#tale{position:fixed;left:50%;bottom:64px;transform:translateX(-50%);width:min(640px,88vw);
  text-align:center;pointer-events:none;font-size:15px;text-wrap:balance;transition:opacity .35s ease-out}
#tale b{color:var(--gold);font-weight:600}
#tale i{display:block;font-style:normal;color:var(--dim);font-size:11.5px;margin-top:7px}
#crumb{position:fixed;top:18px;left:20px;display:flex;gap:4px;align-items:center;font-size:12px}
#crumb button{background:var(--parch);border:1px solid var(--line);color:var(--dim);border-radius:6px;
  padding:5px 11px;font:inherit;font-size:11.5px;cursor:pointer;letter-spacing:.03em}
#crumb button:hover{color:var(--gold)}
#crumb span{color:var(--dim)}
#tip{position:fixed;display:none;background:var(--parch);border:1px solid var(--line);border-radius:7px;
  padding:7px 11px;font-size:11.5px;pointer-events:none;z-index:5;max-width:300px;line-height:1.5}
#tip b{color:var(--gold)}#tip .to{color:var(--teal)}#tip .cargo{color:var(--dim);font-size:10.5px;margin-top:2px}
@media (prefers-reduced-motion:reduce){*{transition:none!important}}
</style>
<canvas id="c"></canvas>
<div id="title"><h1 id="h1t">The Realm of Reqsume</h1><div class="d" id="h1d"></div></div>
<div id="tale"><span id="tt"></span></div>
<div id="crumb"></div>
<div id="tip"></div>
<script>
const TREE=${TREE};
const ALLF=${ALLF};  /* dd: dir→dir · ff: file→file within a dir · fe: file→outside.
   Every entry is [count, [carried symbols…]] — real calls edges from the store. */
const IMPORTS=${IMP};
const TABLES=[["ai_model_configs", "000003"], ["currency_exchange", "000008"], ["app_config", "000009"], ["feedback", "000010"], ["invoice_sequences", "000011"], ["invoices", "000011"], ["user_billing_profiles", "000012"], ["breach_email_logs", "000018"], ["notifications", "000019"], ["gdpr_export_jobs", "000022"], ["breach_email_recipients", "000028"], ["jobs", "000029"], ["ops_audit", "000035"], ["projects", "000040"], ["colleges", "20250705"], ["alert_recipients", "20260518"], ["degree_fields", "20260708"], ["embeddings", "initdb"]];
const FLOWS=Object.fromEntries(Object.entries(ALLF.dd).map(([a,v])=>[a,Object.fromEntries(Object.entries(v).map(([b,x])=>[b,x[0]]))]));
const TESTC='#8a6fb8';
const ROLES=[
 [/handler|route|controller|gateway/,  ['#3f9d8a','castle']],   /* the gates    */
 [/service|domain|core|logic/,          ['#b98a3c','castle']],   /* the halls    */
 [/database|model|repo|store|schema|migration/,['#c2a14e','granary']],
 [/config|settings|env/,                ['#7f8fa6','village']],  /* the scrolls  */
 [/job|queue|cron|worker|task|pipeline/,['#a05b47','forge']],
 [/ai|llm|provider|ml|rag/,             ['#8a6fb8','outpost']],  /* wizard tower */
 [/notif|mail|email|alert|raven/,       ['#c05b47','outpost']],
 [/util|helper|common|lib|shared|pkg/,  ['#6f7a72','village']],
 [/test|spec|fixture|mock/,             [TESTC,'village']],
 [/auth|security|crypto|vault/,         ['#5a7ba6','outpost']],
];
const roleOf=n=>{const l=n.toLowerCase();for(const[re,v]of ROLES)if(re.test(l))return v;return null;};
const isTest=n=>/(_test\.|\.test\.|\.spec\.|^test_)/.test(n);
const disp=p=>(p||'').split('/').filter(x=>x!=='apps'&&x!=='src').join('/')||p;
const kids=d=>Object.keys(TREE).filter(k=>k.lastIndexOf('/')===d.length&&k.startsWith(d+'/')).sort();
/* ═ curated top level ═ */
const HOUSES=[
 {id:'api',path:'apps/api/src',kind:'castle',x:0,y:0,w:150,h:96,col:'#8a6d3b',n:38,
  say:['The great castle — <b>103 gates</b>, 19 granaries.','click again to enter']},
 {id:'ui',path:'apps/ui/src',kind:'town',x:-260,y:120,w:120,h:60,col:'#4fa39a',n:24,
  say:['The market town — <b>137 caravans</b>, one gatehouse.','click again to enter']},
 {id:'home',path:'apps/home/src',kind:'village',x:-150,y:-150,w:86,h:40,col:'#7a8a5a',n:6,
  say:['The village — 3 caravans a season.','click again to enter']},
 {id:'ext',path:'apps/extension/src',kind:'outpost',x:230,y:-140,w:80,h:46,col:'#9a7a9a',n:5,
  say:['The border outpost.','click again to enter']},
 {id:'infra',path:null,kind:'forge',x:210,y:150,w:96,h:50,col:'#a05b47',n:9,
  say:['The forge — <b>141 standing orders</b>.','Taskfile · compose · migrations']}];
const POWERS=[
 {n:'OpenAI',w:15,y:-90,say:['If OpenAI closes its ports, <b>three gates fall</b>.','ai/providers → ai/chat → 3 public routes']},
 {n:'Gemini',w:12,y:-30,say:['Gemini — <b>12 embassies</b>.','ai/providers/gemini.go']},
 {n:'Contabo',w:4,y:30,say:['The storage isles.','eu2.contabostorage.com']},
 {n:'ConvertAPI',w:2,y:90,say:['ConvertAPI — 2 embassies.','v2.convertapi.com']},
 {n:'a nameless tower',w:3,y:150,bad:1,say:['<b>No treaty names this tower.</b>','Azure Logic Apps · notification/teams.go · in no manifest']}];
const ROADS=[['ui','api',137],['home','api',3],['ext','api',2],['infra','api',2]];
const HOUSECOL={};HOUSES.forEach(h=>{if(h.path)HOUSECOL[h.path.split('/')[1]]=h.col;});
const colFor=p=>HOUSECOL[(p||'').split('/')[1]]||'#8a6d3b';

const c=document.getElementById('c'),g=c.getContext('2d');
const RM=matchMedia('(prefers-reduced-motion:reduce)').matches;
let W,H,DPR,last=0,T=0,hov=null,sel=null,raf=null;
let stack=[{t:'realm'}],fade=1;                    // stack of levels; top is current
let cam={x:0,y:0,z:1};                             // user camera — every level is movable
let MSG=[];                                        // live messengers this frame (screen coords + cargo)
const tok=n=>getComputedStyle(document.documentElement).getPropertyValue('--'+n).trim();
const rnd=s=>{let x=Math.sin(s*127.1)*43758.5453;return x-Math.floor(x);};
const sh=(hex,f)=>{const n=parseInt(hex.slice(1),16);let r=n>>16,gg=n>>8&255,b=n&255;
  r=Math.min(255,Math.max(0,r+r*f))|0;gg=Math.min(255,Math.max(0,gg+gg*f))|0;b=Math.min(255,Math.max(0,b+b*f))|0;
  return'#'+((r<<16|gg<<8|b)>>>0).toString(16).padStart(6,'0');};
function resize(){DPR=Math.min(devicePixelRatio||1,2);W=innerWidth;H=innerHeight;
  c.width=W*DPR;c.height=H*DPR;c.style.width=W+'px';c.style.height=H+'px';}
addEventListener('resize',resize);

function flag(x,y,col){g.save();g.translate(x,y);
  g.strokeStyle=tok('dim');g.lineWidth=1;g.beginPath();g.moveTo(0,0);g.lineTo(0,-13);g.stroke();
  const wav=RM?0:Math.sin(T*3.1+x)*1.6;g.fillStyle=col;
  g.beginPath();g.moveTo(0,-13);g.quadraticCurveTo(6,-12+wav,11,-11.5+wav);
  g.lineTo(11,-7.5+wav);g.quadraticCurveTo(6,-8+wav,0,-7);g.closePath();g.fill();g.restore();}
function person(x,y,col){g.fillStyle=col;g.fillRect(x-1.6,y-6,3.2,4.2);
  g.fillStyle=tok('mid');g.beginPath();g.arc(x,y-7.6,1.7,0,6.283);g.fill();}
function building(x,y,w,h,col,lit,kind,seed){
  g.save();g.translate(x,y);const A=g.globalAlpha;g.globalAlpha=A*(.25+lit*.75);
  const base=(bw,bh,bx=0)=>{g.fillStyle=sh(col,-.25);g.fillRect(bx-bw/2,-bh,bw,bh);
    g.fillStyle=sh(col,.12);g.fillRect(bx-bw/2,-bh,bw*.42,bh);};
  const cren=(bx,top,bw)=>{g.fillStyle=sh(col,-.45);
    const n=Math.max(3,(bw/9)|0);for(let i=0;i<n;i++)g.fillRect(bx-bw/2+i*(bw/n)+1,top-5,bw/n-3,5);};
  if(kind==='castle'){base(w*.62,h);base(w*.3,h*1.45,-w*.36);base(w*.3,h*1.3,w*.36);
    cren(-w*.36,-h*1.45,w*.3);cren(0,-h,w*.62);cren(w*.36,-h*1.3,w*.3);
    flag(-w*.36,-h*1.45-6,'#d4a94e');flag(w*.36,-h*1.3-6,'#d4a94e');}
  else if(kind==='town'){for(let i=0;i<4;i++){const bw=w*.2,bx=-w*.36+i*w*.24,bh=h*(.7+rnd(seed+i*3)*.5);
    base(bw,bh,bx);g.fillStyle=sh(col,-.5);
    g.beginPath();g.moveTo(bx-bw/2-2,-bh);g.lineTo(bx,-bh-h*.16);g.lineTo(bx+bw/2+2,-bh);g.closePath();g.fill();}
    flag(-w*.36,-h*1.28,col);}
  else if(kind==='village'){for(let i=0;i<3;i++){const bw=w*.24,bx=-w*.3+i*w*.3,bh=h*.8;base(bw,bh,bx);
    g.fillStyle=sh(col,-.5);g.beginPath();g.moveTo(bx-bw/2-2,-bh);g.lineTo(bx,-bh-h*.12);g.lineTo(bx+bw/2+2,-bh);g.closePath();g.fill();}}
  else if(kind==='outpost'){base(w*.4,h*1.5);cren(0,-h*1.5,w*.4);flag(0,-h*1.5-6,col);}
  else if(kind==='forge'){base(w*.6,h*.8);g.fillStyle=sh(col,-.4);g.fillRect(w*.14,-h*1.35,w*.09,h*.62);
    if(!RM){const s=T*2%1;g.globalAlpha=A*(.25+lit*.75)*(1-s)*.7;
      g.fillStyle=tok('dim');g.beginPath();g.arc(w*.185,-h*1.35-8-s*16,2.2+s*2.6,0,6.283);g.fill();
      g.globalAlpha=A*(.25+lit*.75);}}
  else if(kind==='hut'){base(w*.8,h);g.fillStyle=sh(col,-.5);
    g.beginPath();g.moveTo(-w*.4-2,-h);g.lineTo(0,-h-w*.22);g.lineTo(w*.4+2,-h);g.closePath();g.fill();}
  else if(kind==='sign'){g.strokeStyle=sh(col,-.2);g.lineWidth=2;
    g.beginPath();g.moveTo(0,0);g.lineTo(0,-14);g.stroke();
    g.fillStyle=sh(col,-.1);g.fillRect(-w/2,-26,w,12);}
  else if(kind==='bench'){g.fillStyle=sh(col,-.3);g.fillRect(-w/2,-h,w,h);
    g.fillStyle=sh(col,.15);g.fillRect(-w/2,-h,w,3);}
  g.fillStyle='#ffd98a';const wn=Math.min(8,2+(seed%5));
  for(let i=0;i<wn;i++){const wx=(rnd(i*7+seed)-.5)*w*.5,wy=-h*.25-rnd(i*3+seed)*h*.5;
    if((T*.5+rnd(i*11+seed))%1>.15)g.fillRect(wx,wy,Math.max(2.4,w*.02),Math.max(3,w*.026));}
  g.globalAlpha=A;g.restore();
}

/* ═ generic yard for any real directory ═ */
function yardSpots(path){
  const ds=kids(path).map(d=>({t:'dir',path:d,name:d.slice(path.length+1),
    n:(TREE[d]?.s||0)+kids(d).reduce((a,k)=>a+(TREE[k]?.s||0),0)}));
  const fs=Object.entries(TREE[path]?.f||{}).map(([f,v])=>({t:'file',path,name:f,n:v[0]}));
  let all=[...ds.sort((a,b)=>b.n-a.n),...fs.sort((a,b)=>b.n-a.n)];
  let extra=0;if(all.length>24){extra=all.length-23;all=all.slice(0,23);}
  // sunflower spiral, big first at centre; radius grows with placed area, then
  // a relaxation pass pushes overlaps apart. Deterministic — same yard every time.
  const spots=[];const GA=Math.PI*(3-Math.sqrt(5));
  all.forEach((s,i)=>{
    const big=s.t==='dir';
    const w=big?Math.min(170,52+Math.sqrt(s.n||1)*7.5):Math.min(64,24+Math.sqrt(s.n||1)*6);
    const a=i*GA-Math.PI/2, r=34*Math.sqrt(i+(big?0:1.5));
    spots.push({...s,x:Math.cos(a)*r*1.55,y:Math.sin(a)*r*.8,
      w,h:big?w*.5:w*.55,
      kind:big?(roleOf(s.name)?.[1]||(s.n>120?'castle':s.n>40?'town':'village')):'hut',rank:i});});
  for(let it=0;it<60;it++){let moved=false;      // relax: no two buildings overlap
    for(let i=0;i<spots.length;i++)for(let j=i+1;j<spots.length;j++){
      const A=spots[i],B=spots[j];
      const dx=B.x-A.x,dy=(B.y-A.y)*1.9;
      const need=(A.w+B.w)*.62+16, d=Math.hypot(dx,dy)||1;
      if(d<need){const push=(need-d)/2,ux=dx/d,uy=dy/d/1.9;
        A.x-=ux*push;A.y-=uy*push;B.x+=ux*push;B.y+=uy*push;moved=true;}}
    if(!moved)break;}
  if(extra){let mx=0,my=0;spots.forEach(s=>{mx=Math.max(mx,s.x);my=Math.max(my,s.y+30);});
    spots.push({t:'more',name:'… +'+extra+' more',x:0,y:my+46,w:70,h:20,kind:'hut',n:extra});}
  return spots;
}
function fileBenches(path,fname){
  const v=TREE[path].f[fname],names=v[1],locs=v[2],n=v[0];
  const out=[];names.forEach((nm,i)=>{
    out.push({t:'sym',name:nm,loc:locs[i],x:(i%3-1)*190,y:((i/3|0))*80-60,w:150,h:26,kind:'bench'});});
  if(n>names.length)out.push({t:'more',name:'… +'+(n-names.length)+' more symbols',x:0,y:((names.length/3|0))*80-50+70,w:80,h:18,kind:'hut'});
  const imps=((IMPORTS[path]||{})[fname])||[];
  imps.forEach(([ik,tgt],i)=>{const a=-Math.PI*.92+i*(Math.PI*.84/Math.max(1,imps.length-1||1));
    const nm=ik==='ext'?tgt:disp(tgt).split('/').pop();
    out.push({t:'imp',ikind:ik,tgt,name:nm,x:Math.cos(a)*430,y:Math.sin(a)*230-40,w:60,h:16,kind:'sign'});});
  return out;
}

/* ═ level info (title/subtitle/spots) ═ */
function levelSpots(L){
  if(L.t==='realm')return null;
  if(L.t==='db')return L.spots||(L.spots=TABLES.map(([nm,mig],i)=>{
    const GA=Math.PI*(3-Math.sqrt(5)),a=i*GA-Math.PI/2,r=40*Math.sqrt(i+1);
    return{t:'tbl',name:nm,mig,x:Math.cos(a)*r*1.6,y:Math.sin(a)*r*.85,
      w:Math.min(96,44+nm.length*2),h:34,kind:'granary'};}));
  if(L.t==='dir')return L.spots||(L.spots=yardSpots(L.path));
  if(L.t==='file')return L.spots||(L.spots=fileBenches(L.path,L.name));
}
function levelTitle(L){
  if(L.t==='realm')return['The Realm of Reqsume','five houses · five foreign powers · one false ledger — 17,695 recorded facts'];
  if(L.t==='dir'){const k=TREE[L.path]?.k||{};const bits=[L.path];
    const kk=Object.entries(k).filter(([a])=>!['file','module','section','document'].includes(a));
    if(kk.length)bits.push(kk.map(([a,b])=>b+' '+a).join(' · '));
    const s=TREE[L.path]?.s||0;if(s)bits.push(s+' symbols here');
    return[disp(L.path),bits.join('  ·  ')];}
  if(L.t==='db')return['postgres','19 tables · declared in apps/api/docs/migrations — the granaries of record'];
  if(L.t==='file')return[L.name,L.path+'/'+L.name+'  ·  '+TREE[L.path].f[L.name][0]+' symbols — each bench is one'];
}

/* ═ draw ═ */
const CARAVANS=[];ROADS.forEach(([a,b,n],ri)=>{const k=Math.max(1,Math.min(9,Math.round(n/18)));
  for(let i=0;i<k;i++)CARAVANS.push({a,b,ph:rnd(ri*31+i*7),sp:.028+rnd(ri*13+i)*.018});});
const SHIPS=[];POWERS.forEach((p,pi)=>{const k=Math.max(1,Math.min(4,Math.round(p.w/5)));
  for(let i=0;i<k;i++)SHIPS.push({p:pi,ph:rnd(pi*17+i*11),sp:.02+rnd(pi+i)*.012});});

function drawRealm(alpha){
  const MID=tok('mid'),DIM=tok('dim'),LINE=tok('line'),GOLD=tok('gold'),TEAL=tok('teal'),RED=tok('red');
  const sc=Math.min(W/980,H/640)*cam.z;g.save();g.globalAlpha=alpha;
  g.translate(W/2-60*sc+cam.x,H/2+10*sc+cam.y);g.scale(sc,sc);
  g.fillStyle='#10161f';g.fillRect(370,-260,560,560);
  if(!RM)for(let i=0;i<5;i++){const wy=-200+i*90+Math.sin(T*.7+i)*3;
    g.strokeStyle='#1b2531';g.beginPath();g.moveTo(385,wy);
    for(let x=385;x<900;x+=22)g.lineTo(x,wy+Math.sin(T*.9+x*.05+i)*2.2);g.stroke();}
  const lit=id=>sel===null?1:(sel===id?1:.3);
  for(const[a,b,n]of ROADS){const A=HOUSES.find(h=>h.id===a),B=HOUSES.find(h=>h.id===b);
    g.strokeStyle=LINE;g.lineWidth=Math.min(5,1+n/40);g.globalAlpha=alpha*(.4+Math.min(lit(a),lit(b))*.5);
    g.beginPath();g.moveTo(A.x,A.y);g.quadraticCurveTo((A.x+B.x)/2,(A.y+B.y)/2-24,B.x,B.y);g.stroke();}
  g.globalAlpha=alpha;
  for(const cv of CARAVANS){const A=HOUSES.find(h=>h.id===cv.a),B=HOUSES.find(h=>h.id===cv.b);
    const l=Math.min(lit(cv.a),lit(cv.b));if(l<.3)continue;
    const t=(cv.ph+T*cv.sp)%1,u=1-t,mx=(A.x+B.x)/2,my=(A.y+B.y)/2-24;
    const x=u*u*A.x+2*u*t*mx+t*t*B.x,y=u*u*A.y+2*u*t*my+t*t*B.y;
    g.globalAlpha=alpha*l;g.fillStyle=TEAL;g.fillRect(x-2.4,y-3.6,4.8,3);
    g.fillStyle=DIM;g.beginPath();g.arc(x-1.4,y-.4,1.1,0,6.283);g.arc(x+1.4,y-.4,1.1,0,6.283);g.fill();}
  g.globalAlpha=alpha;const port=[330,-20];
  POWERS.forEach((p,pi)=>{const l=sel===null?1:(sel==='p'+pi?1:.3);const dx=640,dy=p.y;
    g.strokeStyle=p.bad?RED:LINE;g.globalAlpha=alpha*(.35+l*.45);
    g.setLineDash(p.bad?[3,4]:[6,5]);g.beginPath();g.moveTo(port[0],port[1]);
    g.quadraticCurveTo(480,(port[1]+dy)/2,dx-58,dy);g.stroke();g.setLineDash([]);
    g.globalAlpha=alpha*(.3+l*.7);
    if(p.bad){g.fillStyle=RED;g.fillRect(dx-8,dy-26,14,26);
      if(!RM&&(T*1.4%1)>.5){g.beginPath();g.arc(dx-1,dy-31,2,0,6.283);g.fill();}}
    else{g.fillStyle='#3b4657';g.beginPath();g.moveTo(dx-12,dy);g.lineTo(dx,dy-16);g.lineTo(dx+12,dy);g.closePath();g.fill();}
    g.fillStyle=p.bad?RED:MID;g.font='11px ui-monospace,Menlo,monospace';g.textAlign='left';
    g.fillText(p.n+'  ·'+p.w,dx+24,dy-4);});
  g.globalAlpha=alpha;
  for(const s of SHIPS){const p=POWERS[s.p];const l=sel===null?1:(sel==='p'+s.p||sel==='api'?1:.3);
    if(l<.3)continue;const t=(s.ph+T*s.sp)%1,u=1-t,dx=640-58,dy=p.y;
    const x=u*u*port[0]+2*u*t*480+t*t*dx,y=u*u*port[1]+2*u*t*((port[1]+dy)/2)+t*t*dy;
    g.globalAlpha=alpha*l;g.fillStyle=p.bad?RED:'#7d8b9f';
    g.beginPath();g.moveTo(x-4,y);g.lineTo(x+4,y);g.lineTo(x+2.4,y+2.6);g.lineTo(x-2.4,y+2.6);g.closePath();g.fill();
    g.beginPath();g.moveTo(x,y-.5);g.lineTo(x,y-7);g.lineTo(x+4.4,y-2.4);g.closePath();g.fill();}
  g.globalAlpha=alpha;
  { // the granary of record — postgres, 19 tables, fed by models/database
    const l=sel===null?1:(sel==='db'?1:.3);
    g.strokeStyle=LINE;g.lineWidth=2.2;g.globalAlpha=alpha*(.4+l*.5);
    g.beginPath();g.moveTo(40,30);g.quadraticCurveTo(160,150,240,236);g.stroke();g.globalAlpha=alpha;
    if(!RM){const t=(T*.14)%1;const u=1-t;
      const x=u*u*40+2*u*t*160+t*t*240,y=u*u*30+2*u*t*150+t*t*236;
      g.globalAlpha=alpha*l;g.fillStyle='#c2a14e';g.fillRect(x-2.4,y-3.6,4.8,3);
      g.fillStyle=DIM;g.beginPath();g.arc(x-1.4,y-.4,1.1,0,6.283);g.arc(x+1.4,y-.4,1.1,0,6.283);g.fill();g.globalAlpha=alpha;}
    building(255,250,120,52,'#c2a14e',l,'granary',11);
    const on=hov==='db'||sel==='db';
    g.globalAlpha=alpha*(on?1:.4+l*.4);g.fillStyle=on?GOLD:MID;
    g.font='600 12px ui-monospace,Menlo,monospace';g.textAlign='center';
    g.fillText('postgres',255,272);
    g.fillStyle=DIM;g.font='10px ui-monospace,Menlo,monospace';g.fillText('19 tables',255,286);g.globalAlpha=alpha;
  }
  for(const h of HOUSES){const l=lit(h.id);building(h.x,h.y,h.w,h.h,h.col,l,h.kind,h.id.length*7);
    const on=hov===h.id||sel===h.id;
    g.globalAlpha=alpha*(on?1:.4+l*.4);g.fillStyle=on?GOLD:MID;
    g.font='600 12px ui-monospace,Menlo,monospace';g.textAlign='center';
    g.fillText(h.path?disp(h.path):'infra',h.x,h.y+22);g.globalAlpha=alpha;}
  {const sx=60,sy=-64,wav=RM?0:Math.sin(T*2.6)*2;
   g.globalAlpha=alpha*(sel===null||sel==='siege'?1:.4);
   g.strokeStyle=RED;g.beginPath();g.moveTo(sx,sy+26);g.lineTo(sx,sy);g.stroke();
   g.fillStyle=RED;g.beginPath();g.moveTo(sx,sy);g.quadraticCurveTo(sx+9,sy+1+wav,sx+17,sy+2+wav);
   g.lineTo(sx+17,sy+9+wav);g.quadraticCurveTo(sx+9,sy+8+wav,sx,sy+10);g.closePath();g.fill();
   if(hov==='siege'){g.fillStyle=RED;g.font='10px ui-monospace,Menlo,monospace';g.textAlign='left';
     g.fillText('the false ledger',sx+22,sy+9);}}
  g.restore();window._realmView=[W/2-60*sc+cam.x,H/2+10*sc+cam.y,sc];
}
function drawYard(L,alpha){
  const MID=tok('mid'),DIM=tok('dim'),GOLD=tok('gold');
  const col=L.t==='db'?'#c2a14e':colFor(L.path||'');
  const spots=levelSpots(L);
  // fit the view to the actual spread of the yard
  let x0=-200,x1=200,y0=-140,y1=140;
  spots.forEach(s=>{x0=Math.min(x0,s.x-s.w);x1=Math.max(x1,s.x+s.w);
    y0=Math.min(y0,s.y-s.h*1.8-24);y1=Math.max(y1,s.y+40);});
  const sc=Math.min(W/(x1-x0+330),(H-160)/(y1-y0+260))*cam.z;
  const cx=W/2-(x0+x1)/2*sc+cam.x, cy=(H+70)/2-(y0+y1)/2*sc+cam.y;
  g.save();g.globalAlpha=alpha;g.translate(cx,cy);g.scale(sc,sc);
  const rx=(x1-x0)/2+230,ry=(y1-y0)/2+160,ex=(x0+x1)/2,ey=(y0+y1)/2+30;
  g.fillStyle=sh(col,-.72);g.beginPath();g.ellipse(ex,ey,rx,ry,0,0,6.283);g.fill();
  g.strokeStyle=sh(col,-.55);g.lineWidth=2;g.stroke();
  const hue=(name,base)=>{                            // per-quarter tint, deterministic
    const t=(rnd(name.length*13+name.charCodeAt(0))-.5)*.5;
    return sh(base,t*.5);};
  /* ── real movement, at every depth: walkers within, riders out — hover any to see cargo ── */
  {
    const reg=(x,y,from,to,n,cargo,kind)=>MSG.push({x:cx+x*sc,y:cy+y*sc,from,to,n,cargo,kind});
    const draws=[];
    if(L.t==='dir'){
      // subtree ownership: a flow touching pgkit/queue belongs to the pgkit building
      const dirs=spots.filter(s2=>s2.t==='dir').sort((a,b)=>b.path.length-a.path.length);
      const owner=p=>{for(const d of dirs)if(p===d.path||p.startsWith(d.path+'/'))return d;return null;};
      const fat=new Map();for(const s2 of spots)if(s2.t==='file')fat.set(s2.name,s2);
      const walk=new Map(),ride=new Map(),arr=new Map();
      const acc=(m,k,w,cargo,extra)=>{const e2=m.get(k)||{w:0,cargo:[],extra:new Set()};
        e2.w+=w;for(const c2 of cargo.slice(0,2))if(e2.cargo.length<4&&!e2.cargo.includes(c2))e2.cargo.push(c2);
        if(extra)e2.extra.add(extra);m.set(k,e2);};
      for(const[a,out]of Object.entries(ALLF.dd)){const oa=owner(a);
        for(const[b,v]of Object.entries(out)){const ob=owner(b);
          if(oa&&ob&&oa!==ob)acc(walk,oa.name+'|'+ob.name,v[0],v[1]);
          else if(oa&&!ob)acc(ride,oa.name,v[0],v[1],disp(b).split('/').slice(-2).join('/'));
          else if(!oa&&ob)acc(arr,ob.name,v[0],v[1],disp(a).split('/').slice(-2).join('/'));}}
      const byName=new Map();for(const s2 of spots)byName.set(s2.name,s2);
      for(const[k,e2]of walk){const[an,bn]=k.split('|');
        draws.push([byName.get(an),byName.get(bn),e2.w,e2.cargo,'walk']);}
      for(const[an,e2]of ride)draws.push([byName.get(an),null,e2.w,e2.cargo,'ride',[...e2.extra].slice(0,3)]);
      for(const[an,e2]of arr)if(e2.w>=2)draws.push([byName.get(an),null,e2.w,e2.cargo,'arrive',[...e2.extra].slice(0,3)]);
      for(const s2 of spots){
        if(s2.t!=='file')continue;
        const fe=(ALLF.fe[L.path]||{})[s2.name];
        if(fe)draws.push([s2,null,fe[0],fe[1],'ride',['outside this yard']]);
        const fi=(ALLF.fi[L.path]||{})[s2.name];
        if(fi)draws.push([s2,null,fi[0],fi[1],'arrive',fi[2]]);}
      const ff=ALLF.ff[L.path]||{};
      for(const[pair,v]of Object.entries(ff)){const[a,b]=pair.split('|');
        const A=fat.get(a),B=fat.get(b);if(A&&B)draws.push([A,B,v[0],v[1],'walk']);}
    }
    for(const[A,B,w,cargo,kind,exTo]of draws){
      if(kind==='walk'){
        g.strokeStyle=sh(col,-.45);g.lineWidth=Math.min(3,.7+w/10);g.globalAlpha=alpha*.5;
        g.beginPath();g.moveTo(A.x,A.y+6);g.lineTo(B.x,B.y+6);g.stroke();g.globalAlpha=alpha;
        const wn=Math.min(4,1+(w>>3));
        for(let i=0;i<wn;i++){const t=RM?(i+1)/(wn+1):((T*(.05+w*.003)+i/wn+rnd(A.x+i))%1);
          const px=A.x+(B.x-A.x)*t,py=A.y+6+(B.y-A.y)*t;
          person(px,py,isTest(A.name)?TESTC:(roleOf(A.name)?.[0]||hue(A.name,col)));
          reg(px,py,A.name,B.name,w,cargo,'walker');}
      }else if(kind==='arrive'){
        const a=Math.atan2(A.y-ey,(A.x-ex)/1.9)+2.3;
        const gx=ex+Math.cos(a)*rx*.97,gy=ey+Math.sin(a)*ry*.97;
        g.strokeStyle='#9db4c0';g.setLineDash([2,5]);g.lineWidth=1;g.globalAlpha=alpha*.4;
        g.beginPath();g.moveTo(gx,gy);g.lineTo(A.x,A.y+4);g.stroke();g.setLineDash([]);g.globalAlpha=alpha;
        const rn=Math.min(4,1+(w>>2));
        for(let i=0;i<rn;i++){const t=RM?(i+1)/(rn+1):((T*(.05+w*.002)+i/rn+rnd(A.x*3+i))%1);
          const px=gx+(A.x-gx)*t,py=gy+(A.y+4-gy)*t;
          person(px,py,'#9db4c0');
          reg(px,py,(exTo||[]).join(', ')||'beyond the yard',A.name,w,cargo,'visitor');}
      }else{
        const a=Math.atan2(A.y-ey,(A.x-ex)/1.9);
        const gx=ex+Math.cos(a)*rx*.97,gy=ey+Math.sin(a)*ry*.97;
        g.strokeStyle=sh(col,-.4);g.setLineDash([4,5]);g.lineWidth=1;g.globalAlpha=alpha*.45;
        g.beginPath();g.moveTo(A.x,A.y+4);g.lineTo(gx,gy);g.stroke();g.setLineDash([]);g.globalAlpha=alpha;
        const rn=Math.min(3,1+(w>>3));
        for(let i=0;i<rn;i++){const t=RM?(i+1)/(rn+1):((T*(.06+w*.002)+i/rn+rnd(A.y+i))%1);
          const px=A.x+(gx-A.x)*t,py=A.y+4+(gy-A.y)*t;
          g.fillStyle=tok('teal');g.fillRect(px-2.2,py-3.2,4.4,2.6);
          g.fillStyle=tok('dim');g.beginPath();g.arc(px-1.2,py-.2,1,0,6.283);g.arc(px+1.2,py-.2,1,0,6.283);g.fill();
          reg(px,py,A.name,(exTo||[]).map(t2=>disp(t2).split('/').slice(-2).join('/')).join(', ')||'outside',w,cargo,'rider');}
      }
    }
  }
  const paint=[...spots].sort((a,b)=>a.y-b.y);        // painter's order: back first
  for(const s of paint){const id=s.t+':'+s.name;const l=sel===null?1:(sel===id?1:.3);
    const bc=s.t==='imp'?(s.ikind==='ext'?'#6f7a72':col):s.t==='tbl'?'#c2a14e':s.t==='file'?(isTest(s.name)?TESTC:sh(col,.3)):s.t==='sym'?'#7a6a9a':(roleOf(s.name)?.[0]||hue(s.name,col));
    building(s.x,s.y,s.w,s.h,bc,l,s.kind,s.name.length*3);
    const on=hov===id||sel===id;
    // labels: only the 7 largest quarters + anything hovered/selected — the rest stay quiet
    if(on||s.t==='more'||(s.rank!==undefined&&s.rank<7)||s.t==='sym'||s.t==='imp'||s.t==='tbl'){
      g.globalAlpha=alpha*(on?1:.8);
      g.fillStyle=on?GOLD:(s.t==='dir'?MID:DIM);
      g.font=(s.t==='dir'?'600 ':'')+'11.5px ui-monospace,Menlo,monospace';g.textAlign='center';
      let nm=s.name;if(nm.length>26)nm=nm.slice(0,25)+'…';
      const ly=s.y+(s.kind==='bench'?14:20);
      const tw=g.measureText(nm).width;                // scrim so text never fights a wall
      g.fillStyle='rgba(12,15,20,.75)';g.fillRect(s.x-tw/2-14,ly-10,tw+((s.n&&s.t!=='more')?52:28),14);
      g.fillStyle=on?GOLD:(s.t==='file'&&isTest(s.name)?TESTC:(s.t==='dir'?MID:DIM));
      g.fillText(nm+(s.n&&s.t!=='more'?'  ·'+s.n:''),s.x,ly);
      g.globalAlpha=alpha;}
    const fn=s.t==='dir'?Math.min(4,1+((s.n||0)>>6)):0;
    for(let i=0;i<fn;i++){const a=rnd(i*9+s.x)*6.28+(RM?0:T*(.1+rnd(i)*.15)*(i%2?1:-1));
      person(s.x+Math.cos(a)*s.w*.62,s.y+10+Math.sin(a)*12,col);}
  }
  g.restore();window._inView=[cx,cy,sc];
}

let trans=null; // {from:levelIdxSnapshotFn,p}
function wake(){if(raf===null){last=performance.now();raf=requestAnimationFrame(tick);}}
function tick(t){const dt=Math.min((t-last)/1000,.05);last=t;
  if(!RM&&document.visibilityState==='visible')T+=dt;
  if(trans){trans.p+=dt*(RM?99:2.8);if(trans.p>=1)trans=null;}
  draw();raf=requestAnimationFrame(tick);}
function draw(){
  MSG=[];
  g.setTransform(DPR,0,0,DPR,0,0);g.fillStyle=tok('bg');g.fillRect(0,0,W,H);
  const e=x=>1-Math.pow(1-Math.max(0,Math.min(1,x)),3);
  const top=stack[stack.length-1];
  const render=(L,a)=>L.t==='realm'?drawRealm(a):drawYard(L,a);
  if(trans){const p=e(trans.p);render(trans.prev,1-p);render(top,p);}
  else render(top,1);
}
/* ═ interaction ═ */
function hitTop(mx,my){
  const top=stack[stack.length-1];
  if(top.t==='realm'){const[vx,vy,sc]=window._realmView||[W/2,H/2,1];
    const x=(mx-vx)/sc,y=(my-vy)/sc;
    for(const h of HOUSES)if(Math.abs(x-h.x)<h.w*.6&&y<h.y+38&&y>h.y-h.h*1.6)return{k:'house',h};
    for(let i=0;i<POWERS.length;i++)if(x>640-70&&x<640+150&&Math.abs(y-POWERS[i].y+8)<22)return{k:'power',i};
    if(Math.abs(x-100)<70&&Math.abs(y+60)<26)return{k:'siege'};
    if(Math.abs(x-255)<75&&y>190&&y<300)return{k:'db'};return null;}
  const[vx,vy,sc]=window._inView||[W/2,H/2,1];
  const x=(mx-vx)/sc,y=(my-vy)/sc;
  let best=null,bd=1e9;
  for(const s of levelSpots(top))if(Math.abs(x-s.x)<Math.max(38,s.w*.7)&&y<s.y+26&&y>s.y-s.h*1.9){
    const d=Math.hypot(x-s.x,y-s.y);if(d<bd){bd=d;best={k:'spot',s};}}
  return best;}
const tt=document.getElementById('tt'),tale=document.getElementById('tale');
function say(html,sub){tale.style.opacity=0;
  setTimeout(()=>{tt.innerHTML=html+(sub?'<i>'+sub+'</i>':'');tale.style.opacity=1;},RM?0:160);}
const h1t=document.getElementById('h1t'),h1d=document.getElementById('h1d'),crumb=document.getElementById('crumb');
function refresh(){
  const top=stack[stack.length-1];const[t,d]=levelTitle(top);
  h1t.textContent=t;h1d.textContent=d;
  crumb.innerHTML='';
  stack.slice(0,-1).forEach((L,i)=>{const b=document.createElement('button');
    b.textContent=L.t==='realm'?'realm':(L.t==='file'?L.name:disp(L.path).split('/').pop());
    b.onclick=()=>{trans={prev:stack[stack.length-1],p:0};stack=stack.slice(0,i+1);sel=null;cam={x:0,y:0,z:1};refresh();wake();};
    crumb.appendChild(b);
    const s=document.createElement('span');s.textContent='›';crumb.appendChild(s);});
  if(stack.length>1){const cur=document.createElement('span');
    cur.style.color='var(--gold)';cur.textContent=top.t==='file'?top.name:(top.path?disp(top.path).split('/').pop():'');
    crumb.appendChild(cur);}
}
function descend(L,line,sub){trans={prev:stack[stack.length-1],p:0};stack.push(L);sel=null;cam={x:0,y:0,z:1};refresh();say(line,sub);wake();}
let drag=null;
c.addEventListener('mousedown',e=>{
  const h=hitTop(e.clientX,e.clientY);
  if(h&&h.k==='spot'){const[,,sc]=window._inView||[0,0,1];drag={spot:h.s,sc,x:e.clientX,y:e.clientY,m:0};}
  else if(h&&h.k==='house'){const[,,sc]=window._realmView||[0,0,1];drag={spot:h.h,sc,x:e.clientX,y:e.clientY,m:0};}
  else drag={x:e.clientX,y:e.clientY,cx:cam.x,cy:cam.y,m:0};});
addEventListener('mouseup',()=>{drag=null;c.style.cursor=hov?'pointer':'default';});
c.addEventListener('wheel',e=>{e.preventDefault();
  const f=Math.exp(-e.deltaY*.0014),nz=Math.max(.5,Math.min(4,cam.z*f));
  cam.x=(cam.x-(e.clientX-W/2))*(nz/cam.z)+(e.clientX-W/2);
  cam.y=(cam.y-(e.clientY-H/2))*(nz/cam.z)+(e.clientY-H/2);
  cam.z=nz;wake();},{passive:false});
c.addEventListener('mousemove',e=>{
  if(drag){if(Math.abs(e.clientX-drag.x)+Math.abs(e.clientY-drag.y)>3)drag.m=1;
    if(drag.spot){drag.spot.x+=(e.clientX-drag.x)/drag.sc;drag.spot.y+=(e.clientY-drag.y)/drag.sc;
      drag.x=e.clientX;drag.y=e.clientY;c.style.cursor='move';wake();return;}
    cam.x=drag.cx+e.clientX-drag.x;cam.y=drag.cy+e.clientY-drag.y;
    c.style.cursor='grabbing';wake();return;}
  /* messenger hover: who is walking, and what they carry */
  const tip=document.getElementById('tip');let near=null,nd=14;
  for(const m of MSG){const d=Math.hypot(e.clientX-m.x,e.clientY-m.y);if(d<nd){nd=d;near=m;}}
  if(near){tip.style.display='block';tip.style.left=(e.clientX+14)+'px';tip.style.top=(e.clientY+10)+'px';
    tip.innerHTML='<b>'+near.from+'</b>'+(isTest(near.from)?' <span style=color:#8a6fb8>(test)</span>':'')+' → <span class=to>'+near.to+'</span> · '+near.n+' calls'+
      (near.cargo&&near.cargo.length?'<div class=cargo>carrying  '+near.cargo.slice(0,3).join('  ·  ')+'</div>':'');
    hov=null;c.style.cursor='default';return;}
  tip.style.display='none';
  const h=hitTop(e.clientX,e.clientY);
  const id=h?(h.k==='house'?h.h.id:h.k==='power'?'p'+h.i:h.k==='siege'?'siege':h.s.t+':'+h.s.name):null;
  if(id!==hov){hov=id;c.style.cursor=id?'pointer':'default';}});
c.addEventListener('click',e=>{
  if(drag&&drag.m)return;   // a pan is not a click
  const top=stack[stack.length-1];const h=hitTop(e.clientX,e.clientY);
  if(!h){sel=null;const[t,d]=levelTitle(top);say(t==='The Realm of Reqsume'?'Click a house to hear it. Click it again to enter.':'<b>'+t+'</b>',d);return;}
  if(top.t==='realm'){
    if(h.k==='house'){const id=h.h.id;
      if(sel===id&&h.h.path){descend({t:'dir',path:h.h.path},'You enter <b>'+disp(h.h.path)+'</b>.','every building is a real folder or file — keep going in');return;}
      sel=id;say(...h.h.say);}
    else if(h.k==='power'){sel='p'+h.i;say(...POWERS[h.i].say);}
    else if(h.k==='db'){if(sel==='db'){descend({t:'db'},'The granary of record.','19 tables — each declared by a migration · models/database tends 16 of them');return;}
      sel='db';say('<b>postgres</b> — 19 granaries of record.','invoices · jobs · notifications · projects · embeddings … click again to walk the rows');}
    else{sel='siege';say('The ledger names 103 gates. <b>The stones confirm none.</b>','0 routes read from Go source · 14 match no hall');}
  }else{
    const s=h.s,id=s.t+':'+s.name;
    if(s.t==='dir'){if(sel===id)descend({t:'dir',path:s.path},'You enter <b>'+disp(s.path)+'</b>.','deeper still — folders, files, then the symbols themselves');
      else{sel=id;
        const out=FLOWS[s.path],bits=[];
        if(out)for(const[t2,w]of Object.entries(out).sort((a,b)=>b[1]-a[1]).slice(0,3))
          bits.push(disp(t2).split('/').slice(-2).join('/')+' ·'+w);
        say('<b>'+s.name+'</b> — '+(s.n||0)+' symbols within.',
          (bits.length?'sends riders to  '+bits.join('  ·  ')+'   — real call edges · ':'')+'click again to enter');}}
    else if(s.t==='file'){if(sel===id)descend({t:'file',path:s.path,name:s.name},'Inside <b>'+s.name+'</b>.','each bench is one symbol — click for its exact lines');
      else{sel=id;say('<b>'+s.name+'</b> — '+s.n+' symbols.','click again to step inside the workshop');}}
    else if(s.t==='sym'){sel=id;say('<b>'+s.name+'</b>','\`'+stack[stack.length-1].path+'/'+stack[stack.length-1].name+'\` · '+(s.loc||''));}
    else if(s.t==='tbl'){sel=id;say('<b>'+s.name+'</b>','declared by migration '+s.mig+' · apps/api/docs/migrations');}
    else if(s.t==='imp'){if(s.ikind!=='ext'&&sel===id){
        if(s.ikind==='dir')descend({t:'dir',path:s.tgt},'You follow the road to <b>'+disp(s.tgt)+'</b>.','an import, resolved');
        else{const d2=s.tgt.substring(0,s.tgt.lastIndexOf('/'));descend({t:'file',path:d2,name:s.tgt.split('/').pop()},'You follow the road to <b>'+s.tgt.split('/').pop()+'</b>.','an import, resolved');}
        return;}
      sel=id;say('<b>'+s.name+'</b>'+(s.ikind==='ext'?' — from the market.':' — a road out of this workshop.'),
        s.ikind==='ext'?'an npm dependency':'imports resolve to '+disp(s.tgt)+(s.ikind!=='ext'?' · click again to follow':''));}
    else{sel=id;say('<b>'+s.name+'</b>','the store holds them all — this view caps what it draws, never what it knows');}
  }
  wake();});
addEventListener('keydown',e=>{if(e.key==='Escape'&&stack.length>1){
  trans={prev:stack[stack.length-1],p:0};stack.pop();sel=null;cam={x:0,y:0,z:1};refresh();wake();}});
refresh();say('Click a house to hear it. Click it again to enter.','then keep going — folders, files, down to single functions. Esc climbs back.');
resize();wake();
</script>`;
writeFileSync(dir + 'ports-ux.html', html);
console.log('wrote ports-ux.html', (html.length/1024|0)+'KB');
