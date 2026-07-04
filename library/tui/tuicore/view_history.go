// view_history.go - Generic edit/version history view shared by all extensions.
package tuicore

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// ProposalActionFn applies a cross-repo proposal (accept or decline) to the
// given ref and reports the normalized outcome. Injected by the extension view
// so tuicore stays free of the proposals package (which imports extensions).
type ProposalActionFn func(workdir, ref string) (ok bool, errMsg, canonicalRef string)

// HistoryLoadContext carries everything a loader needs to fetch and wrap versions.
type HistoryLoadContext struct {
	Workdir      string
	WorkspaceURL string
	Ref          string
	ShowEmail    bool
	Owned        bool
}

// HistoryExtraKey is an extra keybinding a specific history view adds on top of
// the shared d/A/X set (e.g. the PR view's interdiff and push).
type HistoryExtraKey struct {
	Key     string
	Label   string
	OnPress func(v *HistoryView, state *State) tea.Cmd // handled in Update; nil = not update-handled
	Handler Handler                                    // registry handler for the footer binding; nil = display-only
}

// HistoryConfig parameterizes the shared history view for one extension.
type HistoryConfig struct {
	ParamName  string                                          // route param holding the canonical ref
	Context    Context                                         // footer/binding context
	TitleLabel string                                          // header label ("History", "Version History", ...)
	Load       func(HistoryLoadContext) ([]VersionItem, error) // fetch + wrap versions
	DiffLoc    func(id, fromID, toID string) Location          // nil = no version diff ('d')
	Detail     func(canonicalRef string) Location              // nil = no proposals (no 'A'/'X')
	Accept     ProposalActionFn                                // applies an accepted proposal (set with Detail)
	Decline    ProposalActionFn                                // publishes a decline (set with Detail)
	ExtraKeys  []HistoryExtraKey
}

// HistoryView is the shared version-history picker used by every extension.
type HistoryView struct {
	cfg          HistoryConfig
	picker       *VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
}

// historyLoadedMsg delivers loaded versions, tagged by param so a stale load
// arriving after navigation is ignored by whatever history view is now active.
type historyLoadedMsg struct {
	param string
	items []VersionItem
	err   error
}

// NewHistoryView creates a history view from a config.
func NewHistoryView(workdir string, cfg HistoryConfig) *HistoryView {
	return &HistoryView{
		cfg:          cfg,
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *HistoryView) SetSize(width, height int) {
	v.picker.SetSize(width, height)
}

// Activate loads the history for the ref on the current route.
func (v *HistoryView) Activate(state *State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	ref := state.Router.Location().Param(v.cfg.ParamName)
	if ref == "" {
		return nil
	}
	load := v.cfg.Load
	param := v.cfg.ParamName
	ctx := HistoryLoadContext{
		Workdir:      v.workdir,
		WorkspaceURL: v.workspaceURL,
		Ref:          ref,
		ShowEmail:    v.showEmail,
		Owned:        OwnsCanonical(ref, v.workspaceURL),
	}
	v.picker.SetLoading(true)
	return func() tea.Msg {
		items, err := load(ctx)
		return historyLoadedMsg{param: param, items: items, err: err}
	}
}

// Update handles messages and returns commands.
func (v *HistoryView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if handled, cmd := v.picker.HandleMouse(msg); handled {
			return cmd
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "d" && v.cfg.DiffLoc != nil {
			return OpenHistoryDiff(v.picker, state, v.cfg.ParamName, v.cfg.DiffLoc, 1, nil)
		}
		if v.cfg.Detail != nil {
			switch key {
			case "A":
				return v.runProposal(v.cfg.Accept, false, "accept")
			case "X":
				return v.runProposal(v.cfg.Decline, true, "decline")
			}
		}
		for _, ek := range v.cfg.ExtraKeys {
			if ek.Key == key && ek.OnPress != nil {
				return ek.OnPress(v, state)
			}
		}
		if handled, cmd := v.picker.HandleKey(key); handled {
			return cmd
		}
	case historyLoadedMsg:
		if msg.param != v.cfg.ParamName {
			return nil
		}
		if msg.err != nil {
			v.picker.SetLoading(false)
			return nil
		}
		v.picker.SetItems(msg.items)
	}
	return nil
}

// runProposal applies the selected cross-repo proposed edit via the injected
// action (accept or decline) and reports the result. Accept authors the owner's
// authoritative same-repo mirror; decline publishes a durable decline marker.
func (v *HistoryView) runProposal(action ProposalActionFn, declined bool, verb string) tea.Cmd {
	repoURL, hash, branch, ok := v.selectedRef()
	if !ok {
		return nil
	}
	if repoURL == v.workspaceURL {
		return func() tea.Msg {
			return ProposalAcceptedMsg{Err: fmt.Errorf("select a proposed edit from another repo to %s", verb)}
		}
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch)
	workdir, detail := v.workdir, v.cfg.Detail
	return func() tea.Msg {
		ok, errMsg, canonicalRef := action(workdir, ref)
		if !ok {
			return ProposalAcceptedMsg{Err: fmt.Errorf("%s", errMsg)}
		}
		return ProposalAcceptedMsg{Declined: declined, Location: detail(canonicalRef)}
	}
}

// selectedRef returns the repo/hash/branch of the selected version.
func (v *HistoryView) selectedRef() (repoURL, hash, branch string, ok bool) {
	sel := v.picker.SelectedItem()
	if sel == nil {
		return "", "", "", false
	}
	repoURL, hash, branch = sel.Ref()
	return repoURL, hash, branch, true
}

// acceptInclude force-shows accept/decline in the footer only when the picker
// holds an open cross-repo proposal this workspace owns.
func (v *HistoryView) acceptInclude() map[string]bool {
	for _, it := range v.picker.Items() {
		if it.IsOpenProposal() {
			return map[string]bool{"A": true, "X": true}
		}
	}
	return nil
}

// Render renders the history view to a string.
func (v *HistoryView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	content := v.picker.Render()
	var footer string
	if v.cfg.Detail != nil {
		footer = RenderFooterInclude(state.Registry, v.cfg.Context, nil, v.acceptInclude())
	} else {
		footer = RenderFooter(state.Registry, v.cfg.Context, nil)
	}
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since the history view has no text input.
func (v *HistoryView) IsInputActive() bool {
	return false
}

// Title returns the header title showing the canonical version's author and ref.
func (v *HistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return v.cfg.TitleLabel
	}
	canonical := items[len(items)-1]
	title := v.cfg.TitleLabel + " · " + canonical.AuthorDisplay(v.showEmail)
	title += " · " + FormatFullTime(canonical.GetTimestamp())
	repoURL, hash, branch := canonical.Ref()
	if ref := BuildCommitRef(repoURL, hash, branch, v.workspaceURL); ref != "" {
		title += " · " + ref
	}
	return title
}

// Bindings returns the view's key bindings, derived from the config.
func (v *HistoryView) Bindings() []Binding {
	noop := func(*HandlerContext) (bool, tea.Cmd) { return false, nil }
	var b []Binding
	if v.cfg.DiffLoc != nil {
		b = append(b, Binding{Key: "d", Label: "version diff", Contexts: []Context{v.cfg.Context}, Handler: noop})
	}
	if v.cfg.Detail != nil {
		b = append(b,
			Binding{Key: "A", Label: "accept", Contexts: []Context{v.cfg.Context}, Handler: noop},
			Binding{Key: "X", Label: "decline", Contexts: []Context{v.cfg.Context}, Handler: noop},
		)
	}
	for _, ek := range v.cfg.ExtraKeys {
		handler := ek.Handler
		if handler == nil {
			handler = noop
		}
		b = append(b, Binding{Key: ek.Key, Label: ek.Label, Contexts: []Context{v.cfg.Context}, Handler: handler})
	}
	return b
}
