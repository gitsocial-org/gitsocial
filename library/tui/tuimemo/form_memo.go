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

// MemoFormData holds the editable fields. Tier only applies in create mode.
type MemoFormData struct {
	Subject string
	Body    string
	Labels  []string
	Tier    string // "session" | "personal" | "project" (create mode only)
}

// MemoForm wraps a Huh form for memo create/edit. memoID is empty in create mode.
type MemoForm struct {
	tuicore.FormBase
	workdir       string
	memoID        string
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          MemoFormData
	width         int
	height        int
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
func (f *MemoForm) SetSize(w, h int) {
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
func (f *MemoForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *MemoForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *MemoForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *MemoForm) Reset() { f.buildForm() }

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
		labels := data.Labels
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
			Labels: data.Labels,
		})
		if !res.Success {
			return MemoCreatedMsg{Err: fmt.Errorf("%s", res.Error.Message)}
		}
		return MemoCreatedMsg{Memo: res.Data}
	}
}

// MemoFormView wraps the form as a host view.
type MemoFormView struct {
	tuicore.FormViewBase
	workdir string
}

// NewMemoFormView creates a memo form view.
func NewMemoFormView(workdir string) *MemoFormView { return &MemoFormView{workdir: workdir} }

// Activate loads the memo and builds the form.
func (v *MemoFormView) Activate(state *tuicore.State) tea.Cmd {
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
	prefill := MemoFormData{
		Subject: m.Subject,
		Body:    m.Body,
		Labels:  append([]string(nil), m.Labels...),
	}
	form := NewMemoForm(v.workdir, memoID, prefill)
	v.AttachForm(form)
	return form.Init()
}

// Update dispatches to FormViewBase with the form-specific edit submit.
func (v *MemoFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(MemoEditedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*MemoForm); ok {
			return form.SubmitEdit()
		}
		return nil
	})
}

// Render renders the form panel.
func (v *MemoFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the panel header.
func (v *MemoFormView) Title() string { return "☞  Edit Memo" }

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
	tuicore.FormViewBase
	workdir string
}

// NewMemoCreateFormView creates a memo create form view.
func NewMemoCreateFormView(workdir string) *MemoCreateFormView {
	return &MemoCreateFormView{workdir: workdir}
}

// Activate builds a blank form using the optional tier param from the route.
func (v *MemoCreateFormView) Activate(state *tuicore.State) tea.Cmd {
	defaultTier := state.Router.Location().Params["tier"]
	form := NewMemoCreateForm(v.workdir, defaultTier)
	v.AttachForm(form)
	return form.Init()
}

// Update dispatches to FormViewBase with the form-specific create submit.
func (v *MemoCreateFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(MemoCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*MemoForm); ok {
			return form.SubmitCreate()
		}
		return nil
	})
}

// Render renders the form panel.
func (v *MemoCreateFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the panel header.
func (v *MemoCreateFormView) Title() string { return "☞  New Memo" }
