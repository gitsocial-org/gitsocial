// shots.js - screenshot every repo-shape route at two widths in both themes.
// Builds the fixtures (shapes.sh), serves them (serve.js) and runs one headless
// Chrome per shot into the directory given on argv. Exits 2 without Chrome.
const fs = require("fs");
const path = require("path");
const { execFileSync, spawn, spawnSync } = require("child_process");
const chrome = require("./chrome.js");

const WIDTHS = { 1280: 900, 390: 844 };
const THEMES = { light: "1", dark: "0" };
// BUDGET is the virtual time one page gets to boot, load its shell and settle.
const BUDGET = 20000;

// ROUTES is the surface each fixture is captured on: the rule the fixture checks
// decides the list, and a route with no shape rule is left to the showcase
// battery. A pre-rendered page boots into an app route, so nojs captures it.
const ROUTES = {
  "src-repo": [
    { name: "front", url: "" },
    { name: "issues", url: "#/issues" },
    { name: "releases", url: "#/releases" },
    { name: "code", url: "#/code" },
  ],
  "docs-repo": [
    { name: "front", url: "" },
    { name: "mdx", url: "f/docs/intro.html" },
    { name: "mdx-nojs", url: "f/docs/intro.html", nojs: true },
    { name: "code", url: "#/code" },
  ],
  "empty-repo": [
    { name: "front", url: "" },
    { name: "front-nojs", url: "", nojs: true },
    { name: "code", url: "#/code" },
  ],
  "code-only-repo": [
    { name: "front", url: "" },
    { name: "timeline", url: "#/timeline" },
    { name: "issues", url: "#/issues" },
    { name: "code", url: "#/code" },
  ],
  "big-tree-repo": [
    { name: "front", url: "" },
    { name: "tree", url: "#file:nodes@main" },
  ],
  "binary-repo": [
    { name: "front", url: "" },
    { name: "image", url: "#file:logo.png@main" },
    { name: "binary", url: "#file:data.bin@main" },
    { name: "lfs", url: "#file:assets/model.bin@main" },
    { name: "submodule", url: "#file:vendor/lib@main" },
    { name: "symlink", url: "#file:LINK.md@main" },
  ],
};

// buildFixtures runs shapes.sh and returns the served root and the frozen clock.
function buildFixtures() {
  const home = path.join(__dirname, ".shapes");
  const res = spawnSync("bash", [path.join(__dirname, "shapes.sh"), home], { stdio: "inherit" });
  if (res.status !== 0) throw new Error("shapes.sh failed");
  return { served: path.join(home, "served"), now: fs.readFileSync(path.join(home, "now"), "utf8").trim() };
}

// capture runs one headless Chrome and writes one screenshot.
function capture(bin, url, width, height, theme, out) {
  const args = ["--headless", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
    "--window-size=" + width + "," + height, "--virtual-time-budget=" + BUDGET,
    "--blink-settings=preferredColorScheme=" + THEMES[theme],
    "--screenshot=" + out, url];
  execFileSync(bin, args, { stdio: ["ignore", "ignore", "ignore"], timeout: 120000, env: Object.assign({}, process.env, { TZ: "UTC" }) });
}

// startServer launches serve.js over root and resolves once it prints its port.
function startServer(root) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(__dirname, "serve.js"), root, "0"], { stdio: ["ignore", "pipe", "inherit"] });
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

// shotsFor lists every screenshot one fixture route produces.
function shotsFor(fixture, route, origin, now) {
  const hash = route.url.startsWith("#") ? route.url : "";
  const key = route.url.startsWith("#") ? "" : route.url;
  const url = origin + "/" + fixture + "/" + key + "?now=" + now + (route.nojs ? "&nojs=1" : "") + hash;
  const shots = [];
  for (const [width, height] of Object.entries(WIDTHS)) {
    for (const theme of Object.keys(THEMES)) {
      shots.push({ name: fixture + "-" + route.name + "-" + width + "-" + theme + ".png", url, width: Number(width), height, theme });
    }
  }
  return shots;
}

async function main() {
  const outDir = path.resolve(process.argv[2] || path.join(__dirname, ".shots"));
  const bin = chrome.find();
  if (!bin) { console.log("no chrome"); process.exit(2); }
  fs.mkdirSync(outDir, { recursive: true });
  const { served, now } = buildFixtures();
  const { child, port } = await startServer(served);
  const origin = "http://127.0.0.1:" + port;
  try {
    for (const [fixture, routes] of Object.entries(ROUTES)) {
      for (const route of routes) {
        for (const s of shotsFor(fixture, route, origin, now)) {
          const out = path.join(outDir, s.name);
          capture(bin, s.url, s.width, s.height, s.theme, out);
          console.log("shot " + s.name + " " + fs.statSync(out).size);
        }
      }
    }
  } finally {
    child.kill();
  }
}

main().catch((e) => { console.error(e.message); process.exit(1); });
