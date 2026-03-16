// labels.go - Label mapping from external platforms to GitSocial scoped labels
package importpkg

import "strings"

// MapLabel converts an external label to a GitSocial scoped label.
func MapLabel(label, mode string) string {
	switch mode {
	case "raw":
		return label
	case "skip":
		return ""
	default:
		return autoMapLabel(label)
	}
}

// MapLabels converts a slice of external labels.
func MapLabels(labels []string, mode string) []string {
	if mode == "skip" {
		return nil
	}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		mapped := MapLabel(l, mode)
		if mapped != "" {
			out = append(out, mapped)
		}
	}
	return out
}

func autoMapLabel(label string) string {
	normalized := strings.ToLower(strings.TrimSpace(label))
	normalized = strings.ReplaceAll(normalized, " ", "-")
	if strings.Contains(normalized, "/") {
		return normalized
	}
	switch normalized {
	case "bug", "crash", "defect", "regression":
		return "kind/" + normalized
	case "feature", "enhancement":
		return "kind/" + normalized
	case "documentation", "docs":
		return "kind/docs"
	case "good-first-issue", "contributor-friendly", "help-wanted":
		return "priority/" + normalized
	default:
		return "area/" + normalized
	}
}
