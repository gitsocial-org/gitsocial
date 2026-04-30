// components.go - Component bindings and domain ordering
package tuikeydoc

// CardListKeys defines the shared navigation keys for CardList-based views.
var CardListKeys = []KeyDoc{
	{Key: "j / down", Label: "Move down"},
	{Key: "k / up", Label: "Move up"},
	{Key: "g / home", Label: "Jump to top"},
	{Key: "G / end", Label: "Jump to bottom"},
	{Key: "ctrl+d / pgdown", Label: "Half-page down"},
	{Key: "ctrl+u / pgup", Label: "Half-page up"},
	{Key: "enter", Label: "Open selected"},
	{Key: ";", Label: "Next link"},
	{Key: ",", Label: "Previous link"},
}

// SectionListKeys defines the shared navigation keys for SectionList-based views.
var SectionListKeys = []KeyDoc{
	{Key: "j / down", Label: "Move down"},
	{Key: "k / up", Label: "Move up"},
	{Key: "g / home", Label: "Jump to top"},
	{Key: "G / end", Label: "Jump to bottom"},
	{Key: "ctrl+d / pgdown", Label: "Half-page down"},
	{Key: "ctrl+u / pgup", Label: "Half-page up"},
	{Key: "enter", Label: "Activate selected item or link"},
	{Key: ";", Label: "Next link"},
	{Key: ",", Label: "Previous link"},
	{Key: "/", Label: "Start inline search"},
}

// VersionPickerKeys defines the shared navigation keys for VersionPicker-based views.
var VersionPickerKeys = []KeyDoc{
	{Key: "j / down", Label: "Move down"},
	{Key: "k / up", Label: "Move up"},
	{Key: "g", Label: "Jump to top"},
	{Key: "G", Label: "Jump to bottom"},
	{Key: "home", Label: "Jump to top"},
	{Key: "end", Label: "Jump to bottom"},
	{Key: "ctrl+d / pgdown", Label: "Half-page down"},
	{Key: "ctrl+u / pgup", Label: "Half-page up"},
	{Key: "enter", Label: "Open detail"},
	{Key: "esc / b", Label: "Back (or exit detail)"},
	{Key: "left", Label: "Previous version"},
	{Key: "right", Label: "Next version"},
}

// domainOrder defines the output order of domains.
var domainOrder = []string{"social", "pm", "review", "release", "core"}

// domainTitles maps domain IDs to display titles.
var domainTitles = map[string]string{
	"social":  "Social Extension",
	"pm":      "PM Extension",
	"review":  "Review Extension",
	"release": "Release Extension",
	"core":    "Core Views",
}

// ComponentKeys returns the keydocs for a component type.
func ComponentKeys(component string) []KeyDoc {
	switch component {
	case "CardList":
		return CardListKeys
	case "SectionList":
		return SectionListKeys
	case "VersionPicker":
		return VersionPickerKeys
	}
	return nil
}
