package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// We need an Upgrader
// This will upgrade a standard HTTP connection to a WebSocket connection.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// We can add a CheckOrigin function here for security in production,
	// but for now, we'll allow any connection.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleWebSocket handles the WebSocket connection.
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade the HTTP connection to a WebSocket connection.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket:", err)
		return
	}
	// Make sure we close the connection when the function returns.
	defer conn.Close()
	log.Println("Client connected via WebSocket...")

	// This is our infinite loop for reading messages from the client.
	for {
		// Read message from browser
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Client disconnected:", err)
			break // Exit the loop if the client disconnects.
		}

		// Log the message to the agent's console.
		log.Printf("Received: %s", message)

		// Echo the message back to the browser.
		if err := conn.WriteMessage(messageType, message); err != nil {
			log.Println("Error writing message:", err)
			break // Exit if we can't write a message.
		}
	}
}

func main() {
	// The file server is the same as before.
	fs := http.FileServer(http.Dir("../ui"))
	http.Handle("/", fs)

	// We add a NEW handler for our WebSocket endpoint.
	// All requests to "/ws" will now be handled by handleWebSocket.
	http.HandleFunc("/ws", handleWebSocket)

	log.Println("CollabText agent is running. Visit http://localhost:8080 in your browser.")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}