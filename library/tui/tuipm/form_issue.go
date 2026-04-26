// form.go - Issue creation form using Huh
package tuipm

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// IssueFormData holds the form field values.
type IssueFormData struct {
	Subject   string
	Body      string
	State     string
	Status    string
	Priority  string
	Kind      string
	Assignees []string
	Milestone []string
	Sprint    []string
	Blocks    []string
	BlockedBy []string
	Related   []string
}

// IssueForm wraps a Huh form for issue creation/editing.
type IssueForm struct {
	workdir      string
	issueID      string // Non-empty for edit mode
	form         *huh.Form
	bodyField    *huh.Text
	data         IssueFormData
	framework    *pm.Framework
	contributors []cache.Contributor
	milestones   []pm.Milestone
	sprints      []pm.Sprint
	issues       []pm.Issue
	repoURL      string
	width        int
	height       int
	submitted    bool
	canceled     bool
}

// NewIssueForm creates a new issue form based on the current framework.
func NewIssueForm(workdir string) *IssueForm {
	pmConfig := pm.GetPMConfig(workdir)
	fw := pm.GetFramework(pmConfig.Framework)

	repoURL := gitmsg.ResolveRepoURL(workdir)

	var milestones []pm.Milestone
	if res := pm.GetMilestones("", "", []string{"open"}, "", 100); res.Success {
		milestones = res.Data
	}

	var sprints []pm.Sprint
	if res := pm.GetSprints("", "", []string{"planned", "active"}, "", 100); res.Success {
		sprints = res.Data
	}

	var contributors []cache.Contributor
	if c, err := cache.GetContributors(repoURL); err == nil {
		contributors = c
	}

	var issues []pm.Issue
	if res := pm.GetIssues("", "", []string{"open"}, "", 200); res.Success {
		issues = res.Data
	}

	f := &IssueForm{
		workdir:      workdir,
		framework:    fw,
		contributors: contributors,
		milestones:   milestones,
		sprints:      sprints,
		issues:       issues,
		repoURL:      repoURL,
	}
	f.buildForm()
	return f
}

// NewIssueEditForm creates an edit form pre-populated with issue data.
func NewIssueEditForm(workdir string, issue pm.Issue) *IssueForm {
	pmConfig := pm.GetPMConfig(workdir)
	fw := pm.GetFramework(pmConfig.Framework)

	// Extract label values from issue
	var status, priority, kind string
	for _, label := range issue.Labels {
		switch label.Scope {
		case "status":
			status = label.Value
		case "priority":
			priority = label.Value
		case "kind":
			kind = label.Value
		}
	}

	repoURL := gitmsg.ResolveRepoURL(workdir)

	// Resolve milestone/sprint refs as single-element slices for TagField
	var milestone []string
	if issue.Milestone != nil {
		milestone = []string{protocol.CreateRef(protocol.RefTypeCommit, issue.Milestone.Hash, issue.Milestone.RepoURL, issue.Milestone.Branch)}
	}
	var sprint []string
	if issue.Sprint != nil {
		sprint = []string{protocol.CreateRef(protocol.RefTypeCommit, issue.Sprint.Hash, issue.Sprint.RepoURL, issue.Sprint.Branch)}
	}

	var milestones []pm.Milestone
	if res := pm.GetMilestones("", "", []string{"open"}, "", 100); res.Success {
		milestones = res.Data
	}

	var sprints []pm.Sprint
	if res := pm.GetSprints("", "", []string{"planned", "active"}, "", 100); res.Success {
		sprints = res.Data
	}

	var contributors []cache.Contributor
	if c, err := cache.GetContributors(repoURL); err == nil {
		contributors = c
	}

	var issues []pm.Issue
	if res := pm.GetIssues("", "", []string{"open"}, "", 200); res.Success {
		issues = res.Data
	}

	assignees := make([]string, len(issue.Assignees))
	copy(assignees, issue.Assignees)

	f := &IssueForm{
		workdir:      workdir,
		issueID:      issue.ID,
		framework:    fw,
		contributors: contributors,
		milestones:   milestones,
		sprints:      sprints,
		issues:       issues,
		repoURL:      repoURL,
		data: IssueFormData{
			Subject:   issue.Subject,
			Body:      issue.Body,
			State:     string(issue.State),
			Status:    status,
			Priority:  priority,
			Kind:      kind,
			Assignees: assignees,
			Milestone: milestone,
			Sprint:    sprint,
			Blocks:    issueRefsToTags(issue.Blocks),
			BlockedBy: issueRefsToTags(issue.BlockedBy),
			Related:   issueRefsToTags(issue.Related),
		},
	}
	f.buildFormWithDefaults()
	return f
}

// IsEditMode returns true if this is an edit form.
func (f *IssueForm) IsEditMode() bool {
	return f.issueID != ""
}

// buildForm constructs the Huh form with dynamic fields based on framework.
func (f *IssueForm) buildForm() {
	f.buildFormWithDefaults()
}

// buildFormWithDefaults constructs the form, using data values as defaults.
func (f *IssueForm) buildFormWithDefaults() {
	pad := tuicore.PadLabel
	var fields []huh.Field

	fields = append(fields,
		huh.NewInput().
			Key("subject").
			Title(pad(tuicore.RequiredLabel("Subject"))).
			Placeholder("Issue title...").
			Value(&f.data.Subject).
			Inline(true).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("subject is required")
				}
				return nil
			}),
	)

	// State field (edit mode only)
	if f.IsEditMode() {
		fields = append(fields, tuicore.NewCycleField().
			Key("state").
			Title(pad("State")).
			Options(
				tuicore.CycleOption{Label: "Open", Value: "open"},
				tuicore.CycleOption{Label: "Closed", Value: "closed"},
				tuicore.CycleOption{Label: "Canceled", Value: "canceled"},
			).
			Value(&f.data.State))
	}

	// Framework-specific label fields
	if f.framework != nil && len(f.framework.Labels) > 0 {
		if config, ok := f.framework.Labels["status"]; ok {
			fields = append(fields, f.buildCycleField("status", pad("Status"), config.Values, &f.data.Status))
		}
		if config, ok := f.framework.Labels["priority"]; ok {
			fields = append(fields, f.buildCycleField("priority", pad("Priority"), config.Values, &f.data.Priority))
		}
		if config, ok := f.framework.Labels["kind"]; ok {
			fields = append(fields, f.buildCycleField("kind", pad("Kind"), config.Values, &f.data.Kind))
		}
	}

	assigneeOpts := buildContributorOptions(f.contributors)
	fields = append(fields, tuicore.NewTagField().
		Key("assignees").
		Title(pad("Assignees")).
		Placeholder("Add assignee...").
		Options(assigneeOpts...).
		Value(&f.data.Assignees))

	// Milestone field (single-tag)
	milestoneOpts := buildMilestoneOptions(f.milestones, f.repoURL)
	fields = append(fields, tuicore.NewTagField().
		Key("milestone").
		Title(pad("Milestone")).
		Placeholder("Select or type ref...").
		Options(milestoneOpts...).
		MaxTags(1).
		Value(&f.data.Milestone))

	// Sprint field (single-tag)
	sprintOpts := buildSprintOptions(f.sprints, f.repoURL)
	fields = append(fields, tuicore.NewTagField().
		Key("sprint").
		Title(pad("Sprint")).
		Placeholder("Select or type ref...").
		Options(sprintOpts...).
		MaxTags(1).
		Value(&f.data.Sprint))

	// Link fields (multi-tag)
	issueOpts := buildIssueOptions(f.issues, f.repoURL)
	fields = append(fields, tuicore.NewTagField().
		Key("blocks").
		Title(pad("Blocks")).
		Placeholder("Add issue ref...").
		Options(issueOpts...).
		Value(&f.data.Blocks))
	fields = append(fields, tuicore.NewTagField().
		Key("blocked-by").
		Title(pad("Blocked by")).
		Placeholder("Add issue ref...").
		Options(issueOpts...).
		Value(&f.data.BlockedBy))
	fields = append(fields, tuicore.NewTagField().
		Key("related").
		Title(pad("Related")).
		Placeholder("Add issue ref...").
		Options(issueOpts...).
		Value(&f.data.Related))

	// Description (last, fills remaining space)
	f.bodyField = huh.NewText().
		Key("body").
		Title("Description").
		Placeholder("Optional description...").
		Value(&f.data.Body).
		CharLimit(2000).
		Lines(20)
	fields = append(fields, f.bodyField)

	fields = append(fields, tuicore.NewSubmitField())

	f.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(formKeyMap())
}

// buildCycleField creates a CycleField from framework label values.
func (f *IssueForm) buildCycleField(key, title string, values []string, value *string) huh.Field {
	opts := make([]tuicore.CycleOption, 0, 1+len(values))
	opts = append(opts, tuicore.CycleOption{Label: "(none)", Value: ""})
	for _, v := range values {
		opts = append(opts, tuicore.CycleOption{Label: v, Value: v})
	}
	return tuicore.NewCycleField().Key(key).Title(title).Options(opts...).Value(value)
}

// buildMilestoneOptions converts milestones to TagField options.
func buildMilestoneOptions(milestones []pm.Milestone, repoURL string) []tuicore.TagOption {
	opts := make([]tuicore.TagOption, 0, len(milestones))
	for _, m := range milestones {
		label := m.Title
		if m.Repository != repoURL {
			label = protocol.GetFullDisplayName(m.Repository) + ": " + m.Title
		}
		opts = append(opts, tuicore.TagOption{Label: label, Value: m.ID})
	}
	return opts
}

// buildSprintOptions converts sprints to TagField options.
func buildSprintOptions(sprints []pm.Sprint, repoURL string) []tuicore.TagOption {
	opts := make([]tuicore.TagOption, 0, len(sprints))
	for _, s := range sprints {
		label := s.Title
		if s.Repository != repoURL {
			label = protocol.GetFullDisplayName(s.Repository) + ": " + s.Title
		}
		opts = append(opts, tuicore.TagOption{Label: label, Value: s.ID})
	}
	return opts
}

// formKeyMap returns shared key bindings for all forms.
func formKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	return km
}

// SetSize sets the form dimensions.
func (f *IssueForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if f.form != nil {
		f.form.WithWidth(w).WithHeight(h - 2)
		if f.bodyField != nil {
			f.bodyField.WithHeight(max(5, h-10))
		}
	}
}

// Init initializes the form.
func (f *IssueForm) Init() tea.Cmd {
	return f.form.Init()
}

// Update handles form messages.
func (f *IssueForm) Update(msg tea.Msg) tea.Cmd {
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
func (f *IssueForm) View() string {
	return f.form.View()
}

// IsSubmitted returns true if form was submitted.
func (f *IssueForm) IsSubmitted() bool {
	return f.submitted
}

// IsCancelled returns true if form was canceled.
func (f *IssueForm) IsCancelled() bool {
	return f.canceled
}

// Errors returns the form's current validation errors.
func (f *IssueForm) Errors() []error { return f.form.Errors() }

// GetData returns the form data.
func (f *IssueForm) GetData() IssueFormData {
	return f.data
}

// firstTagRef extracts the first tag value from a slice and strips local repo prefix.
func firstTagRef(tags []string, repoURL string) string {
	if len(tags) == 0 {
		return ""
	}
	return stripLocalRef(tags[0], repoURL)
}

// CreateIssueFromForm creates an issue from form data.
func (f *IssueForm) CreateIssueFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	repoURL := f.repoURL
	return func() tea.Msg {
		opts := pm.CreateIssueOptions{}

		// Build labels from form selections
		if data.Status != "" {
			opts.Labels = append(opts.Labels, pm.Label{Scope: "status", Value: data.Status})
		}
		if data.Priority != "" {
			opts.Labels = append(opts.Labels, pm.Label{Scope: "priority", Value: data.Priority})
		}
		if data.Kind != "" {
			opts.Labels = append(opts.Labels, pm.Label{Scope: "kind", Value: data.Kind})
		}

		opts.Assignees = data.Assignees
		opts.Milestone = firstTagRef(data.Milestone, repoURL)
		opts.Sprint = firstTagRef(data.Sprint, repoURL)
		opts.Blocks = stripLocalRefs(data.Blocks, repoURL)
		opts.BlockedBy = stripLocalRefs(data.BlockedBy, repoURL)
		opts.Related = stripLocalRefs(data.Related, repoURL)

		result := pm.CreateIssue(workdir, data.Subject, data.Body, opts)
		if !result.Success {
			return IssueCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return IssueCreatedMsg{Issue: result.Data}
	}
}

// UpdateIssueFromForm updates an existing issue from form data.
func (f *IssueForm) UpdateIssueFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	issueID := f.issueID
	repoURL := f.repoURL
	return func() tea.Msg {
		// Build labels from form selections
		var labels []pm.Label
		if data.Status != "" {
			labels = append(labels, pm.Label{Scope: "status", Value: data.Status})
		}
		if data.Priority != "" {
			labels = append(labels, pm.Label{Scope: "priority", Value: data.Priority})
		}
		if data.Kind != "" {
			labels = append(labels, pm.Label{Scope: "kind", Value: data.Kind})
		}

		assignees := data.Assignees

		// Parse state
		state := pm.State(data.State)

		milestoneRef := firstTagRef(data.Milestone, repoURL)
		sprintRef := firstTagRef(data.Sprint, repoURL)
		blocks := stripLocalRefs(data.Blocks, repoURL)
		blockedBy := stripLocalRefs(data.BlockedBy, repoURL)
		related := stripLocalRefs(data.Related, repoURL)

		opts := pm.UpdateIssueOptions{
			Subject:   &data.Subject,
			Body:      &data.Body,
			State:     &state,
			Labels:    &labels,
			Assignees: &assignees,
			Milestone: &milestoneRef,
			Sprint:    &sprintRef,
			Blocks:    &blocks,
			BlockedBy: &blockedBy,
			Related:   &related,
		}

		result := pm.UpdateIssue(workdir, issueID, opts)
		if !result.Success {
			return IssueUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return IssueUpdatedMsg{Issue: result.Data}
	}
}

// IssueUpdatedMsg signals that an issue has been updated.
type IssueUpdatedMsg struct {
	Issue pm.Issue
	Err   error
}

// stripLocalRef strips the repo prefix from a ref if it matches the workdir repo.
func stripLocalRef(ref, repoURL string) string {
	if parsed := protocol.ParseRef(ref); parsed.Repository == "" || parsed.Repository == repoURL {
		return protocol.StripRepoFromRef(ref)
	}
	return ref
}

// buildIssueOptions converts issues to TagField options for link fields.
func buildIssueOptions(issues []pm.Issue, repoURL string) []tuicore.TagOption {
	opts := make([]tuicore.TagOption, 0, len(issues))
	for _, iss := range issues {
		label := iss.Subject
		if iss.Repository != repoURL {
			label = protocol.GetFullDisplayName(iss.Repository) + ": " + iss.Subject
		}
		opts = append(opts, tuicore.TagOption{Label: label, Value: iss.ID})
	}
	return opts
}

// issueRefsToTags converts IssueRef slices to tag value strings (ref IDs).
func issueRefsToTags(refs []pm.IssueRef) []string {
	if len(refs) == 0 {
		return nil
	}
	tags := make([]string, len(refs))
	for i, ref := range refs {
		tags[i] = protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch)
	}
	return tags
}

// stripLocalRefs strips local repo prefix from a slice of ref strings.
func stripLocalRefs(refs []string, repoURL string) []string {
	if len(refs) == 0 {
		return nil
	}
	result := make([]string, len(refs))
	for i, r := range refs {
		result[i] = stripLocalRef(r, repoURL)
	}
	return result
}

// buildContributorOptions converts cache contributors to TagField options.
func buildContributorOptions(contributors []cache.Contributor) []tuicore.TagOption {
	opts := make([]tuicore.TagOption, 0, len(contributors))
	for _, c := range contributors {
		label := c.Email
		if c.Name != "" {
			label = c.Name + " <" + c.Email + ">"
		}
		opts = append(opts, tuicore.TagOption{Label: label, Value: c.Email})
	}
	return opts
}

// IssueFormView wraps the form for integration with the TUI host.
type IssueFormView struct {
	form       *IssueForm
	submitting bool
	width      int
	height     int
}

// NewIssueFormView creates a new issue form view. The form itself is
// constructed lazily in Activate to avoid running expensive contributor /
// milestone / sprint queries at TUI startup on large repositories.
func NewIssueFormView(_ string) *IssueFormView {
	return &IssueFormView{}
}

// SetSize sets the view dimensions.
func (v *IssueFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate creates a fresh form and initializes it.
func (v *IssueFormView) Activate(state *tuicore.State) tea.Cmd {
	v.form = NewIssueForm(state.Workdir)
	v.form.SetSize(v.width, v.height)
	return v.form.Init()
}

// Update handles messages.
func (v *IssueFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	cmd := v.form.Update(msg)

	if v.form.IsCancelled() {
		return func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		}
	}

	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return tea.Batch(
			v.form.CreateIssueFromForm(),
			func() tea.Msg { return tuicore.NavigateMsg{Action: tuicore.NavBack} },
		)
	}

	return cmd
}

// Render renders the form view.
func (v *IssueFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	content := v.form.View()
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *IssueFormView) Title() string {
	return "○  New Issue"
}

// Bindings returns keybindings for this view.
func (v *IssueFormView) Bindings() []tuicore.Binding {
	return nil
}

// ViewName returns the view identifier.
func (v *IssueFormView) ViewName() string {
	return "pm.issue_form"
}

// IsInputActive returns true (form always captures input).
func (v *IssueFormView) IsInputActive() bool {
	return true
}

// IssueEditFormView wraps the form for editing an existing issue.
type IssueEditFormView struct {
	workdir    string
	issueID    string
	form       *IssueForm
	issue      *pm.Issue
	loaded     bool
	submitting bool
	width      int
	height     int
}

// NewIssueEditFormView creates a new issue edit form view.
func NewIssueEditFormView(workdir string) *IssueEditFormView {
	return &IssueEditFormView{
		workdir: workdir,
	}
}

// SetSize sets the view dimensions.
func (v *IssueEditFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate loads the issue and initializes the form.
func (v *IssueEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.issueID = state.Router.Location().Param("issueID")
	v.loaded = false
	v.form = nil
	return v.loadIssue()
}

func (v *IssueEditFormView) loadIssue() tea.Cmd {
	issueID := v.issueID
	return func() tea.Msg {
		result := pm.GetIssue(issueID)
		if !result.Success {
			return EditFormLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return EditFormLoadedMsg{Issue: &result.Data}
	}
}

// EditFormLoadedMsg signals that the issue for editing has been loaded.
type EditFormLoadedMsg struct {
	Issue *pm.Issue
	Err   error
}

// Update handles messages.
func (v *IssueEditFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case EditFormLoadedMsg:
		if msg.Err != nil {
			return func() tea.Msg {
				return tuicore.NavigateMsg{Action: tuicore.NavBack}
			}
		}
		v.issue = msg.Issue
		v.form = NewIssueEditForm(v.workdir, *v.issue)
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
		return v.form.UpdateIssueFromForm()
	}

	return cmd
}

// Render renders the edit form view.
func (v *IssueEditFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading issue..."
	} else if v.form == nil {
		content = tuicore.Dim.Render("  Issue not found")
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
func (v *IssueEditFormView) Title() string {
	if v.issue != nil {
		return fmt.Sprintf("○  Edit: %s", v.issue.Subject)
	}
	return "○  Edit Issue"
}

// Bindings returns keybindings for this view.
func (v *IssueEditFormView) Bindings() []tuicore.Binding {
	return nil
}

// ViewName returns the view identifier.
func (v *IssueEditFormView) ViewName() string {
	return "pm.issue_edit_form"
}

// IsInputActive returns true (form always captures input).
func (v *IssueEditFormView) IsInputActive() bool {
	return true
}
