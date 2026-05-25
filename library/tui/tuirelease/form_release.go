// form_release.go - Release creation form using Huh
package tuirelease

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// ReleaseFormData holds the form field values.
type ReleaseFormData struct {
	Subject     string
	Body        string
	Version     string
	Tag         string
	Prerelease  bool
	Artifacts   []string
	ArtifactURL string
	Checksums   string
	SignedBy    string
	SBOM        string
	Labels      []string
}

// ReleaseForm wraps a Huh form for release creation and editing.
type ReleaseForm struct {
	tuicore.FormBase
	workdir       string
	releaseID     string
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          ReleaseFormData
	width         int
	height        int
}

// NewReleaseForm creates a new release creation form.
func NewReleaseForm(workdir string) *ReleaseForm {
	f := &ReleaseForm{workdir: workdir}
	f.buildForm()
	return f
}

// NewReleaseEditForm creates an edit form pre-populated with release data.
func NewReleaseEditForm(workdir string, rel release.Release) *ReleaseForm {
	f := &ReleaseForm{
		workdir:   workdir,
		releaseID: rel.ID,
		data: ReleaseFormData{
			Subject:     rel.Subject,
			Body:        rel.Body,
			Version:     rel.Version,
			Tag:         rel.Tag,
			Prerelease:  rel.Prerelease,
			Artifacts:   append([]string(nil), rel.Artifacts...),
			ArtifactURL: rel.ArtifactURL,
			Checksums:   rel.Checksums,
			SignedBy:    rel.SignedBy,
			SBOM:        rel.SBOM,
			Labels:      append([]string(nil), rel.Labels...),
		},
	}
	f.buildForm()
	return f
}

// IsEditMode returns true if this is an edit form.
func (f *ReleaseForm) IsEditMode() bool {
	return f.releaseID != ""
}

// buildForm constructs the Huh form.
func (f *ReleaseForm) buildForm() {
	pad := tuicore.PadLabel
	fields := make([]huh.Field, 0, 10)
	fields = append(fields,
		huh.NewInput().
			Key("subject").
			Title(pad(tuicore.RequiredLabel("Subject"))).
			Placeholder("Release title...").
			Value(&f.data.Subject).
			Inline(true).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("subject is required")
				}
				return nil
			}),
		huh.NewInput().
			Key("version").
			Title(pad("Version")).
			Placeholder("1.0.0").
			Value(&f.data.Version).
			Inline(true),
		huh.NewInput().
			Key("tag").
			Title(pad("Tag")).
			Placeholder("v1.0.0").
			Value(&f.data.Tag).
			Inline(true),
		huh.NewConfirm().
			Key("prerelease").
			Title(pad("Pre-release")).
			Inline(true).
			Value(&f.data.Prerelease),
		tuicore.NewTagField().
			Key("artifacts").
			Title(pad("Artifacts")).
			Placeholder("app.tar.gz, app.zip...").
			Value(&f.data.Artifacts),
		huh.NewInput().
			Key("artifact_url").
			Title(pad("Artifact URL")).
			Placeholder("https://example.com/releases/").
			Value(&f.data.ArtifactURL).
			Inline(true),
		huh.NewInput().
			Key("checksums").
			Title(pad("Checksums")).
			Placeholder("SHA256SUMS").
			Value(&f.data.Checksums).
			Inline(true),
		huh.NewInput().
			Key("sbom").
			Title(pad("SBOM")).
			Placeholder("sbom.spdx.json").
			Value(&f.data.SBOM).
			Inline(true),
		huh.NewInput().
			Key("signed_by").
			Title(pad("Signed By")).
			Placeholder("Key fingerprint").
			Value(&f.data.SignedBy).
			Inline(true),
	)
	f.bodyField = huh.NewText().
		Key("body").
		Title("Release Notes").
		Placeholder("Optional release notes...").
		Value(&f.data.Body).
		CharLimit(4000).
		Lines(20)
	fields = append(fields, f.bodyField, tuicore.NewLabelsField(&f.data.Labels, ""), tuicore.NewSubmitField())

	f.bodyOtherRows = len(fields)
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
}

// SetSize sets the form dimensions.
func (f *ReleaseForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if form := f.FormPtr(); form != nil {
		form.WithWidth(w).WithHeight(h + 1)
		if f.bodyField != nil {
			f.bodyField.WithHeight(tuicore.BodyHeight(h, f.bodyOtherRows))
		}
	}
}

// Update delegates the standard form lifecycle to FormBase.
func (f *ReleaseForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *ReleaseForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *ReleaseForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *ReleaseForm) Reset() { f.buildForm() }

// CreateReleaseFromForm creates a release from form data.
func (f *ReleaseForm) CreateReleaseFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		opts := release.CreateReleaseOptions{
			Tag:         strings.TrimSpace(data.Tag),
			Version:     strings.TrimSpace(data.Version),
			Prerelease:  data.Prerelease,
			ArtifactURL: strings.TrimSpace(data.ArtifactURL),
			Checksums:   strings.TrimSpace(data.Checksums),
			SignedBy:    strings.TrimSpace(data.SignedBy),
			SBOM:        strings.TrimSpace(data.SBOM),
			Labels:      data.Labels,
		}

		opts.Artifacts = data.Artifacts

		result := release.CreateRelease(workdir, strings.TrimSpace(data.Subject), strings.TrimSpace(data.Body), opts)
		if !result.Success {
			return ReleaseCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ReleaseCreatedMsg{Release: result.Data}
	}
}

// UpdateReleaseFromForm updates an existing release from form data.
func (f *ReleaseForm) UpdateReleaseFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	releaseID := f.releaseID
	return func() tea.Msg {
		subject := strings.TrimSpace(data.Subject)
		body := strings.TrimSpace(data.Body)
		tag := strings.TrimSpace(data.Tag)
		version := strings.TrimSpace(data.Version)
		artifactURL := strings.TrimSpace(data.ArtifactURL)
		checksums := strings.TrimSpace(data.Checksums)
		sbom := strings.TrimSpace(data.SBOM)
		signedBy := strings.TrimSpace(data.SignedBy)

		artifacts := data.Artifacts
		labels := data.Labels

		prerelease := data.Prerelease
		opts := release.EditReleaseOptions{
			Subject:     &subject,
			Body:        &body,
			Tag:         &tag,
			Version:     &version,
			Prerelease:  &prerelease,
			Artifacts:   &artifacts,
			ArtifactURL: &artifactURL,
			Checksums:   &checksums,
			SignedBy:    &signedBy,
			SBOM:        &sbom,
			Labels:      &labels,
		}

		result := release.EditRelease(workdir, releaseID, opts)
		if !result.Success {
			return ReleaseUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ReleaseUpdatedMsg{Release: result.Data}
	}
}

// ReleaseFormView wraps the form for integration with the TUI host.
type ReleaseFormView struct {
	tuicore.FormViewBase
}

// NewReleaseFormView creates a new release form view.
func NewReleaseFormView(_ string) *ReleaseFormView {
	return &ReleaseFormView{}
}

// Activate constructs a fresh form for every re-entry.
func (v *ReleaseFormView) Activate(state *tuicore.State) tea.Cmd {
	form := NewReleaseForm(state.Workdir)
	v.AttachForm(form)
	return form.Init()
}

// Update handles messages.
func (v *ReleaseFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(ReleaseCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*ReleaseForm); ok {
			return form.CreateReleaseFromForm()
		}
		return nil
	})
}

// Render renders the form view.
func (v *ReleaseFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *ReleaseFormView) Title() string { return "⏏  New Release" }

// ReleaseEditFormView wraps the form for editing an existing release.
type ReleaseEditFormView struct {
	tuicore.FormViewBase
	workdir   string
	releaseID string
	rel       *release.Release
	loaded    bool
}

// NewReleaseEditFormView creates a new release edit form view.
func NewReleaseEditFormView(workdir string) *ReleaseEditFormView {
	return &ReleaseEditFormView{workdir: workdir}
}

// Activate loads the release and initializes the form.
func (v *ReleaseEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.releaseID = state.Router.Location().Param("releaseID")
	v.loaded = false
	v.DetachForm()
	return v.loadRelease()
}

func (v *ReleaseEditFormView) loadRelease() tea.Cmd {
	releaseID := v.releaseID
	return func() tea.Msg {
		result := release.GetSingleRelease(releaseID)
		if !result.Success {
			return releaseEditFormLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		rel := result.Data
		return releaseEditFormLoadedMsg{Release: &rel}
	}
}

// releaseEditFormLoadedMsg signals that the release for editing has been loaded.
type releaseEditFormLoadedMsg struct {
	Release *release.Release
	Err     error
}

// Update handles messages.
func (v *ReleaseEditFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case releaseEditFormLoadedMsg:
		if msg.Err != nil {
			return func() tea.Msg {
				return tuicore.NavigateMsg{Action: tuicore.NavBack}
			}
		}
		v.rel = msg.Release
		form := NewReleaseEditForm(v.workdir, *v.rel)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(ReleaseUpdatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*ReleaseForm); ok {
			return form.UpdateReleaseFromForm()
		}
		return nil
	})
}

// Render renders the edit form view.
func (v *ReleaseEditFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		footer := tuicore.FormFooter(true, nil)
		return wrapper.Render("Loading release...", footer)
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *ReleaseEditFormView) Title() string {
	if v.rel != nil {
		return fmt.Sprintf("⏏  Edit: %s", v.rel.Subject)
	}
	return "⏏  Edit Release"
}
