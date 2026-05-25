// form_memo.go - Memo create/edit form (subject, body, labels, tier)
package tuimemo

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// MemoFormData holds the editable fields. Tier only applies in create mode.
type MemoFormData struct {
	Subject string
	Body    string
	Labels  string // comma-separated for input ergonomics
	Tier    string // "session" | "personal" | "project" (create mode only)
}

// MemoForm wraps a Huh form for memo create/edit. memoID is empty in create mode.
type MemoForm struct {
	workdir   string
	memoID    string
	form      *huh.Form
	bodyField *huh.Text
	data      MemoFormData
	width     int
	height    int
	submitted bool
	canceled  bool
}

// NewMemoForm builds an edit form prefilled from the given memo.
func NewMemoForm(workdir, memoID string, prefill MemoFormData) *MemoForm {
	f := &MemoForm{
		workdir: workdir,
		memoID:  memoID,
		data:    prefill,
	}
	f.buildForm()
	return f
}

// NewMemoCreateForm builds a blank create form with the given default tier.
func NewMemoCreateForm(workdir, defaultTier string) *MemoForm {
	if defaultTier == "" {
		defaultTier = string(memo.TierSession)
	}
	f := &MemoForm{
		workdir: workdir,
		data:    MemoFormData{Tier: defaultTier},
	}
	f.buildForm()
	return f
}

func (f *MemoForm) isCreateMode() bool { return f.memoID == "" }

func (f *MemoForm) buildForm() {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))

	pad := tuicore.PadLabel

	subjectField := huh.NewInput().
		Key("subject").
		Title(pad("Subject")).
		Placeholder("Memo subject...").
		Value(&f.data.Subject).
		CharLimit(200)

	f.bodyField = huh.NewText().
		Key("body").
		Title("Body").
		Placeholder("Optional body (markdown)...").
		Value(&f.data.Body).
		CharLimit(8000).
		Lines(15)

	labelsField := huh.NewInput().
		Key("labels").
		Title(pad("Labels")).
		Placeholder("kind/policy,priority/high,topic/cache").
		Value(&f.data.Labels).
		CharLimit(500)

	fields := []huh.Field{subjectField}
	if f.isCreateMode() {
		fields = append(fields, tuicore.NewCycleField().
			Key("tier").
			Title(pad("Tier")).
			Options(
				tuicore.CycleOption{Label: "Session", Value: string(memo.TierSession)},
				tuicore.CycleOption{Label: "Personal", Value: string(memo.TierPersonal)},
				tuicore.CycleOption{Label: "Project", Value: string(memo.TierProject)},
			).
			Value(&f.data.Tier))
	}
	fields = append(fields, labelsField, f.bodyField, tuicore.NewSubmitField())

	f.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(km)
}

// SetSize updates the form dimensions.
func (f *MemoForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if f.form != nil {
		f.form.WithWidth(w).WithHeight(h - 2)
		if f.bodyField != nil {
			f.bodyField.WithHeight(max(5, h-10))
		}
	}
}

// Init kicks off the form lifecycle.
func (f *MemoForm) Init() tea.Cmd { return f.form.Init() }

// Update routes input events to the form.
func (f *MemoForm) Update(msg tea.Msg) tea.Cmd {
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
func (f *MemoForm) View() string { return f.form.View() }

// Errors returns the form's validation errors.
func (f *MemoForm) Errors() []error { return f.form.Errors() }

// IsSubmitted reports whether the form was submitted.
func (f *MemoForm) IsSubmitted() bool { return f.submitted }

// IsCancelled reports whether the form was canceled.
func (f *MemoForm) IsCancelled() bool { return f.canceled }

// SubmitEdit calls memo.EditMemo with the form's data.
func (f *MemoForm) SubmitEdit() tea.Cmd {
	data := f.data
	workdir := f.workdir
	memoID := f.memoID
	return func() tea.Msg {
		subject := strings.TrimSpace(data.Subject)
		if subject == "" {
			return MemoEditedMsg{Err: fmt.Errorf("subject cannot be empty")}
		}
		body := data.Body
		labels := splitLabels(data.Labels)
		opts := memo.EditMemoOptions{
			Subject: &subject,
			Body:    &body,
			Labels:  &labels,
		}
		res := memo.EditMemo(workdir, memoID, opts)
		if !res.Success {
			return MemoEditedMsg{Err: fmt.Errorf("%s", res.Error.Message)}
		}
		return MemoEditedMsg{Memo: res.Data, MemoID: memoID}
	}
}

// SubmitCreate calls memo.CreateMemo with the form's data.
func (f *MemoForm) SubmitCreate() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		subject := strings.TrimSpace(data.Subject)
		if subject == "" {
			return MemoCreatedMsg{Err: fmt.Errorf("subject cannot be empty")}
		}
		tier := memo.Tier(data.Tier)
		if tier == "" {
			tier = memo.TierSession
		}
		res := memo.CreateMemo(workdir, subject, data.Body, memo.CreateMemoOptions{
			Tier:   tier,
			Labels: splitLabels(data.Labels),
		})
		if !res.Success {
			return MemoCreatedMsg{Err: fmt.Errorf("%s", res.Error.Message)}
		}
		return MemoCreatedMsg{Memo: res.Data}
	}
}

// MemoFormView wraps the form as a host view.
type MemoFormView struct {
	workdir    string
	form       *MemoForm
	submitting bool
	width      int
	height     int
}

// NewMemoFormView creates a memo form view.
func NewMemoFormView(workdir string) *MemoFormView { return &MemoFormView{workdir: workdir} }

// SetSize sets the form view dimensions.
func (v *MemoFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate loads the memo and builds the form.
func (v *MemoFormView) Activate(state *tuicore.State) tea.Cmd {
	memoID := state.Router.Location().Params["memoID"]
	if memoID == "" {
		return nil
	}
	workspaceURL := gitmsg.ResolveRepoURL(v.workdir)
	res := memo.GetSingleMemo(memoID, workspaceURL, memo.ListInherits(v.workdir))
	if !res.Success {
		v.form = nil
		return nil
	}
	m := res.Data
	prefill := MemoFormData{
		Subject: m.Subject,
		Body:    m.Body,
		Labels:  strings.Join(m.Labels, ","),
	}
	v.form = NewMemoForm(v.workdir, memoID, prefill)
	v.form.SetSize(v.width, v.height)
	v.submitting = false
	return v.form.Init()
}

// Deactivate is called when the view is hidden.
func (v *MemoFormView) Deactivate() {}

// Update handles form lifecycle.
func (v *MemoFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if v.form == nil {
		return nil
	}
	cmd := v.form.Update(msg)
	if v.form.IsCancelled() {
		return func() tea.Msg { return tuicore.NavigateMsg{Action: tuicore.NavBack} }
	}
	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return v.form.SubmitEdit()
	}
	return cmd
}

// Render renders the form panel.
func (v *MemoFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	if v.form == nil {
		return wrapper.Render(tuicore.Dim.Render("memo not found"), "")
	}
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(v.form.View(), footer)
}

// Title returns the panel header.
func (v *MemoFormView) Title() string { return "☞  Edit Memo" }

// Bindings returns view bindings (none — form captures input).
func (v *MemoFormView) Bindings() []tuicore.Binding { return nil }

// IsInputActive always reports true so global keys defer to the form.
func (v *MemoFormView) IsInputActive() bool { return true }

// MemoEditedMsg is dispatched after submitting an edit.
type MemoEditedMsg struct {
	Memo   memo.Memo
	MemoID string
	Err    error
}

// MemoCreatedMsg is dispatched after submitting a create.
type MemoCreatedMsg struct {
	Memo memo.Memo
	Err  error
}

// MemoCreateFormView wraps a blank memo form as a host view.
type MemoCreateFormView struct {
	workdir    string
	form       *MemoForm
	submitting bool
	width      int
	height     int
}

// NewMemoCreateFormView creates a memo create form view.
func NewMemoCreateFormView(workdir string) *MemoCreateFormView {
	return &MemoCreateFormView{workdir: workdir}
}

// SetSize sets the form view dimensions.
func (v *MemoCreateFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate builds a blank form using the optional tier param from the route.
func (v *MemoCreateFormView) Activate(state *tuicore.State) tea.Cmd {
	defaultTier := state.Router.Location().Params["tier"]
	v.form = NewMemoCreateForm(v.workdir, defaultTier)
	v.form.SetSize(v.width, v.height)
	v.submitting = false
	return v.form.Init()
}

// Deactivate is called when the view is hidden.
func (v *MemoCreateFormView) Deactivate() {}

// Update handles form lifecycle.
func (v *MemoCreateFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if v.form == nil {
		return nil
	}
	cmd := v.form.Update(msg)
	if v.form.IsCancelled() {
		return func() tea.Msg { return tuicore.NavigateMsg{Action: tuicore.NavBack} }
	}
	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return v.form.SubmitCreate()
	}
	return cmd
}

// Render renders the form panel.
func (v *MemoCreateFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	if v.form == nil {
		return wrapper.Render("", "")
	}
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(v.form.View(), footer)
}

// Title returns the panel header.
func (v *MemoCreateFormView) Title() string { return "☞  New Memo" }

// Bindings returns view bindings (none — form captures input).
func (v *MemoCreateFormView) Bindings() []tuicore.Binding { return nil }

// IsInputActive always reports true so global keys defer to the form.
func (v *MemoCreateFormView) IsInputActive() bool { return true }

// splitLabels parses a comma-separated label string into a clean slice.
func splitLabels(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
