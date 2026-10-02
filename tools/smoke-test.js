global.localStorage={_d:{},getItem(k){return this._d[k]??null},setItem(k,v){this._d[k]=String(v)},removeItem(k){delete this._d[k]}};
const {createCanvas}=require('@napi-rs/canvas');const fs=require('fs');
const path=require('path');const html=fs.readFileSync(path.join(__dirname,'..','game','index.html'),'utf8');let js=html.match(/<script>\n([\s\S]*)\n<\/script>/)[1];
js=js.replace("renderStats();renderQuests();\nsetTimeout","global.__g={S,SET,player,enterScene,goScene,closeDlg,SCENES,get scene(){return scene},LAY,layApply,layInit,STOP_POS,RUN_CP,DOORS,OBJ};renderStats();renderQuests();\nsetTimeout");
if(!js.includes('global.__g='))throw new Error('hook point not found: update tools/smoke-test.js');
const listeners={};const els={};
function mk(id){return {id,hidden_:0,append(){},querySelectorAll(){return []},value:'',dataset:{},closest(){return null},querySelector(){return mk('q')},_l:{},_h:'',get innerHTML(){return this._h},set innerHTML(v){this._h=v;this.children=[];this.firstChild={focus(){}}},style:{},hidden:['bagov','dashov','fishov','emotePop','mapov','dlg','sortov','kiteov','fishHint','race','fishCard','shopov','decoov','sortRes','kiteSkins','lookov','kiteEnd'].includes(id),textContent:'',children:[],firstChild:{focus(){}},classList:{s:new Set(),add(c){this.s.add(c)},remove(c){this.s.delete(c)},toggle(c,v){if(v===undefined)v=!this.s.has(c);v?this.s.add(c):this.s.delete(c);return v},contains(c){return this.s.has(c)}},addEventListener(n,f){(this._l[n]=this._l[n]||[]).push(f)},appendChild(c){this.children.push(c);this.firstChild=this.children[0]},focus(){},setAttribute(){},setPointerCapture(){},getBoundingClientRect(){return{left:0,top:0,width:100,height:100}},clientWidth:1372,clientHeight:1181,click(){(this._l.click||[]).forEach(f=>f({stopPropagation(){}}))},remove(){}}}
function el(id){if(els[id])return els[id];let e;
 if(['world','ava','por','mm','fcv','kcv','fishBig','lookCv','travelMap'].includes(id)){const w=id==='lookCv'?92:id==='world'?300:(id==='ava'?20:(id==='fcv'?80:(id==='kcv'?320:(id==='fishBig'?112:26))));e=createCanvas(w,id==='fcv'?150:(id==='kcv'?200:(id==='fishBig'?56:w)));e.style={};e.addEventListener=()=>{};e.focus=()=>{};}
 else e=mk(id);
 els[id]=e;return e}
global.document={querySelectorAll:()=>[],querySelector:()=>null,getElementById:el,createElement:t=>t==='canvas'?createCanvas(1,1):mk('x'),fonts:{load:()=>Promise.resolve()}};
global.addEventListener=(n,f)=>{(listeners[n]=listeners[n]||[]).push(f)};
global.matchMedia=()=>({matches:false});global.innerWidth=1372;global.innerHeight=760;global.window=global;
let now=0;const rafs=[];global.performance={now:()=>now};global.requestAnimationFrame=f=>rafs.push(f);
const t0=Date.now();eval(js);
const step=(ms,n)=>{for(let i=0;i<n;i++){now+=ms;const f=rafs.shift();if(f)f(now)}};
function snap(name){const w=el('world');const big=createCanvas(w.width,w.height);const b=big.getContext('2d');b.imageSmoothingEnabled=false;b.drawImage(w,0,0,big.width,big.height);fs.writeFileSync(name,big.toBuffer('image/png'))}

const sl=ms=>new Promise(r=>setTimeout(r,ms));const key=(k,up)=>(listeners[up?'keyup':'keydown']||[]).forEach(f=>f({key:k,repeat:false,preventDefault(){}}));
setTimeout(async()=>{let fail=0;const chk=(label)=>{const e=el('err')&&el('err').textContent;if(e){console.log('FAIL',label,e);fail++}else console.log('ok  ',label)};
 step(16,3);chk('boot');await sl(900);__g.closeDlg();__g.SET.time='day';
 const spots=[['plaza',63,52],['market',19,47],['forest',62,14],['dept',106,50],['farm',30,78],['windmill',113,80],['beach',60,101],['arena',63,76]];
 for(const [n,x,y] of spots){__g.player.x=x*16;__g.player.y=y*16;step(16,10);chk('town:'+n)}
 for(const k of ['hq','hq2','kbase','dc','edu','mill1','mill2','museum','hof','island','house_IT']){if(!__g.SCENES[k]){console.log('skip',k);continue}__g.goScene(k,__g.SCENES[k].doorX,(__g.SCENES[k].rows-3)*16,0);await sl(650);step(16,8);chk('scene:'+k)}
 // map layout editor: move every object 2 tiles right, check linked system points follow, then revert
 {const L=__g.LAY;__g.layInit();const obj={};for(const [k,o] of L.K)obj[k]=[o.l0.x+32,o.l0.base];const stop=__g.STOP_POS.plaza,sx=stop.x,fin=__g.RUN_CP[__g.RUN_CP.length-1],fx=fin.x,ex=__g.SCENES.hq.exitTo[0];
  __g.layApply({obj,pts:{'run:0':[100,100]}});const bad=[];if(stop.x!==sx+32)bad.push('bus stop arrival');if(fin.x!==fx+32)bad.push('race finish');if(__g.SCENES.hq.exitTo[0]!==ex+32)bad.push('hq exit');if(__g.RUN_CP[0].x!==100)bad.push('pin override');
  for(const d of __g.DOORS){const o=__g.OBJ.find(q=>q.att&&q.att.includes(d));if(!o)bad.push('door '+d.k+' not attached')}
  for(const [n,x,y] of spots){__g.player.x=x*16+32;__g.player.y=y*16;step(16,10);chk('layout moved:'+n)}
  __g.layApply({});if(stop.x!==sx||fin.x!==fx||__g.SCENES.hq.exitTo[0]!==ex)bad.push('revert');
  if(bad.length){console.log('FAIL layout editor:',bad.join(', '));fail++}else console.log('ok   layout editor links')}
 __g.SET.time='night';__g.goScene('town',63*16+8,57*16,0);await sl(650);step(16,8);chk('night');
 console.log(fail?fail+' check(s) failed':'all checks passed');process.exit(fail?1:0)},900);
