// prism-r.js - PrismJS 1.30.0 grammar component, lazily loaded by the static site.
// Official minified component (cdnjs, MIT, github.com/PrismJS/prism v1.30.0):
//   https://cdnjs.cloudflare.com/ajax/libs/prism/1.30.0/components/prism-r.min.js
// Loaded on demand by gs-render.js (ensureGrammar) into window.Prism.languages;
// dependency chains (e.g. cpp->c) are ordered by the reader before this runs.
Prism.languages.r={comment:/#.*/,string:{pattern:/(['"])(?:\\.|(?!\1)[^\\\r\n])*\1/,greedy:!0},"percent-operator":{pattern:/%[^%\s]*%/,alias:"operator"},boolean:/\b(?:FALSE|TRUE)\b/,ellipsis:/\.\.(?:\.|\d+)/,number:[/\b(?:Inf|NaN)\b/,/(?:\b0x[\dA-Fa-f]+(?:\.\d*)?|\b\d+(?:\.\d*)?|\B\.\d+)(?:[EePp][+-]?\d+)?[iL]?/],keyword:/\b(?:NA|NA_character_|NA_complex_|NA_integer_|NA_real_|NULL|break|else|for|function|if|in|next|repeat|while)\b/,operator:/->?>?|<(?:=|<?-)?|[>=!]=?|::?|&&?|\|\|?|[+*\/^$@~]/,punctuation:/[(){}\[\],;]/};
