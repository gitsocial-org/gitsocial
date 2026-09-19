// form_memo.go - Memo create/edit form (subject, body, labels, tier)
package tuimemo

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// memoFormData holds the editable fields. Tier only applies in create mode.
type memoFormData struct {
	Subject string
	Body    string
	Labels  []string
	Tier    string // "session" | "personal" | "project" (create mode only)
}

// memoForm wraps a Huh form for memo create/edit. memoID is empty in create mode.
type memoForm struct {
	tuicore.FormBase
	workdir       string
	memoID        string
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          memoFormData
	width         int
	height        int
}

// newMemoForm builds an edit form prefilled from the given memo.
func newMemoForm(workdir, memoID string, prefill memoFormData) *memoForm {
	f := &memoForm{
		workdir: workdir,
		memoID:  memoID,
		data:    prefill,
	}
	f.buildForm()
	return f
}

// newMemoCreateForm builds a blank create form with the given default tier.
func newMemoCreateForm(workdir, defaultTier string) *memoForm {
	if defaultTier == "" {
		defaultTier = string(memo.TierSession)
	}
	f := &memoForm{
		workdir: workdir,
		data:    memoFormData{Tier: defaultTier},
	}
	f.buildForm()
	return f
}

// isCreateMode reports whether the form creates a memo rather than editing one.
func (f *memoForm) isCreateMode() bool { return f.memoID == "" }

// buildForm assembles the memo form's fields and its huh form.
func (f *memoForm) buildForm() {
	pad := tuicore.PadLabel

	subjectField := huh.NewInput().
		Key("subject").
		Title(pad(tuicore.RequiredLabel("Subject"))).
		Placeholder("Memo subject...").
		Value(&f.data.Subject).
		CharLimit(200).
		Inline(true).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("subject is required")
			}
			return nil
		})

	f.bodyField = huh.NewText().
		Key("body").
		Title("Description").
		Placeholder("Optional description (markdown)...").
		Value(&f.data.Body).
		CharLimit(8000).
		Lines(15)

	labelsField := tuicore.NewLabelsField(&f.data.Labels, "kind/policy, priority/high, expires/2025-12-31")

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
	fields = append(fields, f.bodyField, labelsField, tuicore.NewSubmitField())

	f.bodyOtherRows = len(fields)
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
}

// SetSize updates the form dimensions.
func (f *memoForm) SetSize(w, h int) {
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
func (f *memoForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *memoForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *memoForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *memoForm) Reset() { f.buildForm() }

// submitEdit calls memo.EditMemo with the form's data.
func (f *memoForm) submitEdit() tea.Cmd {
	data := f.data
	workdir := f.workdir
	memoID := f.memoID
	return func() tea.Msg {
		subject := strings.TrimSpace(data.Subject)
		if subject == "" {
			return memoEditedMsg{Err: fmt.Errorf("subject cannot be empty")}
		}
		body := data.Body
		labels := data.Labels
		opts := memo.EditMemoOptions{
			Subject: &subject,
			Body:    &body,
			Labels:  &labels,
		}
		res := memo.EditMemo(workdir, memoID, opts)
		if !res.Success {
			return memoEditedMsg{Err: fmt.Errorf("%s", res.Error.Text())}
		}
		return memoEditedMsg{Memo: res.Data, MemoID: memoID}
	}
}

// submitCreate calls memo.CreateMemo with the form's data.
func (f *memoForm) submitCreate() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		subject := strings.TrimSpace(data.Subject)
		if subject == "" {
			return memoCreatedMsg{Err: fmt.Errorf("subject cannot be empty")}
		}
		tier := memo.Tier(data.Tier)
		if tier == "" {
			tier = memo.TierSession
		}
		res := memo.CreateMemo(workdir, subject, data.Body, memo.CreateMemoOptions{
			Tier:   tier,
			Labels: data.Labels,
		})
		if !res.Success {
			return memoCreatedMsg{Err: fmt.Errorf("%s", res.Error.Text())}
		}
		return memoCreatedMsg{Memo: res.Data}
	}
}

// memoFormView wraps the form as a host view.
type memoFormView struct {
	tuicore.FormViewBase
	workdir string
}

// newMemoFormView creates a memo form view.
func newMemoFormView(workdir string) *memoFormView { return &memoFormView{workdir: workdir} }

// Activate loads the memo and builds the form.
func (v *memoFormView) Activate(state *tuicore.State) tea.Cmd {
	memoID := state.Router.Location().Params["memoID"]
	if memoID == "" {
		return nil
	}
	workspaceURL := gitmsg.ResolveRepoURL(v.workdir)
	res := memo.GetSingleMemo(memoID, workspaceURL, memo.ListInherits(v.workdir))
	if !res.Success {
		v.DetachForm()
		return nil
	}
	m := res.Data
	prefill := memoFormData{
		Subject: m.Subject,
		Body:    m.Body,
		Labels:  append([]string(nil), m.Labels...),
	}
	form := newMemoForm(v.workdir, memoID, prefill)
	v.AttachForm(form)
	return form.Init()
}

// Update dispatches to FormViewBase with the form-specific edit submit.
func (v *memoFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(memoEditedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*memoForm); ok {
			return form.submitEdit()
		}
		return nil
	})
}

// Render renders the form panel.
func (v *memoFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the panel header.
func (v *memoFormView) Title() string { return "☞  Edit Memo" }

// memoEditedMsg is dispatched after submitting an edit.
type memoEditedMsg struct {
	Memo   memo.Memo
	MemoID string
	Err    error
}

// memoCreatedMsg is dispatched after submitting a create.
type memoCreatedMsg struct {
	Memo memo.Memo
	Err  error
}

// memoCreateFormView wraps a blank memo form as a host view.
type memoCreateFormView struct {
	tuicore.FormViewBase
	workdir string
}

// newMemoCreateFormView creates a memo create form view.
func newMemoCreateFormView(workdir string) *memoCreateFormView {
	return &memoCreateFormView{workdir: workdir}
}

// Activate builds a blank form using the optional tier param from the route.
func (v *memoCreateFormView) Activate(state *tuicore.State) tea.Cmd {
	defaultTier := state.Router.Location().Params["tier"]
	form := newMemoCreateForm(v.workdir, defaultTier)
	v.AttachForm(form)
	return form.Init()
}

// Update dispatches to FormViewBase with the form-specific create submit.
func (v *memoCreateFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(memoCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*memoForm); ok {
			return form.submitCreate()
		}
		return nil
	})
}

// Render renders the form panel.
func (v *memoCreateFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the panel header.
func (v *memoCreateFormView) Title() string { return "☞  New Memo" }
