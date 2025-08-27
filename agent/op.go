package main

// Op represents a single operation in the document.
// For now, it's a simple character insertion or deletion at an index.
// NOTE: Using a simple index is NOT a true CRDT and has ordering problems
// that we will solve in the next step. This is a stepping stone.
type Op struct {
	Action string `json:"action"` // "insert" or "delete"
	Char   string `json:"char"`   // The character being inserted
	Index  int    `json:"index"`  // The position of the operation
}