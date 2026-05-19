// Package llm defines the canonical message, content-block, tool, and event
// types used across all LLM providers, plus the Provider interface every
// provider package implements.
package llm

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type StopReason string

const (
	StopEndTurn      StopReason = "end_turn"
	StopToolUse      StopReason = "tool_use"
	StopMaxTokens    StopReason = "max_tokens"
	StopStopSequence StopReason = "stop_sequence"
	StopError        StopReason = "error"
)

// ContentBlock is the sealed union of block types that can appear in a Message.
type ContentBlock interface {
	isContentBlock()
}

type TextBlock struct {
	Text string
}

func (TextBlock) isContentBlock() {}

type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolUseBlock) isContentBlock() {}

type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

func (ToolResultBlock) isContentBlock() {}

// ThinkingBlock carries reasoning content (when a provider/model exposes it).
// Phase 1 providers do not emit thinking blocks; the type is reserved so the
// canonical shape is forward-compatible.
type ThinkingBlock struct {
	Thinking string
}

func (ThinkingBlock) isContentBlock() {}

// ServerToolBlock preserves a provider-managed content block (server-side
// tool calls executed by the provider, and their results) verbatim so it
// round-trips through the conversation history. The agent loop does NOT
// dispatch these — the provider already ran the tool and inlined the
// result. Anthropic web_search uses this for server_tool_use and
// web_search_tool_result blocks.
type ServerToolBlock struct {
	// Provider identifies which provider produced this block, used by
	// translators to decide whether to round-trip the raw payload.
	Provider string
	// BlockType is the provider's JSON `type` field (e.g.
	// "server_tool_use", "web_search_tool_result").
	BlockType string
	// Raw is the full content block JSON exactly as it appeared in the
	// provider's response.
	Raw json.RawMessage
}

func (ServerToolBlock) isContentBlock() {}

type Message struct {
	Role    Role
	Content []ContentBlock
}

type ToolSpec struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	MaxTokens   int
	Temperature float64
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Event is the sealed union of streaming events a Provider emits.
type Event interface {
	isEvent()
}

// TextDelta is a partial text chunk for an in-progress assistant text block.
type TextDelta struct {
	Text string
}

func (TextDelta) isEvent() {}

// ToolUseStart fires when the assistant begins a tool_use block. Input is not
// yet available; consumers should wait for ToolUseStop.
type ToolUseStart struct {
	ID   string
	Name string
}

func (ToolUseStart) isEvent() {}

// ToolUseDelta is a partial JSON chunk for the in-progress tool_use input.
// Consumers that only need the complete input can ignore deltas and wait for
// ToolUseStop.
type ToolUseDelta struct {
	ID          string
	PartialJSON string
}

func (ToolUseDelta) isEvent() {}

// ToolUseStop fires when the tool_use block is complete. Input is the parsed
// (or accumulated, if not valid JSON) input the model produced.
type ToolUseStop struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolUseStop) isEvent() {}

// ServerToolStop fires when a provider-side server tool block finishes
// streaming. The agent loop appends the block to the current assistant
// message's Content as a ServerToolBlock (no dispatch). Consumers may
// use it for UI signaling (e.g. "web_search ran").
type ServerToolStop struct {
	Provider  string
	BlockType string
	// Name is best-effort: present for server_tool_use blocks ("web_search").
	Name string
	Raw  json.RawMessage
}

func (ServerToolStop) isEvent() {}

// MessageStop fires when the assistant has finished its response.
type MessageStop struct {
	Reason StopReason
	Usage  Usage
}

func (MessageStop) isEvent() {}

// ErrorEvent carries a stream-level error. The channel will still close after
// it is emitted.
type ErrorEvent struct {
	Err error
}

func (ErrorEvent) isEvent() {}

// Provider is the abstraction over an LLM backend.
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}
