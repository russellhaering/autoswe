// Package anthropic implements an llm.Provider against Anthropic's Messages
// API (https://docs.anthropic.com/en/api/messages-streaming) using raw HTTP +
// SSE so the wire format is fully under our control.
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/russellhaering/autoswe/pkg/llm"
)

const (
	defaultBaseURL    = "https://api.anthropic.com"
	defaultAPIVersion = "2023-06-01"
	DefaultModel      = "claude-sonnet-4-5"
)

type Config struct {
	APIKey     string
	BaseURL    string
	APIVersion string
	HTTPClient *http.Client
}

type Provider struct {
	apiKey     string
	baseURL    string
	apiVersion string
	httpClient *http.Client
}

func New(cfg Config) *Provider {
	p := &Provider{
		apiKey:     cfg.APIKey,
		baseURL:    cfg.BaseURL,
		apiVersion: cfg.APIVersion,
		httpClient: cfg.HTTPClient,
	}
	if p.baseURL == "" {
		p.baseURL = defaultBaseURL
	}
	if p.apiVersion == "" {
		p.apiVersion = defaultAPIVersion
	}
	if p.httpClient == nil {
		p.httpClient = http.DefaultClient
	}
	return p
}

func (*Provider) Name() string { return "anthropic" }

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("anthropic: api key is required")
	}
	body, err := buildRequestBody(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", p.apiVersion)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	events := make(chan llm.Event, 16)
	go parseStream(resp.Body, events)
	return events, nil
}
