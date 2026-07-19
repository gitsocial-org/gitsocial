// prism-objectivec.js - PrismJS 1.30.0 grammar component, lazily loaded by the static site.
// Official minified component (cdnjs, MIT, github.com/PrismJS/prism v1.30.0):
//   https://cdnjs.cloudflare.com/ajax/libs/prism/1.30.0/components/prism-objectivec.min.js
// Loaded on demand by gs-render.js (ensureGrammar) into window.Prism.languages;
// dependency chains (e.g. cpp->c) are ordered by the reader before this runs.
Prism.languages.objectivec=Prism.languages.extend("c",{string:{pattern:/@?"(?:\\(?:\r\n|[\s\S])|[^"\\\r\n])*"/,greedy:!0},keyword:/\b(?:asm|auto|break|case|char|const|continue|default|do|double|else|enum|extern|float|for|goto|if|in|inline|int|long|register|return|self|short|signed|sizeof|static|struct|super|switch|typedef|typeof|union|unsigned|void|volatile|while)\b|(?:@interface|@end|@implementation|@protocol|@class|@public|@protected|@private|@property|@try|@catch|@finally|@throw|@synthesize|@dynamic|@selector)\b/,operator:/-[->]?|\+\+?|!=?|<<?=?|>>?=?|==?|&&?|\|\|?|[~^%?*\/@]/}),delete Prism.languages.objectivec["class-name"],Prism.languages.objc=Prism.languages.objectivec;
