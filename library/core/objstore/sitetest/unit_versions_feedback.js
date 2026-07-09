// unit_versions_feedback.js - DOM-free core units across two areas:
//  - version resolution: resolveItems edit chains, version ordering, and
//    same-author vs differing-editor (editorName) attribution
//  - review feedback: prFeedback split, reviewSummary verdict tally,
//    anchorFeedback line anchoring + offscreen, suggestion-body extraction
const GS = require("../site/gs-core.js");
let pass=0, fail=0;
function eq(a,b,msg){ if(JSON.stringify(a)===JSON.stringify(b)){pass++;} else {fail++; console.log("FAIL",msg,"got",JSON.stringify(a),"want",JSON.stringify(b));} }
function ok(c,msg){ if(c){pass++;} else {fail++; console.log("FAIL",msg);} }
const H = (s)=> (s.repeat(12)).slice(0,12); // 12-hex short
function commit(short, time, gitmsg, content){
  return { hash: (short+"0".repeat(40)).slice(0,40), short, authorName:"", authorEmail:"", authorTime:time, content, rawMessage: content+"\nGitMsg: ...", gitmsg, refs:[] };
}
const A=H("a"), B=H("b"), C=H("c"), D=H("d");
const canon = commit(A, 100, {ext:"pm",v:"0.1.0",type:"issue",state:"open"}, "Original title\n\nbody v0");
const e1 = commit(B, 110, {ext:"pm",v:"0.1.0",edits:"#commit:"+A+"@gitmsg/pm",state:"open"}, "Edited title\n\nbody v1");
const e2 = commit(C, 120, {ext:"pm",v:"0.1.0",edits:"#commit:"+A+"@gitmsg/pm",state:"closed"}, "Edited title\n\nbody v2");

let items1 = GS.resolveItems([canon]);
eq(items1.length,1,"w1 one item");
eq(items1[0].versions.length,1,"w1 one version");
eq(items1[0].content,"Original title\n\nbody v0","w1 content canonical");

let items2 = GS.resolveItems([e1, canon]);
eq(items2[0].versions.length,2,"w2 gains second version");
eq(items2[0].content,"Edited title\n\nbody v1","w2 latest content");
eq(items2[0].versions[0].content,"Original title\n\nbody v0","w2 v[0] canonical");
eq(items2[0].versions[1].content,"Edited title\n\nbody v1","w2 v[1] edit");
ok(items2[0].versions[0].edited===false && items2[0].versions[1].edited===true,"edited flags");

let items3 = GS.resolveItems([e2, e1, canon]);
eq(items3[0].versions.length,3,"full 3 versions");
eq(items3[0].content,"Edited title\n\nbody v2","full latest content");
eq(items3[0].header.state,"closed","full latest header state");
eq(items3[0].versions.map(v=>v.commit.short),[A,B,C],"version order");
ok(items3[0].versions[0].effectiveTime<items3[0].versions[1].effectiveTime && items3[0].versions[1].effectiveTime<items3[0].versions[2].effectiveTime,"version times chron");
eq(items3[0].versions[1].editorName,"","same-author edit no editorName");

const canonWithAuthor = commit(A,100,{ext:"pm",v:"0.1.0",type:"issue"},"Original\n\nb");
canonWithAuthor.authorEmail="ada@example.com"; canonWithAuthor.authorName="Ada";
const e3 = commit(D,130,{ext:"pm",v:"0.1.0",edits:"#commit:"+A+"@gitmsg/pm",state:"open"}, "t\n\nx");
e3.authorEmail="someoneelse@x.com"; e3.authorName="Bob";
let itemsE = GS.resolveItems([e3, canonWithAuthor]);
eq(itemsE[0].versions[1].editorName,"Bob","differing editor shows editorName");
eq(itemsE[0].versions[0].author,"Ada","canonical author kept across versions");

console.log("=== reviewSummary + anchoring ===");
function fb(short,time,hdr,content){ return { commit: commit(short,time,hdr,content||""), header:hdr, content:content||"", effectiveTime:time }; }
const PR=H("f");
const bobApprove = fb(H("1"),10,{ext:"review",type:"feedback","pull-request":"#commit:"+PR+"@gitmsg/review","review-state":"approved"});
bobApprove.commit.authorEmail="bob@example.com";
const bobChange = fb(H("2"),20,{ext:"review",type:"feedback","pull-request":"#commit:"+PR+"@gitmsg/review","review-state":"changes-requested"});
bobChange.commit.authorEmail="bob@example.com";
const carolInline = fb(H("3"),15,{ext:"review",type:"feedback","pull-request":"#commit:"+PR+"@gitmsg/review",file:"notes.txt","new-line":"2"},"nit here");
carolInline.commit.authorEmail="carol@example.com";
const items = [bobApprove, bobChange, carolInline];
const pf = GS.prFeedback(items,PR);
eq(pf.all.length,3,"prFeedback all");
eq(pf.file.length,1,"prFeedback file");
const sum = GS.reviewSummary(pf.all,"bob@example.com,carol@example.com,dave@example.com");
eq(sum.approved,0,"latest bob change-req");
eq(sum.changesRequested,1,"bob change-req counted");
eq(sum.pending,2,"carol+dave pending (no verdict)");
ok(sum.isBlocked,"blocked");
ok(!sum.isApproved,"not approved");
const bobChip = sum.reviewers.find(r=>r.email==="bob@example.com");
const carolChip = sum.reviewers.find(r=>r.email==="carol@example.com");
eq(bobChip.state,"changes-requested","bob chip latest");
eq(carolChip.state,"commented","carol commented chip");

const hunks=[{lines:[{op:"eq",oldN:1,newN:1,line:"a"},{op:"add",oldN:null,newN:2,line:"b"},{op:"del",oldN:2,newN:null,line:"c"}]}];
const anc = GS.anchorFeedback(pf.file, hunks);
eq([...anc.byKey.keys()],["n2"],"anchor on n2");
eq(anc.offscreen.length,0,"none offscreen");
const offFb = fb(H("4"),16,{ext:"review",type:"feedback",file:"notes.txt","new-line":"99"},"way out");
const anc2 = GS.anchorFeedback([offFb],hunks);
eq(anc2.offscreen.length,1,"offscreen when line not visible");
eq(GS.feedbackAnchorKey({"new-line":"1","new-line-end":"2"}),"n1","range anchors at start new-line");
eq(GS.feedbackAnchorKey({"old-line":"2"}),"o2","old-line key");
eq(GS.suggestionBody("do this\n```suggestion\nnew line\n```\n"),"new line","suggestion body extract");
console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail?1:0);
