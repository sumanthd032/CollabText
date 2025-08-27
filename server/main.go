package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var (
	upgrader   = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	rdb        *redis.Client
	dbpool     *pgxpool.Pool
	ctx        = context.Background()
	docManager = DocumentManager{docs: make(map[uuid.UUID]*ManagedDocument)}
)

type DocumentManager struct {
	docs map[uuid.UUID]*ManagedDocument
	mu   sync.Mutex
}
type ManagedDocument struct {
	ID       uuid.UUID
	Content  []Char
	mu       sync.Mutex
	lastSave time.Time
}
type PresenceMessage struct {
	Action   string `json:"action"`
	ClientID string `json:"clientID"`
	Username string `json:"username"`
}

func (dm *DocumentManager) GetDocument(docID uuid.UUID) *ManagedDocument {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if doc, exists := dm.docs[docID]; exists {
		return doc
	}
	doc := &ManagedDocument{ID: docID, lastSave: time.Now()}
	var initialContent json.RawMessage
	err := dbpool.QueryRow(ctx, "SELECT content FROM documents WHERE id=$1", docID).Scan(&initialContent)
	if err != nil {
		log.Printf("Document %s not found, creating new one.", docID)
		initialContent = []byte(`[{"id":{"clock":0,"peerID":"start"},"position":[0]},{"id":{"clock":0,"peerID":"end"},"position":[10000]}]`)
		_, _ = dbpool.Exec(ctx, "INSERT INTO documents (id, content) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING", docID, initialContent)
	}
	json.Unmarshal(initialContent, &doc.Content)
	dm.docs[docID] = doc
	return doc
}

func (md *ManagedDocument) ApplyAndSave(op Op) {
	md.mu.Lock()
	defer md.mu.Unlock()
	if op.Action == "crdt_insert" {
		applyCrdtInsert(&md.Content, op)
	} else if op.Action == "crdt_delete" {
		applyCrdtDelete(&md.Content, op)
	}
	if time.Since(md.lastSave) > 5*time.Second {
		log.Printf("Saving document %s to database...", md.ID)
		contentBytes, _ := json.Marshal(md.Content)
		_, err := dbpool.Exec(ctx, "UPDATE documents SET content = $1, updated_at = NOW() WHERE id = $2", contentBytes, md.ID)
		if err != nil {
			log.Printf("ERROR saving document %s: %v", md.ID, err)
		}
		md.lastSave = time.Now()
	}
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	tokenStr := r.URL.Query().Get("token")
	var userID uuid.UUID
	err := dbpool.QueryRow(ctx, "SELECT id FROM users WHERE api_token=$1", tokenStr).Scan(&userID)
	if err != nil {
		http.Error(w, "Forbidden: Invalid Token", http.StatusForbidden)
		return
	}
	docID, _ := uuid.Parse(mux.Vars(r)["docID"])
	managedDoc := docManager.GetDocument(docID)
	ws, _ := upgrader.Upgrade(w, r, nil)
	defer ws.Close()
	managedDoc.mu.Lock()
	ws.WriteJSON(map[string]interface{}{"action": "load", "doc": managedDoc.Content})
	managedDoc.mu.Unlock()
	updatesChannel := "updates:" + docID.String()
	pubsub := rdb.Subscribe(ctx, updatesChannel)
	defer pubsub.Close()
	redisChan := pubsub.Channel()
	go func() {
		for msg := range redisChan {
			ws.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
		}
	}()
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var op Op
		if json.Unmarshal(msg, &op) == nil && (op.Action == "crdt_insert" || op.Action == "crdt_delete") {
			managedDoc.ApplyAndSave(op)
		}
		var pres PresenceMessage
		if json.Unmarshal(msg, &pres) == nil && pres.Action == "presence" {
			presenceKey := "presence:" + docID.String()
			rdb.HSet(ctx, presenceKey, pres.ClientID, pres.Username)
			rdb.Expire(ctx, presenceKey, 10*time.Second)
			allUsers, _ := rdb.HGetAll(ctx, presenceKey).Result()
			presenceUpdate := map[string]interface{}{"action": "presence_update", "users": allUsers}
			updateBytes, _ := json.Marshal(presenceUpdate)
			rdb.Publish(ctx, updatesChannel, updateBytes)
			continue
		}
		rdb.Publish(ctx, updatesChannel, msg)
	}
}

func main() {
	redisAddr := os.Getenv("REDIS_ADDR"); if redisAddr == "" { redisAddr = "localhost:6379" }
	rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
	if _, err := rdb.Ping(ctx).Result(); err != nil { log.Fatalf("Could not connect to Redis: %v", err) }
	log.Println("Connected to Redis successfully.")
	dbUrl := os.Getenv("DATABASE_URL"); if dbUrl == "" { dbUrl = "postgres://user:password@localhost:5432/collabtext" }
	var err error; dbpool, err = pgxpool.New(context.Background(), dbUrl)
	if err != nil { log.Fatalf("Unable to connect to database: %v", err) }
	defer dbpool.Close()
	log.Println("Connected to PostgreSQL successfully.")
	r := mux.NewRouter()
	r.HandleFunc("/ws/{docID:[0-9a-fA-F-]+}", handleConnections)
	log.Println("CollabText sync server starting on :8081...")
	if err := http.ListenAndServe(":8081", r); err != nil { log.Fatalf("Failed to start server: %v", err) }
}

func applyCrdtInsert(doc *[]Char, op Op) { for _, char := range *doc { if char.ID == op.Char.ID { return } }; insertIndex := sort.Search(len(*doc), func(i int) bool { return comparePositions((*doc)[i].Position, op.Char.Position) > 0 }); *doc = append((*doc)[:insertIndex], append([]Char{op.Char}, (*doc)[insertIndex:]...)...) }
func applyCrdtDelete(doc *[]Char, op Op) { deleteIndex := -1; for i, char := range *doc { if char.ID == op.Char.ID { deleteIndex = i; break } }; if deleteIndex != -1 { *doc = append((*doc)[:deleteIndex], (*doc)[deleteIndex+1:]...) } }
func comparePositions(pos1, pos2 []int) int { for i := 0; i < len(pos1) && i < len(pos2); i++ { if pos1[i] < pos2[i] { return -1 }; if pos1[i] > pos2[i] { return 1 } }; if len(pos1) < len(pos2) { return -1 }; if len(pos1) > len(pos2) { return 1 }; return 0 }