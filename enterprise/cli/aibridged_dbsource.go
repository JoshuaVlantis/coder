//go:build !slim

package cli

import (
	"context"
	"encoding/json"

	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/aibridge"
	"github.com/coder/coder/v2/aibridge/config"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbauthz"
	"github.com/coder/coder/v2/codersdk"
)

// aiProviderDBSettings mirrors the JSON shape stored in
// ai_providers.settings. Kept duplicated from the enterprise/coderd
// package on purpose to avoid an import cycle.
type aiProviderDBSettings struct {
	BedrockRegion          string `json:"bedrock_region,omitempty"`
	BedrockModel           string `json:"bedrock_model,omitempty"`
	BedrockSmallFastModel  string `json:"bedrock_small_fast_model,omitempty"`
	BedrockAccessKeySecret string `json:"bedrock_access_key_secret,omitempty"`
}

// buildProvidersFromDB constructs the list of aibridge providers from
// rows in the ai_providers table. The deployment-values config is
// still consulted for circuit-breaker and BYOK-style settings that
// are global rather than per-provider.
func buildProvidersFromDB(rows []database.AiProvider, cfg codersdk.AIBridgeConfig) ([]aibridge.Provider, error) {
	var cbConfig *config.CircuitBreaker
	if cfg.CircuitBreakerEnabled.Value() {
		cbConfig = &config.CircuitBreaker{
			FailureThreshold: uint32(cfg.CircuitBreakerFailureThreshold.Value()), //nolint:gosec // Validated by serpent.Validate in deployment options.
			Interval:         cfg.CircuitBreakerInterval.Value(),
			Timeout:          cfg.CircuitBreakerTimeout.Value(),
			MaxRequests:      uint32(cfg.CircuitBreakerMaxRequests.Value()), //nolint:gosec // Validated by serpent.Validate in deployment options.
		}
	}

	out := make([]aibridge.Provider, 0, len(rows))
	for _, row := range rows {
		switch row.Type {
		case database.AiProviderTypeOpenai:
			out = append(out, aibridge.NewOpenAIProvider(aibridge.OpenAIConfig{
				Name:             row.Name,
				BaseURL:          row.BaseUrl,
				Key:              row.APIKey,
				CircuitBreaker:   cbConfig,
				SendActorHeaders: cfg.SendActorHeaders.Value(),
			}))
		case database.AiProviderTypeAnthropic:
			var bedrock *aibridge.AWSBedrockConfig
			if row.Settings != "" {
				var s aiProviderDBSettings
				if err := json.Unmarshal([]byte(row.Settings), &s); err != nil {
					return nil, xerrors.Errorf("decode settings for %q: %w", row.Name, err)
				}
				if s.BedrockRegion != "" || s.BedrockAccessKeySecret != "" || s.BedrockModel != "" || s.BedrockSmallFastModel != "" {
					bedrock = &aibridge.AWSBedrockConfig{
						Region:          s.BedrockRegion,
						AccessKey:       row.APIKey,
						AccessKeySecret: s.BedrockAccessKeySecret,
						Model:           s.BedrockModel,
						SmallFastModel:  s.BedrockSmallFastModel,
					}
				}
			}
			out = append(out, aibridge.NewAnthropicProvider(aibridge.AnthropicConfig{
				Name:             row.Name,
				BaseURL:          row.BaseUrl,
				Key:              row.APIKey,
				CircuitBreaker:   cbConfig,
				SendActorHeaders: cfg.SendActorHeaders.Value(),
			}, bedrock))
		default:
			return nil, xerrors.Errorf("unknown provider type %q for provider %q", row.Type, row.Name)
		}
	}
	return out, nil
}

// loadProvidersFromDB returns the current set of enabled, non-deleted
// AI Bridge providers from the database, converted into aibridge
// Provider instances.
func loadProvidersFromDB(ctx context.Context, db database.Store, cfg codersdk.AIBridgeConfig) ([]aibridge.Provider, error) {
	// The query is authorized via dbauthz on ResourceAibridgeProvider.
	// At reload time we run as the system actor because there is no
	// user-facing request driving the reload.
	//nolint:gocritic // server-side reload, no user actor available
	sysCtx := dbauthz.AsSystemRestricted(ctx)
	rows, err := db.GetEnabledAIProviders(sysCtx)
	if err != nil {
		return nil, xerrors.Errorf("get enabled ai providers: %w", err)
	}
	return buildProvidersFromDB(rows, cfg)
}
