// methods_search.go - Search RPC methods
package rpc

import (
	"encoding/json"
	"time"

	"github.com/gitsocial-org/gitsocial/core/search"
)

// RegisterSearchMethods registers search methods on the server.
func RegisterSearchMethods(s *Server) {
	s.registry.Register("search", s.requireInit(rpcSearch(s)))
}

func rpcSearch(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Query  string `json:"query"`
			Author string `json:"author"`
			Repo   string `json:"repo"`
			Type   string `json:"type"`
			Hash   string `json:"hash"`
			After  string `json:"after"`
			Before string `json:"before"`
			Limit  int    `json:"limit"`
			Scope  string `json:"scope"`
			Sort   string `json:"sort"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		params := search.Params{
			Query:  p.Query,
			Author: p.Author,
			Repo:   p.Repo,
			Type:   p.Type,
			Hash:   p.Hash,
			Limit:  p.Limit,
			Scope:  p.Scope,
			Sort:   p.Sort,
		}
		if p.After != "" {
			if t, err := time.Parse(time.RFC3339, p.After); err == nil {
				params.After = &t
			}
		}
		if p.Before != "" {
			if t, err := time.Parse(time.RFC3339, p.Before); err == nil {
				params.Before = &t
			}
		}
		result, err := search.Search(s.session.Workdir, params)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: err.Error()}
		}
		return result, nil
	}
}
