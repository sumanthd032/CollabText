package main

import (
	"context"
	"encoding/json"
	"flag"
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
	"go.etcd.io/bbolt"
)

// --- Global State ---
var document []Char
var peerID string
var clock int
var db *bbolt.DB
var hub *Hub
var serverConn *websocket.Conn

const serverAddr = "ws://localhost:8081/ws"
const docBucket = "documents"
const defaultDocKey = "default_doc"

// --- Server Connection Logic ---

func connectToServer() {
	for {
		log.Println("Attempting to connect to central server...")
		conn, _, err := websocket.DefaultDialer.Dial(serverAddr, nil)
		if err != nil {
			log.Printf("Failed to connect to server: %v. Retrying in 5 seconds...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("Successfully connected to central server.")
		serverConn = conn
		readFromServer()
		serverConn = nil
		log.Println("Disconnected from central server. Will attempt to reconnect.")
	}
}

func readFromServer() {
	defer serverConn.Close()
	for {
		_, message, err := serverConn.ReadMessage()
		if err != nil {
			log.Printf("Error reading from server: %v", err)
			return
		}
		var op Op
		if err := json.Unmarshal(message, &op); err != nil {
			log.Printf("Error decoding op from server: %v", err)
			continue
		}
		if op.Action == "crdt_insert" {
			applyCrdtInsert(op)
		} else if op.Action == "crdt_delete" {
			applyCrdtDelete(op)
		}
		saveDocument()
		hub.broadcast <- message
	}
}

func broadcastOp(opBytes []byte) {
	hub.broadcast <- opBytes
	if serverConn != nil {
		if err := serverConn.WriteMessage(websocket.TextMessage, opBytes); err != nil {
			log.Printf("Error sending message to server: %v", err)
		}
	}
}

// --- Database Functions ---

func loadDocument() {
	err := db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(docBucket))
		if bucket == nil { return nil }
		docBytes := bucket.Get([]byte(defaultDocKey))
		if docBytes == nil { return nil }
		return json.Unmarshal(docBytes, &document)
	})
	if err != nil {
		log.Fatalf("Failed to load document: %v", err)
	}
	if len(document) == 0 {
		document = []Char{
			{ID: CharID{Clock: 0, PeerID: "start"}, Position: []int{0}},
			{ID: CharID{Clock: 0, PeerID: "end"}, Position: []int{10000}},
		}
		log.Println("Initialized a new empty document.")
	} else {
		log.Println("Document loaded successfully from database.")
	}
}

func saveDocument() {
	err := db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(docBucket))
		if err != nil { return fmt.Errorf("create bucket: %s", err) }
		docBytes, err := json.Marshal(document)
		if err != nil { return fmt.Errorf("marshal document: %s", err) }
		return bucket.Put([]byte(defaultDocKey), docBytes)
	})
	if err != nil {
		log.Printf("Error saving document: %v", err)
	}
}

// --- CRDT and Op Handling ---

func handleIncomingOp(op Op) {
	clock++
	var crdtOp Op
	modified := false

	switch op.Action {
	case "raw_insert":
		charToInsert := op.Char.Value
		index := op.Index + 1
		if index < 1 { index = 1 }
		if index > len(document)-1 { index = len(document) - 1 }
		posPrev := document[index-1].Position
		posNext := document[index].Position
		newPos := generatePositionBetween(posPrev, posNext)
		newChar := Char{ID: CharID{Clock: clock, PeerID: peerID}, Value: charToInsert, Position: newPos}
		crdtOp = Op{Action: "crdt_insert", Char: newChar, ClientID: op.ClientID}
		applyCrdtInsert(crdtOp)
		modified = true
	case "raw_delete":
		index := op.Index + 1
		if index < 1 || index >= len(document)-1 { return }
		charToDelete := document[index]
		crdtOp = Op{Action: "crdt_delete", Char: charToDelete, ClientID: op.ClientID}
		applyCrdtDelete(crdtOp)
		modified = true
	}

	if modified {
		saveDocument()
		opBytes, err := json.Marshal(crdtOp)
		if err != nil {
			log.Printf("Error marshalling CRDT op: %v", err)
			return
		}
		broadcastOp(opBytes)
	}
}

func applyCrdtInsert(op Op) {
	for _, char := range document {
		if char.ID == op.Char.ID {
			return
		}
	}
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
		p1 := 0; if i < len(pos1) { p1 = pos1[i] }; p2 := 10000; if i < len(pos2) { p2 = pos2[i] };
		if p2-p1 > 1 {
			delta := rand.Intn(min(10, p2-p1-1)) + 1
			newPos = append(newPos, p1+delta)
			return newPos
		}
		newPos = append(newPos, p1)
	}
}

func comparePositions(pos1, pos2 []int) int { for i := 0; i < len(pos1) && i < len(pos2); i++ { if pos1[i] < pos2[i] { return -1 }; if pos1[i] > pos2[i] { return 1 } }; if len(pos1) < len(pos2) { return -1 }; if len(pos1) > len(pos2) { return 1 }; return 0 }
func min(a, b int) int { if a < b { return a }; return b }

// --- Networking and Hub Code ---
type Client struct { conn *websocket.Conn; send chan []byte }
type Hub struct { clients map[*Client]bool; broadcast chan []byte; register chan *Client; unregister chan *Client }
func newHub() *Hub { return &Hub{broadcast: make(chan []byte), register: make(chan *Client), unregister: make(chan *Client), clients: make(map[*Client]bool)} }
func (h *Hub) run() { for { select { case client := <-h.register: h.clients[client] = true; case client := <-h.unregister: if _, ok := h.clients[client]; ok { delete(h.clients, client); close(client.send); }; case message := <-h.broadcast: for client := range h.clients { select { case client.send <- message: default: close(client.send); delete(h.clients, client) } } } } }
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) { conn, err := upgrader.Upgrade(w, r, nil); if err != nil { return }; client := &Client{conn: conn, send: make(chan []byte, 256)}; hub.register <- client; go client.writePump(); go client.readPump(hub) }
func (c *Client) readPump(hub *Hub) { defer func() { hub.unregister <- c; c.conn.Close() }(); for { _, message, err := c.conn.ReadMessage(); if err != nil { break }; var op Op; if err := json.Unmarshal(message, &op); err != nil { log.Printf("Error decoding op: %v", err); continue }; handleIncomingOp(op) } }
func (c *Client) writePump() { defer c.conn.Close(); for { message, ok := <-c.send; if !ok { c.conn.WriteMessage(websocket.CloseMessage, []byte{}); return }; c.conn.WriteMessage(websocket.TextMessage, message) } }

// --- mDNS Discovery ---
func startDiscovery(hub *Hub, serviceName string, port int) { host, _ := os.Hostname(); server, err := zeroconf.Register(fmt.Sprintf("%s-%s", "CollabText", host), serviceName, "local.", port, []string{"txtv=0"}, nil); if err != nil { log.Fatalf("Failed to register mDNS: %v", err) }; defer server.Shutdown(); log.Printf("mDNS Service registered: %s on port %d", serviceName, port); resolver, err := zeroconf.NewResolver(nil); if err != nil { log.Fatalf("Failed to init resolver: %v", err) }; entries := make(chan *zeroconf.ServiceEntry); go func(results <-chan *zeroconf.ServiceEntry) { for entry := range results { log.Printf("mDNS Discovered peer: %s at %s:%d", entry.Instance, entry.AddrIPv4[0], entry.Port) } }(entries); ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second); defer cancel(); err = resolver.Browse(ctx, serviceName, "local.", entries); if err != nil { log.Fatalf("Failed to browse mDNS: %v", err) }; <-ctx.Done(); log.Println("mDNS initial browsing finished.") }

// --- Main Function ---
func main() {
	// Add flags for port and database file
	port := flag.String("p", "8080", "port to serve on")
	dbFile := flag.String("db", "collabtext.db", "database file name")
	flag.Parse()

	var err error
	peerID = uuid.New().String()

	// Use the dbFile flag when opening the database
	db, err = bbolt.Open(*dbFile, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	loadDocument()

	hub = newHub()
	go hub.run()
	go startDiscovery(hub, "_collabtext._tcp", 8080)
	go connectToServer()

	fs := http.FileServer(http.Dir("../ui"))
	http.Handle("/", fs)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	log.Printf("CollabText agent is running on port %s, using database %s", *port, *dbFile)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}