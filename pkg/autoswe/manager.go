package autoswe

import (
	"context"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/generative-ai-go/genai"
	"github.com/google/wire"
	"github.com/russellhaering/autoswe/pkg/index"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/repo"
	"github.com/russellhaering/autoswe/pkg/tools/registry"
	"go.uber.org/zap"
	googleoption "google.golang.org/api/option"
)

type (
	GeminiAPIKey    string
	AnthropicAPIKey string
	RootDir         string
)

func ProvideGemini(ctx context.Context, geminiAPIKey GeminiAPIKey) (*genai.Client, func(), error) {
	client, err := genai.NewClient(ctx, googleoption.WithAPIKey(string(geminiAPIKey)))
	if err != nil {
		return nil, nil, err
	}

	close := func() {
		err := client.Close()
		if err != nil {
			log.Error("error closing gemini client", zap.Error(err))
		}
	}

	return client, close, nil
}

func ProvideAnthropic(ctx context.Context, anthropicAPIKey AnthropicAPIKey) *anthropic.Client {
	return anthropic.NewClient(
		anthropicoption.WithAPIKey(string(anthropicAPIKey)),
		anthropicoption.WithMiddleware(func(req *http.Request, next anthropicoption.MiddlewareNext) (*http.Response, error) {
			resp, err := next(req)
			if err != nil {
				log.Error("error calling anthropic", zap.Error(err))
				return nil, err
			}

			if resp.StatusCode == 429 {
				log.Debug("rate-limited by anthropic", zap.Int("status", resp.StatusCode))
			}

			return resp, nil
		}),

		// We need lots of retries to deal with rate limits - we certainly don't want to give up
		anthropicoption.WithMaxRetries(20),
	)
}

func ProvideRepoFS(rootDir RootDir) *repo.RepoFS {
	return repo.NewRepoFS(string(rootDir))
}

func ProvideFilteredFS(ctx context.Context, rfs *repo.RepoFS) (repo.FilteredFS, error) {
	return rfs.Filter()
}

func ProvideIndexer(ctx context.Context, gemini *genai.Client, rfs repo.FilteredFS) (*index.Indexer, func(), error) {
	indexer, err := index.NewIndexer(ctx, gemini, index.FSContextMap{
		index.RepoNamespace: rfs,
		// TODO: extra context
	})
	if err != nil {
		return nil, nil, err
	}

	close := func() {
		err := indexer.Close()
		if err != nil {
			log.Error("error closing indexer", zap.Error(err))
		}
	}

	return indexer, close, nil
}

type Config struct {
	GeminiAPIKey    GeminiAPIKey
	AnthropicAPIKey AnthropicAPIKey
	RootDir         RootDir
}

// Manager handles centralized client instantiation and access
type Manager struct {
	GeminiClient    *genai.Client
	AnthropicClient *anthropic.Client
	RepoFS          *repo.RepoFS
	FilteredFS      repo.FilteredFS
	Indexer         *index.Indexer
	ToolRegistry    *registry.ToolRegistry
}

var ProvideManager = wire.Struct(new(Manager), "*")

func (m *Manager) Close() error {
	m.GeminiClient.Close()
	m.Indexer.Close()
	return nil
}

var ProviderSet = wire.NewSet(
	wire.FieldsOf(new(Config), "GeminiAPIKey", "AnthropicAPIKey", "RootDir"),
	ProvideGemini,
	ProvideAnthropic,
	ProvideRepoFS,
	ProvideFilteredFS,
	ProvideIndexer,
	ProvideManager,
	registry.ToolSet,
)
