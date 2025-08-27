package main

// CharID is a globally unique identifier for a character, combining a logical clock
// and the ID of the peer that created it.
type CharID struct {
	Clock  int    `json:"clock"`
	PeerID string `json:"peerID"`
}

// Char represents a single character in the CRDT sequence. It has a unique ID,
// its value, and a sortable Position that determines its place in the document.
type Char struct {
	ID       CharID `json:"id"`
	Value    string `json:"value"`
	Position []int  `json:"position"`
}

// Op is the message sent over the network. It can represent a raw user action
// from the client or a processed CRDT operation being broadcast to peers.
type Op struct {
	Action   string `json:"action"`   // "raw_insert", "raw_delete", "crdt_insert", "crdt_delete"
	Char     Char   `json:"char"`     // The CRDT character for the operation
	Index    int    `json:"index"`    // Used only for "raw_delete" actions
	ClientID string `json:"clientID"` // ID of the browser tab to prevent echo
}