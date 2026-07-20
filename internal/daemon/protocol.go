package daemon

import "time"

const Address = "127.0.0.1:47812"

type SetRequest struct {
	State string `json:"state"`
}

type Status struct {
	State           string    `json:"state"`
	Since           time.Time `json:"since"`
	ChromaConnected bool      `json:"chromaConnected"`
	LastError       string    `json:"lastError,omitempty"`
	LastErrorCode   string    `json:"lastErrorCode,omitempty"`
	Version         string    `json:"version"`
}
