package coderd_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog/v3"
	"cdr.dev/slog/v3/sloggers/slogtest"
	"github.com/coder/coder/v2/coderd/audit"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbtestutil"
	"github.com/coder/coder/v2/codersdk"
	entcoderd "github.com/coder/coder/v2/enterprise/coderd"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/serpent"
)

func TestSeedAIProvidersFromEnv(t *testing.T) {
	t.Parallel()

	t.Run("EmptyConfigNoOp", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()
		err := entcoderd.SeedAIProvidersFromEnv(ctx, db, codersdk.AIBridgeConfig{}, auditor, testLogger(t))
		require.NoError(t, err)
		require.Empty(t, auditor.AuditLogs())
	})

	t.Run("LegacyOpenAI", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			LegacyOpenAI: codersdk.AIBridgeOpenAIConfig{
				BaseURL: serpent.String("https://api.openai.com/v1"),
				Key:     serpent.String("sk-legacy"),
			},
		}
		err := entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t))
		require.NoError(t, err)

		// One row exists for "openai".
		row, err := db.GetAIProviderByName(ctx, "openai")
		require.NoError(t, err)
		require.Equal(t, database.AiProviderTypeOpenai, row.Type)
		require.Equal(t, "https://api.openai.com/v1", row.BaseUrl)
		require.Equal(t, "sk-legacy", row.APIKey)
		require.True(t, row.Enabled)

		// Re-running with the same config is a no-op (no errors, no
		// new audit logs because the row matches).
		auditor.ResetLogs()
		err = entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t))
		require.NoError(t, err)
		require.Empty(t, auditor.AuditLogs())

		// Verify there's still only one row.
		all, err := db.GetAIProviders(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
	})

	t.Run("DriftFailsStartup", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			LegacyOpenAI: codersdk.AIBridgeOpenAIConfig{
				BaseURL: serpent.String("https://api.openai.com/v1"),
				Key:     serpent.String("sk-original"),
			},
		}
		require.NoError(t, entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t)))

		// Change the api_key in the env config.
		cfg.LegacyOpenAI.Key = serpent.String("sk-rotated")
		err := entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t))
		require.Error(t, err)
		require.Contains(t, err.Error(), "different fields")
	})

	t.Run("LegacyAnthropicWithBedrock", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			LegacyAnthropic: codersdk.AIBridgeAnthropicConfig{
				BaseURL: serpent.String("https://api.anthropic.com/"),
				Key:     serpent.String("sk-ant"),
			},
			LegacyBedrock: codersdk.AIBridgeBedrockConfig{
				Region:          serpent.String("us-west-2"),
				AccessKey:       serpent.String("AKIA"),
				AccessKeySecret: serpent.String("secret"),
				Model:           serpent.String("anthropic.claude-3-5-sonnet"),
				SmallFastModel:  serpent.String("anthropic.claude-3-5-haiku"),
			},
		}
		require.NoError(t, entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t)))

		row, err := db.GetAIProviderByName(ctx, "anthropic")
		require.NoError(t, err)
		require.Equal(t, database.AiProviderTypeAnthropic, row.Type)
		require.Equal(t, "sk-ant", row.APIKey)
		require.Contains(t, row.Settings, "us-west-2")
		require.Contains(t, row.Settings, "anthropic.claude-3-5-sonnet")
		require.Contains(t, row.Settings, "anthropic.claude-3-5-haiku")
	})

	t.Run("BedrockOnlyAnthropic", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			LegacyBedrock: codersdk.AIBridgeBedrockConfig{
				Region:          serpent.String("us-east-1"),
				AccessKey:       serpent.String("AKIAONLY"),
				AccessKeySecret: serpent.String("secretonly"),
				Model:           serpent.String("anthropic.claude-3-5-sonnet"),
			},
		}
		require.NoError(t, entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t)))
		row, err := db.GetAIProviderByName(ctx, "anthropic")
		require.NoError(t, err)
		require.Equal(t, "AKIAONLY", row.APIKey)
		require.Contains(t, row.Settings, "us-east-1")
	})

	t.Run("IndexedProviders", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			Providers: []codersdk.AIBridgeProviderConfig{
				{
					Type:    "openai",
					Name:    "primary-openai",
					BaseURL: "https://api.openai.com/v1",
					Keys:    []string{"sk-1"},
				},
				{
					Type:                  "anthropic",
					Name:                  "primary-anthropic",
					BaseURL:               "https://api.anthropic.com/",
					Keys:                  []string{"sk-ant-1"},
					BedrockRegion:         "us-east-1",
					BedrockModel:          "anthropic.claude-3-5-sonnet",
					BedrockSmallFastModel: "anthropic.claude-3-5-haiku",
				},
			},
		}
		require.NoError(t, entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t)))

		oa, err := db.GetAIProviderByName(ctx, "primary-openai")
		require.NoError(t, err)
		require.Equal(t, database.AiProviderTypeOpenai, oa.Type)
		require.Equal(t, "sk-1", oa.APIKey)

		an, err := db.GetAIProviderByName(ctx, "primary-anthropic")
		require.NoError(t, err)
		require.Equal(t, database.AiProviderTypeAnthropic, an.Type)
		require.Contains(t, an.Settings, "us-east-1")
	})

	t.Run("LegacyAndIndexedSameNameConflict", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			LegacyOpenAI: codersdk.AIBridgeOpenAIConfig{
				BaseURL: serpent.String("https://api.openai.com/v1"),
				Key:     serpent.String("sk-legacy"),
			},
			Providers: []codersdk.AIBridgeProviderConfig{
				{
					Type:    "openai",
					Name:    "openai",
					BaseURL: "https://api.openai.com/v1",
					Keys:    []string{"sk-indexed"},
				},
			},
		}
		err := entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t))
		require.Error(t, err)
		require.Contains(t, err.Error(), "conflicts")
	})

	t.Run("InvalidProviderName", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			Providers: []codersdk.AIBridgeProviderConfig{
				{
					Type:    "openai",
					Name:    "Bad_Name",
					BaseURL: "https://api.openai.com/v1",
				},
			},
		}
		err := entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t))
		require.Error(t, err)
	})

	t.Run("ReservedNameRejected", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			Providers: []codersdk.AIBridgeProviderConfig{
				{
					Type:    "openai",
					Name:    "providers",
					BaseURL: "https://api.openai.com/v1",
				},
			},
		}
		err := entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t))
		require.Error(t, err)
	})

	t.Run("UnknownProviderTypeIsSkipped", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			Providers: []codersdk.AIBridgeProviderConfig{
				{
					Type:    "copilot",
					Name:    "gh-copilot",
					BaseURL: "https://api.githubcopilot.com/",
				},
				{
					Type:    "openai",
					Name:    "real-openai",
					BaseURL: "https://api.openai.com/v1",
					Keys:    []string{"sk"},
				},
			},
		}
		require.NoError(t, entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t)))

		all, err := db.GetAIProviders(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
		require.Equal(t, "real-openai", all[0].Name)
	})

	t.Run("SoftDeletedRowIsNotResurrected", func(t *testing.T) {
		t.Parallel()
		db, _ := dbtestutil.NewDB(t)
		ctx := testutil.Context(t, testutil.WaitShort)
		auditor := audit.NewMock()

		cfg := codersdk.AIBridgeConfig{
			LegacyOpenAI: codersdk.AIBridgeOpenAIConfig{
				BaseURL: serpent.String("https://api.openai.com/v1"),
				Key:     serpent.String("sk-original"),
			},
		}
		require.NoError(t, entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t)))

		row, err := db.GetAIProviderByName(ctx, "openai")
		require.NoError(t, err)
		_, err = db.SoftDeleteAIProviderByID(ctx, row.ID)
		require.NoError(t, err)

		// Re-run seed; the soft-deleted row should remain soft-deleted
		// and no new row should be created.
		require.NoError(t, entcoderd.SeedAIProvidersFromEnv(ctx, db, cfg, auditor, testLogger(t)))

		all, err := db.GetAIProviders(ctx)
		require.NoError(t, err)
		require.Empty(t, all, "expected no active rows after soft-delete + re-seed")
	})
}

func testLogger(t *testing.T) slog.Logger {
	t.Helper()
	return slogtest.Make(t, &slogtest.Options{IgnoreErrors: true})
}
