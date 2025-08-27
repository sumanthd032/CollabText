package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var rdb *redis.Client
var dbpool *pgxpool.Pool
var ctx = context.Background()

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// For now, we'll use a hardcoded document ID for testing.
	docID := "test-doc"
	log.Printf("New connection for document: %s", docID)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

	// 1. Subscribe to the Redis channel for this document.
	pubsub := rdb.Subscribe(ctx, docID)
	defer pubsub.Close()
	
	// 2. Create a channel to receive messages from Redis.
	redisChan := pubsub.Channel()

	// 3. Start a goroutine to forward messages from Redis to the WebSocket client.
	go func() {
		for msg := range redisChan {
			log.Printf("Relaying message from Redis to client: %s", msg.Payload)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				log.Printf("Error writing message to client: %v", err)
				return
			}
		}
	}()

	// 4. Read messages from the WebSocket client and publish them to Redis.
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("Client disconnected: %v", err)
			break
		}
		log.Printf("Received message from client, publishing to Redis: %s", msg)
		if err := rdb.Publish(ctx, docID, msg).Err(); err != nil {
			log.Printf("Error publishing to Redis: %v", err)
		}
	}
}

func main() {
	// --- Connect to Redis ---
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully.")

	// --- Connect to PostgreSQL ---
	// NOTE: We connect to Postgres but don't use it yet in this step.
	dbUrl := os.Getenv("DATABASE_URL")
	if dbUrl == "" {
		dbUrl = "postgres://user:password@localhost:5432/collabtext"
	}
	var err error
	dbpool, err = pgxpool.New(context.Background(), dbUrl)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer dbpool.Close()
	log.Println("Connected to PostgreSQL successfully.")

	http.HandleFunc("/ws", handleConnections)
	log.Println("CollabText sync server starting on :8081...")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}