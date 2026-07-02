// form_pr.go - Pull request creation and edit form using Huh
package tuireview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// PRFormData holds the form field values.
type PRFormData struct {
	Subject   string
	Body      string
	Base      []string
	Head      []string
	Reviewers []string
	Closes    []string
	DependsOn []string
	Labels    []string
	Draft     bool
}

// PRForm wraps a Huh form for pull request creation and editing.
type PRForm struct {
	tuicore.FormBase
	workdir       string
	prID          string
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	baseField     *tuicore.TagField
	headField     *tuicore.TagField
	data          PRFormData
	contributors  []cache.Contributor
	branches      []string
	issues        []pm.Issue
	openPRs       []review.PullRequest
	width         int
	height        int
}

// prFormDataMsg carries async-loaded data for PR form construction.
type prFormDataMsg struct {
	Contributors []cache.Contributor
	Branches     []string
	Issues       []pm.Issue
	OpenPRs      []review.PullRequest
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
		var openPRs []review.PullRequest
		branch := gitmsg.GetExtBranch(workdir, "review")
		if res := review.GetPullRequests(repoURL, branch, []string{"open"}, "", 100); res.Success {
			openPRs = res.Data
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
			OpenPRs:      openPRs,
			DefaultBase:  defaultBase,
			CurrentHead:  currentHead,
		}
	}
}

// loadForkBranches loads fork branches asynchronously. Tries ls-remote first
// (fresh, authoritative when online); on network failure falls back to the
// review_branch_observations cache so the dropdown still has the branches
// referenced by open PRs.
func loadForkBranches(workdir string) tea.Cmd {
	return func() tea.Msg {
		forkBranches := make(map[string][]string)
		for _, forkURL := range review.GetForks(workdir) {
			if fb, err := git.ListRemoteBranches(workdir, forkURL); err == nil && len(fb) > 0 {
				forkBranches[forkURL] = fb
				continue
			}
			if fb := review.LocalKnownBranches(forkURL); len(fb) > 0 {
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
		openPRs:      data.OpenPRs,
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

	dependsOn := make([]string, len(pr.DependsOn))
	copy(dependsOn, pr.DependsOn)

	labels := make([]string, len(pr.Labels))
	copy(labels, pr.Labels)

	f := &PRForm{
		workdir:      workdir,
		prID:         pr.ID,
		contributors: data.Contributors,
		branches:     data.Branches,
		issues:       data.Issues,
		openPRs:      data.OpenPRs,
		data: PRFormData{
			Subject:   pr.Subject,
			Body:      pr.Body,
			Base:      base,
			Head:      head,
			Reviewers: reviewers,
			Closes:    closes,
			DependsOn: dependsOn,
			Labels:    labels,
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

	reviewerOpts := tuicore.ContributorOptions(f.contributors)
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

	dependsOnOpts := buildPROptions(f.openPRs, f.prID)
	fields = append(fields, tuicore.NewTagField().
		Key("depends-on").
		Title(pad("Depends on")).
		Placeholder("Add parent PR for stacking...").
		Options(dependsOnOpts...).
		Value(&f.data.DependsOn))

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
		tuicore.NewLabelsField(&f.data.Labels, ""),
		tuicore.NewSubmitField(),
	)

	f.bodyOtherRows = len(fields)
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
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
	if form := f.FormPtr(); form != nil {
		form.WithWidth(w).WithHeight(h + 1)
		if f.bodyField != nil {
			f.bodyField.WithHeight(tuicore.BodyHeight(h, f.bodyOtherRows))
		}
	}
}

// Update delegates the standard form lifecycle to FormBase.
func (f *PRForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *PRForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *PRForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *PRForm) Reset() { f.buildForm() }

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
			DependsOn: data.DependsOn,
			Labels:    data.Labels,
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
		dependsOn := data.DependsOn
		labels := data.Labels

		draft := data.Draft
		opts := review.UpdatePROptions{
			Subject:   &subject,
			Body:      &body,
			Base:      &base,
			Head:      &head,
			Reviewers: &reviewers,
			Closes:    &closes,
			DependsOn: &dependsOn,
			Labels:    &labels,
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
	tuicore.FormViewBase
	workdir string
	loaded  bool
}

// NewPRFormView creates a new pull request form view.
func NewPRFormView(workdir string) *PRFormView {
	return &PRFormView{workdir: workdir}
}

// Activate loads form data asynchronously.
func (v *PRFormView) Activate(state *tuicore.State) tea.Cmd {
	v.workdir = state.Workdir
	v.loaded = false
	v.DetachForm()
	return tea.Batch(loadPRFormData(state.Workdir), loadForkBranches(state.Workdir))
}

// Update handles messages.
func (v *PRFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case prFormDataMsg:
		form := NewPRForm(v.workdir, msg)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	case prForkBranchesMsg:
		if form, ok := v.CurrentForm().(*PRForm); ok {
			form.AddForkBranches(msg.ForkBranches)
		}
		return nil
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(PRCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*PRForm); ok {
			return form.CreatePRFromForm()
		}
		return nil
	})
}

// Render renders the form view.
func (v *PRFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		return wrapper.Render("Loading...", "")
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *PRFormView) Title() string { return "⑂  New Pull Request" }

// PREditFormView wraps the form for editing an existing pull request.
type PREditFormView struct {
	tuicore.FormViewBase
	workdir string
	prID    string
	pr      *review.PullRequest
	loaded  bool
}

// NewPREditFormView creates a new pull request edit form view.
func NewPREditFormView(workdir string) *PREditFormView {
	return &PREditFormView{workdir: workdir}
}

// Activate loads the pull request and initializes the form.
func (v *PREditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.workdir = state.Workdir
	v.prID = state.Router.Location().Param("prID")
	v.loaded = false
	v.DetachForm()
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
		form := NewPREditForm(v.workdir, *v.pr, msg)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	case prForkBranchesMsg:
		if form, ok := v.CurrentForm().(*PRForm); ok {
			form.AddForkBranches(msg.ForkBranches)
		}
		return nil
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(PRUpdatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*PRForm); ok {
			return form.UpdatePRFromForm()
		}
		return nil
	})
}

// Render renders the edit form view.
func (v *PREditFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		footer := tuicore.FormFooter(true, nil)
		return wrapper.Render("Loading pull request...", footer)
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *PREditFormView) Title() string {
	if v.pr != nil {
		return fmt.Sprintf("⑂  Edit: %s", v.pr.Subject)
	}
	return "⑂  Edit Pull Request"
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

// buildPROptions converts open PRs to TagField options, excluding the PR being edited.
func buildPROptions(prs []review.PullRequest, excludeID string) []tuicore.TagOption {
	opts := make([]tuicore.TagOption, 0, len(prs))
	for _, pr := range prs {
		if pr.ID == excludeID {
			continue
		}
		opts = append(opts, tuicore.TagOption{Label: pr.Subject, Value: pr.ID})
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
