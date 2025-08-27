package main

type CharID struct {
	Clock  int    `json:"clock"`
	PeerID string `json:"peerID"`
}

type Char struct {
	ID       CharID `json:"id"`
	Value    string `json:"value"`
	Position []int  `json:"position"`
}

type Op struct {
	Action   string `json:"action"`
	Char     Char   `json:"char"`
	ClientID string `json:"clientID"`
}