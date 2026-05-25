// component_form_base.go - Shared lifecycle plumbing for huh-backed forms and form views
package tuicore

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// FormBase plumbs esc-cancel and StateCompleted-detection for a huh form.
// Embed in form types; build the form, then call SetForm with the result.
type FormBase struct {
	form      *huh.Form
	submitted bool
	canceled  bool
}

// SetForm stores the underlying huh form. Call after constructing it.
func (f *FormBase) SetForm(form *huh.Form) { f.form = form }

// FormPtr returns the underlying huh form (nil until SetForm).
func (f *FormBase) FormPtr() *huh.Form { return f.form }

// Init starts the form's command lifecycle.
func (f *FormBase) Init() tea.Cmd { return f.form.Init() }

// View renders the form.
func (f *FormBase) View() string { return f.form.View() }

// Errors returns the form's current validation errors.
func (f *FormBase) Errors() []error {
	if f.form == nil {
		return nil
	}
	return f.form.Errors()
}

// IsSubmitted reports whether the form reached StateCompleted (submit pressed).
func (f *FormBase) IsSubmitted() bool { return f.submitted }

// IsCancelled reports whether esc was pressed.
func (f *FormBase) IsCancelled() bool { return f.canceled }

// ResetSubmit clears the submitted flag. Use after surfacing a submit error so
// the form can be resubmitted without remounting.
func (f *FormBase) ResetSubmit() { f.submitted = false }

// UpdateForm processes the message: esc → canceled, otherwise forward to the
// underlying form and flip submitted on StateCompleted. Returns the form's cmd.
func (f *FormBase) UpdateForm(msg tea.Msg) tea.Cmd {
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

// FormLike is the minimum surface a form needs to expose so FormViewBase can
// drive it. Satisfied by anything embedding FormBase plus a SetSize method.
type FormLike interface {
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	View() string
	Errors() []error
	IsCancelled() bool
	IsSubmitted() bool
	SetSize(w, h int)
}

// FormViewBase plumbs the standard "host view that wraps a form" lifecycle:
// SetSize cascade, Update cancel/submit dispatch, Render with FormFooter, plus
// the boilerplate methods (Bindings/IsInputActive/Deactivate) every form view
// implements identically.
type FormViewBase struct {
	form       FormLike
	submitting bool
	width      int
	height     int
}

// AttachForm sets the active form. Call from Activate after constructing it.
func (v *FormViewBase) AttachForm(f FormLike) {
	v.form = f
	v.submitting = false
	if f != nil {
		f.SetSize(v.width, v.height)
	}
}

// DetachForm clears the active form. Use when load fails.
func (v *FormViewBase) DetachForm() {
	v.form = nil
	v.submitting = false
}

// CurrentForm returns the active form (nil if not loaded).
func (v *FormViewBase) CurrentForm() FormLike { return v.form }

// SetSize cascades dimensions to the active form.
func (v *FormViewBase) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// UpdateForm runs the standard view-level lifecycle: cancel → NavBack, submit
// → invoke submit (once). Callers pass the form-specific submit function.
//
// As a side benefit, intercepts ctrl+e on any form implementing
// EditorEscapeForm to suspend into $EDITOR, and intercepts FormEditorReturnMsg
// to write back the edited content.
func (v *FormViewBase) UpdateForm(msg tea.Msg, submit func() tea.Cmd) tea.Cmd {
	if v.form == nil {
		return nil
	}
	if m, ok := msg.(FormEditorReturnMsg); ok {
		if m.Err == nil {
			if ef, ok := v.form.(EditorEscapeForm); ok {
				ef.SetBody(m.Content)
			}
		}
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && keyMsg.String() == "ctrl+e" {
		if ef, ok := v.form.(EditorEscapeForm); ok {
			return OpenFormEditor(ef.Body())
		}
	}
	cmd := v.form.Update(msg)
	if v.form.IsCancelled() {
		return func() tea.Msg { return NavigateMsg{Action: NavBack} }
	}
	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		if submit != nil {
			return submit()
		}
	}
	return cmd
}

// FormResetter is implemented by forms that can rebuild themselves to clear
// huh-internal state (e.g. StateCompleted) while preserving the user's typed
// data. Implementations typically just call their buildForm equivalent.
type FormResetter interface {
	Reset()
}

// ClearSubmitting resets the submitting flag after a backend error so the user
// can fix input and resubmit without remounting the form. If the form
// implements FormResetter, it also rebuilds the underlying huh form so the
// next keystroke doesn't immediately re-fire StateCompleted.
func (v *FormViewBase) ClearSubmitting() {
	v.submitting = false
	if v.form != nil {
		if fb, ok := v.form.(interface{ ResetSubmit() }); ok {
			fb.ResetSubmit()
		}
		if r, ok := v.form.(FormResetter); ok {
			r.Reset()
		}
	}
}

// RenderForm wraps the form output in the standard view wrapper plus FormFooter.
// Forms whose body implements EditorEscapeForm advertise the ctrl+e $EDITOR
// escape-hatch via FormFooter.
func (v *FormViewBase) RenderForm(state *State) string {
	wrapper := NewViewWrapper(state)
	if v.form == nil {
		return wrapper.Render(Dim.Render("(loading)"), "")
	}
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	_, withEditor := v.form.(EditorEscapeForm)
	footer := FormFooter(withEditor, v.form.Errors())
	return wrapper.Render(v.form.View(), footer)
}

// Bindings returns the empty binding set every form view uses (form captures input).
func (v *FormViewBase) Bindings() []Binding { return nil }

// IsInputActive always reports true so global keys defer to the form.
func (v *FormViewBase) IsInputActive() bool { return true }

// Deactivate is a no-op called when the view is hidden.
func (v *FormViewBase) Deactivate() {}

// EditorEscapeForm is implemented by forms that want the FormViewBase ctrl+e
// $EDITOR escape-hatch: opens $EDITOR with the current body, then writes back
// the edited content. SetBody is expected to also rebuild the underlying huh
// form so the displayed text refreshes (huh.Text caches its value internally).
type EditorEscapeForm interface {
	Body() string
	SetBody(string)
}

// FormEditorReturnMsg is dispatched after the external $EDITOR closes.
type FormEditorReturnMsg struct {
	Content string
	Err     error
}

// Editor returns the user's preferred external editor: $EDITOR, then
// $VISUAL, then "vi" as a last-resort fallback.
func Editor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return "vi"
}

// OpenFormEditor runs $EDITOR with the given initial content. The returned
// command emits FormEditorReturnMsg when the editor closes.
func OpenFormEditor(initial string) tea.Cmd {
	f, err := os.CreateTemp("", "gitmsg-form-*.md")
	if err != nil {
		return func() tea.Msg { return FormEditorReturnMsg{Err: err} }
	}
	tmpFile := f.Name()
	f.Close()
	if err := os.WriteFile(tmpFile, []byte(initial), 0600); err != nil {
		return func() tea.Msg { return FormEditorReturnMsg{Err: err} }
	}
	c := exec.Command(Editor(), tmpFile)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpFile)
		if err != nil {
			return FormEditorReturnMsg{Err: err}
		}
		content, readErr := os.ReadFile(tmpFile)
		if readErr != nil {
			return FormEditorReturnMsg{Err: readErr}
		}
		return FormEditorReturnMsg{Content: strings.TrimRight(string(content), "\n")}
	})
}
