// site_pages_icons.go - the file-type icons of a page's file rows, from the vendored set the app renders

package site

import (
	"html/template"
	"regexp"
	"strings"
)

// sitePagesIcons is the vendored icon set of the embedded icons.js, by key, each hidden from screen readers as the app clones it.
var sitePagesIcons = sitePagesReadIcons()

// sitePagesIconRe matches one entry of icons.js: its key and its SVG string, with the quotes escaped as JS does.
var sitePagesIconRe = regexp.MustCompile(`(?m)^\s*"([a-z0-9-]+)": "((?:[^"\\]|\\.)*)",?$`)

// sitePagesReadIcons reads the embedded icons.js; a set with no folder panics at init, like the favicon read.
func sitePagesReadIcons() map[string]template.HTML {
	data, err := siteFiles.ReadFile("assets/icons.js")
	if err != nil {
		panic("site: read embedded assets/icons.js for the pages' file icons: " + err.Error())
	}
	icons := map[string]template.HTML{}
	for _, m := range sitePagesIconRe.FindAllStringSubmatch(string(data), -1) {
		svg := strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[2])
		icons[m[1]] = template.HTML(strings.Replace(svg, ">", ` aria-hidden="true">`, 1))
	}
	if _, ok := icons["folder"]; !ok {
		panic("site: no folder icon in the embedded assets/icons.js: the generated pages have no file icons")
	}
	return icons
}

// siteIconFilenames maps an exact lowercase file name to its icon key, checked before the extension; the copy of ICON_FILENAMES in gs-core.js.
var siteIconFilenames = map[string]string{
	"dockerfile": "docker", "docker-compose.yml": "docker", "docker-compose.yaml": "docker",
	"makefile": "gear", "gnumakefile": "gear",
	".gitignore": "git", ".gitattributes": "git", ".gitmodules": "git", ".gitkeep": "git",
	"package.json": "npm", "package-lock.json": "npm", ".npmrc": "npm",
	"claude.md": "claude", "license": "text", "license.md": "text",
	"go.mod": "go", "go.sum": "go",
}

// siteIconExt maps a lowercase extension to its icon key; the copy of ICON_EXT in gs-core.js.
var siteIconExt = map[string]string{
	"go": "go", "js": "js", "mjs": "js", "cjs": "js", "jsx": "js",
	"ts": "ts", "mts": "ts", "cts": "ts", "tsx": "ts",
	"json": "json", "jsonc": "json", "json5": "json",
	"yaml": "yaml", "yml": "yaml", "toml": "gear", "ini": "gear", "cfg": "gear", "conf": "gear", "env": "gear",
	"md": "md", "markdown": "md", "mdx": "md",
	"html": "html", "htm": "html", "xhtml": "html",
	"css": "css", "scss": "css", "sass": "css", "less": "css",
	"sh": "sh", "bash": "sh", "zsh": "sh", "fish": "sh",
	"py": "py", "pyi": "py", "pyw": "py",
	"rs": "rust", "rb": "ruby", "erb": "ruby",
	"c": "c", "h": "c", "cpp": "c", "cc": "c", "cxx": "c", "hpp": "c", "hh": "c",
	"swift": "swift", "kt": "code", "kts": "code", "java": "code", "xml": "code",
	"php": "code", "cs": "code", "lua": "code", "proto": "code",
	"sql": "sql", "db": "sql", "sqlite": "sql",
	"svg": "svg",
	"png": "image", "jpg": "image", "jpeg": "image", "gif": "image", "webp": "image", "ico": "image", "bmp": "image",
	"zip": "zip", "tar": "zip", "gz": "zip", "tgz": "zip", "bz2": "zip", "xz": "zip", "7z": "zip", "rar": "zip", "jar": "zip",
	"txt": "text", "log": "text", "rst": "text",
	"lock": "gear",
	"ttf":  "font", "otf": "font", "woff": "font", "woff2": "font", "eot": "font",
}

// siteIconColor maps an icon key to its hue class; a key with no entry stays muted. The copy of ICON_COLOR in gs-core.js.
var siteIconColor = map[string]string{
	"go": "i-cyan", "ts": "i-cyan",
	"js":   "i-yellow",
	"html": "i-orange", "swift": "i-orange", "rust": "i-orange", "claude": "i-orange",
	"css": "i-indigo",
	"sh":  "i-green",
	"py":  "i-blue", "c": "i-blue", "docker": "i-blue",
	"yaml": "i-red", "ruby": "i-red", "npm": "i-red",
	"git": "i-vermilion",
}

// siteIconName maps a file name, or a structural kind, to an icon key, as iconName does in gs-core.js.
func siteIconName(name, kind string) string {
	switch kind {
	case "tree":
		return "folder"
	case "tree-open":
		return "folder-open"
	case "commit":
		return "git"
	case "symlink":
		return "symlink"
	}
	lower := strings.ToLower(name[strings.LastIndex(name, "/")+1:])
	if key, ok := siteIconFilenames[lower]; ok {
		return key
	}
	if dot := strings.LastIndex(lower, "."); dot >= 0 {
		if key, ok := siteIconExt[lower[dot+1:]]; ok {
			return key
		}
	}
	return "file"
}

// siteTreeKind maps a tree entry mode to the kind iconName takes: tree, commit, symlink, or "" for a blob.
func siteTreeKind(mode string) string {
	switch mode {
	case siteTreeDirMode:
		return "tree"
	case "160000":
		return "commit"
	case "120000":
		return "symlink"
	}
	return ""
}

// siteTreeIcon renders the icon span of a file row, as treeIcon does in gs-render.js.
func siteTreeIcon(name, kind string) template.HTML {
	key := siteIconName(name, kind)
	class := "gs-icon tree-icon"
	if color := siteIconColor[key]; color != "" {
		class += " " + color
	}
	return template.HTML(`<span class="` + class + `">` + string(sitePagesIcons[key]) + `</span>`)
}
