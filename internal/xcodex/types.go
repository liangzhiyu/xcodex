package codex

import "encoding/json"

type Session struct {
	Meta  SessionMeta
	Turns []Turn
}

type SessionMeta struct {
	ID        string
	Cwd       string
	Model     string
	Version   string
	Timestamp string
	GitBranch string
}

type Turn struct {
	UserMessage    string
	AssistantTexts []string
	ToolCalls      []ToolCall
	Reasoning      []string
	Timestamp      string
}

type ToolCall struct {
	Name   string
	Args   string
	CallID string
	Result string
}

type SessionEntry struct {
	ID          string
	Cwd         string
	Title       string
	RolloutPath string
	UpdatedAt   string
	TokensUsed  int64
}

// --- JSONL line types ---

type jsonlLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type payloadMessage struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Name    string          `json:"name"`
	Args    string          `json:"arguments"`
	CallID  string          `json:"call_id"`
	Output  string          `json:"output"`
	Summary []summaryItem   `json:"summary"`
}

type summaryItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type eventPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Phase   string `json:"phase"`
}

type sessionMetaPayload struct {
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Version   string `json:"cli_version"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	GitBranch string `json:"branch"`
}

type turnContextPayload struct {
	Model string `json:"model"`
	Cwd   string `json:"cwd"`
}