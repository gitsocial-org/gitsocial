// form_pr.go - Pull request creation and edit form using Huh
package tuireview

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// PRFormData holds the form field values.
type PRFormData struct {
	Subject   string
	Body      string
	Base      []string
	Head      []string
	Reviewers []string
	Closes    []string
	Draft     bool
}

// PRForm wraps a Huh form for pull request creation and editing.
type PRForm struct {
	workdir      string
	prID         string
	form         *huh.Form
	bodyField    *huh.Text
	baseField    *tuicore.TagField
	headField    *tuicore.TagField
	data         PRFormData
	contributors []cache.Contributor
	branches     []string
	issues       []pm.Issue
	width        int
	height       int
	submitted    bool
	canceled     bool
}

// prFormDataMsg carries async-loaded data for PR form construction.
type prFormDataMsg struct {
	Contributors []cache.Contributor
	Branches     []string
	Issues       []pm.Issue
	DefaultBase  string
	CurrentHead  string
}

// prForkBranchesMsg carries asynchronously loaded fork branches.
type prForkBranchesMsg struct {
	ForkBranches map[string][]string // fork URL → branch names
}

// loadPRFormData loads contributors, branches, and issues asynchronously.
func loadPRFormData(workdir string) tea.Cmd {
	return func() tea.Msg {
		repoURL := gitmsg.ResolveRepoURL(workdir)
		var contributors []cache.Contributor
		if c, err := cache.GetContributors(repoURL); err == nil {
			contributors = c
		}
		var branches []string
		if b, err := git.ListLocalBranches(workdir); err == nil {
			branches = b
		}
		var issues []pm.Issue
		if res := pm.GetIssues("", "", []string{"open"}, "", 100); res.Success {
			issues = res.Data
		}
		var defaultBase, currentHead string
		if b, err := git.GetDefaultBranch(workdir); err == nil {
			defaultBase = b
		}
		if b, err := git.GetCurrentBranch(workdir); err == nil {
			currentHead = b
		}
		return prFormDataMsg{
			Contributors: contributors,
			Branches:     branches,
			Issues:       issues,
			DefaultBase:  defaultBase,
			CurrentHead:  currentHead,
		}
	}
}

// loadForkBranches loads fork branches asynchronously via network (ls-remote).
func loadForkBranches(workdir string) tea.Cmd {
	return func() tea.Msg {
		forkBranches := make(map[string][]string)
		for _, forkURL := range review.GetForks(workdir) {
			if fb, err := git.ListRemoteBranches(workdir, forkURL); err == nil {
				forkBranches[forkURL] = fb
			}
		}
		return prForkBranchesMsg{ForkBranches: forkBranches}
	}
}

// NewPRForm creates a new pull request creation form with pre-loaded data.
func NewPRForm(workdir string, data prFormDataMsg) *PRForm {
	f := &PRForm{
		workdir:      workdir,
		contributors: data.Contributors,
		branches:     data.Branches,
		issues:       data.Issues,
	}
	f.data.Reviewers = []string{}

	if data.DefaultBase != "" {
		f.data.Base = []string{data.DefaultBase}
	}
	if data.CurrentHead != "" {
		f.data.Head = []string{data.CurrentHead}
	}

	f.buildForm()
	return f
}

// NewPREditForm creates an edit form pre-populated with pull request data.
func NewPREditForm(workdir string, pr review.PullRequest, data prFormDataMsg) *PRForm {
	reviewers := make([]string, len(pr.Reviewers))
	copy(reviewers, pr.Reviewers)

	var base []string
	if b := shortenBranchRef(pr.Base); b != "" {
		base = []string{b}
	}
	var head []string
	if h := shortenBranchRef(pr.Head); h != "" {
		head = []string{h}
	}

	closes := make([]string, len(pr.Closes))
	copy(closes, pr.Closes)

	f := &PRForm{
		workdir:      workdir,
		prID:         pr.ID,
		contributors: data.Contributors,
		branches:     data.Branches,
		issues:       data.Issues,
		data: PRFormData{
			Subject:   pr.Subject,
			Body:      pr.Body,
			Base:      base,
			Head:      head,
			Reviewers: reviewers,
			Closes:    closes,
			Draft:     pr.IsDraft,
		},
	}
	f.buildForm()
	return f
}

// IsEditMode returns true if this is an edit form.
func (f *PRForm) IsEditMode() bool {
	return f.prID != ""
}

// buildForm constructs the Huh form.
func (f *PRForm) buildForm() {
	pad := tuicore.PadLabel
	var fields []huh.Field

	fields = append(fields,
		huh.NewInput().
			Key("subject").
			Title(pad(tuicore.RequiredLabel("Subject"))).
			Placeholder("Pull request title...").
			Value(&f.data.Subject).
			Inline(true).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("subject is required")
				}
				return nil
			}),
	)

	// Branch fields (single-tag, fork branches appended asynchronously)
	branchOpts := buildBranchOptions(f.branches, nil)
	f.baseField = tuicore.NewTagField().
		Key("base").
		Title(pad("Base Branch")).
		Placeholder("Select branch...").
		Options(branchOpts...).
		MaxTags(1).
		LabelFunc(formatBranchLabel).
		Value(&f.data.Base)
	f.headField = tuicore.NewTagField().
		Key("head").
		Title(pad("Head Branch")).
		Placeholder("Select branch...").
		Options(branchOpts...).
		MaxTags(1).
		LabelFunc(formatBranchLabel).
		Value(&f.data.Head)
	fields = append(fields, f.baseField, f.headField)

	reviewerOpts := buildContributorOptions(f.contributors)
	fields = append(fields, tuicore.NewTagField().
		Key("reviewers").
		Title(pad("Reviewers")).
		Placeholder("Add reviewer...").
		Options(reviewerOpts...).
		Value(&f.data.Reviewers))

	issueOpts := buildIssueOptions(f.issues)
	fields = append(fields, tuicore.NewTagField().
		Key("closes").
		Title(pad("Closes")).
		Placeholder("Add issue ref...").
		Options(issueOpts...).
		Value(&f.data.Closes))

	fields = append(fields,
		huh.NewConfirm().
			Key("draft").
			Title(pad("Draft")).
			Inline(true).
			Value(&f.data.Draft))

	f.bodyField = huh.NewText().
		Key("body").
		Title("Description").
		Placeholder("Optional description...").
		Value(&f.data.Body).
		CharLimit(4000).
		Lines(20)
	fields = append(fields,
		f.bodyField,
		tuicore.NewSubmitField(),
	)

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))

	f.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(km)
}

// AddForkBranches appends fork branch options to the base/head fields.
func (f *PRForm) AddForkBranches(forkBranches map[string][]string) {
	opts := buildBranchOptions(nil, forkBranches)
	f.baseField.AppendOptions(opts...)
	f.headField.AppendOptions(opts...)
}

// SetSize sets the form dimensions.
func (f *PRForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if f.form != nil {
		f.form.WithWidth(w).WithHeight(h - 2)
		if f.bodyField != nil {
			f.bodyField.WithHeight(max(5, h-8))
		}
	}
}

// Init initializes the form.
func (f *PRForm) Init() tea.Cmd {
	return f.form.Init()
}

// Update handles form messages.
func (f *PRForm) Update(msg tea.Msg) tea.Cmd {
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
func (f *PRForm) View() string {
	return f.form.View()
}

// Errors returns the form's current validation errors.
func (f *PRForm) Errors() []error { return f.form.Errors() }

// IsSubmitted returns true if form was submitted.
func (f *PRForm) IsSubmitted() bool {
	return f.submitted
}

// IsCancelled returns true if form was canceled.
func (f *PRForm) IsCancelled() bool {
	return f.canceled
}

// CreatePRFromForm creates a pull request from form data.
func (f *PRForm) CreatePRFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		opts := review.CreatePROptions{
			Base:      firstSliceVal(data.Base),
			Head:      firstSliceVal(data.Head),
			Reviewers: data.Reviewers,
			Closes:    data.Closes,
			Draft:     data.Draft,
		}
		result := review.CreatePR(workdir, strings.TrimSpace(data.Subject), strings.TrimSpace(data.Body), opts)
		if !result.Success {
			return PRCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRCreatedMsg{PR: result.Data}
	}
}

// UpdatePRFromForm updates an existing pull request from form data.
func (f *PRForm) UpdatePRFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	prID := f.prID
	return func() tea.Msg {
		subject := strings.TrimSpace(data.Subject)
		body := strings.TrimSpace(data.Body)
		base := firstSliceVal(data.Base)
		head := firstSliceVal(data.Head)
		reviewers := data.Reviewers
		closes := data.Closes

		draft := data.Draft
		opts := review.UpdatePROptions{
			Subject:   &subject,
			Body:      &body,
			Base:      &base,
			Head:      &head,
			Reviewers: &reviewers,
			Closes:    &closes,
			Draft:     &draft,
		}
		result := review.UpdatePR(workdir, prID, opts)
		if !result.Success {
			return PRUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRUpdatedMsg{PR: result.Data}
	}
}

// PRFormView wraps the form for integration with the TUI host.
type PRFormView struct {
	workdir    string
	form       *PRForm
	loaded     bool
	submitting bool
	width      int
	height     int
}

// NewPRFormView creates a new pull request form view.
func NewPRFormView(workdir string) *PRFormView {
	return &PRFormView{workdir: workdir}
}

// SetSize sets the view dimensions.
func (v *PRFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate loads form data asynchronously.
func (v *PRFormView) Activate(state *tuicore.State) tea.Cmd {
	v.workdir = state.Workdir
	v.loaded = false
	v.submitting = false
	v.form = nil
	return tea.Batch(loadPRFormData(state.Workdir), loadForkBranches(state.Workdir))
}

// Deactivate is called when the view is hidden.
func (v *PRFormView) Deactivate() {}

// Update handles messages.
func (v *PRFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case prFormDataMsg:
		v.form = NewPRForm(v.workdir, msg)
		v.form.SetSize(v.width, v.height)
		v.loaded = true
		return v.form.Init()
	case prForkBranchesMsg:
		if v.form != nil {
			v.form.AddForkBranches(msg.ForkBranches)
		}
		return nil
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
		return v.form.CreatePRFromForm()
	}

	return cmd
}

// Render renders the form view.
func (v *PRFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	if !v.loaded || v.form == nil {
		return wrapper.Render("Loading...", "")
	}
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	content := v.form.View()
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *PRFormView) Title() string {
	return "⑂  New Pull Request"
}

// Bindings returns keybindings for this view.
func (v *PRFormView) Bindings() []tuicore.Binding {
	return nil
}

// IsInputActive returns true (form always captures input).
func (v *PRFormView) IsInputActive() bool {
	return true
}

// PREditFormView wraps the form for editing an existing pull request.
type PREditFormView struct {
	workdir    string
	prID       string
	form       *PRForm
	pr         *review.PullRequest
	loaded     bool
	submitting bool
	width      int
	height     int
}

// NewPREditFormView creates a new pull request edit form view.
func NewPREditFormView(workdir string) *PREditFormView {
	return &PREditFormView{workdir: workdir}
}

// SetSize sets the view dimensions.
func (v *PREditFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate loads the pull request and initializes the form.
func (v *PREditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.workdir = state.Workdir
	v.prID = state.Router.Location().Param("prID")
	v.loaded = false
	v.form = nil
	return tea.Batch(v.loadPR(), loadForkBranches(v.workdir))
}

func (v *PREditFormView) loadPR() tea.Cmd {
	prID := v.prID
	return func() tea.Msg {
		result := review.GetPR(prID)
		if !result.Success {
			return prEditFormLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		pr := result.Data
		return prEditFormLoadedMsg{PR: &pr}
	}
}

// prEditFormLoadedMsg signals that the pull request for editing has been loaded.
type prEditFormLoadedMsg struct {
	PR  *review.PullRequest
	Err error
}

// Deactivate is called when the view is hidden.
func (v *PREditFormView) Deactivate() {}

// Update handles messages.
func (v *PREditFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case prEditFormLoadedMsg:
		if msg.Err != nil {
			return func() tea.Msg {
				return tuicore.NavigateMsg{Action: tuicore.NavBack}
			}
		}
		v.pr = msg.PR
		return loadPRFormData(v.workdir)
	case prFormDataMsg:
		if v.pr == nil {
			return nil
		}
		v.form = NewPREditForm(v.workdir, *v.pr, msg)
		v.form.SetSize(v.width, v.height)
		v.loaded = true
		return v.form.Init()
	case prForkBranchesMsg:
		if v.form != nil {
			v.form.AddForkBranches(msg.ForkBranches)
		}
		return nil
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
		return v.form.UpdatePRFromForm()
	}

	return cmd
}

// Render renders the edit form view.
func (v *PREditFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading pull request..."
	} else if v.form == nil {
		content = tuicore.Dim.Render("  Pull request not found")
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
func (v *PREditFormView) Title() string {
	if v.pr != nil {
		return fmt.Sprintf("⑂  Edit: %s", v.pr.Subject)
	}
	return "⑂  Edit Pull Request"
}

// Bindings returns keybindings for this view.
func (v *PREditFormView) Bindings() []tuicore.Binding {
	return nil
}

// IsInputActive returns true (form always captures input).
func (v *PREditFormView) IsInputActive() bool {
	return true
}

// firstSliceVal returns the first element of a slice or empty string.
func firstSliceVal(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// buildIssueOptions converts open issues to TagField options.
func buildIssueOptions(issues []pm.Issue) []tuicore.TagOption {
	opts := make([]tuicore.TagOption, 0, len(issues))
	for _, issue := range issues {
		opts = append(opts, tuicore.TagOption{Label: issue.Subject, Value: issue.ID})
	}
	return opts
}

// buildBranchOptions converts local and fork branch names to TagField options.
func buildBranchOptions(branches []string, forkBranches map[string][]string) []tuicore.TagOption {
	opts := make([]tuicore.TagOption, 0, len(branches))
	for _, b := range branches {
		opts = append(opts, tuicore.TagOption{Label: b, Value: b})
	}
	for forkURL, fbs := range forkBranches {
		for _, b := range fbs {
			ref := forkURL + "#branch:" + b
			opts = append(opts, tuicore.TagOption{Label: ref, Value: ref})
		}
	}
	return opts
}

// formatBranchLabel formats a branch ref value for display in tag chips.
func formatBranchLabel(ref string) string {
	parsed := protocol.ParseRef(ref)
	if parsed.Type == protocol.RefTypeBranch && parsed.Repository == "" {
		return parsed.Value
	}
	return ref
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
