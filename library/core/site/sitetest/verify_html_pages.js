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

// REPLY_TEXT is one fixture reply's body, which the parent post's thread page inlines.
const REPLY_TEXT = "Congrats, this is huge!";

(async () => {
  // The canonical base is whatever the fixture's site.url was at build time
  // (an ephemeral locals3 port); suites map it onto the served origin.
  const cfg = JSON.parse((await get(TD + ".gitsocial/site/site-config.json")).text);
  ok("site-config carries the guards + url", cfg.publish === "true" && cfg.pages === "true" && /^http:\/\/127\.0\.0\.1:\d+\/thread-demo\/$/.test(cfg.url), JSON.stringify(cfg));
  const toServed = (loc) => TD + loc.slice(cfg.url.length);

  console.log("\n--- Front page (index.html: the entry flip) ---");
  const front = await get(TD + "index.html");
  ok("index.html served", front.status === 200);
  ok("index.html is the GENERATED front page", /id="gs-page"/.test(front.text) && /name="gs-route" content="\/"/.test(front.text));
  ok("front carries the site title", /Thread Demo/.test(front.text));
  // The front page IS the app's home landing (the upgrade re-renders the same
  // thing): one section, its head line, the root entries, then the README.
  ok("the head line is the branch chip, then the tip commit, each a link",
    /<div class="home-head"><a class="chip" href="[^"]*index\.html#branch:main">⎇ main<\/a> <a class="home-commit" href="[^"]*index\.html#commit:[0-9a-f]{12}@main"><span class="home-row-subject">Add python and rust sources<\/span> <span class="meta"><span class="author"[^>]*>[^<]+<\/span> · <span class="reltime" title="[^"]+">\d{4}-\d{2}-\d{2}<\/span><\/span><\/a><\/div>/.test(front.text));
  // The sidebar counts are the app's alone: a count baked into a sealed page would go stale.
  ok("a served page's sidebar carries no counts", !/nav-count/.test(front.text.replace(/<style data-gs-core>[\s\S]*?<\/style>/, "")));
  ok("front lists the root files as one-line links", /<a class="home-row" href="[^"]*index\.html#file:notes\.txt@main"><span class="home-row-subject">notes\.txt<\/span><\/a>/.test(front.text));
  ok("the section shows two rows and folds the rest behind its chevron, under a fade",
    /<div class="home-files">(<a class="home-row"[^\n]*?<\/a>\n){2}<details class="home-more"><summary class="home-toggle" aria-label="Show all"><span class="gs-icon chevron"><svg [^]*?<\/details><div class="home-fade"><\/div><\/div>/.test(front.text));
  ok("the section carries no label and no activity rows", !/>Show all|>See more|Recent activity/.test(front.text));
  ok("the README follows the section", front.text.indexOf("Showcase fixture.") > front.text.indexOf('class="home-section"'));
  // The pages' styling is the shell's own two sheets: the inlined core (tokens,
  // theme gates, page-structural rules) plus the linked pages-full.css, which
  // carries the class vocabulary — so a page rule that exists anywhere else is
  // a rule the app would not agree with.
  const css = await get(TD + "pages-full.css");
  ok("pages-full.css is served", css.status === 200);
  ok("pages-full.css carries the class vocabulary the front page's markup needs", /\.card\s*\{/.test(css.text) && /\.card-head\s*\{/.test(css.text) && /\.type-glyph\s*\{/.test(css.text), "len=" + css.text.length);
  // The chip vocabulary is the app's own (.chip.state fills, verdict tints,
  // chip-retracted).
  ok("pages-full.css styles the app's chip classes", /\.chip\.state\s*\{/.test(css.text) && /\.chip\.verdict-approved\s*\{/.test(css.text) && /\.chip\.chip-retracted\s*\{/.test(css.text));
  // The inlined core carries the page-structural rules (scoped to #gs-page) and
  // gates dark on the boot-stamped theme class with a media fallback.
  ok("front inlines the core sheet with the page-structural rules", /<style data-gs-core>/.test(front.text) && /:where\(#gs-page\) h2/.test(front.text));
  // The sidebar and the home section are shared vocabulary, so the core
  // sheet — inlined, hence live before pages-full.css lands — carries them.
  ok("the inlined core carries the shared sidebar and home vocabulary", /\.nav-list a/.test(front.text) && /\.nav-icon\s*\{/.test(front.text) && /\.home-row\s*\{/.test(front.text) && /\.home-toggle\s*\{/.test(front.text));
  ok("the inlined core gates dark on the stored-theme class with a media fallback", /html\.dark-mode/.test(front.text) && /@media \(prefers-color-scheme: ?dark\)/.test(front.text) && /html\.light-mode/.test(front.text));
  ok("front links pages-full.css", /<link rel="stylesheet" href="\.\/pages-full\.css">/.test(front.text));
  ok("front references gs-upgrade.js (defer)", /<script defer src="\.\/gs-upgrade\.js">/.test(front.text));
  ok("front carries the CSP meta", /Content-Security-Policy/.test(front.text));
  ok("front CSP script-src permits eval (lazy grammar loader)", /script-src[^"]*'unsafe-eval'/.test(front.text));
  ok("front CSP media-src permits blob (inline video)", /media-src[^"]*blob:/.test(front.text));
  ok("front canonical points at the site root", front.text.includes('<link rel="canonical" href="' + cfg.url + '">'));
  ok("front carries no 'open in app' link", !/open in app/.test(front.text));
  ok("gs-upgrade.js is served", (await get(TD + "gs-upgrade.js")).status === 200);
  ok("pages-core.css is served", (await get(TD + "pages-core.css")).status === 200);

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
  ok("every item page CSP permits eval (lazy grammar loader)", pages.every((p) => /script-src[^"]*'unsafe-eval'/.test(p)));
  ok("every item page CSP permits blob media (inline video)", pages.every((p) => /media-src[^"]*blob:/.test(p)));

  console.log("\n--- Item pages: threads, edits, feedback ---");
  const thread = pages.find((p) => p.includes("Shipping the S3 static site reader this week."));
  ok("thread root has a page", !!thread);
  ok("thread inlines direct replies", !!thread && thread.includes(REPLY_TEXT) && thread.includes("What about generation-mode buckets?"));
  ok("thread inlines nested replies in order", !!thread && thread.indexOf("Thanks, appreciate it!") > thread.indexOf(REPLY_TEXT) && thread.includes("Seconded, well earned."));
  ok("nested reply carries its reply-to attribution", !!thread && /reply to /.test(thread));
  // A post is the root of its own page, feed entry and OG card, and its first
  // line names it on every other surface, so it keeps that line as its heading.
  // The body-only rule is for replies and for a repost's absent content.
  ok("post page keeps its first line as the heading", !!thread && /<h1 class="subject">Shipping the S3 static site reader this week\.<\/h1>/.test(thread), thread && thread.slice(thread.indexOf("<nav>"), thread.indexOf("<nav>") + 300));
  const edited = pages.find((p) => p.includes("Improve onboarding and setup docs"));
  ok("edited issue renders the resolved version", !!edited);
  ok("edited issue carries closed chip + edited marker", !!edited && /class="chip state closed">closed/.test(edited) && /<span class="edited" title="[^"]+">edited<\/span>/.test(edited));
  // Both renderers head a detail with one card head: the h1 subject, then the
  // head's one chip slot. The state pill lands there, never in the meta line.
  // A body-only root (a quote) promotes no first line, so it heads with none.
  ok("an item page carries at most one h1, always the card head's", pages.every((p) => (p.match(/<h1/g) || []).length <= 1 && (!/<h1/.test(p) || /<div class="card-head"><h1 class="subject">/.test(p))), "a page heads with something other than one card-head h1");
  ok("a state pill rides the detail head, not the meta line", !!edited && /<div class="card-head"><h1 class="subject">[^<]*<\/h1> <span class="chip state closed">closed<\/span><\/div>/.test(edited), edited && edited.slice(edited.indexOf('<div class="card-head">'), edited.indexOf('<div class="card-head">') + 200));
  ok("the detail meta line leads with the author, time and hash skeleton", !!edited && /<div class="detail-meta"><span class="meta"><span class="author"[^>]*>[^<]*<\/span> · <span class="reltime" title="[^"]+">\d{4}-\d{2}-\d{2}<\/span> · <a class="hash" href="[0-9a-f]{12}\.html">[0-9a-f]{12}<\/a>/.test(edited), edited && edited.slice(edited.indexOf('<div class="detail-meta">'), edited.indexOf('<div class="detail-meta">') + 260));
  ok("the page layer's own further bits follow the hash", !!edited && /<a class="hash"[^>]*>[0-9a-f]{12}<\/a>(?: · <span class="edited"[^>]*>[^<]*<\/span>)? · issue<\/span>/.test(edited));
  const pr = pages.find((p) => p.includes("Expand notes with more lines"));
  ok("PR page exists", !!pr);
  ok("PR page inlines line-anchored feedback", !!pr && pr.includes("This wording is clearer, nice.") && pr.includes("notes.txt:2"));
  ok("PR page shows range anchors + suggestion bit", !!pr && pr.includes("notes.txt:4-5") && pr.includes("suggestion"));
  ok("PR page renders feedback as the app's card variant", !!pr && /<div class="card feedback verdict-approved">/.test(pr) && /<div class="card feedback verdict-changes-requested">/.test(pr));
  ok("PR page carries the verdict and the anchor as chips", !!pr && /class="chip verdict-approved">approved/.test(pr) && /class="chip verdict-changes-requested">changes requested/.test(pr) && /class="chip">notes\.txt:2/.test(pr));
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
  // Rows lead with the app's own glyph, tinted by state on issues and PRs, and
  // carry no state chip repeating that tint — the same card the booted app paints.
  ok("issue rows lead with a state-tinted glyph and no state chip", /<div class="card-head"><span class="type-glyph tg-(?:open|closed)" title="issue · (?:open|closed)">[○●]<\/span> <a class="subject"/.test(issues.text) && !/<span class="type-glyph tg-(?:open|closed|merged)"[^>]*>[○●⑂]<\/span> <span class="chip state/.test(issues.text), issues.text.slice(issues.text.indexOf('<div class="card"'), issues.text.indexOf('<div class="card"') + 220));
  // A milestone keeps its chip: its glyph carries no state tint, so the pill is
  // the row's only lifecycle cue.
  ok("milestone rows keep their state chip beside the glyph", /<span class="type-glyph tg-milestone" title="milestone">◇<\/span> <span class="chip state [a-z]+">/.test(issues.text));
  const memos = await get(TD + "memos/index.html");
  ok("memos list carries the fixture memos", memos.text.includes("Cache invalidation policy"));
  const posts = await get(TD + "posts/index.html");
  ok("posts list carries the posts", posts.text.includes("Anyone tried the new thread view yet?"));
  ok("list nav links home (index.html)", /href="\.\.\/index\.html"/.test(posts.text));

  console.log("\n--- Commits list ---");
  // Code commits get no page of their own by design, so the commits list IS their
  // crawl surface: the row's text (subject, author, date, sha) lives on a real
  // page, and the href is just a link back to the app.
  const commits = await get(TD + "commits/index.html");
  ok("commits/index.html served", commits.status === 200);
  ok("commits page reads without JS (heading + card rows)", /<h1>Commits<\/h1>/.test(commits.text) && /<div class="card" id="c-/.test(commits.text));
  ok("commits rows carry a citable anchor, an app link and indexable meta", /<div class="card" id="c-[0-9a-f]{12}"><div class="card-head"><span class="type-glyph tg-commit" title="commit">◦<\/span> <a class="subject" href="\.\.\/index\.html#commit:[0-9a-f]{12}@main">[^<]+<\/a><\/div>\s*<span class="meta"><span class="author"[^>]*>Ada Lovelace<\/span> · <span class="reltime" title="[^"]+">\d{4}-\d{2}-\d{2}<\/span> · <a class="hash" href="[^"]*">[0-9a-f]{12}<\/a><\/span><\/div>/.test(commits.text), commits.text.slice(commits.text.indexOf('<div class="card"'), commits.text.indexOf('<div class="card"') + 300));
  ok("commits page lists the default branch's commits", commits.text.includes("Add python and rust sources") && commits.text.includes("Initial commit: README"));
  // Only the DEFAULT branch: the feature branch's commit is in the code corpus
  // (the timeline interleaves it) but not in this list.
  ok("commits page is default-branch only", !commits.text.includes("Expand and edit notes"), "the feature branch's commit leaked into the list");
  ok("commits page carries the meta line (count · branch · order)", /<p class="meta">\d+ commits · main · newest first<\/p>/.test(commits.text), commits.text.slice(commits.text.indexOf("<h1>Commits</h1>"), commits.text.indexOf("<h1>Commits</h1>") + 200));
  ok("the fixture stays under one commits page (no dangling older link)", !/older →/.test(commits.text));
  // The list has no Atom feed of its own (the code corpus carries no bodies), but
  // it still advertises the site feed like every other page.
  ok("commits page advertises the site feed and no type feed", (commits.text.match(/type="application\/atom\+xml"/g) || []).length === 1);
  ok("every list page's nav links the commits list", /href="\.\.\/commits\/index\.html"/.test(posts.text) && /href="\.\/commits\/index\.html"/.test(front.text));
  ok("sitemap covers the commits list", locs.includes(cfg.url + "commits/index.html"), JSON.stringify(locs.filter((l) => l.includes("commits"))));

  console.log("\n--- File pages ---");
  // A repo's own prose gets a crawlable page keyed by its repo path, whose boot
  // hook is the app's file route over that same path: what the crawler reads and
  // what the app opens are the same document.
  const doc = await get(TD + "f/notes.html");
  ok("f/notes.html served", doc.status === 200);
  ok("file page reads without JS (heading + the file's own text)", /<h1>notes\.txt<\/h1>/.test(doc.text) && /<pre>one\ntwo/.test(doc.text));
  ok("file page boots the app's file route on its own path", /name="gs-route" content="file:notes\.txt@main"/.test(doc.text) && /data-base="\.\.\/"/.test(doc.text));
  ok("file page self-canonicalizes at its own key", doc.text.includes('<link rel="canonical" href="' + cfg.url + 'f/notes.html">'));
  // The README is the front page's, so it gets no second page of its own.
  ok("the root README gets no file page", (await get(TD + "f/README.html")).status === 404);
  const fileIndex = await get(TD + "f/index.html");
  ok("f/index.html lists the published documents", fileIndex.status === 200 && /<h1>Files<\/h1>/.test(fileIndex.text) && /href="notes\.html"/.test(fileIndex.text));
  ok("every page's sidebar links the file index", /href="\.\/f\/index\.html"/.test(front.text) && /href="\.\.\/f\/index\.html"/.test(posts.text));
  // notes.txt is under the 100-word floor: its page exists and carries
  // noindex,follow, and only the file index reaches the sitemap.
  ok("sitemap covers the file index but not a page under the word floor", locs.includes(cfg.url + "f/index.html") && !locs.includes(cfg.url + "f/notes.html"), JSON.stringify(locs.filter((l) => l.includes("/f/"))));
  ok("a file page under the word floor carries noindex,follow", /<meta name="robots" content="noindex,follow">/.test(doc.text));

  console.log("\n--- Guards off: zero page keys, shell index.html intact ---");
  for (const key of ["sitemap.xml", "robots.txt", "posts/index.html", "issues/index.html", "commits/index.html", "f/index.html"]) {
    const r = await get(OTHER + key);
    ok("other-demo has no " + key, r.status === 404, "status=" + r.status);
  }
  // Pages off: index.html is the embedded SPA shell (not a generated front page),
  // and it is NOT deleted — dual-mode ownership keeps it as the shell entry.
  const otherIndex = await get(OTHER + "index.html");
  ok("other-demo still carries the SPA shell at index.html", otherIndex.status === 200);
  ok("other-demo index.html is the shell, not a generated front page", !/name="gs-route"/.test(otherIndex.text));
  ok("other-demo still carries its data artifacts", (await get(OTHER + ".gitsocial/refs.json")).status === 200);

  console.log(`\n${pass} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error("THREW:", e); process.exit(1); });
