// verify_items_releases.js - item + release logic (gs-core), synthetic + live.
// Grab-bag:
//  - parseRefs / refRepoUrl / embeddedRefs (GitMsg-Ref trailers, cross-repo)
//  - groupThread comment nesting
//  - stateCounts, groupPM (milestone/sprint buckets)
//  - releaseAssets (artifacts/checksums/sbom hrefs), authorStats
//  - live demo-project / gitsocial fixture counts
const GS = require("../site/gs-core.js");
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (e ? " :: " + e : "")); };
const mkItem = (short, time, header, content) => ({ commit: { short, hash: short + "0".repeat(40 - short.length), authorTime: time, refs: [] }, header: header || {}, content: content || "" });

async function main() {
  // ---- parseRefs (synthetic, per GITSOCIAL nested-comment example) ----
  const nested = "I agree!\n\nGitMsg: ext=\"social\"; type=\"comment\"; reply-to=\"#commit:def456789abc@main\"; original=\"#commit:abc123456789@main\"; v=\"0.1.0\"\n" +
    "GitMsg-Ref: ext=\"social\"; type=\"comment\"; author=\"Bob\"; email=\"bob@example.com\"; time=\"2025-01-06T11:00:00Z\"; ref=\"#commit:def456789abc@main\"; v=\"0.1.0\"\n" +
    " > Parent comment\n" +
    "GitMsg-Ref: ext=\"social\"; type=\"post\"; author=\"Alice\"; email=\"alice@example.com\"; time=\"2025-01-06T10:30:00Z\"; ref=\"#commit:abc123456789@main\"; v=\"0.1.0\"\n" +
    " > Original post";
  const refs = GS.parseRefs(nested);
  ok("parseRefs finds 2 trailers", refs.length === 2, "got " + refs.length);
  ok("parseRefs[0] author+quoted", refs[0].author === "Bob" && refs[0].quoted === "Parent comment", JSON.stringify(refs[0]));
  ok("parseRefs[1] author+quoted", refs[1].author === "Alice" && refs[1].quoted === "Original post");

  // ---- refRepoUrl ----
  ok("refRepoUrl cross-repo", GS.refRepoUrl("https://github.com/u/r#commit:abc@main") === "https://github.com/u/r");
  ok("refRepoUrl same-repo empty", GS.refRepoUrl("#commit:abc@main") === "");

  // ---- groupThread (synthetic) ----
  const P = "aaaaaaaaaaaa";
  const comments = [
    mkItem("bbbbbbbbbbbb", 100, { type: "comment", original: "#commit:" + P + "@gitmsg/social" }, "c1"),
    mkItem("cccccccccccc", 200, { type: "comment", original: "#commit:" + P + "@gitmsg/social", "reply-to": "#commit:bbbbbbbbbbbb@gitmsg/social" }, "c2 reply"),
    mkItem("dddddddddddd", 50, { type: "comment", original: "#commit:" + P + "@gitmsg/social" }, "c3 earlier"),
    mkItem("eeeeeeeeeeee", 300, { type: "comment", original: "#commit:999999999999@gitmsg/social" }, "other"),
  ];
  const thread = GS.groupThread(P, comments);
  ok("thread top-level count = 2", thread.length === 2, "got " + thread.length);
  ok("thread chronological (c3 before c1)", thread[0].comment.content === "c3 earlier" && thread[1].comment.content === "c1", thread.map(n => n.comment.content).join(","));
  ok("thread c2 nested under c1", thread[1].replies.length === 1 && thread[1].replies[0].comment.content === "c2 reply", JSON.stringify(thread[1].replies.map(r => r.comment.content)));
  ok("thread excludes other-item comment", !thread.some(n => n.comment.content === "other" || n.replies.some(r => r.comment.content === "other")));

  // ---- embeddedRefs (synthetic cross-repo repost) ----
  const repostMsg = "# Alice @ user/repo: excerpt\n\nGitMsg: ext=\"social\"; type=\"repost\"; original=\"https://github.com/user/repo#commit:abc123456789@main\"; v=\"0.1.0\"\n" +
    "GitMsg-Ref: ext=\"social\"; type=\"post\"; author=\"Alice\"; email=\"alice@example.com\"; time=\"2025-01-06T10:30:00Z\"; ref=\"https://github.com/user/repo#commit:abc123456789@main\"; v=\"0.1.0\"\n > Original content";
  const rc = { refs: GS.parseRefs(repostMsg) };
  const emb = GS.embeddedRefs(rc, { type: "repost", original: "https://github.com/user/repo#commit:abc123456789@main" });
  ok("embeddedRefs one cross-repo ref", emb.length === 1, "got " + emb.length);
  ok("embeddedRefs url+author+quoted", emb.length === 1 && emb[0].url === "https://github.com/user/repo" && emb[0].author === "Alice" && emb[0].quoted === "Original content", JSON.stringify(emb[0]));
  ok("embeddedRefs skips same-repo", GS.embeddedRefs({ refs: [] }, { original: "#commit:abc@main" }).length === 0);

  // ---- stateCounts ----
  const sc = GS.stateCounts([mkItem("1", 1, { state: "open" }), mkItem("2", 2, { state: "closed" }), mkItem("3", 3, { state: "open" }), mkItem("4", 4, {})]);
  ok("stateCounts total+byState", sc.total === 4 && sc.byState.open === 3 && sc.byState.closed === 1, JSON.stringify(sc));

  // ---- groupPM (synthetic milestone/sprint) ----
  const M = "aaaaaaaaaaaa", S = "bbbbbbbbbbbb";
  const pm = [
    mkItem(M, 10, { type: "milestone", state: "open", due: "2025-03-15" }, "Release v2.0"),
    mkItem(S, 20, { type: "sprint", state: "active", start: "2025-02-01", end: "2025-02-14" }, "Sprint 23"),
    mkItem("cccccccccccc", 30, { type: "issue", state: "open", milestone: "#commit:" + M + "@gitmsg/pm" }, "issue in milestone"),
    mkItem("dddddddddddd", 40, { type: "issue", state: "open", sprint: "#commit:" + S + "@gitmsg/pm" }, "issue in sprint"),
    mkItem("eeeeeeeeeeee", 50, { type: "issue", state: "closed" }, "loose issue"),
  ];
  const g = GS.groupPM(pm);
  ok("groupPM splits types", g.milestones.length === 1 && g.sprints.length === 1 && g.issues.length === 3, JSON.stringify({ m: g.milestones.length, s: g.sprints.length, i: g.issues.length }));
  ok("groupPM byMilestone bucket", (g.byMilestone.get(M) || []).length === 1 && g.byMilestone.get(M)[0].content === "issue in milestone");
  ok("groupPM bySprint bucket", (g.bySprint.get(S) || []).length === 1 && g.bySprint.get(S)[0].content === "issue in sprint");

  // ---- releaseAssets (synthetic) ----
  const ra = GS.releaseAssets({ "artifact-url": "https://cdn.example/v1", artifacts: "a.tar.gz,b.zip", checksums: "SHA256SUMS", sbom: "sbom.spdx.json", "signed-by": "SHA256:abc" });
  ok("releaseAssets hrefs from artifact-url", ra.artifacts.length === 2 && ra.artifacts[0].href === "https://cdn.example/v1/a.tar.gz" && ra.checksums.href === "https://cdn.example/v1/SHA256SUMS" && ra.sbom.name === "sbom.spdx.json" && ra.signedBy === "SHA256:abc", JSON.stringify(ra));
  const raNoUrl = GS.releaseAssets({ artifacts: "x.tar.gz", checksums: "checksums.txt" });
  ok("releaseAssets null href without artifact-url", raNoUrl.artifacts[0].href === null && raNoUrl.checksums.href === null);
  ok("releaseAssets empty header", GS.releaseAssets({}).artifacts.length === 0);

  // ---- authorStats (synthetic) ----
  const as = GS.authorStats([{ authorName: "A" }, { authorName: "B" }, { authorName: "A" }, { authorName: "A" }]);
  ok("authorStats sorted by count", as.total === 4 && as.authors[0].name === "A" && as.authors[0].count === 3 && as.authors[1].name === "B", JSON.stringify(as));

  // ============ LIVE FIXTURES ============
  // demo: no comments -> empty threads over every item
  const demo = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/demo-project/");
  const demoSocial = (await GS.loadExtItems(demo, "social")).filter((i) => i.header && i.header.original);
  ok("demo has no comments (embedded/thread path is synthetic-only)", demoSocial.length === 0, "found " + demoSocial.length);
  const demoPm = await GS.loadExtItems(demo, "pm");
  const demoG = GS.groupPM(demoPm);
  ok("demo pm: 11 issues, 0 milestones, 0 sprints", demoG.issues.length === 11 && demoG.milestones.length === 0 && demoG.sprints.length === 0, JSON.stringify({ i: demoG.issues.length, m: demoG.milestones.length, s: demoG.sprints.length }));
  const demoPmCounts = GS.stateCounts(demoG.issues);
  ok("demo pm state counts (9 closed, 2 open)", demoPmCounts.byState.closed === 9 && demoPmCounts.byState.open === 2, JSON.stringify(demoPmCounts.byState));

  // gitsocial: releases with assets, authors, pm types
  const gs = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  const rel = await GS.loadExtItems(gs, "release");
  let withArt = 0, withCk = 0, withSbom = 0;
  let sample = null;
  for (const it of rel) { const a = GS.releaseAssets(it.header); if (a.artifacts.length) withArt++; if (a.checksums) withCk++; if (a.sbom) withSbom++; if (!sample && a.artifacts.length && a.artifactUrl) sample = { it, a }; }
  ok("gitsocial 62 releases", rel.length === 62, "got " + rel.length);
  ok("gitsocial releases with artifacts = 52", withArt === 52, "got " + withArt);
  ok("gitsocial releases with checksums = 35 / sbom = 32", withCk === 35 && withSbom === 32, "ck=" + withCk + " sbom=" + withSbom);
  ok("sample artifact href = artifact-url/name", sample && sample.a.artifacts[0].href === sample.a.artifactUrl + "/" + sample.a.artifacts[0].name, sample ? sample.a.artifacts[0].href : "none");

  const gsPm = GS.groupPM(await GS.loadExtItems(gs, "pm"));
  ok("gitsocial pm: 1 issue, 0 milestone, 0 sprint", gsPm.issues.length === 1 && gsPm.milestones.length === 0 && gsPm.sprints.length === 0, JSON.stringify({ i: gsPm.issues.length, m: gsPm.milestones.length, s: gsPm.sprints.length }));

  const head = await GS.resolveHead((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  const mainCommits = await GS.walkHistory(gs, head.sha, GS.WALK_CAP);
  const stats = GS.authorStats(mainCommits);
  ok("gitsocial main walk single author Max Rakhimov", stats.authors.length === 1 && stats.authors[0].name === "Max Rakhimov" && stats.authors[0].count === stats.total, JSON.stringify({ n: stats.authors.length, top: stats.authors[0], total: stats.total }));
  ok("gitsocial main walk total 100-200 (bounded)", stats.total > 100 && stats.total <= GS.WALK_CAP, "total=" + stats.total);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
