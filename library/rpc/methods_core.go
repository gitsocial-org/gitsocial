// methods_core.go - Lifecycle and core methods: initialize, ping, status, config, notifications, settings, push, fetch
package rpc

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/settings"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

type Session struct {
	Workdir     string
	CacheDir    string
	RepoURL     string
	Initialized bool
}

type InitializeParams struct {
	Workdir       string `json:"workdir"`
	CacheDir      string `json:"cacheDir,omitempty"`
	ClientName    string `json:"clientName,omitempty"`
	ClientVersion string `json:"clientVersion,omitempty"`
}

type InitializeResult struct {
	Version    string                     `json:"version"`
	RepoURL    string                     `json:"repoURL"`
	Extensions map[string]ExtensionStatus `json:"extensions"`
}

type ExtensionStatus struct {
	Initialized bool   `json:"initialized"`
	Branch      string `json:"branch,omitempty"`
}

// decodeParams unmarshals JSON params into a typed struct. Returns zero value for empty/null params.
func decodeParams[T any](raw json.RawMessage) (T, *RPCError) {
	var p T
	if len(raw) == 0 || string(raw) == "null" {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, &RPCError{Code: CodeInvalidParams, Message: fmt.Sprintf("invalid params: %s", err)}
	}
	return p, nil
}

// RegisterCoreMethods registers lifecycle and core.* methods on the server.
func RegisterCoreMethods(s *Server, version string) {
	s.registry.Register("ping", func(params json.RawMessage) (any, *RPCError) {
		return "pong", nil
	})
	s.registry.Register("initialize", initializeHandler(s, version))
	s.registry.Register("core.status", s.requireInit(coreStatus(s)))
	s.registry.Register("core.getConfig", s.requireInit(coreGetConfig(s)))
	s.registry.Register("core.setConfig", s.requireInit(coreSetConfig(s)))
	s.registry.Register("core.initExtension", s.requireInit(coreInitExtension(s)))
	s.registry.Register("core.getNotifications", s.requireInit(coreGetNotifications(s)))
	s.registry.Register("core.getUnreadCount", s.requireInit(coreGetUnreadCount(s)))
	s.registry.Register("core.markAsRead", s.requireInit(coreMarkAsRead()))
	s.registry.Register("core.markAllAsRead", s.requireInit(coreMarkAllAsRead(s)))
	s.registry.Register("core.getHistory", s.requireInit(coreGetHistory(s)))
	s.registry.Register("core.getSettings", s.requireInit(coreGetSettings()))
	s.registry.Register("core.setSetting", s.requireInit(coreSetSetting()))
	s.registry.Register("core.push", s.requireInit(corePush(s)))
	s.registry.Register("core.fetch", s.requireInit(coreFetch(s)))
	s.registry.Register("subscribe", coreSubscribe(s))
	s.registry.Register("unsubscribe", coreUnsubscribe(s))
}

func initializeHandler(s *Server, version string) HandlerFunc {
	return func(params json.RawMessage) (any, *RPCError) {
		if s.session.Initialized {
			return nil, appError(CodeConflict, "CONFLICT", "server already initialized")
		}
		p, rpcErr := decodeParams[InitializeParams](params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Workdir == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "workdir is required"}
		}
		if !git.IsRepository(p.Workdir) {
			return nil, appError(CodeNotARepository, "NOT_A_REPOSITORY", "not a git repository")
		}
		cacheDir := p.CacheDir
		if cacheDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, appError(CodeAppInternal, "INTERNAL", "failed to resolve home directory")
			}
			cacheDir = filepath.Join(home, ".cache", "gitsocial")
		}
		if err := cache.Open(cacheDir); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("open cache: %s", err))
		}
		syncExtension("social", p.Workdir, social.SyncWorkspaceToCache)
		syncExtension("pm", p.Workdir, pm.SyncWorkspaceToCache)
		syncExtension("review", p.Workdir, review.SyncWorkspaceToCache)
		syncExtension("release", p.Workdir, release.SyncWorkspaceToCache)
		repoURL := gitmsg.ResolveRepoURL(p.Workdir)
		extensions := buildExtensionStatus(p.Workdir)
		s.session.Workdir = p.Workdir
		s.session.CacheDir = cacheDir
		s.session.RepoURL = repoURL
		s.session.Initialized = true
		return InitializeResult{
			Version:    version,
			RepoURL:    repoURL,
			Extensions: extensions,
		}, nil
	}
}

func coreStatus(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		workdir := s.session.Workdir
		type extStatusWithUnpushed struct {
			Initialized bool   `json:"initialized"`
			Branch      string `json:"branch,omitempty"`
			Unpushed    int    `json:"unpushed,omitempty"`
		}
		extensions := map[string]extStatusWithUnpushed{}
		for _, ext := range []string{"social", "pm", "review", "release"} {
			status := extStatusWithUnpushed{Initialized: gitmsg.IsExtInitialized(workdir, ext)}
			if status.Initialized {
				status.Branch = gitmsg.GetExtBranch(workdir, ext)
				if counts, err := gitmsg.GetUnpushedCounts(workdir, status.Branch); err == nil {
					status.Unpushed = counts.Posts + counts.Lists
				}
			}
			extensions[ext] = status
		}
		return map[string]any{
			"workdir":    workdir,
			"repoURL":    s.session.RepoURL,
			"extensions": extensions,
		}, nil
	}
}

func coreGetConfig(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Extension string `json:"extension"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Extension == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "extension is required"}
		}
		config, err := gitmsg.ReadExtConfig(s.session.Workdir, p.Extension)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("read config: %s", err))
		}
		if config == nil {
			return map[string]any{}, nil
		}
		return config, nil
	}
}

func coreSetConfig(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Extension string                 `json:"extension"`
			Config    map[string]interface{} `json:"config"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Extension == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "extension is required"}
		}
		if p.Config == nil {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "config is required"}
		}
		if err := gitmsg.WriteExtConfig(s.session.Workdir, p.Extension, p.Config); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("write config: %s", err))
		}
		return true, nil
	}
}

func coreInitExtension(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Extension string `json:"extension"`
			Branch    string `json:"branch"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Extension == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "extension is required"}
		}
		if gitmsg.IsExtInitialized(s.session.Workdir, p.Extension) {
			return nil, appError(CodeConflict, "CONFLICT", fmt.Sprintf("extension %s already initialized", p.Extension))
		}
		branch := p.Branch
		if branch == "" {
			branch = "gitmsg/" + p.Extension
		}
		config := map[string]interface{}{
			"branch": branch,
		}
		if err := gitmsg.WriteExtConfig(s.session.Workdir, p.Extension, config); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("init extension: %s", err))
		}
		return true, nil
	}
}

func coreGetNotifications(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			UnreadOnly bool     `json:"unreadOnly"`
			Types      []string `json:"types"`
			Limit      int      `json:"limit"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		items, err := notifications.GetAll(s.session.Workdir, notifications.Filter{
			UnreadOnly: p.UnreadOnly,
			Types:      p.Types,
			Limit:      p.Limit,
		})
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("get notifications: %s", err))
		}
		return items, nil
	}
}

func coreGetUnreadCount(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		count, err := notifications.GetUnreadCount(s.session.Workdir)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("get unread count: %s", err))
		}
		return count, nil
	}
}

func coreMarkAsRead() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			RepoURL string `json:"repoURL"`
			Hash    string `json:"hash"`
			Branch  string `json:"branch"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.RepoURL == "" || p.Hash == "" || p.Branch == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "repoURL, hash, and branch are required"}
		}
		if err := notifications.MarkAsRead(p.RepoURL, p.Hash, p.Branch); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("mark as read: %s", err))
		}
		return true, nil
	}
}

func coreMarkAllAsRead(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		if err := notifications.MarkAllAsRead(s.session.Workdir); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("mark all as read: %s", err))
		}
		return true, nil
	}
}

func coreGetHistory(s *Server) HandlerFunc {
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
		versions, err := gitmsg.GetHistory(p.Ref, s.session.RepoURL)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("get history: %s", err))
		}
		return versions, nil
	}
}

func coreGetSettings() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		path, err := settings.DefaultPath()
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", "failed to resolve settings path")
		}
		s, err := settings.Load(path)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("load settings: %s", err))
		}
		return settings.ListAll(s), nil
	}
}

func coreSetSetting() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Key == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "key is required"}
		}
		path, err := settings.DefaultPath()
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", "failed to resolve settings path")
		}
		s, err := settings.Load(path)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("load settings: %s", err))
		}
		if err := settings.Set(s, p.Key, p.Value); err != nil {
			return nil, appError(CodeInvalidArg, "INVALID_ARGUMENT", err.Error())
		}
		if err := settings.Save(path, s); err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("save settings: %s", err))
		}
		return true, nil
	}
}

func corePush(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		// extensions param accepted for API compatibility; push always pushes all initialized extensions
		_, rpcErr := decodeParams[struct {
			Extensions []string `json:"extensions"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		result, err := gitmsg.Push(s.session.Workdir, false)
		if err != nil {
			return nil, appError(CodeAppInternal, "INTERNAL", fmt.Sprintf("push: %s", err))
		}
		return result, nil
	}
}

func coreFetch(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			ListID   string `json:"listId"`
			Since    string `json:"since"`
			Before   string `json:"before"`
			Parallel int    `json:"parallel"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		s.fetchCounter++
		fetchID := "f-" + strconv.Itoa(s.fetchCounter)
		workdir := s.session.Workdir
		cacheDir := s.session.CacheDir
		fetchAllBranches := resolveRPCWorkspaceMode(workdir)
		go func() {
			opts := &social.FetchOptions{
				ListID:           p.ListID,
				Since:            p.Since,
				Before:           p.Before,
				Parallel:         p.Parallel,
				FetchAllBranches: fetchAllBranches,
				OnProgress: func(repoURL string, processed, total int) {
					s.Emit("fetch", "fetch.progress", map[string]any{
						"fetchId":   fetchID,
						"repoURL":   repoURL,
						"processed": processed,
						"total":     total,
					})
				},
			}
			result := social.Fetch(workdir, cacheDir, opts)
			syncExtension("social", workdir, social.SyncWorkspaceToCache)
			syncExtension("pm", workdir, pm.SyncWorkspaceToCache)
			syncExtension("review", workdir, review.SyncWorkspaceToCache)
			syncExtension("release", workdir, release.SyncWorkspaceToCache)
			review.FetchForks(workdir, cacheDir)
			if result.Success {
				errCount := len(result.Data.Errors)
				s.Emit("fetch", "fetch.complete", map[string]any{
					"fetchId":      fetchID,
					"repositories": result.Data.Repositories,
					"newCommits":   result.Data.Posts,
					"errors":       errCount,
				})
			} else {
				s.Emit("fetch", "fetch.error", map[string]any{
					"fetchId": fetchID,
					"message": result.Error.Message,
				})
			}
			count, _ := notifications.GetUnreadCount(workdir)
			s.Emit("notifications", "notifications.changed", map[string]any{
				"unreadCount": count,
			})
		}()
		return map[string]string{"fetchId": fetchID}, nil
	}
}

func coreSubscribe(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Events []string `json:"events"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if len(p.Events) == 0 {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "events is required"}
		}
		s.subs.mu.Lock()
		for _, event := range p.Events {
			s.subs.events[event] = true
		}
		s.subs.mu.Unlock()
		return true, nil
	}
}

func coreUnsubscribe(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Events []string `json:"events"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if len(p.Events) == 0 {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "events is required"}
		}
		s.subs.mu.Lock()
		for _, event := range p.Events {
			delete(s.subs.events, event)
		}
		s.subs.mu.Unlock()
		return true, nil
	}
}

// buildExtensionStatus returns status for all known extensions.
func buildExtensionStatus(workdir string) map[string]ExtensionStatus {
	extensions := map[string]ExtensionStatus{}
	for _, ext := range []string{"social", "pm", "review", "release"} {
		status := ExtensionStatus{Initialized: gitmsg.IsExtInitialized(workdir, ext)}
		if status.Initialized {
			status.Branch = gitmsg.GetExtBranch(workdir, ext)
		}
		extensions[ext] = status
	}
	return extensions
}

// resolveRPCWorkspaceMode reads the workspace mode setting. Defaults to "default" if unset.
func resolveRPCWorkspaceMode(workdir string) bool {
	originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
	if originURL == "" {
		return false
	}
	settingsPath, err := settings.DefaultPath()
	if err != nil {
		return false
	}
	s, err := settings.Load(settingsPath)
	if err != nil {
		return false
	}
	mode := settings.GetWorkspaceMode(s, originURL)
	return mode == "*"
}

// syncExtension runs a sync function and logs errors without failing.
func syncExtension(name, workdir string, fn func(string) error) {
	if err := fn(workdir); err != nil {
		log.Printf("sync %s: %s", name, err)
	}
}
