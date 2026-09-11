// runner.js - build the fixture, serve it, run the site-test battery.
//
// One tier, self-contained: DOM-free unit suites plus suites that drive the
// showcase fixture this runner builds (fixture.sh) and serves (serve.js) on an
// ephemeral port. Exits nonzero on any suite failure.
const { spawn, spawnSync } = require("child_process");
const path = require("path");

const here = __dirname;

// Default battery: order is unit-first (no server) then fixture-backed.
const DEFAULT = [
  "unit_versions_feedback.js",
  "unit_pm_search.js",
  "unit_graph_order.js",
  "unit_lists_config_analytics.cjs",
  "unit_parity.js",
  "verify_403.js",
  "verify_threads.js",
  "verify_memos.js",
  "verify_pages.js",
  "verify_html_pages.js",
  "verify_upgrade_boot.js",
  "verify_items_shards.js",
  "verify_interrupted_push.js",
  "verify_partial_bootstrap.js",
  "verify_stale_index_bounds.js",
  "verify_stale_index_detail.js",
  "verify_stale_index_routes.js",
  "verify_site_features.js",
  "verify_code_index.js",
  "verify_branch_log_index.js",
  "verify_grammars.js",
  "verify_sparse_repo.js",
  "verify_merged_diff.js",
  "verify_route_supersede.js",
  "verify_packfiles.js",
  "verify_absent_keys.js",
  "verify_prism_deadline.js",
  "verify_request_budget.js",
  "verify_styles.js",
];

// runSuite spawns one suite under node with the given origin and returns its
// tally parsed from the trailing "N passed, M failed" line.
function runSuite(file, origin) {
  const env = Object.assign({}, process.env, { GS_SITE_ORIGIN: origin });
  const res = spawnSync(process.execPath, [path.join(here, file)], { env, encoding: "utf8" });
  const out = (res.stdout || "") + (res.stderr || "");
  const m = out.match(/(\d+) passed, (\d+) failed/);
  const passed = m ? Number(m[1]) : 0;
  const failed = m ? Number(m[2]) : 0;
  const ok = res.status === 0 && failed === 0 && m != null;
  return { file, passed, failed, ok, out };
}

// startServer launches serve.js over root and resolves once it prints its port.
function startServer(root) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(here, "serve.js"), root, "0"], { stdio: ["ignore", "pipe", "inherit"] });
    let buf = "";
    child.stdout.on("data", (d) => {
      buf += d;
      const m = buf.match(/listening (\d+)/);
      if (m) resolve({ child, port: Number(m[1]) });
    });
    child.on("exit", (c) => reject(new Error("serve.js exited early: " + c)));
    setTimeout(() => reject(new Error("serve.js did not start")), 5000);
  });
}

// summarize prints a table and returns the number of failed suites.
function summarize(title, results) {
  console.log("\n" + title);
  console.log("  " + "suite".padEnd(32) + "pass  fail  result");
  let bad = 0;
  for (const r of results) {
    if (!r.ok) bad++;
    console.log("  " + r.file.padEnd(32) + String(r.passed).padStart(4) + "  " + String(r.failed).padStart(4) + "  " + (r.ok ? "ok" : "FAIL"));
  }
  return bad;
}

async function main() {
  const fixtureHome = path.join(here, ".fixture");
  // Build (or reuse) the showcase fixture.
  const fx = spawnSync("bash", [path.join(here, "fixture.sh"), fixtureHome], { stdio: "inherit" });
  if (fx.status !== 0) { console.error("fixture build failed"); process.exit(1); }

  const served = path.join(fixtureHome, "served");
  const { child, port } = await startServer(served);
  const origin = "http://127.0.0.1:" + port;

  let failedSuites = 0;
  const defResults = DEFAULT.map((f) => {
    const r = runSuite(f, origin);
    if (!r.ok) process.stdout.write(r.out);
    return r;
  });
  failedSuites += summarize("Default tier (fixture: " + origin + ")", defResults);

  child.kill();
  console.log("\n" + (failedSuites === 0 ? "ALL GREEN" : failedSuites + " suite(s) FAILED"));
  process.exit(failedSuites === 0 ? 0 : 1);
}
main().catch((e) => { console.error(e); process.exit(1); });
