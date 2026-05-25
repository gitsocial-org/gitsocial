// form_feedback.go - Feedback creation form for pull request reviews
package tuireview

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
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
	tuicore.FormBase
	workdir      string
	prID         string
	commentField *huh.Text
	data         FeedbackFormData
	width        int
	height       int
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

	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
}

// SetSize sets the form dimensions.
func (f *FeedbackForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if form := f.FormPtr(); form != nil {
		form.WithWidth(w).WithHeight(h - 2)
		if f.commentField != nil {
			f.commentField.WithHeight(tuicore.BodyHeight(h, 6))
		}
	}
}

// Update delegates the standard form lifecycle to FormBase.
func (f *FeedbackForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current comment text (for the $EDITOR escape-hatch).
func (f *FeedbackForm) Body() string { return f.data.Comment }

// SetBody writes the comment and rebuilds the form so huh.Text refreshes.
func (f *FeedbackForm) SetBody(s string) {
	f.data.Comment = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *FeedbackForm) Reset() { f.buildForm() }

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
	tuicore.FormViewBase
	workdir string
}

// NewFeedbackFormView creates a new feedback form view.
func NewFeedbackFormView(workdir string) *FeedbackFormView {
	return &FeedbackFormView{workdir: workdir}
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
	form := NewFeedbackForm(v.workdir, prID, initialState, data)
	v.AttachForm(form)
	return form.Init()
}

// Update handles messages.
func (v *FeedbackFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(FeedbackCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*FeedbackForm); ok {
			return form.CreateFeedbackFromForm()
		}
		return nil
	})
}

// Render renders the form view, prepending a file/line context header for
// inline code feedback.
func (v *FeedbackFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	form, ok := v.CurrentForm().(*FeedbackForm)
	if !ok || form == nil {
		return wrapper.Render("Loading...", "")
	}
	form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	var content string
	if form.data.File != "" {
		location := form.data.File
		switch {
		case form.data.NewLine > 0:
			location += fmt.Sprintf(":%d", form.data.NewLine)
		case form.data.OldLine > 0:
			location += fmt.Sprintf(":%d (old)", form.data.OldLine)
		}
		content = tuicore.Dim.Render("Commenting on: "+location) + "\n\n"
	}
	content += form.View()
	footer := tuicore.FormFooter(true, form.Errors())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *FeedbackFormView) Title() string { return "⑂  Review Feedback" }
