//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.

package main

import (
	"context"

	"github.com/google/wire"
	"github.com/russellhaering/auto-swe/pkg/autoswe"
)

func initializeManager(ctx context.Context, config autoswe.Config) (autoswe.Manager, func(), error) {
	wire.Build(autoswe.ProviderSet)
	return autoswe.Manager{}, nil, nil
}
