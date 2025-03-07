package gopls

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/auto-swe/pkg/log"
	"go.uber.org/zap"
)

var (
	client     *Client
	clientOnce sync.Once
	clientErr  error
)

// Input represents the parameters for a gopls LSP request
type Input struct {
	// Method is the LSP method to call (e.g., "textDocument/definition")
	Method string `json:"method"`
	// Params contains the parameters for the LSP method
	Params json.RawMessage `json:"params"`
}

// Output represents the result of an LSP request
type Output struct {
	// Result contains the LSP method result
	Result json.RawMessage `json:"result,omitempty"`
}

// GoplsTool implements the gopls tool
type GoplsTool struct{}

var ProvideGoplsTool = wire.Struct(new(GoplsTool), "*")

// Name returns the name of the tool
func (t *GoplsTool) Name() string {
	return "gopls"
}

// Description returns a description of the gopls tool
func (t *GoplsTool) Description() string {
	return "Makes LSP requests to gopls language server"
}

// Schema returns the JSON schema for the gopls tool
func (t *GoplsTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// getClient returns the singleton LSP client instance
func getClient() (*Client, error) {
	clientOnce.Do(func() {
		var c *Client
		c, clientErr = NewClient()
		if clientErr != nil {
			return
		}

		// Get current working directory
		pwd, err := os.Getwd()
		if err != nil {
			clientErr = fmt.Errorf("failed to get working directory: %w", err)
			return
		}

		// Initialize the client
		if err := c.Initialize(pwd); err != nil {
			clientErr = fmt.Errorf("failed to initialize client: %w", err)
			c.Close()
			return
		}

		client = c
	})

	return client, clientErr
}

// Execute implements the gopls operation
func (t *GoplsTool) Execute(ctx context.Context, input Input) (Output, error) {
	log.Info("Starting gopls operation", zap.String("method", input.Method))

	// Special case: if this is an initialize request, return cached server capabilities
	if input.Method == "initialize" {
		log.Info("Handling initialize request with cached capabilities")
		return Output{
			Result: []byte(`{
				"capabilities": {
					"textDocumentSync": 2,
					"completionProvider": {
						"triggerCharacters": [".", "/", ":"],
						"resolveProvider": true
					},
					"definitionProvider": true,
					"typeDefinitionProvider": true,
					"referencesProvider": true,
					"documentSymbolProvider": true,
					"workspaceSymbolProvider": true,
					"implementationProvider": true,
					"documentFormattingProvider": true,
					"documentRangeFormattingProvider": true,
					"renameProvider": true,
					"hoverProvider": true,
					"documentHighlightProvider": true,
					"codeLensProvider": {
						"resolveProvider": true
					},
					"signatureHelpProvider": {
						"triggerCharacters": ["(", ","]
					}
				}
			}`),
		}, nil
	}

	client, err := getClient()
	if err != nil {
		log.Error("Failed to get LSP client", zap.Error(err))
		return Output{}, fmt.Errorf("failed to get LSP client: %w", err)
	}

	// If the request contains a file URI, ensure it's relative to the workspace root
	if input.Params != nil {
		var params map[string]interface{}
		if err := json.Unmarshal(input.Params, &params); err == nil {
			// Handle textDocument/documentSymbol style requests
			if textDoc, ok := params["textDocument"].(map[string]interface{}); ok {
				if uri, ok := textDoc["uri"].(string); ok {
					// Convert absolute paths to workspace-relative paths
					if strings.HasPrefix(uri, "file:///") {
						pwd, err := os.Getwd()
						if err != nil {
							log.Error("Failed to get working directory", zap.Error(err))
							return Output{}, fmt.Errorf("failed to get working directory: %w", err)
						}
						relPath := strings.TrimPrefix(uri, "file:///")
						params["textDocument"].(map[string]interface{})["uri"] = "file://" + filepath.ToSlash(filepath.Join(pwd, relPath))
						newParams, err := json.Marshal(params)
						if err != nil {
							log.Error("Failed to marshal updated params", zap.Error(err))
							return Output{}, fmt.Errorf("failed to marshal updated params: %w", err)
						}
						input.Params = newParams
					}
				}
			}
		}
	}

	resp, err := client.Call(input.Method, input.Params)
	if err != nil {
		log.Error("LSP request failed", zap.Error(err))
		return Output{}, fmt.Errorf("LSP request failed: %w", err)
	}

	if resp.Error != nil {
		log.Error("LSP error", zap.String("message", resp.Error.Message))
		return Output{}, fmt.Errorf("LSP error: %s", resp.Error.Message)
	}

	log.Info("LSP request completed successfully")

	return Output{
		Result: resp.Result,
	}, nil
}
