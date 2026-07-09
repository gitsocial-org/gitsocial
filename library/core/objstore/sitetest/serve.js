// serve.js - zero-dep static file server for a served bucket root.
// Serves <root> at the given (or ephemeral) port; each repo lives at /<name>/.
// A trailing-slash directory request is answered with its index.html, matching
// how the S3 read surface is browsed. Prints "listening <port>" once bound.
const http = require("http");
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

// cacheControlFor mirrors the upload-time classification (objstore/cache_control.go):
// content-addressed loose objects (objects/<xx>/<38-hex>) and sealed shards of
// either corpus (.gitsocial/site/{bodies,items}/<ext>/shard-<hash>.json,
// content-hashed and written once) are immutable, every other served key
// revalidates. Derived from the URL so the served bucket behaves like a real one
// at 127.0.0.1.
function cacheControlFor(rel) {
  const loose = /(?:^|\/)objects\/[0-9a-fA-F]{2}\/[0-9a-fA-F]{38}$/.test(rel);
  const shard = /\.gitsocial\/site\/(?:bodies|items)\/[^/]+\/shard-[0-9a-f]+\.json$/.test(rel);
  return (loose || shard) ? "public, max-age=31536000, immutable" : "no-cache";
}

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json",
  ".css": "text/css; charset=utf-8",
  ".md": "text/markdown; charset=utf-8",
  ".png": "image/png",
  ".gif": "image/gif",
  ".jpg": "image/jpeg",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};

// createServer builds a static server rooted at an absolute directory.
function createServer(root) {
  return http.createServer((req, res) => {
    let rel = decodeURIComponent(req.url.split("?")[0]);
    if (rel.endsWith("/")) rel += "index.html";
    const file = path.join(root, rel);
    if (!file.startsWith(root)) { res.writeHead(403); res.end(); return; }
    fs.readFile(file, (err, data) => {
      if (err) { res.writeHead(404); res.end("not found"); return; }
      const etag = '"' + crypto.createHash("md5").update(data).digest("hex") + '"';
      const headers = {
        "Content-Type": TYPES[path.extname(file)] || "application/octet-stream",
        "Cache-Control": cacheControlFor(rel.replace(/^\//, "")),
        "ETag": etag,
      };
      // locals3 records a stored object's Content-Encoding in a "<file>.gsenc"
      // sidecar; replay it so brotli artifacts decode transparently in the client.
      let enc = "";
      try { enc = fs.readFileSync(file + ".gsenc", "utf8").trim(); } catch (_) { /* none */ }
      if (enc) headers["Content-Encoding"] = enc;
      // Conditional GET: an unchanged object revalidates to 304, matching a real
      // bucket and exercising the reader's no-cache revalidation path.
      if (req.headers["if-none-match"] === etag) { res.writeHead(304, headers); res.end(); return; }
      res.writeHead(200, headers);
      res.end(data);
    });
  });
}

// Run directly: node serve.js <root> [port]; port 0 (default) picks an ephemeral one.
if (require.main === module) {
  const root = path.resolve(process.argv[2] || ".");
  const port = Number(process.argv[3] || 0);
  const server = createServer(root);
  server.listen(port, "127.0.0.1", () => console.log("listening " + server.address().port));
}

module.exports = { createServer };
