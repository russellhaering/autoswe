package db

import (
	"context"
	"sync"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// ClientManager manages shared Gemini clients
type ClientManager struct {
	apiKey string
	mu     sync.Mutex
	client *genai.Client
}

// NewClientManager creates a new client manager
func NewClientManager(apiKey string) *ClientManager {
	return &ClientManager{
		apiKey: apiKey,
	}
}

// GetClient returns a shared Gemini client, creating it if necessary
func (cm *ClientManager) GetClient() (*genai.Client, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.client == nil {
		client, err := genai.NewClient(context.Background(), option.WithAPIKey(cm.apiKey))
		if err != nil {
			return nil, err
		}
		cm.client = client
	}

	return cm.client, nil
}

// Close closes the shared client
func (cm *ClientManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.client != nil {
		cm.client.Close()
		cm.client = nil
	}
	return nil
}
