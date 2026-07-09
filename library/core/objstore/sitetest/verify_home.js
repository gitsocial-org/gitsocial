// verify_home.js - home page file listing against the live gitsocial bucket:
// the show-more/less control (collapsed count, gradient fade, chevron glyph)
// plus per-extension item counts on the demo-project bucket.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
function findClass(n, cls, out){ out=out||[]; for(const c of n._children||[]){ if(c&&c.nodeType===1){ if(c._cls&&c._cls.has(cls)) out.push(c); findClass(c,cls,out);} } return out; }
function hasClassDeep(n, cls){ return findClass(n, cls).length>0; }
function fire(n, ev){ (n&&n._handlers&&n._handlers[ev]||[]).forEach(fn=>fn({preventDefault(){},stopPropagation(){}})); }
const wait = (ms)=>new Promise(r=>setTimeout(r,ms));
let pass=0,fail=0; const ok=(n,c,e)=>{(c?pass++:fail++);console.log((c?"PASS ":"FAIL ")+n+(!c&&e?" :: "+e:""));};
async function run(hash){ setHash(hash); await GS.route(global.__ctx); await wait(700); }
async function main(){
  await wait(2500);
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  await run("#/");
  const vis = () => findClass(viewNode,"tree-row").filter(r=>r.style.display!=="none").length;
  const ctrl = () => findClass(viewNode,"show-more")[0];
  ok("collapsed count = 3", vis()===3, "visible="+vis());
  ok("gradient element present (tree-fade)", findClass(viewNode,"tree-fade").length===1);
  ok("chevron present in control (gs-icon)", !!ctrl() && hasClassDeep(ctrl(),"gs-icon"));
  ok("control reads 'Show all 16'", /Show all 16/.test(textOf(ctrl())), textOf(ctrl()));
  fire(ctrl(),"click"); await wait(20);
  ok("expand shows all 16", vis()===16, "visible="+vis());
  ok("flips to 'Show less'", /Show less/.test(textOf(ctrl())) && !/Show all/.test(textOf(ctrl())), textOf(ctrl()));
  ok("gradient removed when expanded", findClass(viewNode,"tree-fade").length===0);
  ok("chevron still present when expanded", hasClassDeep(ctrl(),"gs-icon"));
  fire(ctrl(),"click"); await wait(20);
  ok("collapse again -> 3 visible", vis()===3, "visible="+vis());
  ok("collapse again restores gradient", findClass(viewNode,"tree-fade").length===1);
  ok("collapse again restores 'Show all 16'", /Show all 16/.test(textOf(ctrl())));
  // Demo counts (demo-project bucket)
  const counts = {};
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/demo-project/");
  counts.social = (await GS.loadExtItems(global.__ctx,"social")).length;
  counts.pm = (await GS.loadExtItems(global.__ctx,"pm")).length;
  counts.review = (await GS.loadExtItems(global.__ctx,"review")).length;
  counts.release = (await GS.loadExtItems(global.__ctx,"release")).length;
  ok("demo social=6", counts.social===6, "got "+counts.social);
  ok("demo pm=11", counts.pm===11, "got "+counts.pm);
  ok("demo review=2", counts.review===2, "got "+counts.review);
  ok("demo release=1", counts.release===1, "got "+counts.release);
  console.log("\n"+pass+" passed, "+fail+" failed");
  process.exit(fail?1:0);
}
main().catch(e=>{console.error("THREW:",e);process.exit(1)});
