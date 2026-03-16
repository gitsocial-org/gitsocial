// methods_pm.go - PM extension RPC methods: issues, milestones, sprints, board, comments
package rpc

import (
	"encoding/json"
	"time"

	"github.com/gitsocial-org/gitsocial/extensions/pm"
)

// RegisterPMMethods registers pm.* methods on the server.
func RegisterPMMethods(s *Server) {
	s.registry.Register("pm.getIssues", s.requireInit(pmGetIssues(s)))
	s.registry.Register("pm.getIssue", s.requireInit(pmGetIssue()))
	s.registry.Register("pm.createIssue", s.requireInit(pmCreateIssue(s)))
	s.registry.Register("pm.updateIssue", s.requireInit(pmUpdateIssue(s)))
	s.registry.Register("pm.closeIssue", s.requireInit(pmCloseIssue(s)))
	s.registry.Register("pm.reopenIssue", s.requireInit(pmReopenIssue(s)))
	s.registry.Register("pm.retractIssue", s.requireInit(pmRetractIssue(s)))
	s.registry.Register("pm.getLinks", s.requireInit(pmGetLinks()))
	s.registry.Register("pm.isBlocked", s.requireInit(pmIsBlocked()))
	s.registry.Register("pm.getMilestones", s.requireInit(pmGetMilestones(s)))
	s.registry.Register("pm.getMilestone", s.requireInit(pmGetMilestone()))
	s.registry.Register("pm.createMilestone", s.requireInit(pmCreateMilestone(s)))
	s.registry.Register("pm.updateMilestone", s.requireInit(pmUpdateMilestone(s)))
	s.registry.Register("pm.closeMilestone", s.requireInit(pmCloseMilestone(s)))
	s.registry.Register("pm.reopenMilestone", s.requireInit(pmReopenMilestone(s)))
	s.registry.Register("pm.cancelMilestone", s.requireInit(pmCancelMilestone(s)))
	s.registry.Register("pm.retractMilestone", s.requireInit(pmRetractMilestone(s)))
	s.registry.Register("pm.getMilestoneIssues", s.requireInit(pmGetMilestoneIssues()))
	s.registry.Register("pm.getSprints", s.requireInit(pmGetSprints(s)))
	s.registry.Register("pm.getSprint", s.requireInit(pmGetSprint()))
	s.registry.Register("pm.createSprint", s.requireInit(pmCreateSprint(s)))
	s.registry.Register("pm.updateSprint", s.requireInit(pmUpdateSprint(s)))
	s.registry.Register("pm.activateSprint", s.requireInit(pmActivateSprint(s)))
	s.registry.Register("pm.completeSprint", s.requireInit(pmCompleteSprint(s)))
	s.registry.Register("pm.cancelSprint", s.requireInit(pmCancelSprint(s)))
	s.registry.Register("pm.retractSprint", s.requireInit(pmRetractSprint(s)))
	s.registry.Register("pm.getSprintIssues", s.requireInit(pmGetSprintIssues()))
	s.registry.Register("pm.getBoardView", s.requireInit(pmGetBoardView(s)))
	s.registry.Register("pm.commentOnItem", s.requireInit(pmCommentOnItem(s)))
	s.registry.Register("pm.getItemComments", s.requireInit(pmGetItemComments(s)))
}

// --- Issues ---

func pmGetIssues(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			RepoURL string   `json:"repoURL"`
			Branch  string   `json:"branch"`
			States  []string `json:"states"`
			Limit   int      `json:"limit"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		repoURL := p.RepoURL
		if repoURL == "" {
			repoURL = s.session.RepoURL
		}
		limit := p.Limit
		if limit == 0 {
			limit = 1000
		}
		return fromResult(pm.GetIssues(repoURL, p.Branch, p.States, "", limit))
	}
}

func pmGetIssue() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(pm.GetIssue(p.Ref))
	}
}

func pmCreateIssue(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Subject   string     `json:"subject"`
			Body      string     `json:"body"`
			State     string     `json:"state"`
			Assignees []string   `json:"assignees"`
			Due       string     `json:"due"`
			Milestone string     `json:"milestone"`
			Sprint    string     `json:"sprint"`
			Parent    string     `json:"parent"`
			Root      string     `json:"root"`
			Blocks    []string   `json:"blocks"`
			BlockedBy []string   `json:"blockedBy"`
			Related   []string   `json:"related"`
			Labels    []pm.Label `json:"labels"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Subject == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "subject is required"}
		}
		opts := pm.CreateIssueOptions{
			State:     pm.State(p.State),
			Assignees: p.Assignees,
			Milestone: p.Milestone,
			Sprint:    p.Sprint,
			Parent:    p.Parent,
			Root:      p.Root,
			Blocks:    p.Blocks,
			BlockedBy: p.BlockedBy,
			Related:   p.Related,
			Labels:    p.Labels,
		}
		if p.Due != "" {
			if t, err := time.Parse(time.RFC3339, p.Due); err == nil {
				opts.Due = &t
			}
		}
		return fromResult(pm.CreateIssue(s.session.Workdir, p.Subject, p.Body, opts))
	}
}

func pmUpdateIssue(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref       string      `json:"ref"`
			Subject   *string     `json:"subject"`
			Body      *string     `json:"body"`
			State     *string     `json:"state"`
			Assignees *[]string   `json:"assignees"`
			Due       *string     `json:"due"`
			Milestone *string     `json:"milestone"`
			Sprint    *string     `json:"sprint"`
			Parent    *string     `json:"parent"`
			Root      *string     `json:"root"`
			Blocks    *[]string   `json:"blocks"`
			BlockedBy *[]string   `json:"blockedBy"`
			Related   *[]string   `json:"related"`
			Labels    *[]pm.Label `json:"labels"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		opts := pm.UpdateIssueOptions{
			Subject:   p.Subject,
			Body:      p.Body,
			Assignees: p.Assignees,
			Milestone: p.Milestone,
			Sprint:    p.Sprint,
			Parent:    p.Parent,
			Root:      p.Root,
			Blocks:    p.Blocks,
			BlockedBy: p.BlockedBy,
			Related:   p.Related,
			Labels:    p.Labels,
		}
		if p.State != nil {
			state := pm.State(*p.State)
			opts.State = &state
		}
		if p.Due != nil {
			if t, err := time.Parse(time.RFC3339, *p.Due); err == nil {
				opts.Due = &t
			}
		}
		return fromResult(pm.UpdateIssue(s.session.Workdir, p.Ref, opts))
	}
}

func pmCloseIssue(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.CloseIssue(workdir, ref))
	}, s)
}

func pmReopenIssue(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.ReopenIssue(workdir, ref))
	}, s)
}

func pmRetractIssue(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.RetractIssue(workdir, ref))
	}, s)
}

// --- Links ---

func pmGetLinks() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		blocks, err1 := fromResult(pm.GetBlocking(p.Ref))
		blockedBy, err2 := fromResult(pm.GetBlockedBy(p.Ref))
		related, err3 := fromResult(pm.GetRelated(p.Ref))
		if err1 != nil {
			return nil, err1
		}
		if err2 != nil {
			return nil, err2
		}
		if err3 != nil {
			return nil, err3
		}
		return map[string]any{
			"blocks":    blocks,
			"blockedBy": blockedBy,
			"related":   related,
		}, nil
	}
}

func pmIsBlocked() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return pm.IsBlocked(p.Ref), nil
	}
}

// --- Milestones ---

func pmGetMilestones(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			RepoURL string   `json:"repoURL"`
			Branch  string   `json:"branch"`
			States  []string `json:"states"`
			Limit   int      `json:"limit"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		repoURL := p.RepoURL
		if repoURL == "" {
			repoURL = s.session.RepoURL
		}
		limit := p.Limit
		if limit == 0 {
			limit = 1000
		}
		return fromResult(pm.GetMilestones(repoURL, p.Branch, p.States, "", limit))
	}
}

func pmGetMilestone() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(pm.GetMilestone(p.Ref))
	}
}

func pmCreateMilestone(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			State string `json:"state"`
			Due   string `json:"due"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Title == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "title is required"}
		}
		opts := pm.CreateMilestoneOptions{
			State: pm.State(p.State),
		}
		if p.Due != "" {
			if t, err := time.Parse(time.RFC3339, p.Due); err == nil {
				opts.Due = &t
			}
		}
		return fromResult(pm.CreateMilestone(s.session.Workdir, p.Title, p.Body, opts))
	}
}

func pmUpdateMilestone(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref   string  `json:"ref"`
			Title *string `json:"title"`
			Body  *string `json:"body"`
			State *string `json:"state"`
			Due   *string `json:"due"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		opts := pm.UpdateMilestoneOptions{
			Title: p.Title,
			Body:  p.Body,
		}
		if p.State != nil {
			state := pm.State(*p.State)
			opts.State = &state
		}
		if p.Due != nil {
			if t, err := time.Parse(time.RFC3339, *p.Due); err == nil {
				opts.Due = &t
			}
		}
		return fromResult(pm.UpdateMilestone(s.session.Workdir, p.Ref, opts))
	}
}

func pmCloseMilestone(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.CloseMilestone(workdir, ref))
	}, s)
}

func pmReopenMilestone(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.ReopenMilestone(workdir, ref))
	}, s)
}

func pmCancelMilestone(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.CancelMilestone(workdir, ref))
	}, s)
}

func pmRetractMilestone(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.RetractMilestone(workdir, ref))
	}, s)
}

func pmGetMilestoneIssues() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref    string   `json:"ref"`
			States []string `json:"states"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(pm.GetMilestoneIssues(p.Ref, p.States))
	}
}

// --- Sprints ---

func pmGetSprints(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			RepoURL string   `json:"repoURL"`
			Branch  string   `json:"branch"`
			States  []string `json:"states"`
			Limit   int      `json:"limit"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		repoURL := p.RepoURL
		if repoURL == "" {
			repoURL = s.session.RepoURL
		}
		limit := p.Limit
		if limit == 0 {
			limit = 1000
		}
		return fromResult(pm.GetSprints(repoURL, p.Branch, p.States, "", limit))
	}
}

func pmGetSprint() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(pm.GetSprint(p.Ref))
	}
}

func pmCreateSprint(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			State string `json:"state"`
			Start string `json:"start"`
			End   string `json:"end"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Title == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "title is required"}
		}
		opts := pm.CreateSprintOptions{
			State: pm.SprintState(p.State),
		}
		if p.Start != "" {
			if t, err := time.Parse(time.RFC3339, p.Start); err == nil {
				opts.Start = t
			}
		}
		if p.End != "" {
			if t, err := time.Parse(time.RFC3339, p.End); err == nil {
				opts.End = t
			}
		}
		return fromResult(pm.CreateSprint(s.session.Workdir, p.Title, p.Body, opts))
	}
}

func pmUpdateSprint(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref   string  `json:"ref"`
			Title *string `json:"title"`
			Body  *string `json:"body"`
			State *string `json:"state"`
			Start *string `json:"start"`
			End   *string `json:"end"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		opts := pm.UpdateSprintOptions{
			Title: p.Title,
			Body:  p.Body,
		}
		if p.State != nil {
			state := pm.SprintState(*p.State)
			opts.State = &state
		}
		if p.Start != nil {
			if t, err := time.Parse(time.RFC3339, *p.Start); err == nil {
				opts.Start = &t
			}
		}
		if p.End != nil {
			if t, err := time.Parse(time.RFC3339, *p.End); err == nil {
				opts.End = &t
			}
		}
		return fromResult(pm.UpdateSprint(s.session.Workdir, p.Ref, opts))
	}
}

func pmActivateSprint(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.ActivateSprint(workdir, ref))
	}, s)
}

func pmCompleteSprint(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.CompleteSprint(workdir, ref))
	}, s)
}

func pmCancelSprint(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.CancelSprint(workdir, ref))
	}, s)
}

func pmRetractSprint(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(pm.RetractSprint(workdir, ref))
	}, s)
}

func pmGetSprintIssues() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref    string   `json:"ref"`
			States []string `json:"states"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(pm.GetSprintIssues(p.Ref, p.States))
	}
}

// --- Board ---

func pmGetBoardView(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			BoardID string `json:"boardId"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.BoardID != "" {
			return fromResult(pm.GetBoardViewByID(s.session.Workdir, p.BoardID))
		}
		return fromResult(pm.GetBoardView(s.session.Workdir))
	}
}

// --- Comments ---

func pmCommentOnItem(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref     string `json:"ref"`
			Content string `json:"content"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" || p.Content == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref and content are required"}
		}
		return fromResult(pm.CommentOnItem(s.session.Workdir, p.Ref, p.Content))
	}
}

func pmGetItemComments(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(pm.GetItemComments(p.Ref, s.session.RepoURL))
	}
}

// refAction is a helper for methods that take only a ref param and call a (workdir, ref) function.
func refAction(fn func(workdir, ref string) (any, *RPCError), s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fn(s.session.Workdir, p.Ref)
	}
}
