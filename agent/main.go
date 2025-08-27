package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/grandcat/zeroconf"
)

// --- CRDT Document State ---
var document []Char
var peerID string
var clock int

// init runs once when the application starts.
func init() {
	peerID = uuid.New().String()
	log.Printf("Initialized Peer with ID: %s", peerID)
	document = []Char{
		{ID: CharID{Clock: 0, PeerID: "start"}, Position: []int{0}},
		{ID: CharID{Clock: 0, PeerID: "end"}, Position: []int{10000}},
	}
}

// handleIncomingOp processes ops from clients and generates a broadcast-able CRDT op.
func handleIncomingOp(op Op) []byte {
	clock++
	var crdtOp Op

	switch op.Action {
	case "raw_insert":
		charToInsert := op.Char.Value
		index := op.Index + 1 // Client index is offset by 1 due to our "start" boundary marker
		if index < 1 { index = 1 }
		if index > len(document)-1 { index = len(document) - 1 }
		posPrev := document[index-1].Position
		posNext := document[index].Position
		newPos := generatePositionBetween(posPrev, posNext)
		newChar := Char{ID: CharID{Clock: clock, PeerID: peerID}, Value: charToInsert, Position: newPos}
		crdtOp = Op{Action: "crdt_insert", Char: newChar, ClientID: op.ClientID}
		applyCrdtInsert(crdtOp)
	case "raw_delete":
		index := op.Index + 1 // Client index is offset by 1
		if index < 1 || index >= len(document)-1 { return nil }
		charToDelete := document[index]
		crdtOp = Op{Action: "crdt_delete", Char: charToDelete, ClientID: op.ClientID}
		applyCrdtDelete(crdtOp)
	}

	opBytes, err := json.Marshal(crdtOp)
	if err != nil {
		log.Printf("Error marshalling CRDT op: %v", err)
		return nil
	}
	return opBytes
}

func applyCrdtInsert(op Op) {
	insertIndex := sort.Search(len(document), func(i int) bool {
		return comparePositions(document[i].Position, op.Char.Position) > 0
	})
	document = append(document[:insertIndex], append([]Char{op.Char}, document[insertIndex:]...)...)
}

func applyCrdtDelete(op Op) {
	deleteIndex := -1
	for i, char := range document {
		if char.ID == op.Char.ID {
			deleteIndex = i
			break
		}
	}
	if deleteIndex != -1 {
		document = append(document[:deleteIndex], document[deleteIndex+1:]...)
	}
}

func generatePositionBetween(pos1, pos2 []int) []int {
	newPos := []int{}
	for i := 0; ; i++ {
		p1 := 0
		if i < len(pos1) { p1 = pos1[i] }
		p2 := 10000
		if i < len(pos2) { p2 = pos2[i] }
		if p2-p1 > 1 {
			delta := rand.Intn(min(10, p2-p1-1)) + 1
			newPos = append(newPos, p1+delta)
			return newPos
		}
		newPos = append(newPos, p1)
	}
}

func comparePositions(pos1, pos2 []int) int {
	for i := 0; i < len(pos1) && i < len(pos2); i++ {
		if pos1[i] < pos2[i] { return -1 }
		if pos1[i] > pos2[i] { return 1 }
	}
	if len(pos1) < len(pos2) { return -1 }
	if len(pos1) > len(pos2) { return 1 }
	return 0
}

func min(a, b int) int { if a < b { return a }; return b }

// --- Networking and Hub Code ---
type Client struct { conn *websocket.Conn; send chan []byte }
type Hub struct { clients map[*Client]bool; broadcast chan []byte; register chan *Client; unregister chan *Client }
func newHub() *Hub { return &Hub{broadcast: make(chan []byte), register: make(chan *Client), unregister: make(chan *Client), clients: make(map[*Client]bool)} }
func (h *Hub) run() { for { select { case client := <-h.register: h.clients[client] = true; case client := <-h.unregister: if _, ok := h.clients[client]; ok { delete(h.clients, client); close(client.send); }; case message := <-h.broadcast: for client := range h.clients { select { case client.send <- message: default: close(client.send); delete(h.clients, client) } } } } }
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) { conn, err := upgrader.Upgrade(w, r, nil); if err != nil { return }; client := &Client{conn: conn, send: make(chan []byte, 256)}; hub.register <- client; go client.writePump(); go client.readPump(hub) }

func (c *Client) readPump(hub *Hub) {
	defer func() { hub.unregister <- c; c.conn.Close() }()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil { break }
		var op Op
		if err := json.Unmarshal(message, &op); err != nil { log.Printf("Error decoding op: %v", err); continue }
		broadcastMsg := handleIncomingOp(op)
		if broadcastMsg != nil { hub.broadcast <- broadcastMsg }
	}
}

func (c *Client) writePump() { defer c.conn.Close(); for { message, ok := <-c.send; if !ok { c.conn.WriteMessage(websocket.CloseMessage, []byte{}); return }; c.conn.WriteMessage(websocket.TextMessage, message) } }

// --- mDNS Discovery ---
func startDiscovery(hub *Hub, serviceName string, port int) {
	host, _ := os.Hostname()
	server, err := zeroconf.Register(fmt.Sprintf("%s-%s", "CollabText", host), serviceName, "local.", port, []string{"txtv=0"}, nil)
	if err != nil { log.Fatalf("Failed to register mDNS: %v", err) }
	defer server.Shutdown()
	log.Printf("mDNS Service registered: %s on port %d", serviceName, port)
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil { log.Fatalf("Failed to init resolver: %v", err) }
	entries := make(chan *zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			log.Printf("mDNS Discovered peer: %s at %s:%d", entry.Instance, entry.AddrIPv4[0], entry.Port)
		}
	}(entries)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = resolver.Browse(ctx, serviceName, "local.", entries)
	if err != nil { log.Fatalf("Failed to browse mDNS: %v", err) }
	<-ctx.Done()
	log.Println("mDNS initial browsing finished.")
}

// --- Main Function ---
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