// form_commit.go - Commit form: a subject and a body for the commit of the index
package tuisocial

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// commitFormData holds the message fields.
type commitFormData struct {
	Subject string
	Body    string
}

// commitForm wraps a Huh form for a commit of the workspace.
type commitForm struct {
	tuicore.FormBase
	workdir   string
	files     int
	bodyField *huh.Text
	data      commitFormData
}

// newCommitForm constructs the form; files is the count the submit button names.
func newCommitForm(workdir string, files int) *commitForm {
	f := &commitForm{workdir: workdir, files: files}
	f.buildForm()
	return f
}

// buildForm constructs the underlying huh form.
func (f *commitForm) buildForm() {
	subject := huh.NewInput().
		Key("subject").
		Title(tuicore.RequiredLabel("Subject")).
		Placeholder("What this commit does...").
		Value(&f.data.Subject).
		Inline(true).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("a commit needs a subject")
			}
			return nil
		})
	f.bodyField = huh.NewText().
		Key("body").
		Title("").
		Placeholder("Why, in a few lines...").
		Value(&f.data.Body).
		CharLimit(8000).
		Lines(10)
	fields := []huh.Field{subject, f.bodyField, tuicore.NewSubmitField().Label(commitButtonLabel(f.files))}
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
}

// commitButtonLabel names the count of staged files the commit takes on the submit button.
func commitButtonLabel(files int) string {
	if files == 1 {
		return "Commit 1 file"
	}
	return fmt.Sprintf("Commit %d files", files)
}

// SetSize updates the form dimensions.
func (f *commitForm) SetSize(w, h int) {
	if form := f.FormPtr(); form != nil {
		form.WithWidth(w).WithHeight(h + 1)
		if f.bodyField != nil {
			f.bodyField.WithHeight(tuicore.BodyHeight(h, 3))
		}
	}
}

// Update delegates the standard form lifecycle to FormBase.
func (f *commitForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the body text for the editor escape.
func (f *commitForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so the text refreshes.
func (f *commitForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form and keeps the typed message.
func (f *commitForm) Reset() { f.buildForm() }

// Message returns the commit message: the subject, then the body after a blank line.
func (f *commitForm) Message() string {
	message := strings.TrimSpace(f.data.Subject)
	if body := strings.TrimSpace(f.data.Body); body != "" {
		message += "\n\n" + body
	}
	return message
}

// Submit commits the index as it is with the message; nothing is added.
func (f *commitForm) Submit() tea.Cmd {
	workdir, message := f.workdir, f.Message()
	return func() tea.Msg {
		hash, err := git.CommitIndex(workdir, message)
		return commitCreatedMsg{Hash: hash, Err: err}
	}
}

// commitFormView hosts the commit form at its route; the files param is the count the button names.
type commitFormView struct {
	tuicore.FormViewBase
	workdir string
}

// newCommitFormView creates a new commit form view.
func newCommitFormView(workdir string) *commitFormView {
	return &commitFormView{workdir: workdir}
}

// Activate builds a fresh form.
func (v *commitFormView) Activate(state *tuicore.State) tea.Cmd {
	files, _ := strconv.Atoi(state.Router.Location().Param("files"))
	form := newCommitForm(v.workdir, files)
	v.AttachForm(form)
	return form.Init()
}

// Update routes lifecycle events to the form; a failed commit frees the form for another try.
func (v *commitFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(commitCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*commitForm); ok {
			return form.Submit()
		}
		return nil
	})
}

// Render renders the form panel.
func (v *commitFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the panel header.
func (v *commitFormView) Title() string {
	return "±  Commit"
}
