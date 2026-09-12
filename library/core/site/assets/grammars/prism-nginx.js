// prism-nginx.js - PrismJS 1.30.0 grammar component, lazily loaded by the static site.
// Official minified component (cdnjs, MIT, github.com/PrismJS/prism v1.30.0):
//   https://cdnjs.cloudflare.com/ajax/libs/prism/1.30.0/components/prism-nginx.min.js
// Loaded on demand by gs-render.js (ensureGrammar) into window.Prism.languages;
// dependency chains (e.g. cpp->c) are ordered by the reader before this runs.
!function(e){var n=/\$(?:\w[a-z\d]*(?:_[^\x00-\x1F\s"'\\()$]*)?|\{[^}\s"'\\]+\})/i;e.languages.nginx={comment:{pattern:/(^|[\s{};])#.*/,lookbehind:!0,greedy:!0},directive:{pattern:/(^|\s)\w(?:[^;{}"'\\\s]|\\.|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\s+(?:#.*(?!.)|(?![#\s])))*?(?=\s*[;{])/,lookbehind:!0,greedy:!0,inside:{string:{pattern:/((?:^|[^\\])(?:\\\\)*)(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')/,lookbehind:!0,greedy:!0,inside:{escape:{pattern:/\\["'\\nrt]/,alias:"entity"},variable:n}},comment:{pattern:/(\s)#.*/,lookbehind:!0,greedy:!0},keyword:{pattern:/^\S+/,greedy:!0},boolean:{pattern:/(\s)(?:off|on)(?!\S)/,lookbehind:!0},number:{pattern:/(\s)\d+[a-z]*(?!\S)/i,lookbehind:!0},variable:n}},punctuation:/[{};]/}}(Prism);
