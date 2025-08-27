package main

// Op represents a single operation in the document.
type Op struct {
	// Add a ClientID to identify the originator of the op.
	ClientID string `json:"clientID"`
	Action   string `json:"action"` // "insert" or "delete"
	Char     string `json:"char"`   // The character being inserted
	Index    int    `json:"index"`  // The position of the operation
}