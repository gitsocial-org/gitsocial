// chrome.js - locate a Chrome binary. Never a bare PATH lookup: on macOS the
// binary lives inside the app bundle and is never on PATH.
const fs = require("fs");
const { execFileSync } = require("child_process");

const CANDIDATES = [
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  "/Applications/Chromium.app/Contents/MacOS/Chromium",
  "/opt/homebrew/bin/chromium",
  "/usr/bin/google-chrome",
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
];

// find returns a usable Chrome path, or "" when there is none.
function find() {
  if (process.env.CHROME) return fs.existsSync(process.env.CHROME) ? process.env.CHROME : "";
  for (const c of CANDIDATES) if (fs.existsSync(c)) return c;
  for (const name of ["google-chrome", "chromium", "chromium-browser"]) {
    try { return execFileSync("command", ["-v", name], { shell: true, encoding: "utf8" }).trim(); } catch (_) { /* next */ }
  }
  return "";
}

module.exports = { find };
