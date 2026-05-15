// Package openai implements an llm.Provider against OpenAI's Chat Completions
// API (https://platform.openai.com/docs/api-reference/chat) using raw HTTP +
// SSE so the wire format is fully under our control.
package openai

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
	defaultBaseURL = "https://api.openai.com"
	DefaultModel   = "gpt-5"
)

type Config struct {
	APIKey         string
	BaseURL        string
	Organization   string
	Project        string
	HTTPClient     *http.Client
}

type Provider struct {
	apiKey       string
	baseURL      string
	organization string
	project      string
	httpClient   *http.Client
}

func New(cfg Config) *Provider {
	p := &Provider{
		apiKey:       cfg.APIKey,
		baseURL:      cfg.BaseURL,
		organization: cfg.Organization,
		project:      cfg.Project,
		httpClient:   cfg.HTTPClient,
	}
	if p.baseURL == "" {
		p.baseURL = defaultBaseURL
	}
	if p.httpClient == nil {
		p.httpClient = http.DefaultClient
	}
	return p
}

func (*Provider) Name() string { return "openai" }

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("openai: api key is required")
	}
	body, err := buildRequestBody(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("authorization", "Bearer "+p.apiKey)
	if p.organization != "" {
		httpReq.Header.Set("openai-organization", p.organization)
	}
	if p.project != "" {
		httpReq.Header.Set("openai-project", p.project)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	events := make(chan llm.Event, 16)
	go parseStream(resp.Body, events)
	return events, nil
}
