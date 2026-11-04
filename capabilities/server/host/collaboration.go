package host

import (
	"encoding/json"
	"net/http"
	"sync"

	"golang.org/x/net/websocket"
)

// collabOp is a single collaborative operation (insert or delete text).
type collabOp struct {
	ID     string `json:"id"`     // unique operation id (clientId:seq)
	Type   string `json:"type"`   // "insert" or "delete"
	Pos    int    `json:"pos"`    // position in document
	Text   string `json:"text,omitempty"`
	Count  int    `json:"count,omitempty"`
	Client string `json:"client"`
	Seq    int    `json:"seq"`
}

// collabMsg is a message between client and server.
type collabMsg struct {
	Kind    string   `json:"kind"`    // "op", "sync", "presence", "error"
	Op      *collabOp `json:"op,omitempty"`
	Text    string   `json:"text,omitempty"`
	Version int64    `json:"version,omitempty"`
	Client  string   `json:"client,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// collaborationSession holds shared document state and connected clients.
type collaborationSession struct {
	id      string
	doc     []rune
	version int64
	mu      sync.Mutex
	clients map[*collabClient]bool
}

func newCollaborationSession(id string) *collaborationSession {
	return &collaborationSession{id: id, doc: []rune{}, clients: make(map[*collabClient]bool)}
}

func (s *collaborationSession) apply(op *collabOp) *collabOp {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch op.Type {
	case "insert":
		runes := []rune(op.Text)
		s.doc = append(s.doc[:op.Pos], append(runes, s.doc[op.Pos:]...)...)
		s.version++
	case "delete":
		if op.Pos < 0 || op.Pos+op.Count > len(s.doc) {
			return nil
		}
		s.doc = append(s.doc[:op.Pos], s.doc[op.Pos+op.Count:]...)
		s.version++
	}
	return op
}

func (s *collaborationSession) broadcast(data []byte, exclude *collabClient) {
	s.mu.Lock()
	clients := make([]*collabClient, 0, len(s.clients))
	for c := range s.clients {
		if c != exclude {
			clients = append(clients, c)
		}
	}
	s.mu.Unlock()
	for _, c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (s *collaborationSession) sendSync(c *collabClient) {
	s.mu.Lock()
	text := string(s.doc)
	version := s.version
	s.mu.Unlock()
	msg := collabMsg{Kind: "sync", Text: text, Version: version, Client: c.id}
	data, _ := json.Marshal(msg)
	select {
	case c.send <- data:
	default:
	}
}

func (s *collaborationSession) addClient(c *collabClient) {
	s.mu.Lock()
	s.clients[c] = true
	s.mu.Unlock()
}

func (s *collaborationSession) removeClient(c *collabClient) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
}

// collabClient is a single WebSocket client in a session.
type collabClient struct {
	session *collaborationSession
	conn    *websocket.Conn
	send    chan []byte
	id      string
}

// collaborationHub manages all sessions.
type collaborationHub struct {
	mu      sync.Mutex
	sessions map[string]*collaborationSession
}

func newCollaborationHub() *collaborationHub {
	return &collaborationHub{sessions: make(map[string]*collaborationSession)}
}

func (h *collaborationHub) getOrCreate(id string) *collaborationSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	if !ok {
		s = newCollaborationSession(id)
		h.sessions[id] = s
	}
	return s
}

// Collaboration is the WebSocket handler, bound to a Host for authentication.
type Collaboration struct {
	h *Host
	hub *collaborationHub
}

func newCollaboration(h *Host) *Collaboration {
	return &Collaboration{h: h, hub: newCollaborationHub()}
}

func (c *Collaboration) handleWS(ws *websocket.Conn) {
	sessionID := ws.Request().URL.Query().Get("session")
	if sessionID == "" {
		websocket.Message.Send(ws, `{"kind":"error","error":"missing session"}`)
		return
	}

	auth := ws.Request().Header.Get("Authorization")
	token := ""
	if len(auth) > 7 && auth[:7] == "Bearer " {
		token = auth[7:]
	}

	m, t, ok := c.h.authenticateWS(token)
	if !ok {
		websocket.Message.Send(ws, `{"kind":"error","error":"unauthorized"}`)
		return
	}
	if t == nil || t.quarantined() {
		websocket.Message.Send(ws, `{"kind":"error","error":"tenant unavailable"}`)
		return
	}

	session := c.hub.getOrCreate(sessionID)
	client := &collabClient{session: session, conn: ws, send: make(chan []byte, 256), id: m.ID}
	session.addClient(client)
	defer func() {
		session.removeClient(client)
		close(client.send)
		ws.Close()
	}()

	session.sendSync(client)
	session.broadcast(marshalMsg(collabMsg{Kind: "presence", Client: client.id}), nil)

	// Read pump
	go func() {
		defer ws.Close()
		for {
			var raw string
			if err := websocket.Message.Receive(ws, &raw); err != nil {
				return
			}
			var msg collabMsg
			if err := json.Unmarshal([]byte(raw), &msg); err != nil {
				continue
			}
			if msg.Op != nil {
				result := session.apply(msg.Op)
				if result != nil {
					out := collabMsg{Kind: "op", Op: result, Version: session.version}
					data, _ := json.Marshal(out)
					session.broadcast(data, client)
				}
			}
		}
	}()

	// Write pump
	for data := range client.send {
		if err := websocket.Message.Send(ws, string(data)); err != nil {
			return
		}
	}
}

func marshalMsg(msg collabMsg) []byte {
	data, _ := json.Marshal(msg)
	return data
}

func (c *Collaboration) Handler() http.Handler {
	return websocket.Handler(c.handleWS)
}

// registerCollabRoutes registers WebSocket collaboration routes on the mux.
func (c *Collaboration) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/v1/collaborate/", c.Handler())
}
