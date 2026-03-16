// util_register.go - Release extension view and message handler registration
package tuirelease

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func init() {
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/release/list", Context: tuicore.ReleaseList, Title: "Releases", Icon: "⏏", NavItemID: "release", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/release/detail", Context: tuicore.ReleaseDetail, Title: "Release Detail", Icon: "⏏", NavItemID: "release", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/release/new", Context: tuicore.ReleaseDetail, Title: "New Release", Icon: "⏏", NavItemID: "release", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/release/edit", Context: tuicore.ReleaseDetail, Title: "Edit Release", Icon: "⏏", NavItemID: "release", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/release/sbom", Context: tuicore.ReleaseSBOM, Title: "SBOM", Icon: "⏏", NavItemID: "release", Component: "SectionList"})
	tuicore.RegisterMessageHandler(handleReleaseMessages)
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "release", Type: "release"},
		func(id string) tuicore.Location { return tuicore.LocReleaseDetail(id) },
	)
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "release", Type: "release"},
		releaseCardRenderer,
	)
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "release", Type: "new-release"},
		releaseNotificationCardRenderer,
	)
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "release", Type: "release"},
		releaseDimmedChecker,
	)
}

// releaseItemData wraps a release with display context for card rendering
type releaseItemData struct {
	Release      release.Release
	ShowEmail    bool
	UserEmail    string
	WorkspaceURL string
}

// releaseCardRenderer renders a release to a Card.
func releaseCardRenderer(data any, resolver tuicore.ItemResolver) tuicore.Card {
	switch d := data.(type) {
	case releaseItemData:
		return ReleaseToCardWithOptions(d.Release, ReleaseToCardOptions{ShowEmail: d.ShowEmail, UserEmail: d.UserEmail, WorkspaceURL: d.WorkspaceURL})
	case release.Release:
		return ReleaseToCard(d)
	}
	return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid release"}}
}

// releaseDimmedChecker checks if a release should be dimmed.
func releaseDimmedChecker(data any) bool {
	switch d := data.(type) {
	case releaseItemData:
		return d.Release.IsRetracted
	case release.Release:
		return d.IsRetracted
	}
	return false
}

// releaseNotificationCardRenderer renders a release notification (new-release) to a Card.
func releaseNotificationCardRenderer(data any, _ tuicore.ItemResolver) tuicore.Card {
	rn, ok := data.(release.ReleaseNotification)
	if !ok {
		return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid notification"}}
	}
	title := rn.Subject
	if rn.Version != "" {
		title = rn.Version + " " + title
	}
	var subtitleParts []tuicore.HeaderPart
	subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(rn.Timestamp)})
	if rn.RepoURL != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: protocol.GetFullDisplayName(rn.RepoURL)})
	}
	if rn.Tag != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: rn.Tag})
	}
	badge := "new release"
	if rn.Prerelease {
		badge = "pre-release"
	}
	return tuicore.Card{
		Header: tuicore.CardHeader{
			Icon:     "⏏",
			Title:    rn.ActorName,
			Subtitle: subtitleParts,
			Badge:    badge,
		},
		Content: tuicore.CardContent{Text: title},
	}
}

// ReleaseToCardOptions configures how a Release is converted to a Card
type ReleaseToCardOptions struct {
	ShowEmail    bool
	UserEmail    string
	WorkspaceURL string
}

// ReleaseToCard converts a Release to a Card for display.
func ReleaseToCard(rel release.Release) tuicore.Card {
	return ReleaseToCardWithOptions(rel, ReleaseToCardOptions{})
}

// ReleaseToCardWithOptions converts a Release to a Card with configuration options.
func ReleaseToCardWithOptions(rel release.Release, opts ReleaseToCardOptions) tuicore.Card {
	icon := "⏏"
	title := rel.Subject
	if rel.Version != "" {
		title = rel.Version + " " + title
	}

	authorName := rel.Author.Name
	if opts.ShowEmail && rel.Author.Email != "" {
		authorName += " <" + rel.Author.Email + ">"
	}
	if rel.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(rel.Origin, opts.ShowEmail); a != "" {
			authorName = a
		}
	}
	var subtitleParts []tuicore.HeaderPart
	if rel.Origin != nil {
		if b := tuicore.FormatOriginBadge(rel.Origin); b != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: b})
		}
	}
	subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: authorName})
	if rel.Origin != nil && rel.Origin.Time != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatOriginTime(rel.Origin.Time)})
	} else if !rel.Timestamp.IsZero() {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(rel.Timestamp)})
	}
	if rel.Tag != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: rel.Tag})
	}
	if rel.Comments > 0 {
		loc := tuicore.LocReleaseDetail(rel.ID)
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{
			Text: fmt.Sprintf("↩ %d", rel.Comments),
			Link: &loc,
		})
	}

	if rel.SBOM != "" {
		sbomBadge := "SBOM"
		if strings.HasSuffix(rel.SBOM, ".spdx.json") || strings.HasSuffix(rel.SBOM, ".spdx") {
			sbomBadge = "SPDX"
		} else if strings.HasSuffix(rel.SBOM, ".cdx.json") || strings.HasSuffix(rel.SBOM, ".cdx.xml") || strings.HasSuffix(rel.SBOM, ".cdx") {
			sbomBadge = "CDX"
		}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: sbomBadge})
	}
	relOriginURL := ""
	if rel.Origin != nil {
		relOriginURL = rel.Origin.URL
	}
	isWorkspace := opts.WorkspaceURL != "" && rel.Repository == opts.WorkspaceURL
	if ref := tuicore.BuildRef(rel.ID, rel.Repository, rel.Branch, isWorkspace); ref != "" {
		var refLink *tuicore.Location
		if !isWorkspace && rel.Repository != "" {
			loc := tuicore.LocRepository(rel.Repository, rel.Branch)
			refLink = &loc
		}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: ref, Link: refLink})
	}
	badge := ""
	if rel.Prerelease {
		badge = "pre-release"
	}
	if relOriginURL != "" {
		loc := tuicore.Location{Path: relOriginURL}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: "↗", Link: &loc})
	}

	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Icon:     icon,
			Title:    title,
			Subtitle: subtitleParts,
			Badge:    badge,
		},
		Content:      tuicore.CardContent{Text: rel.Body},
		ContentLinks: tuicore.ExtractContentLinks(rel.Body, rel.Repository, ""),
	}
	relEmail := rel.Author.Email
	if rel.Origin != nil && rel.Origin.AuthorEmail != "" {
		relEmail = rel.Origin.AuthorEmail
	}
	if opts.UserEmail != "" && strings.EqualFold(relEmail, opts.UserEmail) {
		card.Header.IsMe = true
	}
	if opts.WorkspaceURL != "" && rel.Repository == opts.WorkspaceURL {
		card.Header.IsOwnRepo = true
	}
	return card
}

// Register registers all release views with the host.
func Register(host tuicore.ViewHost) {
	state := host.State()
	host.AddView("/release/list", NewReleasesView(state.Workdir))
	host.AddView("/release/detail", NewReleaseDetailView(state.Workdir))
	host.AddView("/release/new", NewReleaseFormView(state.Workdir))
	host.AddView("/release/edit", NewReleaseEditFormView(state.Workdir))
	host.AddView("/release/sbom", NewReleaseSBOMView(state.Workdir))
}

// ReleaseCreatedMsg is sent when a release is created.
type ReleaseCreatedMsg struct {
	Release release.Release
	Err     error
}

// ReleaseUpdatedMsg is sent when a release is updated.
type ReleaseUpdatedMsg struct {
	Release release.Release
	Err     error
}

// ReleaseRetractedMsg is sent when a release is retracted.
type ReleaseRetractedMsg struct {
	ID  string
	Err error
}

func handleReleaseMessages(msg tea.Msg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case ReleaseCreatedMsg:
		return handleReleaseCreated(msg, ctx)
	case ReleaseUpdatedMsg:
		return handleReleaseUpdated(msg, ctx)
	case ReleaseRetractedMsg:
		return handleReleaseRetracted(msg, ctx)
	}
	return false, nil
}

func handleReleaseCreated(msg ReleaseCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Created: %s", msg.Release.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReleaseDetail(msg.Release.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handleReleaseUpdated(msg ReleaseUpdatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Updated: %s", msg.Release.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReleaseDetail(msg.Release.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handleReleaseRetracted(msg ReleaseRetractedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout("Release retracted", tuicore.MessageTypeSuccess, 5*time.Second)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{Action: tuicore.NavBack}
	})
}
