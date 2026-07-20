package daemon

import "time"

const Address = "127.0.0.1:47812"

type SetRequest struct {
	SessionID string `json:"sessionId,omitempty"`
	State     string `json:"state"`
	Visible   *bool  `json:"visible,omitempty"`
}

type VisibilityRequest struct {
	SessionID string `json:"sessionId"`
	Visible   bool   `json:"visible"`
}

type ApprovalCandidate struct {
	Key      int    `json:"key"`
	Decision string `json:"decision"`
}

type ApprovalRequest struct {
	SessionID   string              `json:"sessionId"`
	Candidates  []ApprovalCandidate `json:"candidates"`
	SelectedKey int                 `json:"selectedKey"`
	LeaseMS     int                 `json:"leaseMs,omitempty"`
}

type ApprovalClearRequest struct {
	SessionID string `json:"sessionId"`
}

type PresetRequest struct {
	Preset string `json:"preset"`
}

type SessionStatus struct {
	SessionID string    `json:"sessionId"`
	State     string    `json:"state"`
	Since     time.Time `json:"since"`
	UpdatedAt time.Time `json:"updatedAt"`
	Visible   bool      `json:"visible"`
	Displayed bool      `json:"displayed"`
}

type ApprovalStatus struct {
	SessionID   string              `json:"sessionId"`
	Candidates  []ApprovalCandidate `json:"candidates"`
	SelectedKey int                 `json:"selectedKey"`
	ExpiresAt   time.Time           `json:"expiresAt"`
}

type Status struct {
	State           string          `json:"state"`
	Since           time.Time       `json:"since"`
	ChromaConnected bool            `json:"chromaConnected"`
	LastError       string          `json:"lastError,omitempty"`
	LastErrorCode   string          `json:"lastErrorCode,omitempty"`
	Version         string          `json:"version"`
	Preset          string          `json:"preset"`
	ConfigError     string          `json:"configError,omitempty"`
	Sessions        []SessionStatus `json:"sessions,omitempty"`
	Approval        *ApprovalStatus `json:"approval,omitempty"`
}
