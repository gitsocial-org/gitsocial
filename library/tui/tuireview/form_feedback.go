// form_feedback.go - Feedback creation form for pull request reviews
package tuireview

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// FeedbackFormData holds the form field values.
type FeedbackFormData struct {
	ReviewState string
	Comment     string
	File        string
	OldLine     int
	NewLine     int
	Commit      string
}

// FeedbackForm wraps a Huh form for feedback creation.
type FeedbackForm struct {
	workdir      string
	prID         string
	form         *huh.Form
	commentField *huh.Text
	data         FeedbackFormData
	width        int
	height       int
	submitted    bool
	canceled     bool
}

// NewFeedbackForm creates a new feedback form with optional pre-selected state.
func NewFeedbackForm(workdir, prID, initialState string, data FeedbackFormData) *FeedbackForm {
	if data.File == "" {
		data.ReviewState = initialState
	}
	f := &FeedbackForm{
		workdir: workdir,
		prID:    prID,
		data:    data,
	}
	f.buildForm()
	return f
}

// buildForm constructs the Huh form.
func (f *FeedbackForm) buildForm() {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))

	pad := tuicore.PadLabel
	var fields []huh.Field
	if f.data.File == "" {
		// PR-level review: pick a verdict
		fields = append(fields, tuicore.NewCycleField().
			Key("state").
			Title(pad("Review Action")).
			Options(
				tuicore.CycleOption{Label: "Approve", Value: string(review.ReviewStateApproved)},
				tuicore.CycleOption{Label: "Request Changes", Value: string(review.ReviewStateChangesRequested)},
			).
			Value(&f.data.ReviewState))
		f.commentField = huh.NewText().
			Key("comment").
			Title("Comment").
			Placeholder("Optional review comment...").
			Value(&f.data.Comment).
			CharLimit(4000).
			Lines(15)
		fields = append(fields, f.commentField)
	} else {
		// Inline code comment: no verdict needed
		f.commentField = huh.NewText().
			Key("comment").
			Title("Comment").
			Placeholder("Code comment...").
			Value(&f.data.Comment).
			CharLimit(4000).
			Lines(18)
		fields = append(fields, f.commentField)
	}
	fields = append(fields, tuicore.NewSubmitField())

	f.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(km)
}

// SetSize sets the form dimensions.
func (f *FeedbackForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if f.form != nil {
		f.form.WithWidth(w).WithHeight(h - 2)
		if f.commentField != nil {
			f.commentField.WithHeight(max(5, h-8))
		}
	}
}

// Init initializes the form.
func (f *FeedbackForm) Init() tea.Cmd {
	return f.form.Init()
}

// Update handles form messages.
func (f *FeedbackForm) Update(msg tea.Msg) tea.Cmd {
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
func (f *FeedbackForm) View() string {
	return f.form.View()
}

// Errors returns the form's current validation errors.
func (f *FeedbackForm) Errors() []error { return f.form.Errors() }

// IsSubmitted returns true if form was submitted.
func (f *FeedbackForm) IsSubmitted() bool {
	return f.submitted
}

// IsCancelled returns true if form was canceled.
func (f *FeedbackForm) IsCancelled() bool {
	return f.canceled
}

// CreateFeedbackFromForm creates feedback from form data.
func (f *FeedbackForm) CreateFeedbackFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	prID := f.prID
	return func() tea.Msg {
		content := strings.TrimSpace(data.Comment)
		opts := review.CreateFeedbackOptions{
			PullRequest: prID,
			ReviewState: review.ReviewState(data.ReviewState),
			File:        data.File,
			OldLine:     data.OldLine,
			NewLine:     data.NewLine,
			Commit:      data.Commit,
		}
		// Comment-only feedback requires content
		if opts.ReviewState == "" && content == "" {
			return FeedbackCreatedMsg{Err: fmt.Errorf("comment cannot be empty")}
		}
		result := review.CreateFeedback(workdir, content, opts)
		if !result.Success {
			return FeedbackCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return FeedbackCreatedMsg{Feedback: result.Data, PRID: prID}
	}
}

// FeedbackFormView wraps the form for integration with the TUI host.
type FeedbackFormView struct {
	workdir    string
	form       *FeedbackForm
	submitting bool
	width      int
	height     int
}

// NewFeedbackFormView creates a new feedback form view.
func NewFeedbackFormView(workdir string) *FeedbackFormView {
	return &FeedbackFormView{workdir: workdir}
}

// SetSize sets the view dimensions.
func (v *FeedbackFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate initializes the form view from route params.
func (v *FeedbackFormView) Activate(state *tuicore.State) tea.Cmd {
	loc := state.Router.Location()
	prID := loc.Param("prID")
	initialState := loc.Param("state")
	var data FeedbackFormData
	if file := loc.Param("file"); file != "" {
		data.File = file
		if s := loc.Param("oldLine"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				data.OldLine = n
			}
		}
		if s := loc.Param("newLine"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				data.NewLine = n
			}
		}
		data.Commit = loc.Param("commit")
	}
	v.form = NewFeedbackForm(v.workdir, prID, initialState, data)
	v.form.SetSize(v.width, v.height)
	v.submitting = false
	return v.form.Init()
}

// Deactivate is called when the view is hidden.
func (v *FeedbackFormView) Deactivate() {}

// Update handles messages.
func (v *FeedbackFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if v.form == nil {
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
		return v.form.CreateFeedbackFromForm()
	}
	return cmd
}

// Render renders the form view.
func (v *FeedbackFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	if v.form == nil {
		return wrapper.Render("Loading...", "")
	}
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	var content string
	// Show file/line context header when providing inline feedback
	if v.form.data.File != "" {
		location := v.form.data.File
		if v.form.data.NewLine > 0 {
			location += fmt.Sprintf(":%d", v.form.data.NewLine)
		} else if v.form.data.OldLine > 0 {
			location += fmt.Sprintf(":%d (old)", v.form.data.OldLine)
		}
		content = tuicore.Dim.Render("Commenting on: "+location) + "\n\n"
	}
	content += v.form.View()
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *FeedbackFormView) Title() string {
	return "⑂  Review Feedback"
}

// Bindings returns keybindings for this view.
func (v *FeedbackFormView) Bindings() []tuicore.Binding {
	return nil
}

// IsInputActive returns true (form always captures input).
func (v *FeedbackFormView) IsInputActive() bool {
	return true
}
