// util_colors.go - Centralized color definitions for TUI
package tuicore

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// DarkBackground selects which variant every adaptive color resolves to. It
// defaults to dark; the TUI overrides it at startup from the display.theme
// setting via SetDarkBackground. Read lazily by adaptiveColor.RGBA at render
// time, so the package-level styles in util_render.go need no rebuild.
var DarkBackground = true

// adaptiveColor resolves to its dark or light variant based on DarkBackground.
// The raw strings are retained for ThemeString (the string-based diff pipeline).
type adaptiveColor struct {
	dark, light       color.Color
	darkStr, lightStr string
}

// RGBA implements color.Color, picking the variant for the current background.
func (c adaptiveColor) RGBA() (r, g, b, a uint32) {
	if DarkBackground {
		return c.dark.RGBA()
	}
	return c.light.RGBA()
}

// adaptive builds a background-aware color from dark and light color strings
// (ANSI index like "240" or hex like "#15291c").
func adaptive(dark, light string) color.Color {
	return adaptiveColor{lipgloss.Color(dark), lipgloss.Color(light), dark, light}
}

// ThemeString returns the ANSI/hex color string c resolves to for the current
// background, for the string-based diff Cell pipeline. Returns "" for colors
// not built via adaptive().
func ThemeString(c color.Color) string {
	a, ok := c.(adaptiveColor)
	if !ok {
		return ""
	}
	if DarkBackground {
		return a.darkStr
	}
	return a.lightStr
}

// pickThemeColor returns the dark or light color string for the current
// background — the string form the glamour/chroma/ANSI renderers need.
func pickThemeColor(dark, light string) string {
	if DarkBackground {
		return dark
	}
	return light
}

// Border states
var (
	BorderFocused   = adaptive("34", "28")
	BorderUnfocused = adaptive("240", "250")
	BorderError     = adaptive("196", "160")
	BorderWarning   = adaptive("167", "130")
)

// Status messages
var (
	StatusSuccess = adaptive("34", "28")
	StatusWarning = adaptive("214", "130")
	StatusError   = adaptive("196", "160")
	StatusInfo    = adaptive("33", "26")
)

// Canonical gray tokens — the only grays in the palette. Every gray role (text
// tiers + surfaces) derives from one of these (dark, light) pairs, reused both
// as adaptiveColor vars and as strings by the glamour/chroma/ANSI renderers, so
// there are no incidental near-duplicate shades.
const (
	grayPrimaryDark    = "255" // brightest text (darkest on light)
	grayPrimaryLight   = "236"
	grayNormalDark     = "250" // default body text
	grayNormalLight    = "238"
	graySecondaryDark  = "242" // dim: labels, meta, muted, line numbers
	graySecondaryLight = "244"

	graySelectedDark  = "240" // row-selection / focus-highlight background
	graySelectedLight = "252"
	grayFooterDark    = "236" // footer / status-bar background
	grayFooterLight   = "253"
)

// Text hierarchy
var (
	TextPrimary   = adaptive(grayPrimaryDark, grayPrimaryLight)
	TextNormal    = adaptive(grayNormalDark, grayNormalLight)
	TextSecondary = adaptive(graySecondaryDark, graySecondaryLight)
)

// Backgrounds
var (
	BgSelected = adaptive(graySelectedDark, graySelectedLight)
	BgFooter   = adaptive(grayFooterDark, grayFooterLight)
)

// UI accents. AccentHyperlink and AccentImage are also emitted as raw ANSI-256
// escapes (OSC 8 links bypass lipgloss), so their index strings are named here
// and resolved per-theme via pickThemeColor at those sites.
const (
	accentHyperlinkDark  = "33"
	accentHyperlinkLight = "26"
	accentImageDark      = "135"
	accentImageLight     = "97"
)

var (
	AccentEmail     = adaptive("39", "31")
	AccentHighlight = adaptive("226", "220")
	AccentHyperlink = adaptive(accentHyperlinkDark, accentHyperlinkLight)
	AccentImage     = adaptive(accentImageDark, accentImageLight)
	AccentPink      = adaptive("#F780E2", "#C724A8")
)

// Social identity colors — hue carries meaning (own items, mutual follows,
// etc.), so each keeps its hue across themes and only shifts luminance so the
// distinction survives on a light background.
var (
	IdentityMe            = adaptive("207", "163")
	IdentityMeMuted       = adaptive("133", "132")
	IdentityOwnRepo       = adaptive("44", "30")
	IdentityOwnRepoMuted  = adaptive("30", "24")
	IdentityMutual        = adaptive("220", "136")
	IdentityMutualMuted   = adaptive("178", "136")
	IdentityFollowing     = adaptive("34", "28")
	IdentityMuted         = adaptive("28", "65")
	IdentityAssigned      = adaptive("135", "97")
	IdentityAssignedMuted = adaptive("97", "60")
)

// Confirmations
var (
	ConfirmDestructive = adaptive("196", "160")
	ConfirmAction      = adaptive("226", "130")
)

// Form accents
var (
	FormGreen     color.Color = adaptive("#02BF87", "#017a56")
	FormGreenDark color.Color = adaptive("#018858", "#015f3d")
)

// Diff colors. The unexported dark/light string pairs feed both the adaptive
// vars below (general FG styling) and DefaultDiffPalette, whose Cell pipeline
// is string-based — referencing the same consts keeps the two in sync.
const (
	diffAddedDark    = "#4ae04a" // soft green
	diffAddedLight   = "#1a7f1a"
	diffRemovedDark  = "#e06c75" // muted red
	diffRemovedLight = "#b02a37"

	diffAddedBgDark    = "#15291c" // subtle green tint behind added lines
	diffAddedBgLight   = "#e6ffed"
	diffRemovedBgDark  = "#3b1a1e" // subtle red tint behind removed lines
	diffRemovedBgLight = "#ffeef0"

	diffIntraAddedBgDark    = "#2a5a34" // brighter tint for changed words on added lines
	diffIntraAddedBgLight   = "#acf2bd"
	diffIntraRemovedBgDark  = "#6a2a2e" // brighter tint for changed words on removed lines
	diffIntraRemovedBgLight = "#fdb8c0"

	diffFileHeaderBgDark  = "#1a2438" // subtle blue behind the file header
	diffFileHeaderBgLight = "#f1f8ff"

	diffHunkHeaderDark  = "36" // cyan
	diffHunkHeaderLight = "30"

	// muted gray for comments, brighter than chroma's default so they stay
	// legible on the diff line backgrounds (kept distinct for that reason).
	diffCommentDark  = "#a8a39c"
	diffCommentLight = "#6a737d"
)

var (
	DiffAdded      = adaptive(diffAddedDark, diffAddedLight)
	DiffRemoved    = adaptive(diffRemovedDark, diffRemovedLight)
	DiffHunkHeader = adaptive(diffHunkHeaderDark, diffHunkHeaderLight)
	DiffLineNum    = adaptive(graySecondaryDark, graySecondaryLight)
)

// Renderer grays that can't reuse the adaptiveColor tiers directly: chroma
// needs hex strings, so the canonical ANSI tokens above aren't usable here.
const (
	// Dimmed syntax for stale/retracted code: one flat gray so dimmed code
	// reads as uniformly de-emphasized.
	grayDimDark  = "#808080"
	grayDimLight = "#9e9e9e"
)
