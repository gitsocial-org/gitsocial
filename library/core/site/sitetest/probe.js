// probe.js - injected by serve.js under ?probe=1: reads computed styles and
// child structure for a fixed selector list, in both themes, onto <html>.
(function () {
  var SELECTORS = [
    ".card", ".card.comment", ".card.board-card", ".card-head", ".card-chips",
    ".meta", ".edited", ".chip", ".chip.state", ".subject", ".thread", ".thread-head",
    ".card.feedback", ".type-glyph", ".empty", ".err", ".view-count", ".filter-chip",
    ".detail > .card-head", ".detail > .card-head > h1.subject",
    ".detail > .detail-topbar", ".detail > .detail-topbar > .page-actions",
    ".home-head", ".home-commit", ".home-row", ".home-toggle",
  ];
  var PROPS = ["padding", "margin", "fontSize", "fontFamily", "color", "backgroundColor", "borderLeftColor", "borderRadius"];

  // norm rounds fractional lengths and reduces colour functions, so a Chrome
  // that serialises either differently does not diff a whole baseline.
  function norm(v) {
    return String(v)
      .replace(/-?\d+\.\d+/g, function (n) { return String(Math.round(parseFloat(n) * 100) / 100); })
      .replace(/color\(srgb ([^)]*)\)/g, function (_, body) {
        return "srgb(" + body.trim().split(/\s+/).map(function (p) {
          return p === "/" ? "/" : String(Math.round(parseFloat(p) * 1000) / 1000);
        }).join(" ") + ")";
      })
      .trim();
  }

  // shape records an element's child tags and classes, so a wrapper appearing
  // or vanishing is visible where computed styles cannot see it.
  function shape(el) {
    return Array.prototype.map.call(el.children, function (c) {
      var cls = String(c.className || "").trim().split(/\s+/).filter(function (n) {
        // tg-* names the item TYPE, and which types land in a list depends on
        // fixture timestamps. The tint those classes carry still shows up as a
        // colour on its own variant, so dropping the name loses no coverage.
        return n && !/^tg-/.test(n);
      }).sort();
      return c.tagName.toLowerCase() + (cls.length ? "." + cls.join(".") : "");
    }).join(" > ");
  }

  // collect records the distinct variants a selector renders, not whichever
  // element is first: a fixture rebuild reorders lists.
  function collect() {
    var out = {};
    for (var i = 0; i < SELECTORS.length; i++) {
      var sel = SELECTORS[i];
      var els = Array.prototype.slice.call(document.querySelectorAll(sel), 0, 40);
      if (!els.length) continue;
      var seen = {};
      for (var e = 0; e < els.length; e++) {
        var cs = getComputedStyle(els[e]);
        var rec = { shape: shape(els[e]) };
        for (var p = 0; p < PROPS.length; p++) rec[PROPS[p]] = norm(cs[PROPS[p]]);
        seen[JSON.stringify(rec)] = rec;
      }
      out[sel] = Object.keys(seen).sort().map(function (k) { return seen[k]; });
    }
    return out;
  }

  // One theme per run: only the browser's colour-scheme flag flips every
  // palette token. Stamping the light class moves text but leaves panel fills dark.
  function emit() {
    document.documentElement.setAttribute("data-gs-styles", JSON.stringify(collect()));
  }

  // QUIET is how still the DOM must be before a sample; CAP bounds the wait.
  var QUIET = 400, CAP = 6000;

  // emitSettled samples once the DOM stops changing: a route's deferred
  // sections, the front page's activity among them, land after the first view,
  // and a fixed timer races them.
  function emitSettled() {
    var last = Date.now(), start = last;
    var obs = new MutationObserver(function () { last = Date.now(); });
    obs.observe(document.documentElement, { childList: true, subtree: true });
    (function tick() {
      var now = Date.now();
      if (now - last < QUIET && now - start < CAP) { setTimeout(tick, 50); return; }
      obs.disconnect();
      emit();
    })();
  }

  if (window.__gsOnFirstView) window.__gsOnFirstView(emitSettled);
  else addEventListener("load", emitSettled);
})();
