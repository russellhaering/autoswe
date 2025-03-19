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

	cleanup := func() {
		err := client.Close()
		if err != nil {
			log.Error("error closing gemini client", zap.Error(err))
		}
	}

	return client, cleanup, nil
}

func ProvideAnthropic(_ context.Context, anthropicAPIKey AnthropicAPIKey) *anthropic.Client {
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

func ProvideRepoFS(rootDir RootDir) *repo.RepositoryFS {
	return repo.NewRepoFS(string(rootDir))
}

func ProvideFilteredFS(_ context.Context, rfs *repo.RepositoryFS) (repo.FilteredFS, error) {
	return rfs.Filter()
}

func ProvideIndexer(ctx context.Context, gemini *genai.Client, rfs repo.FilteredFS, config Config) (*index.Indexer, func(), error) {
	// Create context map with repo filesystem
	fsContextMap := index.FSContextMap{
		index.RepoNamespace: rfs,
	}

	// Add extra context files if provided
	if len(config.ExtraContextPaths) > 0 {
		log.Info("Adding extra context files", zap.Strings("paths", config.ExtraContextPaths))

		// Create virtual filesystem
		virtualFS := repo.NewVirtualFS()

		// Add each file to the virtual filesystem
		for _, path := range config.ExtraContextPaths {
			if err := virtualFS.AddFile(path); err != nil {
				log.Warn("Failed to add extra context file", zap.String("path", path), zap.Error(err))
				continue
			}
			log.Debug("Added extra context file", zap.String("path", path))
		}

		// Create filtered virtual filesystem
		filteredVirtualFS, err := virtualFS.Filter()
		if err != nil {
			return nil, nil, err
		}

		// Add to context map
		fsContextMap[index.ExtraContextNamespace] = filteredVirtualFS
	}

	indexer, err := index.NewIndexer(ctx, gemini, fsContextMap)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		err := indexer.Close()
		if err != nil {
			log.Error("error closing indexer", zap.Error(err))
		}
	}

	return indexer, cleanup, nil
}

type Config struct {
	GeminiAPIKey      GeminiAPIKey
	AnthropicAPIKey   AnthropicAPIKey
	RootDir           RootDir
	ExtraContextPaths []string
}

// Manager handles centralized client instantiation and access
type Manager struct {
	GeminiClient    *genai.Client
	AnthropicClient *anthropic.Client
	RepoFS          *repo.RepositoryFS
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
	wire.FieldsOf(new(Config), "GeminiAPIKey", "AnthropicAPIKey", "RootDir", "ExtraContextPaths"),
	ProvideGemini,
	ProvideAnthropic,
	ProvideRepoFS,
	ProvideFilteredFS,
	ProvideIndexer,
	ProvideManager,
	registry.ToolSet,
)
