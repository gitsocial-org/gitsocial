// view_release_sbom.go - Dedicated SBOM package list view for a release
package tuirelease

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ReleaseSBOMView displays SBOM package details for a release.
type ReleaseSBOMView struct {
	workdir     string
	width       int
	height      int
	loaded      bool
	rel         *release.Release
	sbomSummary *release.SBOMSummary
	sectionList *tuicore.SectionList
}

// NewReleaseSBOMView creates a new SBOM view.
func NewReleaseSBOMView(workdir string) *ReleaseSBOMView {
	return &ReleaseSBOMView{
		workdir:     workdir,
		sectionList: tuicore.NewSectionList(),
	}
}

// SetSize sets the view dimensions.
func (v *ReleaseSBOMView) SetSize(w, h int) {
	v.width = w
	v.height = h - 3
	v.sectionList.SetSize(w, h-3)
}

// Activate loads the release and its SBOM.
func (v *ReleaseSBOMView) Activate(state *tuicore.State) tea.Cmd {
	v.loaded = false
	v.rel = nil
	v.sbomSummary = nil
	v.sectionList.SetSections(nil)
	releaseID := state.Router.Location().Param("releaseID")
	workdir := v.workdir
	return func() tea.Msg {
		res := release.GetSingleRelease(releaseID)
		if !res.Success {
			return releaseSBOMLoadedMsg{}
		}
		rel := res.Data
		if rel.SBOM == "" || rel.Version == "" {
			return releaseSBOMLoadedMsg{release: &rel}
		}
		repoURL := gitmsg.ResolveRepoURL(workdir)
		summary, _ := release.GetSBOMSummary(workdir, repoURL, rel.Version, rel.SBOM, rel.ArtifactURL)
		return releaseSBOMLoadedMsg{release: &rel, summary: summary}
	}
}

// Deactivate is called when the view is hidden.
func (v *ReleaseSBOMView) Deactivate() {}

// IsInputActive returns true when search input is active.
func (v *ReleaseSBOMView) IsInputActive() bool {
	return v.sectionList.IsInputActive()
}

// Update handles messages.
func (v *ReleaseSBOMView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case releaseSBOMLoadedMsg:
		v.loaded = true
		v.rel = msg.release
		v.sbomSummary = msg.summary
		v.buildSections()
		return nil
	case tea.KeyPressMsg, tea.MouseMsg:
		consumed, cmd := v.sectionList.Update(msg)
		if consumed {
			return cmd
		}
	}
	if v.sectionList.IsInputActive() {
		return v.sectionList.UpdateSearchInput(msg)
	}
	return nil
}

// Render renders the view.
func (v *ReleaseSBOMView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = tuicore.Dim.Render("  Loading SBOM...")
	} else if v.rel == nil {
		content = tuicore.Dim.Render("  Release not found")
	} else if v.sbomSummary == nil {
		content = tuicore.Dim.Render("  No SBOM data available")
	} else {
		content = v.sectionList.View()
	}
	var footer string
	if v.sectionList.IsSearchActive() {
		footer = v.sectionList.SearchFooter(wrapper.ContentWidth())
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ReleaseSBOM, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *ReleaseSBOMView) Title() string {
	if v.rel == nil {
		return "⏏  SBOM"
	}
	return fmt.Sprintf("⏏  SBOM · %s", tuicore.TruncateToWidth(v.rel.Subject, 40))
}

// Bindings returns keybindings for this view.
func (v *ReleaseSBOMView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.ReleaseSBOM}, Handler: noop},
	}
}

func (v *ReleaseSBOMView) buildSections() {
	if v.sbomSummary == nil {
		return
	}
	s := v.sbomSummary
	var sections []tuicore.Section
	// Hero section — SBOM summary
	sections = append(sections, tuicore.Section{
		Items: []tuicore.SectionItem{{
			Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
				return v.renderSummaryCard(s, width, selected)
			},
		}},
	})
	// Packages section
	if len(s.Items) > 0 {
		label := fmt.Sprintf(" Packages (%d)", s.Packages)
		items := make([]tuicore.SectionItem, 0, len(s.Items))
		for _, pkg := range s.Items {
			pkg := pkg
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
					sel := " "
					if selected {
						sel = tuicore.Title.Render("▏")
					}
					nameW := width / 2
					if nameW > 40 {
						nameW = 40
					}
					verW := 12
					name := pkg.Name
					if len(name) > nameW {
						name = name[:nameW-1] + "…"
					}
					line := fmt.Sprintf("%-*s  %-*s  %s", nameW, name, verW, pkg.Version, pkg.License)
					if searchQuery != "" {
						line = tuicore.HighlightInText(line, searchQuery)
					}
					return []string{sel + tuicore.Dim.Render(line)}
				},
				SearchText: func() string { return pkg.Name + " " + pkg.License },
			})
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
	v.sectionList.SetSections(sections)
}

func (v *ReleaseSBOMView) renderSummaryCard(s *release.SBOMSummary, _ int, selected bool) []string {
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	styles := tuicore.RowStylesWithWidths(14, 0)
	var lines []string
	lines = append(lines, selectionBar+styles.Label.Render("Format")+styles.Value.Render(s.Format))
	lines = append(lines, selectionBar+styles.Label.Render("Packages")+styles.Value.Render(fmt.Sprintf("%d", s.Packages)))
	if s.Generator != "" {
		lines = append(lines, selectionBar+styles.Label.Render("Generator")+styles.Value.Render(s.Generator))
	}
	if s.Generated != "" {
		lines = append(lines, selectionBar+styles.Label.Render("Generated")+styles.Value.Render(s.Generated))
	}
	if len(s.Licenses) > 0 {
		entries := release.SortedLicenses(s.Licenses)
		lines = append(lines, selectionBar+styles.Label.Render("Licenses")+styles.Value.Render(fmt.Sprintf("%s (%d)", entries[0].Name, entries[0].Count)))
		for _, e := range entries[1:] {
			lines = append(lines, selectionBar+styles.Label.Render("")+styles.Value.Render(fmt.Sprintf("%s (%d)", e.Name, e.Count)))
		}
	}
	return lines
}

type releaseSBOMLoadedMsg struct {
	release *release.Release
	summary *release.SBOMSummary
}
