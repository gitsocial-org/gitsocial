// form_release.go - Release creation form using Huh
package tuirelease

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ReleaseFormData holds the form field values.
type ReleaseFormData struct {
	Subject     string
	Body        string
	Version     string
	Tag         string
	Prerelease  string
	Artifacts   string
	ArtifactURL string
	Checksums   string
	SignedBy    string
	SBOM        string
}

// ReleaseForm wraps a Huh form for release creation and editing.
type ReleaseForm struct {
	workdir   string
	releaseID string
	form      *huh.Form
	bodyField *huh.Text
	data      ReleaseFormData
	width     int
	height    int
	submitted bool
	canceled  bool
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
			Prerelease:  boolToYesNo(rel.Prerelease),
			Artifacts:   strings.Join(rel.Artifacts, ", "),
			ArtifactURL: rel.ArtifactURL,
			Checksums:   rel.Checksums,
			SignedBy:    rel.SignedBy,
			SBOM:        rel.SBOM,
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
		tuicore.NewCycleField().
			Key("prerelease").
			Title(pad("Pre-release")).
			Options(
				tuicore.CycleOption{Label: "No", Value: "no"},
				tuicore.CycleOption{Label: "Yes", Value: "yes"},
			).
			Value(&f.data.Prerelease),
		huh.NewInput().
			Key("artifacts").
			Title(pad("Artifacts")).
			Placeholder("app.tar.gz, app.zip (comma-separated)").
			Value(&f.data.Artifacts).
			Inline(true),
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
	fields = append(fields, f.bodyField, tuicore.NewSubmitField())

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))

	f.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(km)
}

// SetSize sets the form dimensions.
func (f *ReleaseForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if f.form != nil {
		f.form.WithWidth(w).WithHeight(h - 2)
		if f.bodyField != nil {
			f.bodyField.WithHeight(max(5, h-11))
		}
	}
}

// Init initializes the form.
func (f *ReleaseForm) Init() tea.Cmd {
	return f.form.Init()
}

// Update handles form messages.
func (f *ReleaseForm) Update(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "esc" {
			f.canceled = true
			return nil
		}
	}

	form, cmd := f.form.Update(msg)
	if m, ok := form.(*huh.Form); ok {
		f.form = m
	}

	if f.form.State == huh.StateCompleted {
		f.submitted = true
	}

	return cmd
}

// View renders the form.
func (f *ReleaseForm) View() string {
	return f.form.View()
}

// Errors returns the form's current validation errors.
func (f *ReleaseForm) Errors() []error { return f.form.Errors() }

// IsSubmitted returns true if form was submitted.
func (f *ReleaseForm) IsSubmitted() bool {
	return f.submitted
}

// IsCancelled returns true if form was canceled.
func (f *ReleaseForm) IsCancelled() bool {
	return f.canceled
}

// CreateReleaseFromForm creates a release from form data.
func (f *ReleaseForm) CreateReleaseFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		prerelease := data.Prerelease == "yes"
		opts := release.CreateReleaseOptions{
			Tag:         strings.TrimSpace(data.Tag),
			Version:     strings.TrimSpace(data.Version),
			Prerelease:  prerelease,
			ArtifactURL: strings.TrimSpace(data.ArtifactURL),
			Checksums:   strings.TrimSpace(data.Checksums),
			SignedBy:    strings.TrimSpace(data.SignedBy),
			SBOM:        strings.TrimSpace(data.SBOM),
		}

		if data.Artifacts != "" {
			for _, a := range strings.Split(data.Artifacts, ",") {
				a = strings.TrimSpace(a)
				if a != "" {
					opts.Artifacts = append(opts.Artifacts, a)
				}
			}
		}

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

		var artifacts []string
		if data.Artifacts != "" {
			for _, a := range strings.Split(data.Artifacts, ",") {
				a = strings.TrimSpace(a)
				if a != "" {
					artifacts = append(artifacts, a)
				}
			}
		}

		prerelease := data.Prerelease == "yes"
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
	form       *ReleaseForm
	submitting bool
	width      int
	height     int
}

// NewReleaseFormView creates a new release form view.
func NewReleaseFormView(workdir string) *ReleaseFormView {
	return &ReleaseFormView{
		form: NewReleaseForm(workdir),
	}
}

// SetSize sets the view dimensions.
func (v *ReleaseFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.form.SetSize(w, h)
}

// Activate initializes the form view.
func (v *ReleaseFormView) Activate(state *tuicore.State) tea.Cmd {
	return v.form.Init()
}

// Deactivate is called when the view is hidden.
func (v *ReleaseFormView) Deactivate() {}

// Update handles messages.
func (v *ReleaseFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	cmd := v.form.Update(msg)

	if v.form.IsCancelled() {
		return func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		}
	}

	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return v.form.CreateReleaseFromForm()
	}

	return cmd
}

// Render renders the form view.
func (v *ReleaseFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	content := v.form.View()
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *ReleaseFormView) Title() string {
	return "⏏  New Release"
}

// Bindings returns keybindings for this view.
func (v *ReleaseFormView) Bindings() []tuicore.Binding {
	return nil
}

// IsInputActive returns true (form always captures input).
func (v *ReleaseFormView) IsInputActive() bool {
	return true
}

// ReleaseEditFormView wraps the form for editing an existing release.
type ReleaseEditFormView struct {
	workdir    string
	releaseID  string
	form       *ReleaseForm
	rel        *release.Release
	loaded     bool
	submitting bool
	width      int
	height     int
}

// NewReleaseEditFormView creates a new release edit form view.
func NewReleaseEditFormView(workdir string) *ReleaseEditFormView {
	return &ReleaseEditFormView{workdir: workdir}
}

// SetSize sets the view dimensions.
func (v *ReleaseEditFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate loads the release and initializes the form.
func (v *ReleaseEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.releaseID = state.Router.Location().Param("releaseID")
	v.loaded = false
	v.form = nil
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

// Deactivate is called when the view is hidden.
func (v *ReleaseEditFormView) Deactivate() {}

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
		v.form = NewReleaseEditForm(v.workdir, *v.rel)
		v.form.SetSize(v.width, v.height)
		v.loaded = true
		return v.form.Init()
	}

	if !v.loaded || v.form == nil {
		return nil
	}

	cmd := v.form.Update(msg)

	if v.form.IsCancelled() {
		return func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		}
	}

	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return v.form.UpdateReleaseFromForm()
	}

	return cmd
}

// Render renders the edit form view.
func (v *ReleaseEditFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading release..."
	} else if v.form == nil {
		content = tuicore.Dim.Render("  Release not found")
	} else {
		v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.form.View()
	}
	var footer string
	if v.form != nil {
		footer = tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	} else {
		footer = tuicore.Dim.Render("tab/shift+tab navigate · enter confirm · esc cancel")
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *ReleaseEditFormView) Title() string {
	if v.rel != nil {
		return fmt.Sprintf("⏏  Edit: %s", v.rel.Subject)
	}
	return "⏏  Edit Release"
}

// Bindings returns keybindings for this view.
func (v *ReleaseEditFormView) Bindings() []tuicore.Binding {
	return nil
}

// IsInputActive returns true (form always captures input).
func (v *ReleaseEditFormView) IsInputActive() bool {
	return true
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
