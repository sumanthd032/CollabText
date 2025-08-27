package main

import (
	"log"
	"net/http"
)

func main() {
	// 1. Define the handler for our web server.
	// http.FileServer serves files from a given directory.
	// http.Dir("../ui") tells it to look in the 'ui' directory,
	// which is one level up ('..') from our current 'agent' directory.
	fs := http.FileServer(http.Dir("../ui"))

	// 2. Register the file server handler.
	// We tell our server that any request starting with "/" should be
	// handled by our file server.
	http.Handle("/", fs)

	// 3. Start the server.
	log.Println("CollabText agent is running. Visit http://localhost:8080 in your browser.")

	// http.ListenAndServe starts the server on port 8080.
	// If it fails to start, it will return an error, and we log it.
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}