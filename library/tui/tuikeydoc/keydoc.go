// keydoc.go - Types and binding collection for keybinding documentation generation
package tuikeydoc

import (
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuipm"
	"github.com/gitsocial-org/gitsocial/tui/tuirelease"
	"github.com/gitsocial-org/gitsocial/tui/tuireview"
	"github.com/gitsocial-org/gitsocial/tui/tuisocial"
)

// KeyDoc represents a single keybinding for documentation.
type KeyDoc struct {
	Key   string
	Label string
}

// ContextDoc represents all keybindings for a specific context.
type ContextDoc struct {
	Context   tuicore.Context
	Title     string
	Component string // "CardList", "SectionList", "VersionPicker", ""
	Keys      []KeyDoc
}

// DomainDoc represents all contexts grouped under one domain.
type DomainDoc struct {
	Domain   string
	Title    string
	Contexts []ContextDoc
}

// docHost is a lightweight ViewHost for binding collection without TUI dependencies.
type docHost struct {
	state *tuicore.State
}

func (h *docHost) AddView(_ string, view tuicore.View) {
	if bp, ok := view.(tuicore.BindingProvider); ok {
		h.state.Registry.RegisterView(bp)
	}
}

func (h *docHost) State() *tuicore.State { return h.state }

// CollectAll bootstraps the registry and collects all keybindings grouped by domain.
func CollectAll() []DomainDoc {
	zone.NewGlobal()
	registry := tuicore.NewRegistry()
	state := &tuicore.State{
		Registry: registry,
	}
	host := &docHost{state: state}
	// Register core views
	host.AddView("/settings", tuicore.NewSettingsView())
	host.AddView("/config", tuicore.NewConfigView())
	host.AddView("/cache", tuicore.NewCacheView())
	host.AddView("/analytics", tuicore.NewAnalyticsView())
	host.AddView("/help", tuicore.NewHelpView())
	host.AddView("/search", tuicore.NewSearchView("", nil, nil))
	host.AddView("/search/help", tuicore.NewSearchHelpView())
	host.AddView("/notifications", tuicore.NewNotificationsView("", nil, nil, nil, nil))
	// Register extension views
	tuisocial.Register(host)
	tuipm.Register(host)
	tuirelease.Register(host)
	tuireview.Register(host)
	// Register global keys last (same order as app.go)
	tuicore.RegisterGlobalKeys(registry)
	// Collect per-context bindings from ViewMeta registration order
	domains := make(map[string]*DomainDoc)
	seen := make(map[tuicore.Context]bool)
	for _, meta := range tuicore.AllViewMetas() {
		if seen[meta.Context] {
			continue
		}
		seen[meta.Context] = true
		bindings := registry.ForContext(meta.Context)
		if len(bindings) == 0 {
			continue
		}
		var keys []KeyDoc
		for _, b := range bindings {
			keys = append(keys, KeyDoc{Key: b.Key, Label: b.Label})
		}
		domain := tuicore.DomainOf(meta.Context)
		ctxDoc := ContextDoc{
			Context:   meta.Context,
			Title:     meta.Title,
			Component: meta.Component,
			Keys:      keys,
		}
		if d, ok := domains[domain]; ok {
			d.Contexts = append(d.Contexts, ctxDoc)
		} else {
			domainTitle := domainTitles[domain]
			if domainTitle == "" {
				domainTitle = domain
			}
			domains[domain] = &DomainDoc{
				Domain:   domain,
				Title:    domainTitle,
				Contexts: []ContextDoc{ctxDoc},
			}
		}
	}
	// Sort by domain order
	var result []DomainDoc
	for _, d := range domainOrder {
		if doc, ok := domains[d]; ok {
			result = append(result, *doc)
		}
	}
	return result
}
