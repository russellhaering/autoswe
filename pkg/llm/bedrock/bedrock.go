// Package bedrock implements an llm.Provider against Amazon Bedrock's Converse
// API (https://docs.aws.amazon.com/bedrock/latest/APIReference/API_runtime_ConverseStream.html)
// via the AWS SDK for Go v2.
package bedrock

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/russellhaering/autoswe/pkg/llm"
)

// DefaultModel is the on-Bedrock model id for Anthropic Sonnet (latest).
// Override via --model.
const DefaultModel = "anthropic.claude-sonnet-4-5-v1:0"

type Config struct {
	Region    string
	Profile   string
	AWSConfig *aws.Config // optional override; bypasses default chain when set
}

type Provider struct {
	client *bedrockruntime.Client
}

// New constructs a Provider. If cfg.AWSConfig is non-nil it is used verbatim;
// otherwise config.LoadDefaultConfig is invoked with optional Region/Profile.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	var awsCfg aws.Config
	if cfg.AWSConfig != nil {
		awsCfg = *cfg.AWSConfig
	} else {
		opts := []func(*config.LoadOptions) error{}
		if cfg.Region != "" {
			opts = append(opts, config.WithRegion(cfg.Region))
		}
		if cfg.Profile != "" {
			opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
		}
		var err error
		awsCfg, err = config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("bedrock: load aws config: %w", err)
		}
	}
	return &Provider{client: bedrockruntime.NewFromConfig(awsCfg)}, nil
}

func (*Provider) Name() string { return "bedrock" }

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	in, err := buildConverseInput(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.ConverseStream(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("bedrock: ConverseStream: %w", err)
	}
	events := make(chan llm.Event, 16)
	go parseStream(resp.GetStream(), events)
	return events, nil
}
