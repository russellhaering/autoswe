package gopls

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

// Client represents a gopls LSP client
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int64

	// Protects response channels map
	mu       sync.RWMutex
	respChan map[int64]chan *LSPMessage

	// Notification handler
	notifHandler func(*LSPMessage)
}

// NewClient creates a new gopls LSP client
func NewClient() (*Client, error) {
	log.Info("Starting gopls in LSP mode")

	// Start gopls in LSP mode
	cmd := exec.Command("gopls", "serve")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Capture stderr for logging
	stderr := &logWriter{prefix: "gopls stderr"}
	cmd.Stderr = stderr

	log.Info("Starting gopls process")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start gopls: %w", err)
	}
	log.Info("gopls process started", zap.Int("pid", cmd.Process.Pid))

	client := &Client{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdout),
		respChan: make(map[int64]chan *LSPMessage),
	}

	// Start reading responses
	go client.readResponses()

	return client, nil
}

// logWriter implements io.Writer to capture and log stderr output
type logWriter struct {
	prefix string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	log.Info(w.prefix, zap.ByteString("output", p))
	return len(p), nil
}

// Initialize initializes the LSP connection
func (c *Client) Initialize(rootDir string) error {
	log.Info("initializing LSP connection", zap.String("rootDir", rootDir))

	// Convert rootDir to URI
	rootURI := "file://" + filepath.ToSlash(rootDir)

	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   rootURI,
		Capabilities: ClientCapabilities{
			TextDocument: struct {
				Completion struct {
					CompletionItem struct {
						SnippetSupport bool `json:"snippetSupport"`
					} `json:"completionItem"`
				} `json:"completion"`
				Definition struct {
					LinkSupport bool `json:"linkSupport"`
				} `json:"definition"`
			}{
				Completion: struct {
					CompletionItem struct {
						SnippetSupport bool `json:"snippetSupport"`
					} `json:"completionItem"`
				}{
					CompletionItem: struct {
						SnippetSupport bool `json:"snippetSupport"`
					}{
						SnippetSupport: true,
					},
				},
				Definition: struct {
					LinkSupport bool `json:"linkSupport"`
				}{
					LinkSupport: true,
				},
			},
			Workspace: struct {
				WorkspaceFolders bool `json:"workspaceFolders"`
			}{
				WorkspaceFolders: true,
			},
		},
		WorkspaceFolders: []WorkspaceFolder{
			{
				URI:  rootURI,
				Name: filepath.Base(rootDir),
			},
		},
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal initialize params: %w", err)
	}

	// Create a channel for the initialize response
	id := atomic.AddInt64(&c.nextID, 1)
	ch := make(chan *LSPMessage, 1)

	// Register response channel
	c.mu.Lock()
	c.respChan[id] = ch
	c.mu.Unlock()

	// Send initialize request
	log.Info("sending initialize request")
	msg := LSPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params:  paramsJSON,
	}

	if err := c.send(&msg); err != nil {
		c.mu.Lock()
		delete(c.respChan, id)
		c.mu.Unlock()
		return fmt.Errorf("failed to send initialize request: %w", err)
	}

	// Wait for response
	resp := <-ch

	// Clean up response channel
	c.mu.Lock()
	delete(c.respChan, id)
	c.mu.Unlock()

	if resp.Error != nil {
		return fmt.Errorf("initialize failed: %s", resp.Error.Message)
	}

	log.Info("initialization successful", zap.String("response", string(resp.Result)))

	// Send initialized notification
	log.Info("sending initialized notification")
	if err := c.Notify("initialized", json.RawMessage("{}")); err != nil {
		return fmt.Errorf("initialized notification failed: %w", err)
	}

	log.Info("LSP connection fully initialized")
	return nil
}

// Call makes a synchronous LSP request
func (c *Client) Call(method string, params json.RawMessage) (*LSPMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	ch := make(chan *LSPMessage, 1)

	// Register response channel
	c.mu.Lock()
	c.respChan[id] = ch
	c.mu.Unlock()

	msg := LSPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  params,
	}

	if err := c.send(&msg); err != nil {
		log.Error("failed to send request", zap.Int64("id", id), zap.Error(err))
		c.mu.Lock()
		delete(c.respChan, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	log.Info("waiting for response", zap.Int64("id", id))

	// Wait for response
	resp := <-ch

	// Clean up response channel
	c.mu.Lock()
	delete(c.respChan, id)
	c.mu.Unlock()

	return resp, nil
}

// Notify sends an LSP notification
func (c *Client) Notify(method string, params json.RawMessage) error {
	msg := LSPMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	return c.send(&msg)
}

// SetNotificationHandler sets the handler for LSP notifications
func (c *Client) SetNotificationHandler(handler func(*LSPMessage)) {
	c.notifHandler = handler
}

// Close closes the LSP connection
func (c *Client) Close() error {
	if err := c.Notify("exit", nil); err != nil {
		log.Error("failed to send exit notification", zap.Error(err))
	}

	if err := c.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill gopls process: %w", err)
	}

	return c.cmd.Wait()
}

// send sends an LSP message
func (c *Client) send(msg *LSPMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	log.Debug("sending LSP message",
		zap.String("method", msg.Method),
		zap.Any("id", msg.ID),
		zap.String("content", string(data)))

	// Write header
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	log.Debug("sending LSP header", zap.String("header", header))

	if _, err := fmt.Fprint(c.stdin, header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write message
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// handleServerRequest handles requests from the server to the client
func (c *Client) handleServerRequest(msg *LSPMessage) error {
	var result interface{}

	switch msg.Method {
	case "workspace/configuration":
		// Return empty array as default configuration
		result = []interface{}{}

	case "client/registerCapability":
		// Accept all capability registrations
		result = nil

	case "window/showMessage":
		// Log message notifications
		var params struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			log.Error("failed to unmarshal showMessage params", zap.Error(err))
		} else {
			log.Info("gopls message", zap.String("message", params.Message))
		}
		return nil // This is actually a notification, no response needed

	default:
		log.Warn("unhandled server request", zap.String("method", msg.Method))
		result = nil
	}

	// Send success response
	response := &LSPMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
	}
	if result != nil {
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("failed to marshal response result: %w", err)
		}
		response.Result = resultJSON
	}
	return c.send(response)
}

// readResponses reads and processes LSP responses
func (c *Client) readResponses() {
	log.Info("Starting LSP response reader")

	for {
		// Read header
		header := make(map[string]string)
		log.Debug("waiting for LSP message header")

		for {
			log.Debug("reading LSP header line")
			line, err := c.stdout.ReadString('\n')
			log.Debug("read LSP header line", zap.String("line", line))
			if err != nil {
				log.Error("failed to read header line", zap.Error(err))
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				break
			}

			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			header[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}

		log.Debug("received LSP header", zap.Any("header", header))

		// Get content length
		contentLength := 0
		if cl, ok := header["Content-Length"]; ok {
			if length, err := fmt.Sscanf(cl, "%d", &contentLength); err != nil || length != 1 {
				log.Error("invalid Content-Length", zap.String("value", cl))
				continue
			}
		} else {
			log.Error("missing Content-Length header")
			continue
		}

		// Read content
		content := make([]byte, contentLength)
		if _, err := io.ReadFull(c.stdout, content); err != nil {
			log.Error("failed to read message content", zap.Error(err))
			return
		}

		log.Debug("received LSP message content", zap.String("content", string(content)))

		// Parse message
		var msg LSPMessage
		if err := json.Unmarshal(content, &msg); err != nil {
			log.Error("failed to unmarshal message", zap.Error(err))
			continue
		}

		// Handle message
		if msg.ID != nil {
			if msg.Method != "" {
				log.Info("received server request",
					zap.String("method", msg.Method),
					zap.Any("id", msg.ID),
					zap.String("params", string(msg.Params)))

				// This is a request from server to client
				if err := c.handleServerRequest(&msg); err != nil {
					log.Error("failed to handle server request",
						zap.String("method", msg.Method),
						zap.Error(err))
				}
			} else {
				log.Info("received response",
					zap.Any("id", msg.ID),
					zap.String("result", string(msg.Result)))

				// This is a response to our request
				c.mu.RLock()
				if msg.ID != nil {
					ch, ok := c.respChan[*msg.ID]
					c.mu.RUnlock()

					if ok {
						ch <- &msg
					} else {
						log.Warn("no handler found for response", zap.Any("id", msg.ID))
					}
				} else {
					c.mu.RUnlock()
					log.Warn("received response without ID")
				}
			}
		} else if msg.Method != "" {
			log.Info("received server notification",
				zap.String("method", msg.Method),
				zap.String("params", string(msg.Params)))

			if c.notifHandler != nil {
				c.notifHandler(&msg)
			}
		}
	}
}
