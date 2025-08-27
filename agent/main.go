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

type PresenceMessage struct {
	Action   string `json:"action"`
	ClientID string `json:"clientID"`
	Username string `json:"username"`
}
type Client struct { conn *websocket.Conn; send chan []byte }
type Hub struct { clients map[*Client]bool; broadcast chan []byte; register chan *Client; unregister chan *Client }

var (
	document   []Char
	peerID     string
	clock      int
	db         *bbolt.DB
	hub        *Hub
	serverConn *websocket.Conn
	authToken  = "123e4567-e89b-12d3-a456-426614174000"
)
const (
	serverAddr    = "ws://localhost:8081/ws"
	docBucket     = "documents"
	defaultDocKey = "default_doc"
)

func main() {
	port := flag.String("p", "8080", "port to serve on")
	dbFile := flag.String("db", "collabtext.db", "database file name")
	flag.Parse()
	var err error
	peerID = uuid.New().String()
	db, err = bbolt.Open(*dbFile, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil { log.Fatalf("Failed to open database: %v", err) }
	defer db.Close()
	loadDocument()
	hub = newHub()
	go hub.run()
	go startDiscovery(hub, "_collabtext._tcp", 8080)
	fs := http.FileServer(http.Dir("../ui"))
	http.Handle("/", http.StripPrefix("/", fs))
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { serveWs(hub, w, r) })
	log.Printf("CollabText agent is running on port %s, using database %s", *port, *dbFile)
	if err := http.ListenAndServe(":"+*port, nil); err != nil { log.Fatalf("Failed to start server: %v", err) }
}

func connectToServer(docID string) {
	if serverConn != nil { serverConn.Close() }
	serverURL := fmt.Sprintf("%s/%s?token=%s", serverAddr, docID, authToken)
	log.Printf("Connecting to server: %s", serverURL)
	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil { log.Printf("Failed to connect to server: %v.", err); return }
	serverConn = conn
	go readFromServer()
}

func readFromServer() {
	defer func() { if serverConn != nil { serverConn.Close() }; serverConn = nil; log.Println("Disconnected from central server.") }()
	for {
		_, message, err := serverConn.ReadMessage()
		if err != nil { log.Printf("Error reading from server: %v", err); return }
		var op Op
		if json.Unmarshal(message, &op) == nil {
			if op.Action == "crdt_insert" { applyCrdtInsert(op) }
			if op.Action == "crdt_delete" { applyCrdtDelete(op) }
			saveDocument()
		}
		hub.broadcast <- message
	}
}

func broadcastOp(opBytes []byte) {
	hub.broadcast <- opBytes
	if serverConn != nil {
		if err := serverConn.WriteMessage(websocket.TextMessage, opBytes); err != nil { log.Printf("Error sending message to server: %v", err) }
	}
}

func loadDocument() { err := db.View(func(tx *bbolt.Tx) error { b := tx.Bucket([]byte(docBucket)); if b == nil { return nil }; v := b.Get([]byte(defaultDocKey)); if v == nil { return nil }; return json.Unmarshal(v, &document) }); if err != nil { log.Fatalf("Failed to load document: %v", err) }; if len(document) == 0 { document = []Char{{ID: CharID{Clock: 0, PeerID: "start"}, Position: []int{0}}, {ID: CharID{Clock: 0, PeerID: "end"}, Position: []int{10000}}}; log.Println("Initialized a new empty document.") } else { log.Println("Document loaded from database.") } }
func saveDocument() { err := db.Update(func(tx *bbolt.Tx) error { b, err := tx.CreateBucketIfNotExists([]byte(docBucket)); if err != nil { return err }; v, err := json.Marshal(document); if err != nil { return err }; return b.Put([]byte(defaultDocKey), v) }); if err != nil { log.Printf("Error saving document: %v", err) } }
func handleIncomingOp(op Op) { clock++; var crdtOp Op; modified := false; switch op.Action { case "raw_insert": c := op.Char.Value; i := op.Index + 1; if i < 1 { i = 1 }; if i > len(document)-1 { i = len(document) - 1 }; p1 := document[i-1].Position; p2 := document[i].Position; nP := generatePositionBetween(p1, p2); nC := Char{ID: CharID{Clock: clock, PeerID: peerID}, Value: c, Position: nP}; crdtOp = Op{Action: "crdt_insert", Char: nC, ClientID: op.ClientID}; applyCrdtInsert(crdtOp); modified = true; case "raw_delete": i := op.Index + 1; if i < 1 || i >= len(document)-1 { return }; cTD := document[i]; crdtOp = Op{Action: "crdt_delete", Char: cTD, ClientID: op.ClientID}; applyCrdtDelete(crdtOp); modified = true }; if modified { saveDocument(); oB, err := json.Marshal(crdtOp); if err != nil { return }; broadcastOp(oB) } }
func applyCrdtInsert(op Op) { for _, char := range document { if char.ID == op.Char.ID { return } }; iI := sort.Search(len(document), func(i int) bool { return comparePositions(document[i].Position, op.Char.Position) > 0 }); document = append(document[:iI], append([]Char{op.Char}, document[iI:]...)...) }
func applyCrdtDelete(op Op) { dI := -1; for i, char := range document { if char.ID == op.Char.ID { dI = i; break } }; if dI != -1 { document = append(document[:dI], document[dI+1:]...) } }
func generatePositionBetween(p1, p2 []int) []int { nP := []int{}; for i := 0; ; i++ { v1 := 0; if i < len(p1) { v1 = p1[i] }; v2 := 10000; if i < len(p2) { v2 = p2[i] }; if v2-v1 > 1 { d := rand.Intn(min(10, v2-v1-1)) + 1; nP = append(nP, v1+d); return nP }; nP = append(nP, v1) } }
func comparePositions(p1, p2 []int) int { for i := 0; i < len(p1) && i < len(p2); i++ { if p1[i] < p2[i] { return -1 }; if p1[i] > p2[i] { return 1 } }; if len(p1) < len(p2) { return -1 }; if len(p1) > len(p2) { return 1 }; return 0 }
func min(a, b int) int { if a < b { return a }; return b }
func newHub() *Hub { return &Hub{broadcast: make(chan []byte), register: make(chan *Client), unregister: make(chan *Client), clients: make(map[*Client]bool)} }
func (h *Hub) run() { for { select { case c := <-h.register: h.clients[c] = true; case c := <-h.unregister: if _, ok := h.clients[c]; ok { delete(h.clients, c); close(c.send) }; case m := <-h.broadcast: for c := range h.clients { select { case c.send <- m: default: close(c.send); delete(h.clients, c) } } } } }
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) { c, err := upgrader.Upgrade(w, r, nil); if err != nil { return }; cl := &Client{conn: c, send: make(chan []byte, 256)}; hub.register <- cl; go cl.writePump(); go cl.readPump() }
func (c *Client) readPump() { defer func() { hub.unregister <- c; c.conn.Close() }(); for { _, m, err := c.conn.ReadMessage(); if err != nil { break }; var gM map[string]interface{}; if json.Unmarshal(m, &gM) != nil { continue }; a, _ := gM["action"].(string); switch a { case "join": if dID, ok := gM["docID"].(string); ok { go connectToServer(dID) }; case "save_snapshot": if dD, ok := gM["doc"]; ok { dB, _ := json.Marshal(dD); json.Unmarshal(dB, &document); saveDocument(); log.Println("Local snapshot saved.") }; default: var op Op; json.Unmarshal(m, &op); handleIncomingOp(op) } } }
func (c *Client) writePump() { defer c.conn.Close(); for { m, ok := <-c.send; if !ok { c.conn.WriteMessage(websocket.CloseMessage, []byte{}); return }; c.conn.WriteMessage(websocket.TextMessage, m) } }
func startDiscovery(hub *Hub, sN string, p int) { h, _ := os.Hostname(); s, err := zeroconf.Register(fmt.Sprintf("%s-%s", "CollabText", h), sN, "local.", p, []string{"txtv=0"}, nil); if err != nil { log.Fatalf("mDNS register failed: %v", err) }; defer s.Shutdown(); r, err := zeroconf.NewResolver(nil); if err != nil { log.Fatalf("mDNS resolver failed: %v", err) }; e := make(chan *zeroconf.ServiceEntry); go func(r <-chan *zeroconf.ServiceEntry) { for entry := range r { log.Printf("mDNS discovered: %s", entry.Instance) } }(e); ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second); defer cancel(); err = r.Browse(ctx, sN, "local.", e); if err != nil { log.Fatalf("mDNS browse failed: %v", err) }; <-ctx.Done() }