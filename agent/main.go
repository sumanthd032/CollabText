package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grandcat/zeroconf"
)

// --- Document State ---
// We hold the document state in memory on the agent.
var document []string
var docMutex = &sync.Mutex{}

// applyOp modifies the document based on an Op. It's protected by a mutex.
func applyOp(op Op) {
	docMutex.Lock()
	defer docMutex.Unlock()

	switch op.Action {
	case "insert":
		if op.Index > len(document) { // Bounds check
			document = append(document, op.Char)
		} else {
			// Insert the character at the given index
			document = append(document[:op.Index], append([]string{op.Char}, document[op.Index:]...)...)
		}
	case "delete":
		if op.Index < len(document) { // Bounds check
			// Delete the character at the given index
			document = append(document[:op.Index], document[op.Index+1:]...)
		}
	}
}

// Client represents a single connected peer (either a browser UI or another agent).
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered. Total clients: %d", len(h.clients))
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("Client unregistered. Total clients: %d", len(h.clients))
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{conn: conn, send: make(chan []byte, 256)}
	hub.register <- client
	go client.writePump()
	go client.readPump(hub)
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var op Op
		if err := json.Unmarshal(message, &op); err != nil {
			log.Printf("Error decoding op: %v", err)
			continue
		}
		applyOp(op)
		hub.broadcast <- message
	}
}

func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for {
		message, ok := <-c.send
		if !ok {
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		c.conn.WriteMessage(websocket.TextMessage, message)
	}
}

func startDiscovery(hub *Hub, serviceName string, port int) {
	host, _ := os.Hostname()
	server, err := zeroconf.Register(
		fmt.Sprintf("%s-%s", "CollabText", host),
		serviceName,
		"local.",
		port,
		[]string{"txtv=0", "lo=1", "la=2"},
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to register mDNS service: %v", err)
	}
	defer server.Shutdown()
	log.Printf("mDNS Service registered: %s on port %d", serviceName, port)
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalf("Failed to initialize mDNS resolver: %v", err)
	}
	entries := make(chan *zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			log.Printf("mDNS Discovered peer: %s at %s:%d", entry.Instance, entry.AddrIPv4[0], entry.Port)
		}
	}(entries)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	err = resolver.Browse(ctx, serviceName, "local.", entries)
	if err != nil {
		log.Fatalf("Failed to browse for mDNS services: %v", err)
	}
	<-ctx.Done()
	log.Println("mDNS browsing finished.")
}

func main() {
	hub := newHub()
	go hub.run()
	go startDiscovery(hub, "_collabtext._tcp", 8080)
	fs := http.FileServer(http.Dir("../ui"))
	http.Handle("/", fs)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})
	log.Println("CollabText agent is running on port 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}