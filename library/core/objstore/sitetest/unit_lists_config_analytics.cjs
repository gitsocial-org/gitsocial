// unit_lists_config_analytics.cjs - DOM-free core units across three areas:
//  - lists: parseListRef, enumerateLists, listMemberRef
//  - config: jsonCommitMessage parsing
//  - analytics: commitsByMonth, extensionStats, latestReleaseVersion
const GS = require('../site/gs-core.js');
let pass=0, fail=0;
function eq(name, got, want){ const a=JSON.stringify(got), b=JSON.stringify(want); if(a===b){pass++;} else {fail++; console.log("FAIL",name,"\n got",a,"\n want",b);} }

// parseListRef
eq("meta", GS.parseListRef("refs/gitmsg/social/lists/team/_meta"), {ext:"social",name:"team",kind:"meta",hash:""});
eq("item", GS.parseListRef("refs/gitmsg/social/lists/team/items/abcdef"), {ext:"social",name:"team",kind:"item",hash:"abcdef"});
eq("legacy", GS.parseListRef("refs/gitmsg/social/lists/team"), {ext:"social",name:"team",kind:"meta",hash:""});
eq("nonlist", GS.parseListRef("refs/gitmsg/social/config"), null);
eq("head", GS.parseListRef("refs/heads/main"), null);

// enumerateLists
const man = {
  "refs/heads/main":"x",
  "refs/gitmsg/social/lists/team/_meta":"a",
  "refs/gitmsg/social/lists/team/items/1":"b",
  "refs/gitmsg/social/lists/team/items/2":"c",
  "refs/gitmsg/social/lists/friends/_meta":"d",
  "refs/gitmsg/social/config":"z",
};
const lists = GS.enumerateLists(man);
eq("listcount", lists.length, 2);
eq("list0id", lists[0].id, "social/friends");
eq("list1items", lists[1].itemRefs.length, 2);
eq("enum-empty", GS.enumerateLists(null), []);

// listMemberRef
eq("local", GS.listMemberRef("#branch:main"), {repoUrl:"",ref:"#branch:main",local:true});
eq("foreign", GS.listMemberRef("https://x/y#branch:main").local, false);

// jsonCommitMessage
eq("json-ok", GS.jsonCommitMessage('{"name":"team","version":"0.1.0"}'), {name:"team",version:"0.1.0"});
eq("json-bad", GS.jsonCommitMessage("not json"), null);
eq("json-arr", GS.jsonCommitMessage("[1,2]"), [1,2]);

// commitsByMonth
const mk = (iso) => ({authorTime: Math.floor(Date.parse(iso)/1000)});
const cm = GS.commitsByMonth([mk("2026-01-15T00:00:00Z"), mk("2026-01-20T00:00:00Z"), mk("2026-03-05T00:00:00Z")]);
eq("cm-buckets", cm.buckets.map(b=>b.month+":"+b.count), ["2026-01:2","2026-02:0","2026-03:1"]);
eq("cm-max", cm.max, 2);
eq("cm-empty", GS.commitsByMonth([]), {buckets:[],max:0});

// extensionStats
const item = (type,state)=>({header:{type,state},effectiveTime:1});
const perExt = {
  pm:[item("issue","open"),item("issue","closed"),item("milestone","open"),item("sprint","open")],
  review:[item("pull-request","open"),item("pull-request","merged"),item("pull-request","closed"),item("feedback","")],
  release:[item("release"),item("release")],
  social:[item("post"),item("post")],
  memo:[item("memo")],
};
const st = GS.extensionStats(perExt);
eq("issues", st.issues, {open:1,closed:1,total:2});
eq("milestones", st.milestones, 1);
eq("sprints", st.sprints, 1);
eq("prs", st.prs, {open:1,merged:1,closed:1,total:3});
eq("releases", st.releases, 2);
eq("posts", st.posts, 2);
eq("memos", st.memos, 1);

// latestReleaseVersion
eq("lrv-ver", GS.latestReleaseVersion([{header:{type:"release",version:"1.2.0"},effectiveTime:1},{header:{type:"release",version:"1.3.0"},effectiveTime:2}]), "v1.3.0");
eq("lrv-tag", GS.latestReleaseVersion([{header:{type:"release",tag:"nightly"},effectiveTime:5}]), "nightly");
eq("lrv-none", GS.latestReleaseVersion([]), "");

// forkRefNames (manifest fork refs, valid shas only, sorted)
const fman = {
  "refs/heads/main": "a".repeat(40),
  "refs/gitmsg/core/forks/def456": "d".repeat(40),
  "refs/gitmsg/core/forks/abc123": "c".repeat(40),
  "refs/gitmsg/core/forks/bad": "not-a-sha",
  "refs/gitmsg/social/config": "e".repeat(40),
};
eq("forks-names", GS.forkRefNames(fman), ["refs/gitmsg/core/forks/abc123", "refs/gitmsg/core/forks/def456"]);
eq("forks-empty", GS.forkRefNames(null), []);

// activityBuckets (contiguous per-period, per-kind counts, peak)
const KINDS = ["posts", "issues"];
const ent = (kind, iso) => ({ kind, time: Math.floor(Date.parse(iso) / 1000), author: "A", email: "a@x" });
const ents = [ent("posts","2026-01-15T00:00:00Z"), ent("posts","2026-01-20T00:00:00Z"), ent("issues","2026-01-10T00:00:00Z"), ent("posts","2026-03-05T00:00:00Z")];
const mb = GS.activityBuckets(ents, "monthly", KINDS);
eq("ab-monthly", mb.buckets.map(b => b.label + ":" + b.total), ["2026-01:3", "2026-02:0", "2026-03:1"]);
eq("ab-monthly-kinds", mb.buckets[0].counts, {posts:2, issues:1});
eq("ab-monthly-max", mb.max, 3);
eq("ab-yearly", GS.activityBuckets(ents, "yearly", KINDS).buckets.map(b => b.label + ":" + b.total), ["2026:4"]);
const wb = GS.activityBuckets(ents, "weekly", KINDS);
eq("ab-weekly-monday-start", [wb.buckets[0].label, wb.buckets[wb.buckets.length - 1].label], ["2026-01-05", "2026-03-02"]);
eq("ab-empty", GS.activityBuckets([], "monthly", KINDS), {buckets:[], max:0});

// topItemAuthors (merge by email, descending by count then name)
eq("top-authors", GS.topItemAuthors([{author:"Ann",email:"ann@x"},{author:"Ann",email:"ann@x"},{author:"Bob",email:"bob@x"}]),
  [{name:"Ann",email:"ann@x",count:2},{name:"Bob",email:"bob@x",count:1}]);
eq("top-authors-limit", GS.topItemAuthors([{author:"Ann",email:"ann@x"},{author:"Bob",email:"bob@x"}], 1).length, 1);

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail?1:0);
