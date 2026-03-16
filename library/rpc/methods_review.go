// methods_review.go - Review extension RPC methods: pull requests, feedback, diffs, forks
package rpc

import (
	"encoding/json"
	"fmt"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/review"
)

// RegisterReviewMethods registers review.* methods on the server.
func RegisterReviewMethods(s *Server) {
	s.registry.Register("review.getPullRequests", s.requireInit(reviewGetPullRequests(s)))
	s.registry.Register("review.getPR", s.requireInit(reviewGetPR()))
	s.registry.Register("review.createPR", s.requireInit(reviewCreatePR(s)))
	s.registry.Register("review.updatePR", s.requireInit(reviewUpdatePR(s)))
	s.registry.Register("review.mergePR", s.requireInit(reviewMergePR(s)))
	s.registry.Register("review.closePR", s.requireInit(reviewClosePR(s)))
	s.registry.Register("review.retractPR", s.requireInit(reviewRetractPR(s)))
	s.registry.Register("review.markReady", s.requireInit(reviewMarkReady(s)))
	s.registry.Register("review.convertToDraft", s.requireInit(reviewConvertToDraft(s)))
	s.registry.Register("review.getFeedbackForPR", s.requireInit(reviewGetFeedbackForPR(s)))
	s.registry.Register("review.createFeedback", s.requireInit(reviewCreateFeedback(s)))
	s.registry.Register("review.updateFeedback", s.requireInit(reviewUpdateFeedback(s)))
	s.registry.Register("review.retractFeedback", s.requireInit(reviewRetractFeedback(s)))
	s.registry.Register("review.applySuggestion", s.requireInit(reviewApplySuggestion(s)))
	s.registry.Register("review.getDiff", s.requireInit(reviewGetDiff(s)))
	s.registry.Register("review.getDiffStats", s.requireInit(reviewGetDiffStats(s)))
	s.registry.Register("review.getFileDiff", s.requireInit(reviewGetFileDiff(s)))
	s.registry.Register("review.getFileContent", s.requireInit(reviewGetFileContent(s)))
	s.registry.Register("review.getPRComments", s.requireInit(reviewGetPRComments(s)))
	s.registry.Register("review.getForks", s.requireInit(reviewGetForks(s)))
	s.registry.Register("review.addFork", s.requireInit(reviewAddFork(s)))
	s.registry.Register("review.removeFork", s.requireInit(reviewRemoveFork(s)))
}

// --- Pull Requests ---

func reviewGetPullRequests(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			RepoURL      string   `json:"repoURL"`
			Branch       string   `json:"branch"`
			States       []string `json:"states"`
			Limit        int      `json:"limit"`
			IncludeForks bool     `json:"includeForks"`
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
		if p.IncludeForks {
			forks := review.GetForks(s.session.Workdir)
			return fromResult(review.GetPullRequestsWithForks(repoURL, p.Branch, forks, p.States, "", limit))
		}
		return fromResult(review.GetPullRequests(repoURL, p.Branch, p.States, "", limit))
	}
}

func reviewGetPR() HandlerFunc {
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
		return fromResult(review.GetPR(p.Ref))
	}
}

func reviewCreatePR(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Subject   string   `json:"subject"`
			Body      string   `json:"body"`
			Base      string   `json:"base"`
			Head      string   `json:"head"`
			Closes    []string `json:"closes"`
			Reviewers []string `json:"reviewers"`
			MergeBase string   `json:"mergeBase"`
			MergeHead string   `json:"mergeHead"`
			Draft     bool     `json:"draft"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Subject == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "subject is required"}
		}
		return fromResult(review.CreatePR(s.session.Workdir, p.Subject, p.Body, review.CreatePROptions{
			Base:      p.Base,
			Head:      p.Head,
			Closes:    p.Closes,
			Reviewers: p.Reviewers,
			MergeBase: p.MergeBase,
			MergeHead: p.MergeHead,
			Draft:     p.Draft,
		}))
	}
}

func reviewUpdatePR(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref       string    `json:"ref"`
			Subject   *string   `json:"subject"`
			Body      *string   `json:"body"`
			State     *string   `json:"state"`
			Draft     *bool     `json:"draft"`
			Base      *string   `json:"base"`
			Head      *string   `json:"head"`
			Closes    *[]string `json:"closes"`
			Reviewers *[]string `json:"reviewers"`
			MergeBase *string   `json:"mergeBase"`
			MergeHead *string   `json:"mergeHead"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		opts := review.UpdatePROptions{
			Subject:   p.Subject,
			Body:      p.Body,
			Draft:     p.Draft,
			Base:      p.Base,
			Head:      p.Head,
			Closes:    p.Closes,
			Reviewers: p.Reviewers,
			MergeBase: p.MergeBase,
			MergeHead: p.MergeHead,
		}
		if p.State != nil {
			state := review.PRState(*p.State)
			opts.State = &state
		}
		return fromResult(review.UpdatePR(s.session.Workdir, p.Ref, opts))
	}
}

func reviewMergePR(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(review.MergePR(workdir, ref, review.MergeStrategyFF))
	}, s)
}

func reviewClosePR(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(review.ClosePR(workdir, ref))
	}, s)
}

func reviewRetractPR(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(review.RetractPR(workdir, ref))
	}, s)
}

func reviewMarkReady(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(review.MarkReady(workdir, ref))
	}, s)
}

func reviewConvertToDraft(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(review.ConvertToDraft(workdir, ref))
	}, s)
}

// --- Feedback ---

func reviewGetFeedbackForPR(s *Server) HandlerFunc {
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
		parsed := protocol.ParseRef(p.Ref)
		repoURL := parsed.Repository
		if repoURL == "" {
			repoURL = s.session.RepoURL
		}
		return fromResult(review.GetFeedbackForPR(repoURL, parsed.Value, parsed.Branch))
	}
}

func reviewCreateFeedback(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Content     string `json:"content"`
			PullRequest string `json:"pullRequest"`
			Commit      string `json:"commit"`
			File        string `json:"file"`
			OldLine     int    `json:"oldLine"`
			NewLine     int    `json:"newLine"`
			OldLineEnd  int    `json:"oldLineEnd"`
			NewLineEnd  int    `json:"newLineEnd"`
			ReviewState string `json:"reviewState"`
			Suggestion  bool   `json:"suggestion"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Content == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "content is required"}
		}
		return fromResult(review.CreateFeedback(s.session.Workdir, p.Content, review.CreateFeedbackOptions{
			PullRequest: p.PullRequest,
			Commit:      p.Commit,
			File:        p.File,
			OldLine:     p.OldLine,
			NewLine:     p.NewLine,
			OldLineEnd:  p.OldLineEnd,
			NewLineEnd:  p.NewLineEnd,
			ReviewState: review.ReviewState(p.ReviewState),
			Suggestion:  p.Suggestion,
		}))
	}
}

func reviewUpdateFeedback(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref         string  `json:"ref"`
			Content     *string `json:"content"`
			ReviewState *string `json:"reviewState"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		opts := review.UpdateFeedbackOptions{
			Content: p.Content,
		}
		if p.ReviewState != nil {
			state := review.ReviewState(*p.ReviewState)
			opts.ReviewState = &state
		}
		return fromResult(review.UpdateFeedback(s.session.Workdir, p.Ref, opts))
	}
}

func reviewRetractFeedback(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(review.RetractFeedback(workdir, ref))
	}, s)
}

func reviewApplySuggestion(s *Server) HandlerFunc {
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
		fb := review.GetFeedback(p.Ref)
		if !fb.Success {
			return fromResult(fb)
		}
		return fromResult(review.ApplySuggestion(s.session.Workdir, fb.Data))
	}
}

// --- Diffs ---

func reviewGetDiff(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		ctx, rpcErr := resolvePRDiff(s, raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		diffs, err := git.GetDiff(ctx.Workdir, ctx.Base, ctx.Head)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("get diff: %s", err))
		}
		return diffs, nil
	}
}

func reviewGetDiffStats(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		ctx, rpcErr := resolvePRDiff(s, raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		stats, err := git.GetDiffStats(ctx.Workdir, ctx.Base, ctx.Head)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("get diff stats: %s", err))
		}
		return stats, nil
	}
}

func reviewGetFileDiff(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref  string `json:"ref"`
			File string `json:"file"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		if p.File == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "file is required"}
		}
		pr := review.GetPR(p.Ref)
		if !pr.Success {
			return fromResult(pr)
		}
		ctx := review.ResolveDiffContext(s.session.Workdir, s.session.CacheDir, pr.Data.Base, pr.Data.Head)
		diff, err := git.GetFileDiff(ctx.Workdir, ctx.Base, ctx.Head, p.File)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("get file diff: %s", err))
		}
		return diff, nil
	}
}

func reviewGetFileContent(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref  string `json:"ref"`
			File string `json:"file"`
			Side string `json:"side"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		if p.File == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "file is required"}
		}
		pr := review.GetPR(p.Ref)
		if !pr.Success {
			return fromResult(pr)
		}
		ctx := review.ResolveDiffContext(s.session.Workdir, s.session.CacheDir, pr.Data.Base, pr.Data.Head)
		ref := ctx.Head
		if p.Side == "base" {
			ref = ctx.Base
		}
		content, err := git.GetFileContent(ctx.Workdir, ref, p.File)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("get file content: %s", err))
		}
		return content, nil
	}
}

// resolvePRDiff decodes a ref param, resolves the PR, and returns the diff context.
func resolvePRDiff(s *Server, raw json.RawMessage) (review.DiffContext, *RPCError) {
	p, rpcErr := decodeParams[struct {
		Ref string `json:"ref"`
	}](raw)
	if rpcErr != nil {
		return review.DiffContext{}, rpcErr
	}
	if p.Ref == "" {
		return review.DiffContext{}, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
	}
	pr := review.GetPR(p.Ref)
	if !pr.Success {
		_, rpcErr := fromResult(pr)
		return review.DiffContext{}, rpcErr
	}
	return review.ResolveDiffContext(s.session.Workdir, s.session.CacheDir, pr.Data.Base, pr.Data.Head), nil
}

// --- Comments ---

func reviewGetPRComments(s *Server) HandlerFunc {
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
		return fromResult(review.GetPRComments(p.Ref, s.session.RepoURL))
	}
}

// --- Forks ---

func reviewGetForks(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		return review.GetForks(s.session.Workdir), nil
	}
}

func reviewAddFork(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			URL string `json:"url"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.URL == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "url is required"}
		}
		if err := review.AddFork(s.session.Workdir, p.URL); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("add fork: %s", err))
		}
		return true, nil
	}
}

func reviewRemoveFork(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			URL string `json:"url"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.URL == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "url is required"}
		}
		if err := review.RemoveFork(s.session.Workdir, p.URL); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("remove fork: %s", err))
		}
		return true, nil
	}
}
