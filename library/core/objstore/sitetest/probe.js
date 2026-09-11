// probe.js - injected by serve.js under ?probe=1: reads computed styles and
// child structure for a fixed selector list, in both themes, onto <html>.
(function () {
  var SELECTORS = [
    ".card", ".card.comment", ".card.board-card", ".card-head", ".card-chips",
    ".meta", ".chip", ".chip.state", ".subject", ".thread", ".thread-head",
    ".fb-card", ".type-glyph", ".empty", ".view-count", ".filter-chip",
  ];
  var PROPS = ["padding", "margin", "fontSize", "color", "backgroundColor", "borderLeftColor", "borderRadius"];

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

  // collect records the DISTINCT variants a selector renders, sorted, rather
  // than whichever element happens to be first. A fixture rebuild reorders
  // lists, so a first-match reading diffs on churn instead of on a change.
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

  // One theme per run: the browser's colour-scheme flag is the only thing that
  // flips the whole palette. Stamping the light class moves the text colour but
  // leaves the derived panel fills dark, so it would bake a theme that exists
  // nowhere.
  function emit() {
    document.documentElement.setAttribute("data-gs-styles", JSON.stringify(collect()));
  }

  if (window.__gsOnFirstView) window.__gsOnFirstView(function () { setTimeout(emit, 400); });
  else addEventListener("load", function () { setTimeout(emit, 2500); });
})();
