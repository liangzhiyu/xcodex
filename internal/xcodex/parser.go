package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func extractTextFromContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var items []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &items) == nil {
		var texts []string
		for _, item := range items {
			if item.Type == "input_text" || item.Type == "output_text" {
				texts = append(texts, item.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

func ParseSession(filePath string) (*Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	session := &Session{}
	var currentTurn *Turn
	pendingCalls := make(map[string]*ToolCall)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var obj jsonlLine
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}

		switch obj.Type {
		case "session_meta":
			var p sessionMetaPayload
			if json.Unmarshal(obj.Payload, &p) == nil {
				session.Meta = SessionMeta{
					ID:        p.ID,
					Cwd:       p.Cwd,
					Version:   p.Version,
					Timestamp: p.Timestamp,
					GitBranch: p.GitBranch,
				}
			}

		case "turn_context":
			var p turnContextPayload
			if json.Unmarshal(obj.Payload, &p) == nil {
				if p.Model != "" {
					session.Meta.Model = p.Model
				}
				if currentTurn != nil {
					session.Turns = append(session.Turns, *currentTurn)
				}
				currentTurn = &Turn{Timestamp: obj.Timestamp}
			}

		case "response_item":
			var p payloadMessage
			if json.Unmarshal(obj.Payload, &p) != nil {
				break
			}
			if currentTurn == nil {
				currentTurn = &Turn{Timestamp: obj.Timestamp}
			}

			switch p.Type {
			case "message":
				if p.Role == "developer" {
					break
				}
				text := extractTextFromContent(p.Content)
				if text == "" {
					break
				}
				if p.Role == "user" {
					if currentTurn.UserMessage == "" &&
						!strings.HasPrefix(text, "# AGENTS.md") &&
						!strings.HasPrefix(text, "<environment_context>") &&
						!strings.HasPrefix(text, "<permissions") {
						currentTurn.UserMessage = text
					}
				} else if p.Role == "assistant" {
					currentTurn.AssistantTexts = append(currentTurn.AssistantTexts, text)
				}

			case "function_call":
				tc := ToolCall{
					Name:   p.Name,
					Args:   p.Args,
					CallID: p.CallID,
				}
				pendingCalls[tc.CallID] = &tc
				currentTurn.ToolCalls = append(currentTurn.ToolCalls, tc)

			case "function_call_output":
				if tc, ok := pendingCalls[p.CallID]; ok {
					tc.Result = p.Output
					delete(pendingCalls, p.CallID)
					for i := range currentTurn.ToolCalls {
						if currentTurn.ToolCalls[i].CallID == p.CallID {
							currentTurn.ToolCalls[i].Result = p.Output
						}
					}
				}

			case "reasoning":
				for _, s := range p.Summary {
					if s.Type == "summary_text" && s.Text != "" {
						currentTurn.Reasoning = append(currentTurn.Reasoning, s.Text)
					}
				}
			}

		case "event_msg":
			var p eventPayload
			if json.Unmarshal(obj.Payload, &p) != nil {
				break
			}
			if currentTurn == nil {
				currentTurn = &Turn{Timestamp: obj.Timestamp}
			}
			if p.Type == "user_message" && currentTurn.UserMessage == "" {
				currentTurn.UserMessage = p.Message
			}
			if p.Type == "agent_message" && p.Phase == "commentary" && p.Message != "" {
				currentTurn.AssistantTexts = append(currentTurn.AssistantTexts, p.Message)
			}
		}
	}

	if currentTurn != nil {
		session.Turns = append(session.Turns, *currentTurn)
	}

	return session, nil
}