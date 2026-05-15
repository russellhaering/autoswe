package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"` // for server-initiated notifications
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("mcp rpc %d: %s", e.Code, e.Message) }

// StdioConfig configures a stdio MCP server subprocess.
type StdioConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	Stderr  io.Writer // optional; defaults to os.Stderr
}

// NewStdioClient starts the configured subprocess and returns a Client
// connected to it via newline-delimited JSON-RPC 2.0 on its stdin/stdout.
func NewStdioClient(ctx context.Context, cfg StdioConfig) (Client, error) {
	if cfg.Command == "" {
		return nil, errors.New("mcp stdio: Command is required")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = mergeEnv(cfg.Env)
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio: start %q: %w", cfg.Command, err)
	}

	c := &stdioClient{
		name:    cfg.Name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: map[int]chan rpcResponse{},
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func mergeEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

type stdioClient struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResponse

	closeOnce sync.Once
	closed    chan struct{}
	readErr   error
}

func (c *stdioClient) Name() string { return c.name }

func (c *stdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		raw = b
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	req := rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}
	if err := c.write(req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		if c.readErr != nil {
			return nil, c.readErr
		}
		return nil, errors.New("mcp: connection closed")
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (c *stdioClient) notify(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		raw = b
	}
	req := rpcRequest{JSONRPC: "2.0", Method: method, Params: raw}
	return c.write(req)
}

func (c *stdioClient) write(req rpcRequest) error {
	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write rpc: %w", err)
	}
	return nil
}

func (c *stdioClient) readLoop() {
	defer func() {
		c.closeOnce.Do(func() { close(c.closed) })
	}()
	for {
		line, err := c.stdout.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			c.readErr = err
			return
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			// skip malformed frames; servers occasionally log to stdout.
			continue
		}
		if resp.ID == nil {
			// notification — ignored in Phase 3
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		c.mu.Unlock()
		if !ok {
			continue
		}
		select {
		case ch <- resp:
		default:
		}
	}
}

func (c *stdioClient) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		_ = c.cmd.Process.Kill()
		close(c.closed)
	})
	_ = c.cmd.Wait()
	return nil
}

// Initialize performs the initialize handshake.
func (c *stdioClient) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "autoswe",
			"version": "0.1",
		},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp initialize (%s): %w", c.name, err)
	}
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("mcp initialized notification (%s): %w", c.name, err)
	}
	return nil
}

func (c *stdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	raw, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list (%s): %w", c.name, err)
	}
	var resp struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("mcp tools/list (%s) decode: %w", c.name, err)
	}
	return resp.Tools, nil
}

func (c *stdioClient) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	params := map[string]any{
		"name":      name,
		"arguments": json.RawMessage(args),
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return CallResult{}, fmt.Errorf("mcp tools/call %s on %s: %w", name, c.name, err)
	}
	var res CallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return CallResult{}, fmt.Errorf("mcp tools/call %s on %s decode: %w", name, c.name, err)
	}
	return res, nil
}
