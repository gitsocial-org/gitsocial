// shellrev.js - the versioned shell directory this checkout's assets hash to,
// mirroring the Go siteVersion hash; a drift fails every asset fetch.
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const assets = path.join(__dirname, "../assets");
const names = [];
(function walk(dir, rel) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    if (e.name.startsWith(".") || e.name.startsWith("_")) continue; // go:embed skips these
    const r = rel ? rel + "/" + e.name : e.name;
    if (e.isDirectory()) walk(path.join(dir, e.name), r);
    else names.push(r);
  }
})(assets, "");
names.sort();
const h = crypto.createHash("sha256");
for (const name of names) {
  const data = fs.readFileSync(path.join(assets, name));
  h.update(name + " " + data.length + "\n");
  h.update(data);
}
module.exports = ".gitsocial/site/shell/" + h.digest("hex").slice(0, 12) + "/";
