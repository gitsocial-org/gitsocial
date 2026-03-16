// server.go - JSON-RPC 2.0 stdio server with line-delimited read loop
package rpc

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
)

const maxLineSize = 1024 * 1024 // 1MB

// Server reads JSON-RPC requests from a reader and writes responses to a writer.
type Server struct {
	registry     *Registry
	reader       *bufio.Scanner
	writer       io.Writer
	mu           sync.Mutex
	done         bool
	session      *Session
	subs         subscriptions
	fetchCounter int
}

// subscriptions tracks which event categories the client is subscribed to.
type subscriptions struct {
	mu     sync.RWMutex
	events map[string]bool
}

// NewServer creates a server that reads from in and writes to out.
func NewServer(registry *Registry, in io.Reader, out io.Writer) *Server {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, maxLineSize), maxLineSize)
	return &Server{
		registry: registry,
		reader:   scanner,
		writer:   out,
		session:  &Session{},
		subs:     subscriptions{events: make(map[string]bool)},
	}
}

// Run starts the blocking read loop. Returns nil on clean shutdown.
func (s *Server) Run() error {
	s.registry.Register("shutdown", func(params json.RawMessage) (any, *RPCError) {
		if s.session.Initialized {
			cache.Close()
		}
		s.done = true
		return "ok", nil
	})

	for s.reader.Scan() {
		line := s.reader.Bytes()
		if len(line) == 0 {
			continue
		}

		s.handleLine(line)
		if s.done {
			return nil
		}
	}
	return s.reader.Err()
}

// handleLine parses a line as a batch or single request and sends responses.
func (s *Server) handleLine(line []byte) {
	// Try batch (JSON array)
	var batch []Request
	if err := json.Unmarshal(line, &batch); err == nil {
		if len(batch) == 0 {
			s.send(Response{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: CodeInvalidRequest, Message: "empty batch"},
			})
			return
		}
		responses := make([]Response, 0, len(batch))
		for _, req := range batch {
			if isNotification(req.ID) {
				s.processRequest(req)
				continue
			}
			responses = append(responses, s.processRequest(req))
		}
		if len(responses) > 0 {
			s.sendBatch(responses)
		}
		return
	}

	// Try single request
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.send(Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: CodeParseError, Message: "parse error"},
		})
		return
	}

	if isNotification(req.ID) {
		s.processRequest(req)
		return
	}
	s.send(s.processRequest(req))
}

// isNotification returns true if the request has no id field (JSON-RPC notification).
func isNotification(id json.RawMessage) bool {
	return len(id) == 0
}

// processRequest validates and dispatches a single request.
func (s *Server) processRequest(req Request) Response {
	if req.JSONRPC != "2.0" || req.Method == "" {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidRequest, Message: "invalid request"},
		}
	}

	result, rpcErr := s.registry.Dispatch(req.Method, req.Params)
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
		Error:   rpcErr,
	}
}

// send writes a single JSON-RPC response followed by a newline.
func (s *Server) send(resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(resp)
	_, _ = s.writer.Write(data)
	_, _ = s.writer.Write([]byte("\n"))
}

// sendBatch writes a JSON array of responses followed by a newline.
func (s *Server) sendBatch(responses []Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(responses)
	_, _ = s.writer.Write(data)
	_, _ = s.writer.Write([]byte("\n"))
}

// notification is a server-initiated message with no ID.
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// notify sends a server-initiated notification (no ID). Stub for future use.
func (s *Server) notify(method string, params any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(notification{JSONRPC: "2.0", Method: method, Params: params})
	_, _ = s.writer.Write(data)
	_, _ = s.writer.Write([]byte("\n"))
}

// Emit sends a server notification if the client is subscribed to the event category.
func (s *Server) Emit(event, method string, params any) {
	s.subs.mu.RLock()
	subscribed := s.subs.events[event]
	s.subs.mu.RUnlock()
	if !subscribed {
		return
	}
	s.notify(method, params)
}

// Session returns the current session state.
func (s *Server) Session() *Session {
	return s.session
}

// requireInit wraps a handler to reject calls before initialization.
func (s *Server) requireInit(handler HandlerFunc) HandlerFunc {
	return func(params json.RawMessage) (any, *RPCError) {
		if !s.session.Initialized {
			return nil, appError(CodeNotReady, "NOT_READY", "server not initialized")
		}
		return handler(params)
	}
}
