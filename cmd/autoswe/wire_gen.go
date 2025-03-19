// Code generated by Wire. DO NOT EDIT.

//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package main

import (
	"context"
	"github.com/russellhaering/autoswe/pkg/autoswe"
	"github.com/russellhaering/autoswe/pkg/tools/astgrep"
	"github.com/russellhaering/autoswe/pkg/tools/build"
	"github.com/russellhaering/autoswe/pkg/tools/dependencies"
	"github.com/russellhaering/autoswe/pkg/tools/exec"
	"github.com/russellhaering/autoswe/pkg/tools/format"
	"github.com/russellhaering/autoswe/pkg/tools/fs"
	"github.com/russellhaering/autoswe/pkg/tools/git"
	"github.com/russellhaering/autoswe/pkg/tools/lint"
	"github.com/russellhaering/autoswe/pkg/tools/query"
	"github.com/russellhaering/autoswe/pkg/tools/registry"
	"github.com/russellhaering/autoswe/pkg/tools/test"
)

// Injectors from injector.go:

func initializeManager(ctx context.Context, config autoswe.Config) (autoswe.Manager, func(), error) {
	geminiAPIKey := config.GeminiAPIKey
	client, cleanup, err := autoswe.ProvideGemini(ctx, geminiAPIKey)
	if err != nil {
		return autoswe.Manager{}, nil, err
	}
	anthropicAPIKey := config.AnthropicAPIKey
	anthropicClient := autoswe.ProvideAnthropic(ctx, anthropicAPIKey)
	autosweRootDir := config.RootDir
	repositoryFS := autoswe.ProvideRepoFS(autosweRootDir)
	filteredFS, err := autoswe.ProvideFilteredFS(ctx, repositoryFS)
	if err != nil {
		cleanup()
		return autoswe.Manager{}, nil, err
	}
	indexer, cleanup2, err := autoswe.ProvideIndexer(ctx, client, filteredFS, config)
	if err != nil {
		cleanup()
		return autoswe.Manager{}, nil, err
	}
	tool := &astgrep.Tool{}
	buildTool := &build.Tool{}
	fetchTool := &dependencies.FetchTool{}
	listTool := &dependencies.ListTool{}
	execTool := &exec.Tool{}
	formatTool := &format.Tool{}
	commandTool := &git.CommandTool{
		RepoFS: repositoryFS,
	}
	commitTool := &git.CommitTool{
		RepoFS: repositoryFS,
	}
	lintTool := &lint.Tool{}
	testTool := &test.Tool{}
	queryTool := &query.Tool{
		Indexer: indexer,
	}
	fsFetchTool := &fs.FetchTool{
		FilteredFS: filteredFS,
	}
	grepTool := &fs.GrepTool{
		FilteredFS: filteredFS,
	}
	fsListTool := &fs.ListTool{
		FilteredFS: filteredFS,
	}
	patchTool := &fs.PatchTool{
		Gemini:     client,
		FilteredFS: filteredFS,
	}
	putTool := &fs.PutTool{
		FilteredFS: filteredFS,
	}
	rmTool := &fs.RmTool{
		FilteredFS: filteredFS,
	}
	toolRegistry := registry.ProvideToolRegistry(tool, buildTool, fetchTool, listTool, execTool, formatTool, commandTool, commitTool, lintTool, testTool, queryTool, fsFetchTool, grepTool, fsListTool, patchTool, putTool, rmTool)
	autosweManager := autoswe.Manager{
		GeminiClient:    client,
		AnthropicClient: anthropicClient,
		RepoFS:          repositoryFS,
		FilteredFS:      filteredFS,
		Indexer:         indexer,
		ToolRegistry:    toolRegistry,
	}
	return autosweManager, func() {
		cleanup2()
		cleanup()
	}, nil
}
