// form_post.go - Social post create/comment/quote/edit form (body + labels)
package tuisocial

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// PostFormMode discriminates the post-form intent.
type PostFormMode int

const (
	// PostFormNew creates a new top-level post.
	PostFormNew PostFormMode = iota
	// PostFormComment creates a comment on the targetID post.
	PostFormComment
	// PostFormQuote creates a quote of the targetID post.
	PostFormQuote
	// PostFormEdit edits the targetID post.
	PostFormEdit
)

// PostFormData holds the editable fields.
type PostFormData struct {
	Body   string
	Labels []string
}

// PostForm wraps a Huh form for social post create/edit.
type PostForm struct {
	tuicore.FormBase
	workdir       string
	mode          PostFormMode
	targetID      string
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          PostFormData
	width         int
	height        int
}

// NewPostForm constructs a post form for the given mode. targetID is required
// for comment/quote/edit; ignored for new posts.
func NewPostForm(workdir string, mode PostFormMode, targetID string, prefill PostFormData) *PostForm {
	f := &PostForm{
		workdir:  workdir,
		mode:     mode,
		targetID: targetID,
		data:     prefill,
	}
	f.buildForm()
	return f
}

// buildForm constructs the underlying huh form.
func (f *PostForm) buildForm() {
	f.bodyField = huh.NewText().
		Key("body").
		Title("").
		Placeholder(f.bodyPlaceholder()).
		Value(&f.data.Body).
		CharLimit(8000).
		Lines(15)

	labelsField := tuicore.NewLabelsField(&f.data.Labels, "")

	fields := []huh.Field{f.bodyField, labelsField, tuicore.NewSubmitField()}
	f.bodyOtherRows = len(fields)
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
}

// bodyPlaceholder returns mode-specific placeholder copy.
func (f *PostForm) bodyPlaceholder() string {
	switch f.mode {
	case PostFormComment:
		return "Write a comment..."
	case PostFormQuote:
		return "Add commentary (leave empty for plain repost)..."
	case PostFormEdit:
		return "Edit your post..."
	default:
		return "What's on your mind?"
	}
}

// SetSize updates the form dimensions.
func (f *PostForm) SetSize(w, h int) {
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
func (f *PostForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *PostForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *PostForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *PostForm) Reset() { f.buildForm() }

// Mode returns the post-form mode.
func (f *PostForm) Mode() PostFormMode { return f.mode }

// Submit dispatches the appropriate social create/edit call.
func (f *PostForm) Submit() tea.Cmd {
	data := f.data
	workdir := f.workdir
	mode := f.mode
	targetID := f.targetID
	return func() tea.Msg {
		body := strings.TrimSpace(data.Body)
		switch mode {
		case PostFormNew:
			if body == "" {
				return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("post cannot be empty")}
			}
			res := social.CreatePost(workdir, body, &social.CreatePostOptions{Labels: data.Labels})
			if !res.Success {
				return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Message)}
			}
			return PostSubmittedMsg{Mode: mode, Post: res.Data}
		case PostFormComment:
			if body == "" {
				return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("comment cannot be empty")}
			}
			res := social.CreateComment(workdir, targetID, body, &social.CreateCommentOptions{Labels: data.Labels})
			if !res.Success {
				return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Message)}
			}
			return PostSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
		case PostFormQuote:
			// Empty body in quote mode collapses to a plain repost (current convention).
			if body == "" {
				res := social.CreateRepost(workdir, targetID, &social.CreateRepostOptions{Labels: data.Labels})
				if !res.Success {
					return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Message)}
				}
				return PostSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
			}
			res := social.CreateQuote(workdir, targetID, body, &social.CreateQuoteOptions{Labels: data.Labels})
			if !res.Success {
				return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Message)}
			}
			return PostSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
		case PostFormEdit:
			if body == "" {
				return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("post cannot be empty")}
			}
			labels := data.Labels
			res := social.EditPost(workdir, targetID, body, &social.EditPostOptions{Labels: &labels})
			if !res.Success {
				return PostSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Message)}
			}
			return PostSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
		}
		return nil
	}
}

// PostSubmittedMsg signals completion of a post form submission.
type PostSubmittedMsg struct {
	Mode     PostFormMode
	Post     social.Post
	TargetID string
	Err      error
}

// PostFormView hosts a PostForm at a route. The mode is taken from the route's
// `mode` param (defaults to "new"); targetID from the `targetID` param.
type PostFormView struct {
	tuicore.FormViewBase
	workdir string
}

// NewPostFormView creates a new post form view.
func NewPostFormView(workdir string) *PostFormView {
	return &PostFormView{workdir: workdir}
}

// Activate builds a fresh form for the route's mode + targetID.
func (v *PostFormView) Activate(state *tuicore.State) tea.Cmd {
	mode := parsePostFormMode(state.Router.Location().Param("mode"))
	targetID := state.Router.Location().Param("targetID")

	var prefill PostFormData
	if mode == PostFormEdit && targetID != "" {
		// Prefill from the existing post.
		res := social.GetPosts(v.workdir, "post:"+targetID, nil)
		if res.Success && len(res.Data) > 0 {
			p := res.Data[0]
			prefill = PostFormData{
				Body:   p.CleanContent,
				Labels: append([]string(nil), p.Labels...),
			}
		}
	}

	form := NewPostForm(v.workdir, mode, targetID, prefill)
	v.AttachForm(form)
	return form.Init()
}

// Update routes lifecycle events to the form.
func (v *PostFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(PostSubmittedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*PostForm); ok {
			return form.Submit()
		}
		return nil
	})
}

// Render renders the form panel.
func (v *PostFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the panel header.
func (v *PostFormView) Title() string {
	form, ok := v.CurrentForm().(*PostForm)
	if !ok {
		return "•  Post"
	}
	switch form.Mode() {
	case PostFormComment:
		return "↩  Comment"
	case PostFormQuote:
		return "❝  Quote"
	case PostFormEdit:
		return "✎  Edit Post"
	default:
		return "•  New Post"
	}
}

// parsePostFormMode parses the route mode param.
func parsePostFormMode(s string) PostFormMode {
	switch s {
	case "comment":
		return PostFormComment
	case "quote":
		return PostFormQuote
	case "edit":
		return PostFormEdit
	default:
		return PostFormNew
	}
}
