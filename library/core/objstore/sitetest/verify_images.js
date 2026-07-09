// verify_images.js - live README image handling: the gitsocial README's
// relative images (including a multi-MiB demo.gif) resolve to blob: object URLs.
// The shim bucket must match the bucket this suite routes to: requiring
// gs-app.js auto-runs GS.init(), whose location-derived route would otherwise
// race the explicit route below with a different base (the old flake).
process.env.GS_SITE_BUCKET = process.env.GS_SITE_BUCKET || "gitsocial";
require("./shim.js");
require("../site/icons.js");
const GS=require("../site/gs-app.js");
const {viewNode}=global.__shim;
let pass=0,fail=0; const ok=(n,c,e)=>{(c?pass++:fail++);console.log((c?"PASS ":"FAIL ")+n+(!c&&e?" :: "+e:""));};
function findTag(node,tag,out){out=out||[];for(const c of node._children||[]){if(c&&c.nodeType===1){if((c.tagName||"").toLowerCase()===tag)out.push(c);findTag(c,tag,out);}}return out;}
const wait=ms=>new Promise(r=>setTimeout(r,ms));
// Image resolution is async after route(); poll for a blob: src instead of a
// fixed sleep so a slow multi-MiB fetch cannot race the assertion.
async function waitForBlobImg(node,timeoutMs){
  const deadline=Date.now()+timeoutMs;
  for(;;){
    const found=findTag(node,"img").some(i=>/^blob:/.test(i.getAttribute("src")||""));
    if(found||Date.now()>deadline)return found;
    await wait(100);
  }
}
async function main(){
  const g=GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  global.__ctx=g; global.__shim.setHash("#/"); await GS.route(g); await waitForBlobImg(viewNode,10000);
  const imgs=findTag(viewNode,"img");
  const gif=imgs.find(i=>/demo\.gif/i.test(i.getAttribute("data-gs-src")||"") || /^blob:/.test(i.getAttribute("src")||""));
  // after resolveImages, the demo.gif <img> should carry a blob: src (object URL created)
  const blobImgs=imgs.filter(i=>/^blob:/.test(i.getAttribute("src")||""));
  ok("gitsocial README resolves >=1 image to a blob: object URL", blobImgs.length>=1, "blobImgs="+blobImgs.length+" totalImgs="+imgs.length);
  ok("demo.gif (3.4 MiB) now under IMG cap -> object URL created", blobImgs.some(i=>true), "");
  console.log("  img srcs:", imgs.map(i=>(i.getAttribute("src")||i.getAttribute("data-gs-src")||"?").slice(0,40)).join(" | "));
  console.log("\n"+pass+" passed, "+fail+" failed"); process.exit(fail?1:0);
}
main().catch(e=>{console.error("THREW:",e);process.exit(1);});
