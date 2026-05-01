package coderd

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"cdr.dev/slog/v3"
	"github.com/coder/coder/v2/aibridge"
	"github.com/coder/coder/v2/coderd/audit"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbauthz"
	"github.com/coder/coder/v2/codersdk"
)

// canonicalAIProvider is the shape we hash to detect drift between the
// configured environment and the row stored in the database. The fields
// we hash are exactly the operator-controllable inputs that affect
// runtime behavior.
type canonicalAIProvider struct {
	Type                  string `json:"type"`
	BaseURL               string `json:"base_url"`
	APIKey                string `json:"api_key"`
	BedrockRegion         string `json:"bedrock_region"`
	BedrockBaseURL        string `json:"bedrock_base_url"`
	BedrockModel          string `json:"bedrock_model"`
	BedrockSmallFastModel string `json:"bedrock_small_fast_model"`
	BedrockAccessKey      string `json:"bedrock_access_key"`
	BedrockAccessSecret   string `json:"bedrock_access_secret"`
}

// desiredAIProvider is a normalized provider description sourced from
// environment configuration that we want to materialize as a row.
type desiredAIProvider struct {
	Name    string
	Type    database.AiProviderType
	BaseURL string
	APIKey  string
	Blob    aiProviderSettingsBlob
	Hash    string
}

func (d desiredAIProvider) canonical() canonicalAIProvider {
	return canonicalAIProvider{
		Type:                  string(d.Type),
		BaseURL:               d.BaseURL,
		APIKey:                d.APIKey,
		BedrockRegion:         d.Blob.BedrockRegion,
		BedrockModel:          d.Blob.BedrockModel,
		BedrockSmallFastModel: d.Blob.BedrockSmallFastModel,
		BedrockAccessSecret:   d.Blob.BedrockAccessKeySecret,
	}
}

func computeProviderHash(c canonicalAIProvider) string {
	// json.Marshal is deterministic for structs because field order is
	// fixed, but we still sort the resulting JSON via canonical struct
	// shape rather than maps to keep it deterministic and explicit.
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// providersFromEnv normalizes the deployment-values AI Bridge config
// (legacy single-provider env vars and indexed CODER_AIBRIDGE_PROVIDER_<N>_*
// env vars) into the deduplicated set of providers we want present in
// the database. Conflicts between legacy and indexed providers under
// the same canonical name are surfaced as errors.
func providersFromEnv(cfg codersdk.AIBridgeConfig) ([]desiredAIProvider, error) {
	out := make(map[string]desiredAIProvider)
	legacyNames := make(map[string]bool)

	addLegacy := func(name string, p desiredAIProvider) {
		out[name] = p
		legacyNames[name] = true
	}

	// Legacy OpenAI.
	if cfg.LegacyOpenAI.Key.String() != "" {
		dp := desiredAIProvider{
			Name:    aibridge.ProviderOpenAI,
			Type:    database.AiProviderTypeOpenai,
			BaseURL: cfg.LegacyOpenAI.BaseURL.String(),
			APIKey:  cfg.LegacyOpenAI.Key.String(),
		}
		dp.Hash = computeProviderHash(dp.canonical())
		addLegacy(aibridge.ProviderOpenAI, dp)
	}

	// Legacy Anthropic + Bedrock. Anthropic is enabled if either an
	// Anthropic key OR Bedrock credentials are configured.
	hasAnthropicKey := cfg.LegacyAnthropic.Key.String() != ""
	hasLegacyBedrock := cfg.LegacyBedrock.Region.String() != "" ||
		cfg.LegacyBedrock.BaseURL.String() != "" ||
		cfg.LegacyBedrock.AccessKey.String() != "" ||
		cfg.LegacyBedrock.AccessKeySecret.String() != "" ||
		cfg.LegacyBedrock.Model.String() != "" ||
		cfg.LegacyBedrock.SmallFastModel.String() != ""
	if hasAnthropicKey || hasLegacyBedrock {
		dp := desiredAIProvider{
			Name:    aibridge.ProviderAnthropic,
			Type:    database.AiProviderTypeAnthropic,
			BaseURL: cfg.LegacyAnthropic.BaseURL.String(),
			APIKey:  cfg.LegacyAnthropic.Key.String(),
			Blob: aiProviderSettingsBlob{
				BedrockRegion:          cfg.LegacyBedrock.Region.String(),
				BedrockModel:           cfg.LegacyBedrock.Model.String(),
				BedrockSmallFastModel:  cfg.LegacyBedrock.SmallFastModel.String(),
				BedrockAccessKeySecret: cfg.LegacyBedrock.AccessKeySecret.String(),
			},
		}
		// The legacy Bedrock access key is the user-facing api_key on
		// the resulting row when no Anthropic API key is configured.
		// Match the existing buildProviders behavior, which paired
		// AccessKey with AccessKeySecret as the credential.
		if !hasAnthropicKey && cfg.LegacyBedrock.AccessKey.String() != "" {
			dp.APIKey = cfg.LegacyBedrock.AccessKey.String()
		}
		dp.Hash = computeProviderHash(dp.canonical())
		addLegacy(aibridge.ProviderAnthropic, dp)
	}

	// Indexed providers.
	for _, p := range cfg.Providers {
		name := p.Name
		if name == "" {
			name = p.Type
		}
		if name == "" {
			return nil, xerrors.Errorf("indexed AI Bridge provider must have a name or type")
		}
		// Reject reserved sub-paths and invalid characters here so
		// that bad env values fail startup rather than producing a
		// hidden runtime row.
		if errs := validateAIProviderName(name); len(errs) > 0 {
			return nil, xerrors.Errorf("invalid AI Bridge provider name %q: %s", name, errs[0].Detail)
		}

		dp := desiredAIProvider{
			Name: name,
		}
		switch p.Type {
		case aibridge.ProviderOpenAI:
			dp.Type = database.AiProviderTypeOpenai
		case aibridge.ProviderAnthropic:
			dp.Type = database.AiProviderTypeAnthropic
		default:
			// Skip other types (e.g. copilot) until they are added
			// to the database enum.
			continue
		}

		dp.BaseURL = p.BaseURL
		if len(p.Keys) > 0 {
			dp.APIKey = p.Keys[0]
		}
		// Bedrock fields only apply to Anthropic. Preserve them
		// regardless so the canonical hash is stable.
		if dp.Type == database.AiProviderTypeAnthropic {
			dp.Blob.BedrockRegion = p.BedrockRegion
			dp.Blob.BedrockModel = p.BedrockModel
			dp.Blob.BedrockSmallFastModel = p.BedrockSmallFastModel
			if len(p.BedrockAccessKeySecrets) > 0 {
				dp.Blob.BedrockAccessKeySecret = p.BedrockAccessKeySecrets[0]
			}
			// If no Anthropic key is set but a Bedrock access key
			// is, surface it as the api_key column the way the
			// legacy path does.
			if dp.APIKey == "" && len(p.BedrockAccessKeys) > 0 {
				dp.APIKey = p.BedrockAccessKeys[0]
			}
		}

		dp.Hash = computeProviderHash(dp.canonical())
		if legacyNames[name] {
			return nil, xerrors.Errorf("indexed AI Bridge provider %q conflicts with the legacy env var of the same name; remove one or the other", name)
		}
		if existing, ok := out[name]; ok {
			if existing.Hash != dp.Hash {
				return nil, xerrors.Errorf("duplicate AI Bridge provider name %q with conflicting fields", name)
			}
			continue
		}
		out[name] = dp
	}

	// Stable order so audit log entries are deterministic across
	// restarts, which makes comparison in tests trivial.
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	res := make([]desiredAIProvider, 0, len(out))
	for _, name := range names {
		res = append(res, out[name])
	}
	return res, nil
}

// SeedAIProvidersFromEnv reconciles the deployment's environment-derived
// AI Bridge provider configuration with rows in the ai_providers table
// at server startup. Concurrent server starts are serialized via a
// Postgres advisory lock; rows that already exist with a matching
// canonical hash are left alone, missing rows are inserted, and rows
// whose hash differs from the env-derived value cause startup to fail
// with a descriptive error.
//
// Only env-sourced providers participate in the seed; rows created via
// the HTTP CRUD endpoints are not affected.
//
// Audit entries are recorded via the system actor for any inserts.
func SeedAIProvidersFromEnv(
	ctx context.Context,
	db database.Store,
	cfg codersdk.AIBridgeConfig,
	auditor audit.Auditor,
	logger slog.Logger,
) error {
	desired, err := providersFromEnv(cfg)
	if err != nil {
		return xerrors.Errorf("compute providers from env: %w", err)
	}
	if len(desired) == 0 {
		return nil
	}

	// All of the work runs as the system actor so that audit entries
	// are attributed to the deployment rather than a user, and so
	// that dbauthz allows the writes. There is no user-driven request
	// here; this only runs at server startup before the API is
	// serving traffic.
	//nolint:gocritic // server startup, no user actor available
	sysCtx := dbauthz.AsSystemRestricted(ctx)

	return db.InTx(func(tx database.Store) error {
		// Acquire the advisory lock. The lock is released when the
		// transaction ends.
		if err := tx.AcquireLock(sysCtx, database.LockIDAIProvidersEnvSeed); err != nil {
			return xerrors.Errorf("acquire ai providers env seed lock: %w", err)
		}

		for _, dp := range desired {
			settings, err := encodeSettingsBlob(dp.Blob)
			if err != nil {
				return xerrors.Errorf("encode settings for %q: %w", dp.Name, err)
			}

			existing, err := tx.GetAIProviderByNameIncludeDeleted(sysCtx, dp.Name)
			switch {
			case err == nil && existing.Deleted:
				// The provider was created here, then explicitly
				// deleted by an operator. We do NOT re-create it
				// from env; the operator's deletion is sticky.
				logger.Info(sysCtx, "skipping env-seeded ai provider that was previously soft-deleted",
					slog.F("name", dp.Name))
				continue
			case err == nil:
				existingHash := computeProviderHash(canonicalAIProvider{
					Type:                  string(existing.Type),
					BaseURL:               existing.BaseUrl,
					APIKey:                existing.APIKey,
					BedrockRegion:         settingsField(existing.Settings, "bedrock_region"),
					BedrockModel:          settingsField(existing.Settings, "bedrock_model"),
					BedrockSmallFastModel: settingsField(existing.Settings, "bedrock_small_fast_model"),
					BedrockAccessSecret:   settingsField(existing.Settings, "bedrock_access_key_secret"),
				})
				if existingHash == dp.Hash {
					continue
				}
				return xerrors.Errorf("AI Bridge provider %q exists in the database with different fields than the environment configuration; either remove the env var or update the row via the API to match. The deployment refuses to start until the conflict is resolved", dp.Name)
			case errors.Is(err, sql.ErrNoRows):
				// Fall through to the insert below.
			default:
				return xerrors.Errorf("look up ai provider %q: %w", dp.Name, err)
			}

			row, err := tx.InsertAIProvider(sysCtx, database.InsertAIProviderParams{
				ID:            uuid.New(),
				Type:          dp.Type,
				Name:          dp.Name,
				DisplayName:   dp.Name,
				Enabled:       true,
				BaseUrl:       dp.BaseURL,
				APIKey:        dp.APIKey,
				ApiKeyKeyID:   sql.NullString{},
				Settings:      settings,
				SettingsKeyID: sql.NullString{},
			})
			if err != nil {
				return xerrors.Errorf("insert ai provider %q: %w", dp.Name, err)
			}

			audit.BackgroundAudit(sysCtx, &audit.BackgroundAuditParams[database.AiProvider]{
				Audit:  auditor,
				Log:    logger,
				Action: database.AuditActionCreate,
				New:    row,
			})

			logger.Info(sysCtx, "seeded ai provider from environment",
				slog.F("name", dp.Name),
				slog.F("type", string(dp.Type)),
			)
		}
		return nil
	}, nil)
}

// settingsField extracts a single string field from a JSON settings
// blob without unmarshalling into a struct. Returns "" for missing or
// non-string values.
func settingsField(blob, key string) string {
	if blob == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(blob), &m); err != nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
