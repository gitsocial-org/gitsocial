// prism-matlab.js - PrismJS 1.30.0 grammar component, lazily loaded by the static site.
// Official minified component (cdnjs, MIT, github.com/PrismJS/prism v1.30.0):
//   https://cdnjs.cloudflare.com/ajax/libs/prism/1.30.0/components/prism-matlab.min.js
// Loaded on demand by gs-render.js (ensureGrammar) into window.Prism.languages;
// dependency chains (e.g. cpp->c) are ordered by the reader before this runs.
Prism.languages.matlab={comment:[/%\{[\s\S]*?\}%/,/%.+/],string:{pattern:/\B'(?:''|[^'\r\n])*'/,greedy:!0},number:/(?:\b\d+(?:\.\d*)?|\B\.\d+)(?:[eE][+-]?\d+)?(?:[ij])?|\b[ij]\b/,keyword:/\b(?:NaN|break|case|catch|continue|else|elseif|end|for|function|if|inf|otherwise|parfor|pause|pi|return|switch|try|while)\b/,function:/\b(?!\d)\w+(?=\s*\()/,operator:/\.?[*^\/\\']|[+\-:@]|[<>=~]=?|&&?|\|\|?/,punctuation:/\.{3}|[.,;\[\](){}!]/};
