package gopls

import (
	"encoding/json"
)

// LSPMessage represents a JSON-RPC message for LSP
type LSPMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *LSPError       `json:"error,omitempty"`
}

// LSPError represents a JSON-RPC error
type LSPError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// InitializeParams represents the parameters for the LSP initialize request
type InitializeParams struct {
	ProcessID             int                    `json:"processId"`
	RootURI               string                 `json:"rootUri"`
	Capabilities          ClientCapabilities     `json:"capabilities"`
	WorkspaceFolders      []WorkspaceFolder      `json:"workspaceFolders"`
	InitializationOptions map[string]interface{} `json:"initializationOptions,omitempty"`
}

// ClientCapabilities represents the capabilities of the LSP client
type ClientCapabilities struct {
	TextDocument struct {
		Completion struct {
			CompletionItem struct {
				SnippetSupport bool `json:"snippetSupport"`
			} `json:"completionItem"`
		} `json:"completion"`
		Definition struct {
			LinkSupport bool `json:"linkSupport"`
		} `json:"definition"`
	} `json:"textDocument"`
	Workspace struct {
		WorkspaceFolders bool `json:"workspaceFolders"`
	} `json:"workspace"`
}

// WorkspaceFolder represents a workspace folder in LSP
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// TextDocumentItem represents an open text document
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentIdentifier identifies a text document
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// Position represents a position in a text document
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range represents a range in a text document
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a location in a text document
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}
