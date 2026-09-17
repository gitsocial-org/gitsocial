// cdp.js - drive headless Chrome over the DevTools pipe: one browser, a page
// per capture, and a layout viewport of the exact width asked for. Chrome's
// --window-size cannot go under the platform window floor; CDP can.
const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawn } = require("child_process");

// FLAGS are the launch flags every capture shares; the viewport comes from CDP.
const FLAGS = ["--remote-debugging-pipe", "--headless", "--disable-gpu", "--hide-scrollbars",
  "--force-device-scale-factor=1", "--no-first-run", "--no-default-browser-check"];

// launch starts Chrome on the pipe transport and returns its message channel.
function launch(bin, env) {
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "gs-cdp-"));
  const args = FLAGS.concat(["--user-data-dir=" + profile]);
  const child = spawn(bin, args, { stdio: ["ignore", "ignore", "ignore", "pipe", "pipe"], env: env || process.env });
  const browser = { child, profile, id: 0, buf: Buffer.alloc(0), pending: new Map(), handlers: [] };
  child.stdio[4].on("data", (chunk) => receive(browser, chunk));
  return browser;
}

// receive splits the NUL-delimited stream and settles calls or fans out events.
function receive(b, chunk) {
  b.buf = Buffer.concat([b.buf, chunk]);
  let end;
  while ((end = b.buf.indexOf(0)) !== -1) {
    const msg = JSON.parse(b.buf.slice(0, end).toString("utf8"));
    b.buf = b.buf.slice(end + 1);
    const call = msg.id && b.pending.get(msg.id);
    if (call) {
      b.pending.delete(msg.id);
      if (msg.error) call.reject(new Error(call.method + ": " + msg.error.message));
      else call.resolve(msg.result);
      continue;
    }
    for (const handler of b.handlers.slice()) handler(msg);
  }
}

// send issues one command and resolves with its result.
function send(b, method, params, sessionId) {
  return new Promise((resolve, reject) => {
    b.id++;
    b.pending.set(b.id, { resolve, reject, method });
    const msg = { id: b.id, method, params: params || {} };
    if (sessionId) msg.sessionId = sessionId;
    b.child.stdio[3].write(JSON.stringify(msg) + "\0");
  });
}

// once resolves the next time one event arrives on a session.
function once(b, method, sessionId) {
  return new Promise((resolve) => {
    const handler = (msg) => {
      if (msg.method !== method || msg.sessionId !== sessionId) return;
      b.handlers.splice(b.handlers.indexOf(handler), 1);
      resolve(msg.params);
    };
    b.handlers.push(handler);
  });
}

// open creates a page in its own browser context, so storage never crosses captures.
async function open(b, width, height, theme) {
  const { browserContextId } = await send(b, "Target.createBrowserContext");
  const { targetId } = await send(b, "Target.createTarget", { url: "about:blank", browserContextId });
  const { sessionId } = await send(b, "Target.attachToTarget", { targetId, flatten: true });
  await send(b, "Page.enable", {}, sessionId);
  await send(b, "Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false }, sessionId);
  await send(b, "Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: theme }] }, sessionId);
  return { browserContextId, targetId, sessionId };
}

// load navigates the page and runs its virtual clock out, so a capture is deterministic.
async function load(b, page, url, budget) {
  await send(b, "Emulation.setVirtualTimePolicy", { policy: "pause" }, page.sessionId);
  const expired = once(b, "Emulation.virtualTimeBudgetExpired", page.sessionId);
  await send(b, "Page.navigate", { url }, page.sessionId);
  await send(b, "Emulation.setVirtualTimePolicy", { policy: "pauseIfNetworkFetchesPending", budget }, page.sessionId);
  await expired;
}

// screenshot returns the page's viewport as PNG bytes.
async function screenshot(b, page) {
  const res = await send(b, "Page.captureScreenshot", { format: "png" }, page.sessionId);
  return Buffer.from(res.data, "base64");
}

// evaluate runs an expression in the page and returns its value.
async function evaluate(b, page, expression) {
  const res = await send(b, "Runtime.evaluate", { expression, returnByValue: true }, page.sessionId);
  if (res.exceptionDetails) throw new Error(res.exceptionDetails.text);
  return res.result.value;
}

// close disposes the page's context, releasing its storage with it.
function close(b, page) {
  return send(b, "Target.disposeBrowserContext", { browserContextId: page.browserContextId });
}

// quit ends the browser process and removes its profile once it is gone.
function quit(b) {
  return new Promise((resolve) => {
    const done = () => {
      fs.rmSync(b.profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
      resolve();
    };
    b.child.once("exit", done);
    setTimeout(done, 5000).unref();
    b.child.kill();
  });
}

module.exports = { launch, open, load, screenshot, evaluate, close, quit };
