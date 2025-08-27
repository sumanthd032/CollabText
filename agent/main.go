package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grandcat/zeroconf"
)

// Client represents a single connected peer (either a browser UI or another agent).
type Client struct {
	conn *websocket.Conn
	// A buffered channel of outbound messages.
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

// run is the core of the Hub. It's a long-running function that listens for events.
func (h *Hub) run() {
	for {
		select {
		// A new client has connected.
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered. Total clients: %d", len(h.clients))

		// A client has disconnected.
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("Client unregistered. Total clients: %d", len(h.clients))
			}

		// A message needs to be broadcast to all clients.
		case message := <-h.broadcast:
			for client := range h.clients {
				// Send the message to the client's send channel.
				// We use a select with a default to avoid blocking if the channel is full.
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

// serveWs handles websocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	client := &Client{conn: conn, send: make(chan []byte, 256)}
	hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in new goroutines.
	go client.writePump()
	go client.readPump(hub)
}

// readPump pumps messages from the websocket connection to the hub.
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
		// When a message is received, send it to the hub's broadcast channel.
		hub.broadcast <- message
	}
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for {
		message, ok := <-c.send
		if !ok {
			// The hub closed the channel.
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		c.conn.WriteMessage(websocket.TextMessage, message)
	}
}

// startDiscovery uses mDNS (zeroconf) to find other peers on the network.
func startDiscovery(hub *Hub, serviceName string, port int) {
	// 1. Register our own service
	host, _ := os.Hostname()
	server, err := zeroconf.Register(
		fmt.Sprintf("%s-%s", "CollabText", host), // Unique instance name
		serviceName, // Service type
		"local.",    // Domain
		port,        // Port
		[]string{"txtv=0", "lo=1", "la=2"}, // TXT records
		nil, // Network interfaces
	)
	if err != nil {
		log.Fatalf("Failed to register mDNS service: %v", err)
	}
	defer server.Shutdown()
	log.Printf("mDNS Service registered: %s on port %d", serviceName, port)

	// 2. Discover other services
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalf("Failed to initialize mDNS resolver: %v", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			log.Printf("mDNS Discovered peer: %s at %s:%d", entry.Instance, entry.AddrIPv4[0], entry.Port)
			// Here you would connect to the peer. For now, we'll just log.
			// In the next steps, we will add logic to establish a WebSocket connection to this peer.
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
	// Start the hub in a separate goroutine.
	go hub.run()

	// Start mDNS discovery in a separate goroutine.
	go startDiscovery(hub, "_collabtext._tcp", 8080)

	// Serve the UI files.
	fs := http.FileServer(http.Dir("../ui"))
	http.Handle("/", fs)

	// Handle WebSocket connections.
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	log.Println("CollabText agent is running on port 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}