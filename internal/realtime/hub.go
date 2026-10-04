// Package realtime fans out bounded aggregate events to WebSocket clients.
// It never carries raw samples (docs/API.md §12): one event per window.
package realtime

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	c "network-traffic-visualizer/internal/contract"
)

// Client receives pre-serialised envelopes.
type Client struct {
	Send chan []byte
}

type Hub struct {
	mu         sync.Mutex
	clients    map[*Client]struct{}
	maxClients int
	seq        atomic.Int64
	dropped    atomic.Int64
}

func NewHub(maxClients int) *Hub {
	return &Hub{clients: map[*Client]struct{}{}, maxClients: maxClients}
}

// Subscribe registers a client; ok is false when the client limit is reached.
func (h *Hub) Subscribe() (cl *Client, unsubscribe func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxClients > 0 && len(h.clients) >= h.maxClients {
		return nil, nil, false
	}
	cl = &Client{Send: make(chan []byte, 8)}
	h.clients[cl] = struct{}{}
	return cl, func() {
		h.mu.Lock()
		delete(h.clients, cl)
		h.mu.Unlock()
	}, true
}

func (h *Hub) Envelope(typ string, payload any) c.ServerEnvelope {
	return c.ServerEnvelope{Type: typ, Sequence: h.seq.Add(1), ServerTime: c.TS(time.Now()), Payload: payload}
}

// Broadcast sends to every client without blocking. A slow client misses the
// event (the next window supersedes it) and the drop is counted.
func (h *Hub) Broadcast(env c.ServerEnvelope) {
	msg, err := json.Marshal(env)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for cl := range h.clients {
		select {
		case cl.Send <- msg:
		default:
			h.dropped.Add(1)
		}
	}
}

func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) Dropped() int64 { return h.dropped.Load() }
