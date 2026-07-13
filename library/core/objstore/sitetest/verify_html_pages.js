// verify_html_pages.js - the push-generated static HTML page layer on the live
// fixture (M3-M5): guard-gated generation, item pages with threads inlined,
// type list pages + chain, the timeline front page (code commits + README),
// sitemap/robots coverage, and the guards-off bucket carrying zero page keys.
// Pure HTTP over the served fixture — the pages are the no-JS surface, so no
// shim/DOM is involved.
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const TD = ORIGIN + "/" + (process.env.GS_SITE_BUCKET || "thread-demo") + "/";
const OTHER = ORIGIN + "/" + (process.env.GS_SITE_BUCKET_EMPTY || "other-demo") + "/";

// get fetches one served key, returning { status, text }.
async function get(url) {
  const res = await fetch(url);
  return { status: res.status, text: res.status === 200 ? await res.text() : "" };
}

// pageLocs extracts <loc> values from a sitemap urlset.
function pageLocs(xml) {
  return [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1]);
}

(async () => {
  // The canonical base is whatever the fixture's site.url was at build time
  // (an ephemeral locals3 port); suites map it onto the served origin.
  const cfg = JSON.parse((await get(TD + ".gitsocial/site/site-config.json")).text);
  ok("site-config carries the guards + url", cfg.publish === "true" && cfg.pages === "true" && /^http:\/\/127\.0\.0\.1:\d+\/thread-demo\/$/.test(cfg.url), JSON.stringify(cfg));
  const toServed = (loc) => TD + loc.slice(cfg.url.length);

  console.log("\n--- Front page (index.html: the M8 entry flip) ---");
  const front = await get(TD + "index.html");
  ok("index.html served", front.status === 200);
  ok("index.html is the GENERATED front page", /id="gs-page"/.test(front.text) && /name="gs-route" content="\/"/.test(front.text));
  ok("front carries the site title", /Thread Demo/.test(front.text));
  ok("front interleaves code commits (app-linked)", /Add python and rust sources/.test(front.text) && /class="chip code">commit/.test(front.text));
  ok("front carries the README text after the entries", front.text.indexOf("Showcase fixture.") > front.text.indexOf("</ol>"), "idx=" + front.text.indexOf("Showcase fixture."));
  ok("front references gs-upgrade.js (defer)", /<script defer src="\.\/gs-upgrade\.js">/.test(front.text));
  ok("front carries the CSP meta", /Content-Security-Policy/.test(front.text));
  ok("front canonical points at the site root", front.text.includes('<link rel="canonical" href="' + cfg.url + '">'));
  ok("front carries no 'open in app' link", !/open in app/.test(front.text));
  ok("gs-upgrade.js is served", (await get(TD + "gs-upgrade.js")).status === 200);
  ok("pages-app.css is served", (await get(TD + "pages-app.css")).status === 200);

  console.log("\n--- timeline.html retired (the flip dropped it) ---");
  ok("timeline.html is gone (404)", (await get(TD + "timeline.html")).status === 404);

  console.log("\n--- robots.txt + sitemap.xml ---");
  const robots = await get(TD + "robots.txt");
  ok("robots.txt served", robots.status === 200);
  ok("robots allows all and names the sitemap", robots.text.includes("User-agent: *") && robots.text.includes("Allow: /") && robots.text.includes("Sitemap: " + cfg.url + "sitemap.xml"), robots.text);
  ok("robots has no .gitsocial Disallow", !/Disallow/.test(robots.text));
  const sitemap = await get(TD + "sitemap.xml");
  ok("sitemap.xml served as a urlset", sitemap.status === 200 && /<urlset/.test(sitemap.text));
  const locs = pageLocs(sitemap.text);
  ok("sitemap covers the site root", locs[0] === cfg.url, locs[0]);
  const itemLocs = locs.filter((l) => l.includes("/i/"));
  ok("sitemap covers a real item-page set", itemLocs.length >= 15, "items=" + itemLocs.length);
  ok("every sitemap loc is under site.url", locs.every((l) => l.startsWith(cfg.url)));
  const pages = [];
  for (const loc of itemLocs) {
    const p = await get(toServed(loc));
    if (p.status === 200 && /id="gs-page"/.test(p.text)) pages.push(p.text);
  }
  ok("every sitemapped item page serves with the mount div", pages.length === itemLocs.length, pages.length + "/" + itemLocs.length);
  ok("sitemap lastmod is W3C dates", /<lastmod>\d{4}-\d{2}-\d{2}<\/lastmod>/.test(sitemap.text));

  ok("every item page references gs-upgrade.js (defer)", pages.every((p) => /<script defer src="\.\.\/gs-upgrade\.js">/.test(p)), "some item page missing the upgrade script");
  ok("no item page carries an 'open in app' link", pages.every((p) => !/open in app/.test(p)), "an item page still links 'open in app'");
  ok("every item page carries the CSP meta", pages.every((p) => /Content-Security-Policy/.test(p)));

  console.log("\n--- Item pages: threads, edits, feedback ---");
  const thread = pages.find((p) => p.includes("Shipping the S3 static site reader this week."));
  ok("thread root has a page", !!thread);
  ok("thread inlines direct replies", !!thread && thread.includes("Congrats, this is huge!") && thread.includes("What about generation-mode buckets?"));
  ok("thread inlines nested replies in order", !!thread && thread.indexOf("Thanks, appreciate it!") > thread.indexOf("Congrats, this is huge!") && thread.includes("Seconded, well earned."));
  ok("nested reply carries its reply-to attribution", !!thread && /reply to /.test(thread));
  const edited = pages.find((p) => p.includes("Improve onboarding and setup docs"));
  ok("edited issue renders the resolved version", !!edited);
  ok("edited issue carries closed chip + edited marker", !!edited && /class="chip closed">closed/.test(edited) && / edited /.test(edited.replace(/·/g, " ")));
  const pr = pages.find((p) => p.includes("Expand notes with more lines"));
  ok("PR page exists", !!pr);
  ok("PR page inlines line-anchored feedback", !!pr && pr.includes("This wording is clearer, nice.") && pr.includes("notes.txt:2"));
  ok("PR page shows range anchors + suggestion bit", !!pr && pr.includes("notes.txt:4-5") && pr.includes("suggestion"));
  ok("PR page shows review-state chips", !!pr && /class="chip approve">approved/.test(pr) && /class="chip changes">changes requested/.test(pr));
  ok("PR page shows head → base", !!pr && pr.includes("feature/notes-expand → main"));
  const issue = pages.find((p) => p.includes("Static site: thread view needs live fixture"));
  ok("issue page inlines cross-extension social comments", !!issue && issue.includes("I can build the fixture this week.") && issue.includes("Great, assign it to me."));
  const quote = pages.find((p) => p.includes("Great point from upstream"));
  ok("cross-repo quote is a top-level page", !!quote);

  console.log("\n--- Type list pages ---");
  for (const dir of ["issues", "prs", "posts", "releases", "memos"]) {
    const list = await get(TD + dir + "/index.html");
    ok(dir + "/index.html served", list.status === 200);
    if (list.status !== 200) continue;
    // The fixture stays under one list page: the chain has no sealed pages, so
    // walk = assert no dangling older link (sealing is covered by Go tests).
    ok(dir + " has no dangling older link", !/older →/.test(list.text));
  }
  const issues = await get(TD + "issues/index.html");
  ok("issues list folds milestones/sprints in", issues.text.includes("v1.0 Launch") && issues.text.includes("Sprint 1: Foundations"));
  ok("issues list links item pages", /href="\.\.\/i\/[0-9a-f]{12}\.html"/.test(issues.text));
  const memos = await get(TD + "memos/index.html");
  ok("memos list carries the fixture memos", memos.text.includes("Cache invalidation policy"));
  const posts = await get(TD + "posts/index.html");
  ok("posts list carries the posts", posts.text.includes("Anyone tried the new thread view yet?"));
  ok("list nav links home (index.html), not timeline.html", /href="\.\.\/index\.html"/.test(posts.text) && !/timeline\.html/.test(posts.text));

  console.log("\n--- Guards off: zero page keys, shell index.html intact ---");
  for (const key of ["timeline.html", "sitemap.xml", "robots.txt", "pages.css", "posts/index.html", "issues/index.html"]) {
    const r = await get(OTHER + key);
    ok("other-demo has no " + key, r.status === 404, "status=" + r.status);
  }
  // Pages off: index.html is the embedded SPA shell (not a generated front page),
  // and it is NOT deleted — dual-mode ownership keeps it as the shell entry.
  const otherIndex = await get(OTHER + "index.html");
  ok("other-demo still carries the SPA shell at index.html", otherIndex.status === 200);
  ok("other-demo index.html is the shell, not a generated front page", !/name="gs-route"/.test(otherIndex.text));
  ok("other-demo still carries its data artifacts", (await get(OTHER + ".gitsocial/site/refs.json")).status === 200);

  console.log(`\n${pass} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error("THREW:", e); process.exit(1); });
