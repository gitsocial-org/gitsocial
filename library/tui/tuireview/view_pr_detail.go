// view_pr_detail.go - Single pull request detail view with threaded discussion
package tuireview

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuisocial"
)

// PRDetailView displays a single pull request with reviews and discussion.
type PRDetailView struct {
	workdir        string
	cacheDir       string
	width          int
	height         int
	loaded         bool
	diffLoaded     bool
	pr             *review.PullRequest
	reviews        []review.Feedback
	comments       []social.Post
	diffStats      git.DiffStats
	commits        []git.Commit
	diffCtx        review.DiffContext
	userEmail      string
	showEmail      bool
	workspaceURL   string
	focusID        string
	reviewComments map[int][]social.Post
	behindCount    int
	versionReviews []review.VersionAwareReview
	confirm        tuicore.ConfirmDialog
	choice         tuicore.ChoiceDialog
	sectionList    *tuicore.SectionList
	// reviewFlatMap maps sectionList flat index (within reviews section) to feedback/comment indices.
	// Each entry is [feedbackIdx, commentIdx] where commentIdx=-1 means the feedback itself.
	reviewFlatMap [][2]int
	showRaw       bool
	sourceIndex   int
	sourceTotal   int
}

// NewPRDetailView creates a new PR detail view.
func NewPRDetailView(workdir string) *PRDetailView {
	return &PRDetailView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		sectionList:  tuicore.NewSectionList(),
	}
}

// SetSize sets the view dimensions.
func (v *PRDetailView) SetSize(w, h int) {
	v.width = w
	v.height = h - 3
	v.sectionList.SetSize(w, h-3)
}

// Activate loads the pull request.
func (v *PRDetailView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	prID := state.Router.Location().Param("prID")
	v.focusID = state.Router.Location().Param("focusID")
	preserveCursor := v.pr != nil && v.pr.ID == prID && v.focusID == ""
	v.loaded = false
	v.confirm.Reset()
	v.choice.Reset()
	if !preserveCursor {
		v.diffLoaded = false
		v.diffStats = git.DiffStats{}
		v.commits = nil
		v.diffCtx = review.DiffContext{}
		v.sectionList.SetSections(nil)
	}
	if state.DetailSource != nil {
		v.sourceIndex = state.DetailSource.Index
		v.sourceTotal = state.DetailSource.Total
		if state.DetailSource.SearchQuery != "" {
			v.sectionList.SetHighlightQuery(tuicore.ExtractSearchTerms(state.DetailSource.SearchQuery))
		}
	} else {
		v.sourceIndex = 0
		v.sourceTotal = 0
		v.sectionList.SetHighlightQuery("")
	}
	workdir := v.workdir
	v.cacheDir = state.CacheDir
	return func() tea.Msg {
		if err := review.SyncWorkspaceToCache(workdir); err != nil {
			log.Debug("review sync before PR detail load failed", "error", err)
		}
		branch := gitmsg.GetExtBranch(workdir, "review")
		// Start GetUnpushedCommits in parallel with GetPR (only needs branch)
		var unpushed map[string]struct{}
		var wgPre sync.WaitGroup
		wgPre.Add(1)
		go func() {
			defer wgPre.Done()
			var err error
			unpushed, err = git.GetUnpushedCommits(workdir, branch)
			if err != nil {
				log.Debug("failed to get unpushed commits", "error", err)
			}
		}()
		res := review.GetPR(prID)
		if !res.Success {
			return prDetailLoadedMsg{}
		}
		pr := res.Data
		hash := extractHashFromID(pr.ID)
		// Resolve state change info (fast DB query, needed for diff context on merged PRs)
		switch pr.State {
		case review.PRStateMerged:
			if info, err := review.GetStateChangeInfo(pr.Repository, hash, pr.Branch, review.PRStateMerged); err == nil {
				pr.MergedBy = &review.Author{Name: info.AuthorName, Email: info.AuthorEmail}
				pr.MergedAt = info.Timestamp
				pr.MergeBase = info.MergeBase
				pr.MergeHead = info.MergeHead
			}
		case review.PRStateClosed:
			if info, err := review.GetStateChangeInfo(pr.Repository, hash, pr.Branch, review.PRStateClosed); err == nil {
				pr.ClosedBy = &review.Author{Name: info.AuthorName, Email: info.AuthorEmail}
				pr.ClosedAt = info.Timestamp
			}
		}
		// Feedback + comments
		var (
			reviews        []review.Feedback
			reviewComments map[int][]social.Post
			prComments     []social.Post
		)
		reviewsRes := review.GetFeedbackForPR(pr.Repository, hash, pr.Branch)
		if reviewsRes.Success {
			reviews = reviewsRes.Data
		}
		pr.ReviewSummary = review.ComputeReviewSummary(reviews, pr.Reviewers)
		reviewComments = map[int][]social.Post{}
		var mu sync.Mutex
		var wgComments sync.WaitGroup
		for i, r := range reviews {
			if r.Comments > 0 {
				wgComments.Add(1)
				go func(idx int, repo, h, id string) {
					defer wgComments.Done()
					res := review.GetFeedbackCommentsByKey(repo, h, id)
					if res.Success {
						mu.Lock()
						reviewComments[idx] = res.Data
						mu.Unlock()
					}
				}(i, r.Repository, extractHashFromID(r.ID), r.ID)
			}
		}
		wgComments.Add(1)
		go func() {
			defer wgComments.Done()
			commentsRes := review.GetPRCommentsByKey(pr.Repository, hash, pr.ID)
			if commentsRes.Success {
				mu.Lock()
				prComments = commentsRes.Data
				mu.Unlock()
			}
		}()
		wgComments.Wait()
		wgPre.Wait()
		if _, ok := unpushed[hash]; ok {
			pr.IsUnpushed = true
		}
		return prDetailLoadedMsg{pr: &pr, reviews: reviews, reviewComments: reviewComments, comments: prComments}
	}
}

// Deactivate is called when the view is hidden.
func (v *PRDetailView) Deactivate() {}

// Update handles messages.
func (v *PRDetailView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case prDetailLoadedMsg:
		preserveCursor := v.pr != nil && msg.pr != nil && v.pr.ID == msg.pr.ID && v.focusID == ""
		v.loaded = true
		v.pr = msg.pr
		v.reviews = msg.reviews
		for idx, comments := range msg.reviewComments {
			for i := range comments {
				if comments[i].Repository == v.workspaceURL {
					comments[i].Display.IsWorkspacePost = true
				}
			}
			msg.reviewComments[idx] = comments
		}
		v.reviewComments = msg.reviewComments
		for i := range msg.comments {
			if msg.comments[i].Repository == v.workspaceURL {
				msg.comments[i].Display.IsWorkspacePost = true
			}
		}
		v.comments = msg.comments
		prevSelected := v.sectionList.Selected()
		v.buildSections()
		if preserveCursor {
			v.sectionList.SetSelected(prevSelected)
		}
		if v.focusID != "" {
			v.focusToID()
			v.focusID = ""
		}
		if v.isLocalPR() {
			return v.loadDiff(msg.pr)
		}
		return nil
	case prDetailDiffMsg:
		if v.pr == nil || v.pr.ID != msg.prID {
			return nil
		}
		prevSelected := v.sectionList.Selected()
		commitDelta := len(msg.commits) - len(v.commits)
		v.diffLoaded = true
		v.diffStats = msg.diffStats
		v.commits = msg.commits
		v.diffCtx = msg.diffCtx
		v.behindCount = msg.behindCount
		v.versionReviews = msg.versionReviews
		v.buildSections()
		if prevSelected > 0 && commitDelta > 0 {
			v.sectionList.SetSelected(prevSelected + commitDelta)
		} else {
			v.sectionList.SetSelected(prevSelected)
		}
		if msg.forkFetched {
			return refreshCacheSize(v.cacheDir)
		}
		return nil
	case SuggestionAppliedMsg:
		return nil
	case tea.KeyPressMsg, tea.MouseMsg:
		if key, ok := msg.(tea.KeyPressMsg); ok {
			if handled, cmd := v.confirm.HandleKey(key.String()); handled {
				return cmd
			}
			if handled, cmd := v.choice.HandleKey(key.String()); handled {
				return cmd
			}
		}
		consumed, cmd := v.sectionList.Update(msg)
		if consumed {
			return cmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "left":
				return v.navigateSource(state, -1)
			case "right":
				return v.navigateSource(state, 1)
			case "M":
				if v.pr != nil && v.pr.State == review.PRStateOpen && !v.pr.IsDraft && v.targetsWorkspace() {
					v.choice.Show("Merge strategy?", []tuicore.Choice{
						{Key: "f", Label: "ast-forward"},
						{Key: "s", Label: "quash"},
						{Key: "r", Label: "ebase"},
						{Key: "m", Label: "erge"},
					}, func(key string) tea.Cmd {
						strategies := map[string]review.MergeStrategy{
							"f": review.MergeStrategyFF,
							"s": review.MergeStrategySquash,
							"r": review.MergeStrategyRebase,
							"m": review.MergeStrategyMerge,
						}
						return v.doMerge(strategies[key])
					})
					return nil
				}
			case "C":
				if v.pr != nil && v.pr.State == review.PRStateOpen && v.targetsWorkspace() {
					v.confirm.Show("Close this pull request?", false, func() tea.Cmd { return v.doClose() })
					return nil
				}
			case "D":
				if v.pr != nil && v.pr.State == review.PRStateOpen && v.targetsWorkspace() {
					if v.pr.IsDraft {
						return v.doMarkReady()
					}
					return v.doConvertToDraft()
				}
			case "h":
				if v.pr != nil && v.pr.IsEdited {
					prID := v.pr.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReviewPRHistory(prID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "i":
				if v.pr != nil && v.pr.IsEdited {
					prID := v.pr.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReviewInterdiff(prID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "e":
				if v.pr != nil {
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReviewEditPR(v.pr.ID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "r":
				if v.pr != nil && v.pr.State == review.PRStateOpen {
					return v.navigateToFeedback("")
				}
			case "c":
				if v.pr != nil {
					return func() tea.Msg {
						return tuicore.OpenEditorMsg{Mode: "comment", TargetID: v.pr.ID}
					}
				}
			case "d":
				if v.pr != nil {
					prID := v.pr.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReviewDiff(prID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "S":
				if v.pr != nil && v.pr.State == review.PRStateOpen && v.targetsWorkspace() {
					prompt := "Sync branch?"
					if v.behindCount > 0 {
						baseName := shortenBranchRef(v.pr.Base)
						prompt = fmt.Sprintf("%d commits behind %s. Sync?", v.behindCount, baseName)
					}
					v.choice.Show(prompt, []tuicore.Choice{
						{Key: "r", Label: "ebase"},
						{Key: "m", Label: "erge"},
					}, func(key string) tea.Cmd {
						strategies := map[string]string{"r": "rebase", "m": "merge"}
						return v.doSync(strategies[key])
					})
					return nil
				}
			case "X":
				if v.pr != nil {
					v.confirm.Show("Retract this pull request?", false, func() tea.Cmd { return v.doRetract() })
					return nil
				}
			case "A":
				return v.applySuggestion()
			}
		}
	}
	if v.sectionList.IsInputActive() {
		return v.sectionList.UpdateSearchInput(msg)
	}
	return nil
}

// navigateSource navigates to adjacent items in the source list.
func (v *PRDetailView) navigateSource(state *tuicore.State, offset int) tea.Cmd {
	if state.DetailSource == nil {
		return nil
	}
	return func() tea.Msg {
		return tuicore.SourceNavigateMsg{Offset: offset, MakeLocation: tuicore.LocReviewPRDetail}
	}
}

// IsInputActive returns true when confirmation or search input is active.
func (v *PRDetailView) IsInputActive() bool {
	return v.confirm.IsActive() || v.choice.IsActive() || v.sectionList.IsInputActive()
}

// applySuggestion applies the suggestion at the current selection.
func (v *PRDetailView) applySuggestion() tea.Cmd {
	fi, ci := v.resolveCurrentReview()
	if fi < 0 || ci != -1 {
		return nil
	}
	r := v.reviews[fi]
	if !r.Suggestion {
		return nil
	}
	workdir := v.workdir
	return func() tea.Msg {
		res := review.ApplySuggestion(workdir, r)
		if !res.Success {
			return SuggestionAppliedMsg{Err: fmt.Errorf("%s", res.Error.Message)}
		}
		return SuggestionAppliedMsg{File: res.Data}
	}
}

// resolveCurrentReview returns the feedback index and comment index for the current selection.
// Returns (-1, -1) if not in reviews section.
func (v *PRDetailView) resolveCurrentReview() (feedbackIdx, commentIdx int) {
	sec, idx := v.sectionList.SectionAndIndex()
	// Find the reviews section index
	reviewSec := -1
	secIdx := 0
	if v.pr != nil {
		secIdx++ // PR detail
	}
	if len(v.commits) > 0 {
		if secIdx == sec {
			return -1, -1
		}
		secIdx++
	}
	if len(v.reviews) > 0 {
		reviewSec = secIdx
	}
	if sec != reviewSec || reviewSec < 0 {
		return -1, -1
	}
	if idx < 0 || idx >= len(v.reviewFlatMap) {
		return -1, -1
	}
	entry := v.reviewFlatMap[idx]
	return entry[0], entry[1]
}

func (v *PRDetailView) focusToID() {
	// Try comments first
	for i, c := range v.comments {
		if c.ID == v.focusID {
			// Comments offset: 1 (PR) + commits + reviewItems + i
			offset := 1 + len(v.commits) + len(v.reviewFlatMap) + i
			v.sectionList.SetSelected(offset)
			return
		}
	}
	// Try reviews
	for i, r := range v.reviews {
		if r.ID == v.focusID {
			offset := 1 + len(v.commits)
			for j, entry := range v.reviewFlatMap {
				if entry[0] == i && entry[1] == -1 {
					v.sectionList.SetSelected(offset + j)
					return
				}
			}
		}
	}
	// Try review comments
	for i, comments := range v.reviewComments {
		for j, c := range comments {
			if c.ID == v.focusID {
				offset := 1 + len(v.commits)
				for k, entry := range v.reviewFlatMap {
					if entry[0] == i && entry[1] == j {
						v.sectionList.SetSelected(offset + k)
						return
					}
				}
			}
		}
	}
}

// loadDiff kicks off phase 2: resolve diff context, stats, and commits in the background.
func (v *PRDetailView) loadDiff(pr *review.PullRequest) tea.Cmd {
	if pr == nil {
		return nil
	}
	workdir, cacheDir, prID := v.workdir, v.cacheDir, pr.ID
	prCopy := *pr
	return func() tea.Msg {
		diffCtx := review.ResolvePRDiff(workdir, cacheDir, &prCopy, "")
		var diffStats git.DiffStats
		var commits []git.Commit
		var behindCount int
		var versionReviews []review.VersionAwareReview
		var wg sync.WaitGroup
		if diffCtx.Base != "" && diffCtx.Head != "" {
			wg.Add(3)
			go func() {
				defer wg.Done()
				var err error
				diffStats, err = git.GetDiffStats(diffCtx.Workdir, diffCtx.Base, diffCtx.Head)
				if err != nil {
					log.Debug("failed to get diff stats", "error", err)
				}
			}()
			go func() {
				defer wg.Done()
				var err error
				commits, err = git.GetCommitRange(diffCtx.Workdir, diffCtx.Base, diffCtx.Head)
				if err != nil {
					log.Debug("failed to get commit range", "error", err)
				}
			}()
			go func() {
				defer wg.Done()
				var err error
				behindCount, err = git.GetBehindCount(diffCtx.Workdir, diffCtx.Base, diffCtx.Head)
				if err != nil {
					log.Debug("failed to get behind count", "error", err)
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := review.GetVersionAwareReviews(workdir, prID)
			if res.Success {
				versionReviews = res.Data
			}
		}()
		wg.Wait()
		return prDetailDiffMsg{prID: prID, diffStats: diffStats, commits: commits, diffCtx: diffCtx, forkFetched: diffCtx.Workdir != workdir, behindCount: behindCount, versionReviews: versionReviews}
	}
}

func (v *PRDetailView) buildSections() {
	var sections []tuicore.Section
	v.reviewFlatMap = nil
	// Hero section (no label) — the PR card
	pr := v.pr
	if pr == nil {
		v.sectionList.SetSections(nil)
		return
	}
	sections = append(sections, tuicore.Section{
		Items: []tuicore.SectionItem{{
			Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
				return v.renderPRCard(width, selected, searchQuery, anchors)
			},
			SearchText: func() string { return pr.Subject + " " + pr.Body },
			Links: func() []tuicore.CardLink {
				var links []tuicore.CardLink
				if pr.Origin != nil && pr.Origin.URL != "" {
					links = append(links, tuicore.CardLink{Label: "Source", Location: tuicore.Location{Path: pr.Origin.URL}})
				}
				if loc := branchRefLocation(pr.Base, v.workspaceURL); loc != nil {
					links = append(links, tuicore.CardLink{Label: shortenBranchRef(pr.Base), Location: *loc})
				}
				if loc := branchRefLocation(pr.Head, v.workspaceURL); loc != nil {
					links = append(links, tuicore.CardLink{Label: shortenBranchRef(pr.Head), Location: *loc})
				}
				if pr.Head != "" {
					parsed := protocol.ParseRef(pr.Head)
					if parsed.Repository != "" && parsed.Repository != v.workspaceURL {
						links = append(links, tuicore.CardLink{Label: "Fork", Location: tuicore.Location{Path: parsed.Repository}})
					}
				}
				for _, ref := range pr.Closes {
					fullRef := protocol.NormalizeRefWithContext(ref, v.workspaceURL, "")
					links = append(links, tuicore.CardLink{
						Label:    protocol.FormatShortRef(ref, v.workspaceURL),
						Location: tuicore.LocPMIssueDetail(fullRef),
					})
				}
				links = append(links, tuicore.ExtractContentLinks(pr.Body, pr.Repository, "")...)
				return links
			},
		}},
	})
	// Commits section
	if len(v.commits) > 0 {
		label := fmt.Sprintf(" Commits (%d)", len(v.commits))
		items := make([]tuicore.SectionItem, 0, len(v.commits))
		for _, c := range v.commits {
			c := c
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, _ *tuicore.AnchorCollector) []string {
					return v.renderCommitRow(c, width, selected, searchQuery)
				},
				SearchText: func() string {
					return strings.SplitN(strings.TrimSpace(c.Message), "\n", 2)[0]
				},
				OnActivate: func() tea.Cmd {
					prID := pr.ID
					hash := c.Hash
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReviewDiffCommit(prID, hash),
							Action:   tuicore.NavPush,
						}
					}
				},
			})
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
	// Reviews section (flattened: feedback + their comments interleaved)
	if len(v.reviews) > 0 {
		label := fmt.Sprintf(" Reviews (%d)", len(v.reviews))
		var items []tuicore.SectionItem
		versionReviewMap := map[string]*review.VersionAwareReview{}
		for i := range v.versionReviews {
			vr := &v.versionReviews[i]
			versionReviewMap[strings.ToLower(vr.ReviewerEmail)] = vr
		}
		for i, r := range v.reviews {
			r := r
			ri := i
			isLastReview := i == len(v.reviews)-1
			var vr *review.VersionAwareReview
			if r.Author.Email != "" {
				vr = versionReviewMap[strings.ToLower(r.Author.Email)]
			}
			v.reviewFlatMap = append(v.reviewFlatMap, [2]int{i, -1})
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
					lines := v.renderReviewRow(r, width, selected, searchQuery, anchors, vr)
					if comments, ok := v.reviewComments[ri]; !ok || len(comments) == 0 {
						if !isLastReview {
							lines = append(lines, "", tuicore.RenderItemSeparator(width, 0), "")
						}
					}
					return lines
				},
				SearchText: func() string { return r.Content },
				Links: func() []tuicore.CardLink {
					isWorkspace := r.Repository == v.workspaceURL
					card := FeedbackToCard(r, v.userEmail, isWorkspace, v.showEmail)
					return card.AllLinks()
				},
				OnActivate: func() tea.Cmd {
					id := r.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocDetail(id),
							Action:   tuicore.NavPush,
						}
					}
				},
			})
			if comments, ok := v.reviewComments[i]; ok {
				for j, comment := range comments {
					comment := comment
					isLastComment := j == len(comments)-1
					v.reviewFlatMap = append(v.reviewFlatMap, [2]int{i, j})
					items = append(items, tuicore.SectionItem{
						Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
							lines := tuisocial.RenderCommentCard(comment, width, selected, searchQuery, v.userEmail, v.showEmail, anchors)
							if isLastComment && !isLastReview {
								lines = append(lines, "", tuicore.RenderItemSeparator(width, 0), "")
							} else if !isLastComment {
								lines = append(lines, "", tuicore.RenderItemSeparator(width, comment.Depth), "")
							}
							return lines
						},
						SearchText: func() string { return comment.Content },
						Links: func() []tuicore.CardLink {
							card := tuisocial.PostToCardWithOptions(comment, nil, tuisocial.PostToCardOptions{SkipNested: true, UserEmail: v.userEmail, ShowEmail: v.showEmail})
							return card.AllLinks()
						},
						OnActivate: func() tea.Cmd {
							id := comment.ID
							return func() tea.Msg {
								return tuicore.NavigateMsg{
									Location: tuicore.LocDetail(id),
									Action:   tuicore.NavPush,
								}
							}
						},
					})
				}
			}
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
	// Comments section
	if len(v.comments) > 0 {
		label := fmt.Sprintf(" Comments (%d)", len(v.comments))
		items := make([]tuicore.SectionItem, 0, len(v.comments))
		for i, comment := range v.comments {
			comment := comment
			isLast := i == len(v.comments)-1
			nextDepth := 0
			if !isLast {
				nextDepth = v.comments[i+1].Depth
			}
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
					lines := tuisocial.RenderCommentCard(comment, width, selected, searchQuery, v.userEmail, v.showEmail, anchors)
					if !isLast {
						lines = append(lines, "", tuicore.RenderItemSeparator(width, nextDepth), "")
					}
					return lines
				},
				SearchText: func() string { return comment.Content },
				Links: func() []tuicore.CardLink {
					card := tuisocial.PostToCardWithOptions(comment, nil, tuisocial.PostToCardOptions{SkipNested: true, UserEmail: v.userEmail, ShowEmail: v.showEmail})
					return card.AllLinks()
				},
				OnActivate: func() tea.Cmd {
					id := comment.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocDetail(id),
							Action:   tuicore.NavPush,
						}
					}
				},
			})
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
	v.sectionList.SetSections(sections)
}

func (v *PRDetailView) navigateToFeedback(state string) tea.Cmd {
	prID := v.pr.ID
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReviewFeedback(prID, state),
			Action:   tuicore.NavPush,
		}
	}
}

func (v *PRDetailView) doMerge(strategy review.MergeStrategy) tea.Cmd {
	prID := v.pr.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := review.MergePR(workdir, prID, strategy)
		if !result.Success {
			return PRUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRUpdatedMsg{PR: result.Data}
	}
}

func (v *PRDetailView) doSync(strategy string) tea.Cmd {
	prID := v.pr.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := review.SyncPRBranch(workdir, prID, strategy)
		if !result.Success {
			return PRUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRUpdatedMsg{PR: result.Data}
	}
}

func (v *PRDetailView) doClose() tea.Cmd {
	prID := v.pr.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := review.ClosePR(workdir, prID)
		if !result.Success {
			return PRUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRUpdatedMsg{PR: result.Data}
	}
}

func (v *PRDetailView) doMarkReady() tea.Cmd {
	prID := v.pr.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := review.MarkReady(workdir, prID)
		if !result.Success {
			return PRUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRUpdatedMsg{PR: result.Data}
	}
}

func (v *PRDetailView) doConvertToDraft() tea.Cmd {
	prID := v.pr.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := review.ConvertToDraft(workdir, prID)
		if !result.Success {
			return PRUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRUpdatedMsg{PR: result.Data}
	}
}

func (v *PRDetailView) doRetract() tea.Cmd {
	prID := v.pr.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := review.RetractPR(workdir, prID)
		if !result.Success {
			return PRRetractedMsg{ID: prID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return PRRetractedMsg{ID: prID}
	}
}

// Render renders the view.
func (v *PRDetailView) Render(state *tuicore.State) string {
	if v.pr != nil && v.pr.IsRetracted {
		state.BorderVariant = "warning"
	}
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = tuicore.Dim.Render("  Loading pull request...")
	} else if v.pr == nil {
		content = tuicore.Dim.Render("  Pull request not found")
	} else {
		content = v.sectionList.View()
	}
	var footer string
	if v.sectionList.IsSearchActive() {
		footer = v.sectionList.SearchFooter(wrapper.ContentWidth())
	} else if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else if v.choice.IsActive() {
		footer = v.choice.Render()
	} else {
		exclude := map[string]bool{}
		if v.pr != nil && v.pr.State != review.PRStateOpen {
			exclude["r"] = true
			exclude["M"] = true
			exclude["C"] = true
			exclude["S"] = true
		}
		if v.pr != nil && !v.targetsWorkspace() {
			exclude["M"] = true
			exclude["C"] = true
			exclude["S"] = true
		}
		if v.pr != nil && !v.pr.IsEdited {
			exclude["h"] = true
			exclude["i"] = true
		}
		footer = tuicore.RenderFooterWithPosition(state.Registry, tuicore.ReviewPRDetail, wrapper.ContentWidth(), v.sourceIndex+1, v.sourceTotal, exclude)
	}
	return wrapper.Render(content, footer)
}

func (v *PRDetailView) renderPRCard(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
	pr := v.pr
	var lines []string
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	title := pr.Subject
	if searchQuery != "" {
		title = tuicore.HighlightInText(title, searchQuery)
	}
	lines = append(lines, selectionBar+tuicore.Bold.Render(title))
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	styles := tuicore.RowStylesWithWidths(14, 0)
	stateStr := string(pr.State)
	switch pr.State {
	case review.PRStateOpen:
		if pr.IsDraft {
			stateStr = tuicore.Dim.Render("draft")
		} else {
			stateStr = tuicore.Title.Render("open")
		}
	case review.PRStateMerged:
		stateStr = tuicore.Dim.Render("merged")
	case review.PRStateClosed:
		stateStr = tuicore.Dim.Render("closed")
	}
	lines = append(lines, selectionBar+styles.Label.Render("State")+stateStr)
	lines = append(lines, tuicore.RenderOriginRows(pr.Origin, styles, selectionBar, anchors, v.showEmail)...)
	displayAuthor := pr.Author
	displayTime := pr.Timestamp
	if pr.OriginalAuthor != nil {
		displayAuthor = *pr.OriginalAuthor
		if !pr.OriginalTime.IsZero() {
			displayTime = pr.OriginalTime
		}
	}
	if pr.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(pr.Origin, v.showEmail); a != "" {
			displayAuthor = review.Author{Name: a}
		}
		if pr.Origin.Time != "" {
			if t, err := time.Parse(time.RFC3339, pr.Origin.Time); err == nil {
				displayTime = t
			}
		}
	}
	authorName := displayAuthor.Name
	if v.showEmail && displayAuthor.Email != "" {
		authorName += " <" + displayAuthor.Email + ">"
	}
	authorStyle := tuicore.AuthorStyle(displayAuthor.Email, v.userEmail, styles.Value)
	lines = append(lines, selectionBar+styles.Label.Render("Author")+authorStyle.Render(
		authorName+" · "+tuicore.FormatTime(displayTime)))
	if pr.MergedBy != nil {
		mergedByName := pr.MergedBy.Name
		if v.showEmail && pr.MergedBy.Email != "" {
			mergedByName += " <" + pr.MergedBy.Email + ">"
		}
		mergedByStyle := tuicore.AuthorStyle(pr.MergedBy.Email, v.userEmail, styles.Value)
		lines = append(lines, selectionBar+styles.Label.Render("Merged by")+mergedByStyle.Render(
			mergedByName+" · "+tuicore.FormatTime(pr.MergedAt)))
	}
	if pr.ClosedBy != nil {
		closedByName := pr.ClosedBy.Name
		if v.showEmail && pr.ClosedBy.Email != "" {
			closedByName += " <" + pr.ClosedBy.Email + ">"
		}
		closedByStyle := tuicore.AuthorStyle(pr.ClosedBy.Email, v.userEmail, styles.Value)
		lines = append(lines, selectionBar+styles.Label.Render("Closed by")+closedByStyle.Render(
			closedByName+" · "+tuicore.FormatTime(pr.ClosedAt)))
	}
	if pr.Base != "" {
		display := shortenBranchRef(pr.Base)
		parsed := protocol.ParseRef(pr.Base)
		repoURL := parsed.Repository
		if repoURL == "" {
			repoURL = v.workspaceURL
		}
		branchURL := protocol.BranchURL(repoURL, parsed.Value)
		var baseText string
		if loc := branchRefLocation(pr.Base, v.workspaceURL); loc != nil {
			baseText = anchors.MarkLink(display, branchURL, *loc)
		} else if branchURL != "" {
			baseText = tuicore.Hyperlink(branchURL, display)
		} else {
			baseText = styles.Value.Render(display)
		}
		lines = append(lines, selectionBar+styles.Label.Render("Base")+baseText)
	}
	if pr.Head != "" {
		display := shortenBranchRef(pr.Head)
		parsed := protocol.ParseRef(pr.Head)
		repoURL := parsed.Repository
		if repoURL == "" {
			repoURL = v.workspaceURL
		}
		branchURL := protocol.BranchURL(repoURL, parsed.Value)
		var headText string
		if loc := branchRefLocation(pr.Head, v.workspaceURL); loc != nil {
			headText = anchors.MarkLink(display, branchURL, *loc)
		} else if branchURL != "" {
			headText = tuicore.Hyperlink(branchURL, display)
		} else {
			headText = styles.Value.Render(display)
		}
		lines = append(lines, selectionBar+styles.Label.Render("Head")+headText)
		if parsed.Repository != "" && parsed.Repository != v.workspaceURL {
			forkDisplay := strings.TrimPrefix(strings.TrimPrefix(parsed.Repository, "https://"), "http://")
			forkLink := anchors.MarkLink(forkDisplay, parsed.Repository, tuicore.Location{Path: parsed.Repository})
			lines = append(lines, selectionBar+styles.Label.Render("Fork")+forkLink)
		}
	}
	if v.behindCount > 0 && pr.Base != "" {
		baseName := shortenBranchRef(pr.Base)
		lines = append(lines, selectionBar+styles.Label.Render("Behind")+tuicore.Dim.Render(
			fmt.Sprintf("%d commits behind %s", v.behindCount, baseName)))
	}
	if len(pr.Reviewers) > 0 {
		lines = append(lines, selectionBar+styles.Label.Render("Reviewers")+styles.Value.Render(strings.Join(pr.Reviewers, ", ")))
	}
	if len(pr.Closes) > 0 {
		refs := make([]string, len(pr.Closes))
		for i, ref := range pr.Closes {
			short := protocol.FormatShortRef(ref, v.workspaceURL)
			fullRef := protocol.NormalizeRefWithContext(ref, v.workspaceURL, "")
			parsed := protocol.ParseRef(fullRef)
			commitURL := protocol.CommitURL(parsed.Repository, parsed.Value)
			refs[i] = anchors.MarkLink(short, commitURL, tuicore.LocPMIssueDetail(fullRef))
		}
		lines = append(lines, selectionBar+styles.Label.Render("Closes")+strings.Join(refs, styles.Value.Render(", ")))
	}
	if !v.diffLoaded && !v.isLocalPR() {
		lines = append(lines, selectionBar+styles.Label.Render("Files")+tuicore.Dim.Render("Press [d] to view diff"))
	} else if !v.diffLoaded {
		lines = append(lines, selectionBar+styles.Label.Render("Files")+tuicore.Dim.Render("Loading..."))
	} else if v.diffCtx.Error != "" {
		lines = append(lines, selectionBar+styles.Label.Render("Files")+tuicore.Dim.Render(v.diffCtx.Error))
	} else if v.diffStats.Files > 0 {
		lines = append(lines, selectionBar+styles.Label.Render("Files")+styles.Value.Render(
			fmt.Sprintf("%d changed  %s  [d]", v.diffStats.Files, tuicore.RenderDiffStatsBadge(v.diffStats.Added, v.diffStats.Removed))))
	} else if v.diffCtx.Base != "" && v.diffCtx.Head != "" {
		lines = append(lines, selectionBar+styles.Label.Render("Files")+tuicore.Dim.Render("0 files changed"))
	}
	summary := pr.ReviewSummary
	if summary.Approved > 0 || summary.ChangesRequested > 0 || summary.Pending > 0 {
		lines = append(lines, selectionBar+styles.Label.Render("Reviews")+styles.Value.Render(
			fmt.Sprintf("%d approved, %d changes requested, %d pending", summary.Approved, summary.ChangesRequested, summary.Pending)))
		if summary.IsApproved {
			lines = append(lines, selectionBar+styles.Label.Render("Status")+tuicore.Title.Render("Ready to merge"))
		} else if summary.IsBlocked {
			lines = append(lines, selectionBar+styles.Label.Render("Status")+tuicore.Dim.Render("Changes requested"))
		}
	}
	prRef := protocol.ParseRef(pr.ID)
	if trailerRefs, err := cache.GetTrailerRefsTo(prRef.Repository, prRef.Value, prRef.Branch); err == nil && len(trailerRefs) > 0 {
		for i, tr := range trailerRefs {
			rowLabel := "Referenced by"
			if i > 0 {
				rowLabel = ""
			}
			subject, _ := protocol.SplitSubjectBody(tr.Message)
			display := subject + tuicore.Dim.Render("  "+tr.Hash[:12]+"  "+tr.TrailerKey)
			lines = append(lines, selectionBar+styles.Label.Render(rowLabel)+styles.Value.Render(display))
		}
	}
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	if v.showRaw {
		lines = append(lines, tuicore.RenderCommitMessage(pr.ID, selectionBar, width-3)...)
	} else if pr.Body != "" {
		for _, line := range strings.Split(tuicore.RenderMarkdownWithAnchors(pr.Body, width-3, anchors), "\n") {
			if searchQuery != "" {
				line = tuicore.HighlightInText(line, searchQuery)
			}
			lines = append(lines, selectionBar+line)
		}
	} else {
		lines = append(lines, selectionBar+tuicore.Dim.Render("No description"))
	}
	return lines
}

func (v *PRDetailView) renderCommitRow(c git.Commit, _ int, selected bool, searchQuery string) []string {
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	subject := strings.SplitN(strings.TrimSpace(c.Message), "\n", 2)[0]
	if searchQuery != "" {
		subject = tuicore.HighlightInText(subject, searchQuery)
	}
	line := fmt.Sprintf("%s  %s  %s",
		tuicore.Dim.Render(c.Hash[:7]),
		subject,
		tuicore.Dim.Render(c.Author+" · "+tuicore.FormatTime(c.Timestamp)),
	)
	return []string{selectionBar + line}
}

func (v *PRDetailView) renderReviewRow(r review.Feedback, width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector, vr *review.VersionAwareReview) []string {
	isWorkspace := r.Repository == v.workspaceURL
	card := FeedbackToCard(r, v.userEmail, isWorkspace, v.showEmail)
	opts := tuicore.CardOptions{
		MaxLines:      -1,
		ShowStats:     true,
		Selected:      selected,
		Width:         width,
		Markdown:      true,
		WrapWidth:     width - 2,
		HighlightText: searchQuery,
		Anchors:       anchors,
	}
	lines := strings.Split(tuicore.RenderCard(card, opts), "\n")
	if vr != nil {
		selectionBar := " "
		if selected {
			selectionBar = tuicore.Title.Render("▏")
		}
		iconPad := strings.Repeat(" ", tuicore.AnsiWidth(card.Header.Icon)+2)
		if vr.Stale {
			lines = append(lines, selectionBar+iconPad+tuicore.Dim.Render(
				fmt.Sprintf("  reviewed %s [stale]", vr.ReviewedLabel)))
		} else if vr.HeadChanged && !vr.CodeChanged {
			lines = append(lines, selectionBar+iconPad+tuicore.Dim.Render(
				fmt.Sprintf("  reviewed %s, no code changes", vr.ReviewedLabel)))
		}
	}
	if !r.IsRetracted && r.File != "" {
		selectionBar := " "
		if selected {
			selectionBar = tuicore.Title.Render("▏")
		}
		iconPad := strings.Repeat(" ", tuicore.AnsiWidth(card.Header.Icon)+2)
		lines = append(lines, v.renderCodeContextLines(r, width, selectionBar, iconPad)...)
		if r.Suggestion {
			lines = append(lines, v.renderSuggestionPreview(r, width, selectionBar, iconPad)...)
		}
	}
	return lines
}

func (v *PRDetailView) renderSuggestionPreview(r review.Feedback, width int, selectionBar, iconPad string) []string {
	suggested := review.ParseSuggestionCode(r.Content)
	if suggested == "" {
		return nil
	}
	if v.pr == nil {
		return nil
	}
	targetLine := r.NewLine
	ref := v.diffCtx.Head
	if targetLine <= 0 {
		targetLine = r.OldLine
		ref = v.diffCtx.Base
	}
	if targetLine <= 0 || ref == "" {
		return nil
	}
	content, err := git.GetFileContent(v.diffCtx.Workdir, ref, r.File)
	if err != nil {
		return nil
	}
	fileLines := strings.Split(content, "\n")
	endLine := r.NewLineEnd
	if endLine <= 0 {
		endLine = targetLine
	}
	if targetLine > len(fileLines) {
		return nil
	}
	if endLine > len(fileLines) {
		endLine = len(fileLines)
	}
	originalLines := fileLines[targetLine-1 : endLine]
	suggestedLines := strings.Split(suggested, "\n")
	lang := tuicore.DetectLanguageFromPath(r.File)
	codeWidth := width - len(iconPad) - 5
	if codeWidth < 0 {
		codeWidth = 0
	}
	var lines []string
	lines = append(lines, selectionBar+iconPad+tuicore.Dim.Render("  Suggestion:"))
	for _, ol := range originalLines {
		code := strings.ReplaceAll(ol, "\t", "    ")
		if runes := []rune(code); len(runes) > codeWidth {
			code = string(runes[:codeWidth])
		}
		highlighted := tuicore.HighlightLine(code, lang, false)
		removedLine := fmt.Sprintf("  %s %s", lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.DiffRemoved)).Render("-"), highlighted)
		lines = append(lines, selectionBar+iconPad+removedLine)
	}
	for _, sl := range suggestedLines {
		code := strings.ReplaceAll(sl, "\t", "    ")
		if runes := []rune(code); len(runes) > codeWidth {
			code = string(runes[:codeWidth])
		}
		highlighted := tuicore.HighlightLine(code, lang, false)
		addedLine := fmt.Sprintf("  %s %s", lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.DiffAdded)).Render("+"), highlighted)
		lines = append(lines, selectionBar+iconPad+addedLine)
	}
	lines = append(lines, selectionBar+iconPad+tuicore.Dim.Render("  [A]pply suggestion"))
	return lines
}

func (v *PRDetailView) renderCodeContextLines(r review.Feedback, width int, selectionBar, iconPad string) []string {
	if v.pr == nil {
		return nil
	}
	targetLine := r.NewLine
	ref := v.diffCtx.Head
	if targetLine <= 0 {
		targetLine = r.OldLine
		ref = v.diffCtx.Base
	}
	if targetLine <= 0 || ref == "" {
		return nil
	}
	content, err := git.GetFileContent(v.diffCtx.Workdir, ref, r.File)
	if err != nil {
		return nil
	}
	fileLines := strings.Split(content, "\n")
	start := targetLine - 3
	if start < 0 {
		start = 0
	}
	end := targetLine + 2
	if end > len(fileLines) {
		end = len(fileLines)
	}
	lang := tuicore.DetectLanguageFromPath(r.File)
	codeWidth := width - len(iconPad) - 8
	if codeWidth < 0 {
		codeWidth = 0
	}
	var lines []string
	for i := start; i < end; i++ {
		lineNum := i + 1
		code := strings.ReplaceAll(fileLines[i], "\t", "    ")
		if runes := []rune(code); len(runes) > codeWidth {
			code = string(runes[:codeWidth])
		}
		highlighted := tuicore.HighlightLine(code, lang, false)
		prefix := "  "
		if lineNum == targetLine {
			prefix = "> "
		}
		lines = append(lines, selectionBar+iconPad+fmt.Sprintf("%s%s %s", prefix, tuicore.Dim.Render(fmt.Sprintf("%4d", lineNum)), highlighted))
	}
	return lines
}

// targetsWorkspace returns true if the PR's base branch targets the current workspace repo.
func (v *PRDetailView) targetsWorkspace() bool {
	if v.pr == nil {
		return false
	}
	baseRepo := protocol.ParseRef(v.pr.Base).Repository
	return baseRepo == "" || baseRepo == v.workspaceURL
}

// isLocalPR returns true if the PR belongs to the workspace repo or one of its registered forks.
func (v *PRDetailView) isLocalPR() bool {
	if v.pr == nil {
		return false
	}
	repo := v.pr.Repository
	if repo == "" || repo == v.workspaceURL {
		return true
	}
	for _, f := range review.GetForks(v.workdir) {
		if f == repo {
			return true
		}
	}
	return false
}

// ShowRawView toggles between rendered body and full commit message.
func (v *PRDetailView) ShowRawView() tea.Cmd {
	v.showRaw = !v.showRaw
	return func() tea.Msg { return nil }
}

// Title returns the view title.
func (v *PRDetailView) Title() string {
	if v.pr == nil {
		return "⑂  Pull Request"
	}
	pr := v.pr
	id := protocol.FormatShortRef(pr.ID, v.workspaceURL)
	icon := "⑂"
	if pr.IsUnpushed {
		icon += "  ⇡"
	}
	titleDisplayAuthor := pr.Author
	titleDisplayTime := pr.Timestamp
	if pr.OriginalAuthor != nil {
		titleDisplayAuthor = *pr.OriginalAuthor
		if !pr.OriginalTime.IsZero() {
			titleDisplayTime = pr.OriginalTime
		}
	}
	titleAuthor := titleDisplayAuthor.Name
	if v.showEmail && titleDisplayAuthor.Email != "" {
		titleAuthor += " <" + titleDisplayAuthor.Email + ">"
	}
	if pr.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(pr.Origin, v.showEmail); a != "" {
			titleAuthor = a
		}
		if pr.Origin.Time != "" {
			if t, err := time.Parse(time.RFC3339, pr.Origin.Time); err == nil {
				titleDisplayTime = t
			}
		}
	}
	return fmt.Sprintf("%s  %s · %s · %s · %s", icon, tuicore.TruncateToWidth(pr.Subject, 40), titleAuthor, tuicore.FormatTime(titleDisplayTime), id)
}

// Bindings returns keybindings for this view.
func (v *PRDetailView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "d", Label: "diff", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "r", Label: "review", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "M", Label: "merge", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "C", Label: "close", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "D", Label: "draft", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "S", Label: "sync", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "h", Label: "history", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "i", Label: "interdiff", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "v", Label: "raw", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: tuicore.RawViewHandler},
		{Key: "X", Label: "retract", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "A", Label: "apply suggestion", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "left", Label: "prev", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "right", Label: "next", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ReviewPRDetail}, Handler: push},
	}
}

type prDetailLoadedMsg struct {
	pr             *review.PullRequest
	reviews        []review.Feedback
	reviewComments map[int][]social.Post
	comments       []social.Post
}

type prDetailDiffMsg struct {
	prID           string
	diffStats      git.DiffStats
	commits        []git.Commit
	diffCtx        review.DiffContext
	forkFetched    bool
	behindCount    int
	versionReviews []review.VersionAwareReview
}

// SuggestionAppliedMsg is sent when a suggestion is applied to the working tree.
type SuggestionAppliedMsg struct {
	File string
	Err  error
}
