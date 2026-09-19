// methods_social.go - Social extension RPC methods: posts, lists, repositories, logs, search
package rpc

import (
	"encoding/json"
	"time"

	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// RegisterSocialMethods registers social.* methods on the server.
func RegisterSocialMethods(s *Server) {
	s.registry.Register("social.getPosts", s.requireInit(socialGetPosts(s)))
	s.registry.Register("social.createPost", s.requireInit(socialCreatePost(s)))
	s.registry.Register("social.editPost", s.requireInit(socialEditPost(s)))
	s.registry.Register("social.retractPost", s.requireInit(socialRetractPost(s)))
	s.registry.Register("social.createComment", s.requireInit(socialCreateComment(s)))
	s.registry.Register("social.createRepost", s.requireInit(socialCreateRepost(s)))
	s.registry.Register("social.createQuote", s.requireInit(socialCreateQuote(s)))
	s.registry.Register("social.getLists", s.requireInit(socialGetLists(s)))
	s.registry.Register("social.getList", s.requireInit(socialGetList(s)))
	s.registry.Register("social.createList", s.requireInit(socialCreateList(s)))
	s.registry.Register("social.deleteList", s.requireInit(socialDeleteList(s)))
	s.registry.Register("social.addToList", s.requireInit(socialAddToList(s)))
	s.registry.Register("social.removeFromList", s.requireInit(socialRemoveFromList(s)))
	s.registry.Register("social.getRepositories", s.requireInit(socialGetRepositories(s)))
	s.registry.Register("social.getLogs", s.requireInit(socialGetLogs(s)))
}

// socialGetPosts handles social.getPosts, returning a scope's posts.
func socialGetPosts(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Scope           string   `json:"scope"`
			Types           []string `json:"types"`
			Since           string   `json:"since"`
			Until           string   `json:"until"`
			Limit           int      `json:"limit"`
			IncludeImplicit bool     `json:"includeImplicit"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		opts := &social.GetPostsOptions{
			Limit:           p.Limit,
			IncludeImplicit: p.IncludeImplicit,
		}
		for _, t := range p.Types {
			opts.Types = append(opts.Types, social.PostType(t))
		}
		if p.Since != "" {
			if t, err := time.Parse(time.RFC3339, p.Since); err == nil {
				opts.Since = &t
			}
		}
		if p.Until != "" {
			if t, err := time.Parse(time.RFC3339, p.Until); err == nil {
				opts.Until = &t
			}
		}
		scope := p.Scope
		if scope == "" {
			scope = "timeline"
		}
		return fromResult(social.GetPosts(s.session.Workdir, scope, opts))
	}
}

// socialCreatePost handles social.createPost, publishing a post.
func socialCreatePost(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Content string `json:"content"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Content == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "content is required"}
		}
		return fromResult(social.CreatePost(s.session.Workdir, p.Content, nil))
	}
}

// socialEditPost handles social.editPost, replacing a post's content.
func socialEditPost(s *Server) HandlerFunc {
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
		return fromResult(social.EditPost(s.session.Workdir, p.Ref, p.Content, nil))
	}
}

// socialRetractPost handles social.retractPost, retracting a post.
func socialRetractPost(s *Server) HandlerFunc {
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
		return fromResult(social.RetractPost(s.session.Workdir, p.Ref))
	}
}

// socialCreateComment handles social.createComment, commenting on a target.
func socialCreateComment(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Target  string `json:"target"`
			Content string `json:"content"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Target == "" || p.Content == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "target and content are required"}
		}
		return fromResult(social.CreateComment(s.session.Workdir, p.Target, p.Content, nil))
	}
}

// socialCreateRepost handles social.createRepost, reposting a target.
func socialCreateRepost(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Target string `json:"target"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Target == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "target is required"}
		}
		return fromResult(social.CreateRepost(s.session.Workdir, p.Target, nil))
	}
}

// socialCreateQuote handles social.createQuote, quoting a target.
func socialCreateQuote(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Target  string `json:"target"`
			Content string `json:"content"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Target == "" || p.Content == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "target and content are required"}
		}
		return fromResult(social.CreateQuote(s.session.Workdir, p.Target, p.Content, nil))
	}
}

// socialGetLists handles social.getLists, returning every list.
func socialGetLists(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		return fromResult(social.GetLists(s.session.Workdir))
	}
}

// socialGetList handles social.getList, returning one list by id.
func socialGetList(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			ID string `json:"id"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.ID == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "id is required"}
		}
		return fromResult(social.GetList(s.session.Workdir, p.ID))
	}
}

// socialCreateList handles social.createList, creating a list.
func socialCreateList(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.ID == "" || p.Name == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "id and name are required"}
		}
		return fromResult(social.CreateList(s.session.Workdir, p.ID, p.Name))
	}
}

// socialDeleteList handles social.deleteList, deleting a list.
func socialDeleteList(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			ID string `json:"id"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.ID == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "id is required"}
		}
		return fromResult(social.DeleteList(s.session.Workdir, p.ID))
	}
}

// socialAddToList handles social.addToList, adding a repository to a list.
func socialAddToList(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			ListID      string `json:"listId"`
			RepoURL     string `json:"repoURL"`
			Branch      string `json:"branch"`
			AllBranches bool   `json:"allBranches"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.ListID == "" || p.RepoURL == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "listId and repoURL are required"}
		}
		return fromResult(social.AddRepositoryToList(s.session.Workdir, p.ListID, p.RepoURL, p.Branch, p.AllBranches))
	}
}

// socialRemoveFromList handles social.removeFromList, removing a repository from a list.
func socialRemoveFromList(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			ListID  string `json:"listId"`
			RepoURL string `json:"repoURL"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.ListID == "" || p.RepoURL == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "listId and repoURL are required"}
		}
		return fromResult(social.RemoveRepositoryFromList(s.session.Workdir, p.ListID, p.RepoURL))
	}
}

// socialGetRepositories handles social.getRepositories, returning a scope's repositories.
func socialGetRepositories(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Scope string `json:"scope"`
			Limit int    `json:"limit"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return fromResult(social.GetRepositories(s.session.Workdir, p.Scope, p.Limit))
	}
}

// socialGetLogs handles social.getLogs, returning filtered log entries.
func socialGetLogs(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Scope  string   `json:"scope"`
			Limit  int      `json:"limit"`
			Types  []string `json:"types"`
			After  string   `json:"after"`
			Before string   `json:"before"`
			Author string   `json:"author"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		opts := &social.GetLogsOptions{
			Limit:  p.Limit,
			Author: p.Author,
		}
		for _, t := range p.Types {
			opts.Types = append(opts.Types, social.LogEntryType(t))
		}
		if p.After != "" {
			if t, err := time.Parse(time.RFC3339, p.After); err == nil {
				opts.After = &t
			}
		}
		if p.Before != "" {
			if t, err := time.Parse(time.RFC3339, p.Before); err == nil {
				opts.Before = &t
			}
		}
		return fromResult(social.GetLogs(s.session.Workdir, p.Scope, opts))
	}
}
