// util_colors.go - Centralized color definitions for TUI
package tuicore

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Border states
const (
	BorderFocused   = "34"
	BorderUnfocused = "240"
	BorderError     = "196"
	BorderWarning   = "167"
)

// Status messages
const (
	StatusSuccess = "34"
	StatusWarning = "214"
	StatusError   = "196"
	StatusInfo    = "33"
)

// Text hierarchy
const (
	TextPrimary   = "255"
	TextNormal    = "250"
	TextSecondary = "242"
)

// Backgrounds
const (
	BgSelected = "240"
	BgFooter   = "236"
)

// UI accents
const (
	AccentEmail     = "39"
	AccentHighlight = "226"
	AccentHyperlink = "33"
	AccentImage     = "135"
	AccentPink      = "#F780E2"
)

// Social identity colors
const (
	IdentityMe            = "207"
	IdentityMeMuted       = "133"
	IdentityOwnRepo       = "44"
	IdentityOwnRepoMuted  = "30"
	IdentityMutual        = "220"
	IdentityMutualMuted   = "178"
	IdentityFollowing     = "34"
	IdentityMuted         = "28"
	IdentityAssigned      = "135"
	IdentityAssignedMuted = "97"
)

// Confirmations
const (
	ConfirmDestructive = "196"
	ConfirmAction      = "226"
)

// Form accents (dark theme defaults)
var (
	FormGreen     color.Color = lipgloss.Color("#02BF87")
	FormGreenDark color.Color = lipgloss.Color("#018858")
)

// Diff
const (
	DiffAdded         = "#4ae04a" // soft green
	DiffRemoved       = "#e06c75" // muted red
	DiffAddedBg       = "#1a3524" // subtle dark green
	DiffRemovedBg     = "#3b1a1e" // subtle dark red
	DiffHunkHeader    = "36"      // cyan
	DiffLineNum       = "240"     // dim
	DiffWordAdded     = "46"      // bright green (word-level)
	DiffWordRemoved   = "203"     // bright red (word-level)
	DiffWordAddedBg   = "#2a5a34" // word-level added background
	DiffWordRemovedBg = "#5a2a2e" // word-level removed background
)
