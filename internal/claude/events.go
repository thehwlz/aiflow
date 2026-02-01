package claude

import (
	"encoding/json"
)

// EventType represents the type of JSONL event from Claude Code
type EventType string

const (
	EventTypeAssistant EventType = "assistant"
	EventTypeUser      EventType = "user"
	EventTypeSystem    EventType = "system"
	EventTypeResult    EventType = "result"
	EventTypeError     EventType = "error"
)

// ContentBlockType represents the type of content block in a message
type ContentBlockType string

const (
	ContentBlockText      ContentBlockType = "text"
	ContentBlockToolUse   ContentBlockType = "tool_use"
	ContentBlockToolResult ContentBlockType = "tool_result"
)

// Event represents a parsed JSONL event from Claude Code
type Event struct {
	Type      EventType       `json:"type"`
	Message   *Message        `json:"message,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Raw       json.RawMessage `json:"-"` // Original JSON for debugging
}

// Message represents a message in an event
type Message struct {
	Role    string         `json:"role,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
}

// ContentBlock represents a content block in a message
type ContentBlock struct {
	Type    ContentBlockType `json:"type"`
	Text    string           `json:"text,omitempty"`
	ID      string           `json:"id,omitempty"`       // Tool use ID
	Name    string           `json:"name,omitempty"`     // Tool name
	Input   json.RawMessage  `json:"input,omitempty"`    // Tool input (parse as needed)
	ToolUseID string         `json:"tool_use_id,omitempty"` // For tool_result
	Content   interface{}    `json:"content,omitempty"`  // Tool result content (can be string or array)
	IsError   bool           `json:"is_error,omitempty"` // For tool_result errors
}

// ToolUse represents a tool use event extracted from an assistant message
type ToolUse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// AskUserQuestionInput represents the input for the AskUserQuestion tool
type AskUserQuestionInput struct {
	Questions []Question `json:"questions"`
}

// Question represents a single question in AskUserQuestion
type Question struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
}

// QuestionOption represents an option for a question
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// ToolResult represents a tool result to send back to Claude
type ToolResult struct {
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content"` // Usually string or []ContentBlock
	IsError   bool        `json:"is_error,omitempty"`
}

// UserMessage represents a user message to send to Claude via stdin
type UserMessage struct {
	Type    string         `json:"type"`
	Message UserMessageBody `json:"message"`
}

// UserMessageBody represents the body of a user message
type UserMessageBody struct {
	Role    string         `json:"role"`
	Content []ToolResult   `json:"content"`
}

// NewToolResultMessage creates a user message containing tool results
func NewToolResultMessage(results ...ToolResult) UserMessage {
	content := make([]ToolResult, len(results))
	copy(content, results)

	return UserMessage{
		Type: "user",
		Message: UserMessageBody{
			Role:    "user",
			Content: content,
		},
	}
}

// ParseEvent parses a JSONL line into an Event
func ParseEvent(line []byte) (*Event, error) {
	var event Event
	if err := json.Unmarshal(line, &event); err != nil {
		return nil, err
	}
	event.Raw = line
	return &event, nil
}

// GetToolUses extracts all tool use blocks from an assistant event
func (e *Event) GetToolUses() []ToolUse {
	if e.Type != EventTypeAssistant || e.Message == nil {
		return nil
	}

	var toolUses []ToolUse
	for _, block := range e.Message.Content {
		if block.Type == ContentBlockToolUse {
			toolUses = append(toolUses, ToolUse{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}
	return toolUses
}

// GetText extracts all text content from an assistant event
func (e *Event) GetText() string {
	if e.Type != EventTypeAssistant || e.Message == nil {
		return ""
	}

	var text string
	for _, block := range e.Message.Content {
		if block.Type == ContentBlockText {
			text += block.Text
		}
	}
	return text
}

// IsAskUserQuestion checks if a tool use is an AskUserQuestion call
func (tu *ToolUse) IsAskUserQuestion() bool {
	return tu.Name == "AskUserQuestion"
}

// ParseAskUserQuestionInput parses the input for AskUserQuestion
func (tu *ToolUse) ParseAskUserQuestionInput() (*AskUserQuestionInput, error) {
	var input AskUserQuestionInput
	if err := json.Unmarshal(tu.Input, &input); err != nil {
		return nil, err
	}
	return &input, nil
}

// AnswerAskUserQuestion creates a tool result for an answered question
func AnswerAskUserQuestion(toolUseID string, answers map[string]string) ToolResult {
	// Convert answers map to the format Claude expects
	content, _ := json.Marshal(answers)
	return ToolResult{
		ToolUseID: toolUseID,
		Content:   string(content),
	}
}
