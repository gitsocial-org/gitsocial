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

// postFormMode discriminates the post-form intent.
type postFormMode int

const (
	// postFormNew creates a new top-level post.
	postFormNew postFormMode = iota
	// postFormComment creates a comment on the targetID post.
	postFormComment
	// postFormQuote creates a quote of the targetID post.
	postFormQuote
	// postFormEdit edits the targetID post.
	postFormEdit
)

// postFormData holds the editable fields.
type postFormData struct {
	Body   string
	Labels []string
}

// postForm wraps a Huh form for social post create/edit.
type postForm struct {
	tuicore.FormBase
	workdir       string
	mode          postFormMode
	targetID      string
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          postFormData
	width         int
	height        int
}

// newPostForm constructs a post form for the given mode. targetID is required
// for comment/quote/edit; ignored for new posts.
func newPostForm(workdir string, mode postFormMode, targetID string, prefill postFormData) *postForm {
	f := &postForm{
		workdir:  workdir,
		mode:     mode,
		targetID: targetID,
		data:     prefill,
	}
	f.buildForm()
	return f
}

// buildForm constructs the underlying huh form.
func (f *postForm) buildForm() {
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
func (f *postForm) bodyPlaceholder() string {
	switch f.mode {
	case postFormComment:
		return "Write a comment..."
	case postFormQuote:
		return "Add commentary (leave empty for plain repost)..."
	case postFormEdit:
		return "Edit your post..."
	default:
		return "What's on your mind?"
	}
}

// SetSize updates the form dimensions.
func (f *postForm) SetSize(w, h int) {
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
func (f *postForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *postForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *postForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *postForm) Reset() { f.buildForm() }

// Mode returns the post-form mode.
func (f *postForm) Mode() postFormMode { return f.mode }

// Submit dispatches the appropriate social create/edit call.
func (f *postForm) Submit() tea.Cmd {
	data := f.data
	workdir := f.workdir
	mode := f.mode
	targetID := f.targetID
	return func() tea.Msg {
		body := strings.TrimSpace(data.Body)
		switch mode {
		case postFormNew:
			if body == "" {
				return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("post cannot be empty")}
			}
			res := social.CreatePost(workdir, body, &social.CreatePostOptions{Labels: data.Labels})
			if !res.Success {
				return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Text())}
			}
			return postSubmittedMsg{Mode: mode, Post: res.Data}
		case postFormComment:
			if body == "" {
				return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("comment cannot be empty")}
			}
			res := social.CreateComment(workdir, targetID, body, &social.CreateCommentOptions{Labels: data.Labels})
			if !res.Success {
				return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Text())}
			}
			return postSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
		case postFormQuote:
			// Empty body in quote mode collapses to a plain repost (current convention).
			if body == "" {
				res := social.CreateRepost(workdir, targetID, &social.CreateRepostOptions{Labels: data.Labels})
				if !res.Success {
					return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Text())}
				}
				return postSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
			}
			res := social.CreateQuote(workdir, targetID, body, &social.CreateQuoteOptions{Labels: data.Labels})
			if !res.Success {
				return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Text())}
			}
			return postSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
		case postFormEdit:
			if body == "" {
				return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("post cannot be empty")}
			}
			labels := data.Labels
			res := social.EditPost(workdir, targetID, body, &social.EditPostOptions{Labels: &labels})
			if !res.Success {
				return postSubmittedMsg{Mode: mode, Err: fmt.Errorf("%s", res.Error.Text())}
			}
			return postSubmittedMsg{Mode: mode, Post: res.Data, TargetID: targetID}
		}
		return nil
	}
}

// postSubmittedMsg signals completion of a post form submission.
type postSubmittedMsg struct {
	Mode     postFormMode
	Post     social.Post
	TargetID string
	Err      error
}

// postFormView hosts a postForm at a route. The mode is taken from the route's
// `mode` param (defaults to "new"); targetID from the `targetID` param.
type postFormView struct {
	tuicore.FormViewBase
	workdir string
}

// newPostFormView creates a new post form view.
func newPostFormView(workdir string) *postFormView {
	return &postFormView{workdir: workdir}
}

// Activate builds a fresh form for the route's mode + targetID.
func (v *postFormView) Activate(state *tuicore.State) tea.Cmd {
	mode := parsePostFormMode(state.Router.Location().Param("mode"))
	targetID := state.Router.Location().Param("targetID")

	var prefill postFormData
	if mode == postFormEdit && targetID != "" {
		// Prefill from the existing post.
		res := social.GetPosts(v.workdir, "post:"+targetID, nil)
		if res.Success && len(res.Data) > 0 {
			p := res.Data[0]
			prefill = postFormData{
				Body:   p.CleanContent,
				Labels: append([]string(nil), p.Labels...),
			}
		}
	}

	form := newPostForm(v.workdir, mode, targetID, prefill)
	v.AttachForm(form)
	return form.Init()
}

// Update routes lifecycle events to the form.
func (v *postFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(postSubmittedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*postForm); ok {
			return form.Submit()
		}
		return nil
	})
}

// Render renders the form panel.
func (v *postFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the panel header.
func (v *postFormView) Title() string {
	form, ok := v.CurrentForm().(*postForm)
	if !ok {
		return "•  Post"
	}
	switch form.Mode() {
	case postFormComment:
		return "↩  Comment"
	case postFormQuote:
		return "❝  Quote"
	case postFormEdit:
		return "✎  Edit Post"
	default:
		return "•  New Post"
	}
}

// parsePostFormMode parses the route mode param.
func parsePostFormMode(s string) postFormMode {
	switch s {
	case "comment":
		return postFormComment
	case "quote":
		return postFormQuote
	case "edit":
		return postFormEdit
	default:
		return postFormNew
	}
}
