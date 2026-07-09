// shim.js - shared DOM + DOMParser shim for headless route/render tests.
// A minimal, dependency-free document/window/location stand-in that lets the
// browser reader (gs-core + gs-render + gs-app) run under Node. The boot
// location is derived from GS_SITE_ORIGIN + GS_SITE_BUCKET so a suite can point
// the initial route at whichever served bucket the runner started.
const path = require("path");
function mkEl(tag) {
  return {
    nodeType: 1, tagName: (tag || "").toUpperCase(), _attrs: new Map(), _children: [],
    style: {}, _cls: new Set(), _handlers: {}, _parent: null,
    get attributes() { return Array.from(this._attrs, ([name, value]) => ({ name, value })); },
    get childNodes() { return this._children; },
    get children() { return this._children.filter((c) => c && c.nodeType === 1); },
    setAttribute(k, v) { this._attrs.set(k, String(v)); },
    getAttribute(k) { return this._attrs.has(k) ? this._attrs.get(k) : null; },
    hasAttribute(k) { return this._attrs.has(k); },
    removeAttribute(k) { this._attrs.delete(k); },
    set className(v) { this._cls = new Set(String(v).split(/\s+/).filter(Boolean)); },
    get className() { return Array.from(this._cls).join(" "); },
    // Functional classList backed by _cls, so class flips (e.g. the list-card
    // body-clamp toggle) are observable in tests. Fresh facade per access; the
    // arrow closures read this._cls at call time so a later className set (which
    // replaces the Set) stays coherent.
    get classList() {
      const n = this;
      return {
        add(...cs) { for (const c of cs) n._cls.add(c); },
        remove(...cs) { for (const c of cs) n._cls.delete(c); },
        toggle(c) { if (n._cls.has(c)) { n._cls.delete(c); return false; } n._cls.add(c); return true; },
        contains(c) { return n._cls.has(c); },
      };
    },
    append(...cs) { for (const c of cs) { const n = norm(c); if (n && typeof n === "object") n._parent = this; this._children.push(n); } },
    appendChild(c) { const n = norm(c); if (n && typeof n === "object") n._parent = this; this._children.push(n); return n; },
    insertBefore(node, ref) { const n = norm(node); if (n && typeof n === "object") n._parent = this; const i = ref ? this._children.indexOf(ref) : -1; if (i >= 0) this._children.splice(i, 0, n); else this._children.push(n); return n; },
    get nextSibling() { const p = this._parent; if (!p) return null; const i = p._children.indexOf(this); return i >= 0 && i + 1 < p._children.length ? p._children[i + 1] : null; },
    prepend(...cs) { this._children.unshift(...cs.map(norm)); },
    replaceChildren(...cs) { this._children = cs.map(norm); },
    remove() { if (this._parent) { const i = this._parent._children.indexOf(this); if (i >= 0) this._parent._children.splice(i, 1); } },
    cloneNode() { const n = mkEl(this.tagName); n._attrs = new Map(this._attrs); n._children = this._children.map((c) => (c && c.cloneNode ? c.cloneNode(true) : c)); return n; },
    addEventListener(ev, fn) { (this._handlers[ev] = this._handlers[ev] || []).push(fn); },
    removeEventListener() {}, scrollIntoView() {}, focus() { global.__lastFocused = this; },
    get parentNode() { return this._parent; },
    closest(sel) { let n = this; while (n && n.nodeType === 1) { if (matchSel(n, sel)) return n; n = n._parent; } return null; },
    querySelectorAll(sel) { const out = []; collect(this, sel, out); return out; },
    set textContent(v) { this._children = [{ nodeType: 3, nodeValue: String(v) }]; },
    get textContent() { return textOf(this); },
    set innerHTML(v) { this._html = String(v); },
  };
}
function norm(c) { return (typeof c === "string" || typeof c === "number") ? { nodeType: 3, nodeValue: String(c) } : c; }
function textOf(n) { if (n == null) return ""; if (typeof n === "string") return n; if (n.nodeType === 3) return n.nodeValue || ""; let s = ""; for (const c of n._children || []) s += textOf(c); return s; }
function matchSel(node, sel) {
  const m = /^([a-zA-Z0-9]*)(?:\[([^\]=]+)(?:=["']?([^\]"']*)["']?)?\])?$/.exec(sel.trim());
  if (!m) return false;
  if (m[1] && node.tagName.toLowerCase() !== m[1].toLowerCase()) return false;
  if (m[2]) { if (!node.hasAttribute(m[2])) return false; if (m[3] != null && m[3] !== "" && node.getAttribute(m[2]) !== m[3]) return false; }
  return true;
}
function collect(node, sel, out) { for (const c of node._children || []) { if (c && c.nodeType === 1) { if (matchSel(c, sel)) out.push(c); collect(c, sel, out); } } }
function findTag(node, tag, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c.tagName.toLowerCase() === tag) out.push(c); findTag(c, tag, out); } } return out; }

const VOID = new Set(["br", "hr", "img", "source", "col", "input", "wbr", "area", "meta", "link", "base"]);
function parseAttrs(str) {
  const attrs = []; const re = /([a-zA-Z_:][\w:.-]*)(?:\s*=\s*("([^"]*)"|'([^']*)'|[^\s"'>]+))?/g; let m;
  while ((m = re.exec(str)) !== null) attrs.push({ name: m[1], value: m[3] != null ? m[3] : m[4] != null ? m[4] : (m[2] != null ? m[2] : "") });
  return attrs;
}
function parseHTML(html) {
  const root = mkEl("body"); const stack = [root];
  const re = /<!--[\s\S]*?-->|<\/([a-zA-Z][\w-]*)\s*>|<([a-zA-Z][\w-]*)((?:[^<>"']|"[^"]*"|'[^']*')*?)(\/?)>|([^<]+)/g; let m;
  while ((m = re.exec(html)) !== null) {
    if (m[0].startsWith("<!--")) continue;
    const cur = stack[stack.length - 1];
    if (m[1]) { for (let i = stack.length - 1; i > 0; i--) { if (stack[i].tagName.toLowerCase() === m[1].toLowerCase()) { stack.length = i; break; } } }
    else if (m[2]) { const node = mkEl(m[2]); for (const a of parseAttrs(m[3] || "")) node._attrs.set(a.name, a.value); cur._children.push(node); node._parent = cur; if (!m[4] && !VOID.has(m[2].toLowerCase())) stack.push(node); }
    else if (m[5]) cur._children.push({ nodeType: 3, nodeValue: m[5] });
  }
  return root;
}

const viewNode = mkEl("main");
const docHandlers = {};
// Persist nodes by id so elements the app fills over time (e.g. the sidebar
// #nav-tree-slot) survive across getElementById calls, matching the real DOM.
const idNodes = { view: viewNode };
// A live <head> so the app's site-customization inject (accent <style>, favicon
// <link>) lands somewhere querySelector/getElementById can find it, matching the
// real DOM closely enough to assert the applied overrides.
const headNode = mkEl("head");
global.document = {
  createElement: (t) => mkEl(t),
  // SVG elements (the commit graph gutter) go through createElementNS; the shim
  // treats them like any other element node (namespace is irrelevant headless).
  createElementNS: (_ns, t) => mkEl(t),
  createTextNode: (v) => ({ nodeType: 3, nodeValue: String(v) }),
  getElementById: (id) => {
    for (const c of headNode._children) { if (c && c.nodeType === 1 && c.getAttribute("id") === id) return c; }
    return idNodes[id] || (idNodes[id] = mkEl("div"));
  },
  querySelector: (sel) => { const out = []; collect(headNode, sel, out); return out[0] || null; },
  querySelectorAll: (sel) => { const out = []; collect(headNode, sel, out); return out; },
  addEventListener: (ev, fn) => { (docHandlers[ev] = docHandlers[ev] || []).push(fn); },
  removeEventListener: (ev, fn) => { if (docHandlers[ev]) docHandlers[ev] = docHandlers[ev].filter((f) => f !== fn); },
  head: headNode,
  body: mkEl("body"),
};
// Selection stub: the real DOM exposes window.getSelection(); the shim lacks
// it, so tests drive card-nav suppression by mutating global.__selection.
global.__selection = { isCollapsed: true };
global.window = { addEventListener() {}, matchMedia: () => ({ matches: false }), getSelection: () => global.__selection };
// In-memory localStorage so persisted UI state (diffview, theme) is assertable.
global.localStorage = {
  _m: {},
  getItem(k) { return k in this._m ? this._m[k] : null; },
  setItem(k, v) { this._m[k] = String(v); },
  removeItem(k) { delete this._m[k]; },
};
const shimOrigin = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const shimBucket = process.env.GS_SITE_BUCKET || "thread-demo";
global.location = { hash: "#/", pathname: "/" + shimBucket + "/", origin: shimOrigin, search: "" };
global.DOMParser = function () {};
global.DOMParser.prototype.parseFromString = function (html) { return { body: parseHTML(html) }; };
let objCounter = 0;
global.URL.createObjectURL = () => "blob:mock/" + (++objCounter);
global.URL.revokeObjectURL = () => {};
global.Blob = function (parts, opts) { this.type = opts && opts.type; };

global.__shim = { viewNode, textOf, findTag, docHandlers, mkEl, setHash: (h) => { global.location.hash = h; } };

// Load the split reader into the shimmed context, in dependency order: the
// DOM-free core, then the render layer. The app/boot layer is loaded by each
// suite after icons.js (its require of gs-app.js auto-runs init(), so icons.js
// must be in place first). All three share one global GS namespace, so a later
// require of any of them returns the same populated object.
require(path.join(__dirname, "../site/gs-core.js"));
require(path.join(__dirname, "../site/gs-render.js"));
