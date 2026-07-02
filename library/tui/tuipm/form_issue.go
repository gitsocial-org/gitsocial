// form.go - Issue creation form using Huh
package tuipm

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
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
	// Labels carries free-form labels (anything outside the framework-driven
	// status / priority / kind scopes), serialized as `scope/value` strings.
	Labels []string
}

// IssueForm wraps a Huh form for issue creation/editing.
type IssueForm struct {
	tuicore.FormBase
	workdir       string
	issueID       string // Non-empty for edit mode
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          IssueFormData
	framework     *pm.Framework
	contributors  []cache.Contributor
	milestones    []pm.Milestone
	sprints       []pm.Sprint
	issues        []pm.Issue
	repoURL       string
	width         int
	height        int
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
	f.buildFormWithDefaults()
	return f
}

// NewIssueEditForm creates an edit form pre-populated with issue data.
func NewIssueEditForm(workdir string, issue pm.Issue) *IssueForm {
	pmConfig := pm.GetPMConfig(workdir)
	fw := pm.GetFramework(pmConfig.Framework)

	// Split labels: structured scopes (status/priority/kind) feed dedicated
	// cycle fields; everything else flows into the free-form Labels field.
	var status, priority, kind string
	var freeLabels []string
	for _, label := range issue.Labels {
		switch label.Scope {
		case "status":
			status = label.Value
		case "priority":
			priority = label.Value
		case "kind":
			kind = label.Value
		default:
			freeLabels = append(freeLabels, issueLabelToString(label))
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
			Labels:    freeLabels,
		},
	}
	f.buildFormWithDefaults()
	return f
}

// IsEditMode returns true if this is an edit form.
func (f *IssueForm) IsEditMode() bool {
	return f.issueID != ""
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

	assigneeOpts := tuicore.ContributorOptions(f.contributors)
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

	// Free-form labels (other than the structured status/priority/kind cycles).
	fields = append(fields, tuicore.NewLabelsField(&f.data.Labels, "topic/area, area/api, ..."))

	fields = append(fields, tuicore.NewSubmitField())

	f.bodyOtherRows = len(fields)
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
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

// SetSize sets the form dimensions.
func (f *IssueForm) SetSize(w, h int) {
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
func (f *IssueForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *IssueForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *IssueForm) SetBody(s string) {
	f.data.Body = s
	f.buildFormWithDefaults()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *IssueForm) Reset() { f.buildFormWithDefaults() }

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

		opts.Labels = buildIssueLabelsFromForm(data)

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
		labels := buildIssueLabelsFromForm(data)

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

// IssueFormView wraps the form for integration with the TUI host.
type IssueFormView struct {
	tuicore.FormViewBase
}

// NewIssueFormView creates a new issue form view. The form itself is
// constructed lazily in Activate to avoid running expensive contributor /
// milestone / sprint queries at TUI startup on large repositories.
func NewIssueFormView(_ string) *IssueFormView {
	return &IssueFormView{}
}

// Activate creates a fresh form and initializes it.
func (v *IssueFormView) Activate(state *tuicore.State) tea.Cmd {
	form := NewIssueForm(state.Workdir)
	v.AttachForm(form)
	return form.Init()
}

// Update handles messages.
func (v *IssueFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(IssueCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*IssueForm); ok {
			return form.CreateIssueFromForm()
		}
		return nil
	})
}

// Render renders the form view.
func (v *IssueFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *IssueFormView) Title() string { return "○  New Issue" }

// ViewName returns the view identifier.
func (v *IssueFormView) ViewName() string { return "pm.issue_form" }

// IssueEditFormView wraps the form for editing an existing issue.
type IssueEditFormView struct {
	tuicore.FormViewBase
	workdir string
	issueID string
	issue   *pm.Issue
	loaded  bool
}

// NewIssueEditFormView creates a new issue edit form view.
func NewIssueEditFormView(workdir string) *IssueEditFormView {
	return &IssueEditFormView{
		workdir: workdir,
	}
}

// Activate loads the issue and initializes the form.
func (v *IssueEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.issueID = state.Router.Location().Param("issueID")
	v.loaded = false
	v.DetachForm()
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
		form := NewIssueEditForm(v.workdir, *v.issue)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(IssueUpdatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*IssueForm); ok {
			return form.UpdateIssueFromForm()
		}
		return nil
	})
}

// Render renders the edit form view.
func (v *IssueEditFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		footer := tuicore.FormFooter(true, nil)
		return wrapper.Render("Loading issue...", footer)
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *IssueEditFormView) Title() string {
	if v.issue != nil {
		return fmt.Sprintf("○  Edit: %s", v.issue.Subject)
	}
	return "○  Edit Issue"
}

// ViewName returns the view identifier.
func (v *IssueEditFormView) ViewName() string { return "pm.issue_edit_form" }

// buildIssueLabelsFromForm assembles the full label slice from the form data:
// structured cycles (status/priority/kind) plus any free-form labels parsed
// from the Labels TagField.
func buildIssueLabelsFromForm(data IssueFormData) []pm.Label {
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
	for _, raw := range data.Labels {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		labels = append(labels, parseIssueFormLabel(raw))
	}
	return labels
}

// issueLabelToString serializes a pm.Label as its on-wire `scope/value` form.
func issueLabelToString(l pm.Label) string {
	if l.Scope != "" {
		return l.Scope + "/" + l.Value
	}
	return l.Value
}

// parseIssueFormLabel parses a raw `scope/value` or bare `value` string from
// the Labels TagField into a pm.Label.
func parseIssueFormLabel(s string) pm.Label {
	if idx := strings.Index(s, "/"); idx > 0 {
		return pm.Label{Scope: s[:idx], Value: s[idx+1:]}
	}
	return pm.Label{Value: s}
}
