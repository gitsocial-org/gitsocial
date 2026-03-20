// view_release_detail.go - Single release detail view with comments
package tuirelease

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuisocial"
)

// ReleaseDetailView displays a single release with comments.
type ReleaseDetailView struct {
	workdir       string
	width         int
	height        int
	loaded        bool
	rel           *release.Release
	comments      []social.Post
	sbomSummary   *release.SBOMSummary
	sbomLoading   bool
	artifactInfos map[string]release.ArtifactInfo
	userEmail     string
	showEmail     bool
	workspaceURL  string
	focusID       string
	showRaw       bool
	confirm       tuicore.ConfirmDialog
	sectionList   *tuicore.SectionList
	sourceIndex   int
	sourceTotal   int
}

// NewReleaseDetailView creates a new release detail view.
func NewReleaseDetailView(workdir string) *ReleaseDetailView {
	return &ReleaseDetailView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		sectionList:  tuicore.NewSectionList(),
	}
}

// SetSize sets the view dimensions.
func (v *ReleaseDetailView) SetSize(w, h int) {
	v.width = w
	v.height = h - 3
	v.sectionList.SetSize(w, h-3)
}

// Activate loads the release.
func (v *ReleaseDetailView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.loaded = false
	v.confirm.Reset()
	v.rel = nil
	v.comments = nil
	v.sbomSummary = nil
	v.sbomLoading = false
	v.artifactInfos = nil
	v.sectionList.SetSections(nil)
	if state.DetailSource != nil {
		v.sourceIndex = state.DetailSource.Index
		v.sourceTotal = state.DetailSource.Total
		if state.DetailSource.SearchQuery != "" {
			v.sectionList.SetHighlightQuery(tuicore.ExtractSearchTerms(state.DetailSource.SearchQuery))
		}
	} else {
		v.sourceIndex = 0
		v.sourceTotal = 0
		v.sectionList.SetHighlightQuery("")
	}
	releaseID := state.Router.Location().Param("releaseID")
	v.focusID = state.Router.Location().Param("focusID")
	workdir := v.workdir
	return func() tea.Msg {
		if err := release.SyncWorkspaceToCache(workdir); err != nil {
			log.Debug("release sync before detail load failed", "error", err)
		}
		res := release.GetSingleRelease(releaseID)
		if !res.Success {
			return releaseDetailLoadedMsg{}
		}
		rel := res.Data
		branch := gitmsg.GetExtBranch(workdir, "release")
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		ref := protocol.ParseRef(rel.ID)
		if _, ok := unpushed[ref.Value]; ok {
			rel.IsUnpushed = true
		}
		var comments []social.Post
		commentsRes := release.GetReleaseComments(rel.ID, "")
		if commentsRes.Success {
			comments = commentsRes.Data
		}
		return releaseDetailLoadedMsg{release: &rel, comments: comments}
	}
}

// Deactivate is called when the view is hidden.
func (v *ReleaseDetailView) Deactivate() {}

// Update handles messages.
func (v *ReleaseDetailView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case releaseDetailLoadedMsg:
		v.loaded = true
		v.rel = msg.release
		for i := range msg.comments {
			if msg.comments[i].Repository == v.workspaceURL {
				msg.comments[i].Display.IsWorkspacePost = true
			}
		}
		v.comments = msg.comments
		v.buildSections()
		if v.focusID != "" {
			for i, c := range v.comments {
				if c.ID == v.focusID {
					v.sectionList.SetSelected(1 + i)
					break
				}
			}
			v.focusID = ""
		}
		var cmds []tea.Cmd
		if v.rel != nil && v.rel.SBOM != "" && v.rel.Version != "" {
			v.sbomLoading = true
			rel := v.rel
			workdir := v.workdir
			cmds = append(cmds, func() tea.Msg {
				repoURL := gitmsg.ResolveRepoURL(workdir)
				s, _ := release.GetSBOMSummary(workdir, repoURL, rel.Version, rel.SBOM, rel.ArtifactURL)
				return releaseDetailSBOMMsg{summary: s}
			})
		}
		if v.rel != nil && len(v.rel.Artifacts) > 0 && v.rel.ArtifactURL == "" && v.rel.Version != "" {
			rel := v.rel
			workdir := v.workdir
			cmds = append(cmds, func() tea.Msg {
				res := release.ListArtifacts(workdir, rel.Version)
				if !res.Success {
					return releaseDetailArtifactInfoMsg{}
				}
				return releaseDetailArtifactInfoMsg{infos: res.Data}
			})
		}
		return tea.Batch(cmds...)
	case releaseDetailSBOMMsg:
		v.sbomLoading = false
		v.sbomSummary = msg.summary
		v.buildSections()
		return nil
	case releaseDetailArtifactInfoMsg:
		if len(msg.infos) > 0 {
			v.artifactInfos = make(map[string]release.ArtifactInfo, len(msg.infos))
			for _, info := range msg.infos {
				v.artifactInfos[info.Filename] = info
			}
			v.buildSections()
		}
		return nil
	case tea.KeyPressMsg, tea.MouseMsg:
		if key, ok := msg.(tea.KeyPressMsg); ok {
			if handled, cmd := v.confirm.HandleKey(key.String()); handled {
				return cmd
			}
		}
		consumed, cmd := v.sectionList.Update(msg)
		if consumed {
			return cmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "left":
				return v.navigateSource(state, -1)
			case "right":
				return v.navigateSource(state, 1)
			case "e":
				if v.rel != nil {
					releaseID := v.rel.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReleaseEdit(releaseID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "c":
				if v.rel != nil {
					releaseID := v.rel.ID
					return func() tea.Msg {
						return tuicore.OpenEditorMsg{
							Mode:     "comment",
							TargetID: releaseID,
						}
					}
				}
			case "s":
				if v.rel != nil && v.rel.SBOM != "" {
					releaseID := v.rel.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReleaseSBOM(releaseID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "X":
				if v.rel != nil {
					v.confirm.Show("Retract this release?", false, func() tea.Cmd { return v.doRetract() })
					return nil
				}
			}
		}
	}
	if v.sectionList.IsInputActive() {
		return v.sectionList.UpdateSearchInput(msg)
	}
	return nil
}

// navigateSource navigates to adjacent items in the source list.
func (v *ReleaseDetailView) navigateSource(state *tuicore.State, offset int) tea.Cmd {
	if state.DetailSource == nil {
		return nil
	}
	return func() tea.Msg {
		return tuicore.SourceNavigateMsg{Offset: offset, MakeLocation: tuicore.LocReleaseDetail}
	}
}

// IsInputActive returns true when confirmation or search input is active.
func (v *ReleaseDetailView) IsInputActive() bool {
	return v.confirm.IsActive() || v.sectionList.IsInputActive()
}

func (v *ReleaseDetailView) buildSections() {
	if v.rel == nil {
		return
	}
	var sections []tuicore.Section
	// Hero section (no label) — the release card
	rel := v.rel
	sections = append(sections, tuicore.Section{
		Items: []tuicore.SectionItem{{
			Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
				return v.renderReleaseCard(rel, width, selected, searchQuery, anchors)
			},
			SearchText: func() string { return rel.Subject + " " + rel.Body },
			Links: func() []tuicore.CardLink {
				var links []tuicore.CardLink
				// Order must match anchor registration in renderReleaseCard
				if rel.Origin != nil && rel.Origin.URL != "" {
					links = append(links, tuicore.CardLink{Label: "Source", Location: tuicore.Location{Path: rel.Origin.URL}})
				}
				if rel.ArtifactURL != "" {
					base := strings.TrimRight(rel.ArtifactURL, "/")
					for _, a := range rel.Artifacts {
						links = append(links, tuicore.CardLink{Label: a, Location: tuicore.Location{Path: base + "/" + a}})
					}
					links = append(links, tuicore.CardLink{Label: "Artifact URL", Location: tuicore.Location{Path: rel.ArtifactURL}})
					if rel.Checksums != "" {
						links = append(links, tuicore.CardLink{Label: rel.Checksums, Location: tuicore.Location{Path: base + "/" + rel.Checksums}})
					}
					if rel.SBOM != "" {
						links = append(links, tuicore.CardLink{Label: rel.SBOM, Location: tuicore.Location{Path: base + "/" + rel.SBOM}})
					}
				} else {
					for _, a := range rel.Artifacts {
						links = append(links, tuicore.CardLink{Label: a, Location: tuicore.Location{
							Path:   "/export-artifact",
							Params: map[string]string{"repoURL": rel.Repository, "version": rel.Version, "filename": a},
						}})
					}
					if rel.Checksums != "" && rel.Version != "" {
						links = append(links, tuicore.CardLink{Label: rel.Checksums, Location: tuicore.Location{
							Path:   "/export-artifact",
							Params: map[string]string{"repoURL": rel.Repository, "version": rel.Version, "filename": rel.Checksums},
						}})
					}
					if rel.SBOM != "" && rel.Version != "" {
						links = append(links, tuicore.CardLink{Label: rel.SBOM, Location: tuicore.Location{
							Path:   "/export-artifact",
							Params: map[string]string{"repoURL": rel.Repository, "version": rel.Version, "filename": rel.SBOM},
						}})
					}
				}
				links = append(links, tuicore.ExtractContentLinks(rel.Body, rel.Repository, "")...)
				return links
			},
		}},
	})
	// Comments section
	if len(v.comments) > 0 {
		label := fmt.Sprintf(" Comments (%d)", len(v.comments))
		items := make([]tuicore.SectionItem, 0, len(v.comments))
		for i, comment := range v.comments {
			comment := comment
			isLast := i == len(v.comments)-1
			nextDepth := 0
			if !isLast {
				nextDepth = v.comments[i+1].Depth
			}
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
					lines := tuisocial.RenderCommentCard(comment, width, selected, searchQuery, v.userEmail, v.showEmail, anchors)
					if !isLast {
						lines = append(lines, "", tuicore.RenderItemSeparator(width, nextDepth), "")
					}
					return lines
				},
				SearchText: func() string { return comment.Content },
				Links: func() []tuicore.CardLink {
					card := tuisocial.PostToCardWithOptions(comment, nil, tuisocial.PostToCardOptions{SkipNested: true, UserEmail: v.userEmail, ShowEmail: v.showEmail})
					return card.AllLinks()
				},
				OnActivate: func() tea.Cmd {
					commentID := comment.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocDetail(commentID),
							Action:   tuicore.NavPush,
						}
					}
				},
			})
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
	v.sectionList.SetSections(sections)
}

func (v *ReleaseDetailView) doRetract() tea.Cmd {
	releaseID := v.rel.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := release.RetractRelease(workdir, releaseID)
		if !result.Success {
			return ReleaseRetractedMsg{ID: releaseID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ReleaseRetractedMsg{ID: releaseID}
	}
}

// Render renders the view.
func (v *ReleaseDetailView) Render(state *tuicore.State) string {
	if v.rel != nil && v.rel.IsRetracted {
		state.BorderVariant = "warning"
	}
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = tuicore.Dim.Render("  Loading release...")
	} else if v.rel == nil {
		content = tuicore.Dim.Render("  Release not found")
	} else {
		content = v.sectionList.View()
	}
	var exclude map[string]bool
	var footer string
	if v.sectionList.IsSearchActive() {
		footer = v.sectionList.SearchFooter(wrapper.ContentWidth())
	} else if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		footer = tuicore.RenderFooterWithPosition(state.Registry, tuicore.ReleaseDetail, wrapper.ContentWidth(), v.sourceIndex+1, v.sourceTotal, exclude)
	}
	return wrapper.Render(content, footer)
}

func (v *ReleaseDetailView) renderReleaseCard(rel *release.Release, width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
	var lines []string
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	title := rel.Subject
	if searchQuery != "" {
		title = tuicore.HighlightInText(title, searchQuery)
	}
	lines = append(lines, selectionBar+tuicore.Bold.Render(title))
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	styles := tuicore.RowStylesWithWidths(14, 0)
	if rel.Version != "" {
		lines = append(lines, selectionBar+styles.Label.Render("Version")+styles.Value.Render(rel.Version))
	}
	if rel.Tag != "" {
		lines = append(lines, selectionBar+styles.Label.Render("Tag")+styles.Value.Render(rel.Tag))
	}
	lines = append(lines, tuicore.RenderOriginRows(rel.Origin, styles, selectionBar, anchors, v.showEmail)...)
	if rel.Prerelease {
		lines = append(lines, selectionBar+styles.Label.Render("Pre-release")+styles.Value.Render("yes"))
	}
	if len(rel.Artifacts) > 0 {
		for i, a := range rel.Artifacts {
			label := "Artifacts"
			if i > 0 {
				label = ""
			}
			var val string
			if rel.ArtifactURL != "" {
				url := strings.TrimRight(rel.ArtifactURL, "/") + "/" + a
				val = anchors.MarkLink(a, url, tuicore.Location{Path: url})
			} else {
				loc := tuicore.Location{Path: "/export-artifact", Params: map[string]string{
					"repoURL": rel.Repository, "version": rel.Version, "filename": a,
				}}
				styled := tuicore.LinkStyle(a)
				val = anchors.Mark(styled, loc)
			}
			if info, ok := v.artifactInfos[a]; ok && info.Size > 0 {
				val += tuicore.Dim.Render(fmt.Sprintf(" (%s)", release.FormatSize(info.Size)))
			}
			lines = append(lines, selectionBar+styles.Label.Render(label)+val)
		}
	}
	if rel.ArtifactURL != "" {
		link := anchors.MarkLink(rel.ArtifactURL, rel.ArtifactURL, tuicore.Location{Path: rel.ArtifactURL})
		lines = append(lines, selectionBar+styles.Label.Render("Artifact URL")+link)
	}
	if rel.Checksums != "" {
		var val string
		if rel.ArtifactURL != "" {
			url := strings.TrimRight(rel.ArtifactURL, "/") + "/" + rel.Checksums
			val = anchors.MarkLink(rel.Checksums, url, tuicore.Location{Path: url})
		} else if rel.Version != "" {
			loc := tuicore.Location{Path: "/export-artifact", Params: map[string]string{
				"repoURL": rel.Repository, "version": rel.Version, "filename": rel.Checksums,
			}}
			val = anchors.Mark(tuicore.LinkStyle(rel.Checksums), loc)
		} else {
			val = styles.Value.Render(rel.Checksums)
		}
		lines = append(lines, selectionBar+styles.Label.Render("Checksums")+val)
	}
	if rel.SBOM != "" {
		var sbomLink string
		if rel.ArtifactURL != "" {
			url := strings.TrimRight(rel.ArtifactURL, "/") + "/" + rel.SBOM
			sbomLink = anchors.MarkLink(rel.SBOM, url, tuicore.Location{Path: url})
		} else if rel.Version != "" {
			loc := tuicore.Location{Path: "/export-artifact", Params: map[string]string{
				"repoURL": rel.Repository, "version": rel.Version, "filename": rel.SBOM,
			}}
			sbomLink = anchors.Mark(tuicore.LinkStyle(rel.SBOM), loc)
		} else {
			sbomLink = styles.Value.Render(rel.SBOM)
		}
		if v.sbomSummary != nil {
			s := v.sbomSummary
			lines = append(lines, selectionBar+styles.Label.Render("SBOM")+sbomLink+tuicore.Dim.Render(fmt.Sprintf(" (%s) · %d packages", s.Format, s.Packages)))
			if s.Generator != "" {
				lines = append(lines, selectionBar+styles.Label.Render("")+tuicore.Dim.Render("Generated by "+s.Generator))
			}
			if len(s.Licenses) > 0 {
				entries := release.SortedLicenses(s.Licenses)
				lines = append(lines, selectionBar+styles.Label.Render("")+tuicore.Dim.Render(fmt.Sprintf("Licenses: %d %s", entries[0].Count, entries[0].Name)))
				for _, e := range entries[1:] {
					lines = append(lines, selectionBar+styles.Label.Render("")+tuicore.Dim.Render(fmt.Sprintf("          %d %s", e.Count, e.Name)))
				}
			}
		} else if v.sbomLoading {
			lines = append(lines, selectionBar+styles.Label.Render("SBOM")+tuicore.Dim.Render("Loading..."))
		} else {
			lines = append(lines, selectionBar+styles.Label.Render("SBOM")+sbomLink)
		}
	}
	if rel.SignedBy != "" {
		lines = append(lines, selectionBar+styles.Label.Render("Signed by")+styles.Value.Render(rel.SignedBy))
	}
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	if v.showRaw {
		lines = append(lines, tuicore.RenderCommitMessage(rel.ID, selectionBar, width-3)...)
	} else if rel.Body != "" {
		for _, line := range strings.Split(tuicore.RenderMarkdownWithAnchors(rel.Body, width-3, anchors), "\n") {
			if searchQuery != "" {
				line = tuicore.HighlightInText(line, searchQuery)
			}
			lines = append(lines, selectionBar+line)
		}
	} else {
		lines = append(lines, selectionBar+tuicore.Dim.Render("No description"))
	}
	return lines
}

// ShowRawView toggles between rendered body and full commit message.
func (v *ReleaseDetailView) ShowRawView() tea.Cmd {
	v.showRaw = !v.showRaw
	return func() tea.Msg { return nil }
}

// Title returns the view title.
func (v *ReleaseDetailView) Title() string {
	if v.rel == nil {
		return "⏏  Release"
	}
	rel := v.rel
	id := protocol.FormatShortRef(rel.ID, v.workspaceURL)
	icon := "⏏"
	if rel.IsUnpushed {
		icon += "  ⇡"
	}
	relAuthor := rel.Author.Name
	if v.showEmail && rel.Author.Email != "" {
		relAuthor += " <" + rel.Author.Email + ">"
	}
	if rel.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(rel.Origin, v.showEmail); a != "" {
			relAuthor = a
		}
	}
	relTime := tuicore.FormatTime(rel.Timestamp)
	if rel.Origin != nil && rel.Origin.Time != "" {
		relTime = tuicore.FormatOriginTime(rel.Origin.Time)
	}
	title := tuicore.TruncateToWidth(rel.Subject, 40)
	if rel.Prerelease {
		title += " · pre-release"
	}
	return fmt.Sprintf("%s  %s · %s · %s · %s", icon, title, relAuthor, relTime, id)
}

// Bindings returns keybindings for this view.
func (v *ReleaseDetailView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	lfsPush := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartLFSPush == nil {
			return false, nil
		}
		return true, ctx.StartLFSPush()
	}
	return []tuicore.Binding{
		{Key: "s", Label: "sbom", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: noop},
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: noop},
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: noop},
		{Key: "v", Label: "raw", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: tuicore.RawViewHandler},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: noop},
		{Key: "X", Label: "retract", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: noop},
		{Key: "left", Label: "prev", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: noop},
		{Key: "right", Label: "next", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: push},
		{Key: "L", Label: "push lfs", Contexts: []tuicore.Context{tuicore.ReleaseDetail}, Handler: lfsPush},
	}
}

type releaseDetailLoadedMsg struct {
	release  *release.Release
	comments []social.Post
}

type releaseDetailSBOMMsg struct {
	summary *release.SBOMSummary
}

type releaseDetailArtifactInfoMsg struct {
	infos []release.ArtifactInfo
}
