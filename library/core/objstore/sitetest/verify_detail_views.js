// verify_detail_views.js - live detail-view + timeline rendering. Grab-bag:
//  - meshtastic merged timeline cards show the origin author (not the importer)
//  - thread-demo issue detail: detail-meta row, Rendered/Raw toggle, raw GitMsg
//  - thread-demo post detail: nested thread rail connector guides
require("./shim.js");
require("../site/icons.js");
const GS=require("../site/gs-app.js");
const {viewNode,textOf}=global.__shim;
let pass=0,fail=0; const ok=(n,c,e)=>{(c?pass++:fail++);console.log((c?"PASS ":"FAIL ")+n+(!c&&e?" :: "+e:""));};
function findClass(node,cls,out){out=out||[];for(const c of node._children||[]){if(c&&c.nodeType===1){if(c._cls&&c._cls.has(cls))out.push(c);findClass(c,cls,out);}}return out;}
const wait=ms=>new Promise(r=>setTimeout(r,ms));
async function route(ctx,h){global.__ctx=ctx;global.__shim.setHash(h);await GS.route(ctx);await wait(700);}
async function main(){
  // meshtastic merged timeline renders cards
  const m=GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/meshtastic/");
  await route(m,"#/timeline");
  const cards=findClass(viewNode,"card");
  ok("meshtastic #/timeline renders cards (merged feed)", cards.length>5, "cards="+cards.length);
  ok("timeline card shows origin author (not importer 'Max Rakhimov')", !/Max Rakhimov/.test(textOf(cards[0]||{_children:[]})) , textOf(cards[0]).slice(0,80));

  // thread-demo issue detail: thread with rail connectors + detail-meta toggle + raw GitMsg
  const t=GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/thread-demo/");
  await route(t,"#commit:f7bff875a7a2@gitmsg/pm");
  ok("issue detail has a detail-meta row", findClass(viewNode,"detail-meta").length===1);
  const dm=findClass(viewNode,"detail-meta")[0];
  ok("Rendered|Raw toggle sits inside the detail-meta row", dm && findClass(dm,"view-modes").length===1, "toggle not in meta row");
  ok("meta row + toggle share the same parent", dm && findClass(dm,"meta").length>=1 && findClass(dm,"view-modes").length===1);
  ok("issue detail renders a thread with comment cards", findClass(viewNode,"comment").length>=1, "comments="+findClass(viewNode,"comment").length);
  // click Raw -> full verbatim GitMsg trailer visible
  const rawBtn=findClass(viewNode,"view-toggle").find(b=>textOf(b)==="Raw");
  if(rawBtn){ (rawBtn._handlers.click||[]).forEach(fn=>fn({preventDefault(){}})); await wait(50); }
  ok("Raw pane shows the literal GitMsg: trailer", /GitMsg:/.test(textOf(viewNode)), "no GitMsg in raw");
  // Raw pane reads as a code block: the pane node carries the raw-body class,
  // which index.html styles with var(--fs-code)/panel/border (code-block look).
  ok("Raw pane carries the code-block class (raw-body)", findClass(viewNode,"raw-body").length===1, "raw-body nodes="+findClass(viewNode,"raw-body").length);

  // thread-demo post detail with nested replies -> rail guides present
  await route(t,"#commit:3b0bbe8caec3@gitmsg/social");
  const rails=findClass(viewNode,"thread-rail");
  ok("nested thread renders rail connector guides", rails.length>=1, "rails="+rails.length);
  ok("rail has depth guides", findClass(viewNode,"rail-guide").length>=1);

  console.log("\n"+pass+" passed, "+fail+" failed"); process.exit(fail?1:0);
}
main().catch(e=>{console.error("THREW:",e);process.exit(1);});
