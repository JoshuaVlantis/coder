package coderd_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/coderdtest"
	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/enterprise/coderd/coderdenttest"
	"github.com/coder/coder/v2/enterprise/coderd/license"
	"github.com/coder/coder/v2/testutil"
)

func TestAIProvidersCRUD(t *testing.T) {
	t.Parallel()

	t.Run("RequiresLicenseFeature", func(t *testing.T) {
		t.Parallel()

		dv := coderdtest.DeploymentValues(t)
		client, _ := coderdenttest.New(t, &coderdenttest.Options{
			Options: &coderdtest.Options{
				DeploymentValues: dv,
			},
			LicenseOptions: &coderdenttest.LicenseOptions{
				// No aibridge feature.
				Features: license.Features{},
			},
		})

		ctx := testutil.Context(t, testutil.WaitLong)
		//nolint:gocritic // Owner role is the audience for this endpoint.
		_, err := client.AIProviders(ctx)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusForbidden, sdkErr.StatusCode())
	})

	t.Run("EmptyList", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)
		//nolint:gocritic // Owner role is the audience for this endpoint.
		got, err := client.AIProviders(ctx)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("CreateGetUpdateDelete", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		// Create.
		req := codersdk.CreateAIProviderRequest{
			Type:        codersdk.AIProviderTypeAnthropic,
			Name:        "primary-anthropic",
			DisplayName: "Primary Anthropic",
			Enabled:     true,
			BaseURL:     "https://api.anthropic.com/",
			APIKey:      "sk-ant-secret", //nolint:gosec // test fixture, not a real credential
			Settings: codersdk.AIProviderSettings{
				BedrockRegion: "us-east-1",
			},
			BedrockAccessKeySecret: "aws-secret-access-key",
		}
		//nolint:gocritic // Owner role is the audience for this endpoint.
		created, err := client.CreateAIProvider(ctx, req)
		require.NoError(t, err)
		require.NotEqual(t, [16]byte{}, created.ID)
		require.Equal(t, req.Type, created.Type)
		require.Equal(t, req.Name, created.Name)
		require.Equal(t, req.DisplayName, created.DisplayName)
		require.Equal(t, req.Enabled, created.Enabled)
		require.Equal(t, req.BaseURL, created.BaseURL)
		require.Equal(t, req.Settings.BedrockRegion, created.Settings.BedrockRegion)

		// Get by ID.
		gotByID, err := client.AIProvider(ctx, created.ID.String())
		require.NoError(t, err)
		require.Equal(t, created.ID, gotByID.ID)

		// Get by name.
		gotByName, err := client.AIProvider(ctx, created.Name)
		require.NoError(t, err)
		require.Equal(t, created.ID, gotByName.ID)

		// List.
		list, err := client.AIProviders(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		require.Equal(t, created.ID, list[0].ID)

		// Update.
		newDisplay := "Updated Display"
		newURL := "https://api.anthropic.com/v1"
		newAPIKey := "sk-ant-updated" //nolint:gosec // test fixture, not a real credential
		disabled := false
		updated, err := client.UpdateAIProvider(ctx, created.Name, codersdk.UpdateAIProviderRequest{
			DisplayName: &newDisplay,
			BaseURL:     &newURL,
			APIKey:      &newAPIKey,
			Enabled:     &disabled,
			Settings: &codersdk.AIProviderSettings{
				BedrockRegion: "us-west-2",
				BedrockModel:  "anthropic.claude-3-5-sonnet",
			},
		})
		require.NoError(t, err)
		require.Equal(t, newDisplay, updated.DisplayName)
		require.Equal(t, newURL, updated.BaseURL)
		require.False(t, updated.Enabled)
		require.Equal(t, "us-west-2", updated.Settings.BedrockRegion)
		require.Equal(t, "anthropic.claude-3-5-sonnet", updated.Settings.BedrockModel)

		// Delete.
		err = client.DeleteAIProvider(ctx, created.ID.String())
		require.NoError(t, err)

		// Subsequent get returns 404.
		_, err = client.AIProvider(ctx, created.ID.String())
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusNotFound, sdkErr.StatusCode())

		// List excludes the deleted provider.
		list, err = client.AIProviders(ctx)
		require.NoError(t, err)
		require.Empty(t, list)

		// Soft-deleted name remains reserved.
		_, err = client.CreateAIProvider(ctx, req)
		require.Error(t, err)
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusConflict, sdkErr.StatusCode())
	})

	t.Run("DefaultDisplayName", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // Owner role is the audience for this endpoint.
		created, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "no-display",
			Enabled: true,
			BaseURL: "https://api.openai.com/v1",
		})
		require.NoError(t, err)
		// Server falls back to Name when DisplayName is empty.
		require.Equal(t, "no-display", created.DisplayName)
	})

	t.Run("DuplicateNameConflict", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		req := codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "duplicate",
			Enabled: true,
			BaseURL: "https://api.openai.com/v1",
		}
		//nolint:gocritic // Owner role is the audience for this endpoint.
		_, err := client.CreateAIProvider(ctx, req)
		require.NoError(t, err)
		_, err = client.CreateAIProvider(ctx, req)
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusConflict, sdkErr.StatusCode())
	})

	t.Run("InvalidName", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		// Invalid character in name.
		//nolint:gocritic // Owner role is the audience for this endpoint.
		_, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "Bad_Name",
			Enabled: true,
			BaseURL: "https://api.openai.com/v1",
		})
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode())
	})

	t.Run("ReservedName", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		for _, name := range []string{"providers", "proxy", "interceptions", "sessions", "models", "clients"} {
			_, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
				Type:    codersdk.AIProviderTypeOpenAI,
				Name:    name,
				Enabled: true,
				BaseURL: "https://api.openai.com/v1",
			})
			require.Errorf(t, err, "expected reserved name %q to be rejected", name)
			var sdkErr *codersdk.Error
			require.ErrorAs(t, err, &sdkErr)
			require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode(), "name %q", name)
		}
	})

	t.Run("InvalidType", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // Owner role is the audience for this endpoint.
		_, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    "google",
			Name:    "google",
			Enabled: true,
			BaseURL: "https://api.example.com",
		})
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode())
	})

	t.Run("InvalidBaseURL", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // Owner role is the audience for this endpoint.
		_, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "bad-url",
			Enabled: true,
			BaseURL: "not-a-url",
		})
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode())

		_, err = client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "bad-scheme",
			Enabled: true,
			BaseURL: "ftp://api.example.com",
		})
		require.Error(t, err)
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode())
	})

	t.Run("UpdateNoFields", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // Owner role is the audience for this endpoint.
		created, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "patchable",
			Enabled: true,
			BaseURL: "https://api.openai.com/v1",
		})
		require.NoError(t, err)

		_, err = client.UpdateAIProvider(ctx, created.Name, codersdk.UpdateAIProviderRequest{})
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode())
	})

	t.Run("NotFound", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // Owner role is the audience for this endpoint.
		_, err := client.AIProvider(ctx, "missing")
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusNotFound, sdkErr.StatusCode())

		err = client.DeleteAIProvider(ctx, "missing")
		require.Error(t, err)
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusNotFound, sdkErr.StatusCode())
	})

	t.Run("NonOwnerForbidden", func(t *testing.T) {
		t.Parallel()
		ownerClient, _, firstUser := coderdenttest.NewWithDatabase(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		// Create as owner.
		_, err := ownerClient.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "owner-only",
			Enabled: true,
			BaseURL: "https://api.openai.com/v1",
		})
		require.NoError(t, err)

		// Member is not allowed to read or write providers.
		memberClient, _ := coderdtest.CreateAnotherUser(t, ownerClient, firstUser.OrganizationID)

		_, err = memberClient.AIProviders(ctx)
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusForbidden, sdkErr.StatusCode())

		_, err = memberClient.AIProvider(ctx, "owner-only")
		require.Error(t, err)
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusForbidden, sdkErr.StatusCode())

		_, err = memberClient.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:    codersdk.AIProviderTypeOpenAI,
			Name:    "member-attempt",
			Enabled: true,
			BaseURL: "https://api.openai.com/v1",
		})
		require.Error(t, err)
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusForbidden, sdkErr.StatusCode())

		err = memberClient.DeleteAIProvider(ctx, "owner-only")
		require.Error(t, err)
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusForbidden, sdkErr.StatusCode())
	})

	t.Run("Unauthenticated", func(t *testing.T) {
		t.Parallel()
		ownerClient, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		anon := codersdk.New(ownerClient.URL)
		_, err := anon.AIProviders(ctx)
		require.Error(t, err)
		var sdkErr *codersdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusUnauthorized, sdkErr.StatusCode())
	})

	t.Run("APIKeyHidden", func(t *testing.T) {
		t.Parallel()
		client, _ := coderdenttest.New(t, aibridgeOpts(t))
		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // Owner role is the audience for this endpoint.
		_, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
			Type:                   codersdk.AIProviderTypeAnthropic,
			Name:                   "secret-leak",
			Enabled:                true,
			BaseURL:                "https://api.anthropic.com/",
			APIKey:                 "sk-ant-supersecret", //nolint:gosec // test fixture, not a real credential
			BedrockAccessKeySecret: "bedrock-supersecret",
		})
		require.NoError(t, err)

		// Hit the raw HTTP endpoints to inspect the response body.
		res, err := client.Request(ctx, http.MethodGet, "/api/v2/aibridge/providers", nil)
		require.NoError(t, err)
		defer res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)
		bodyBytes, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		body := string(bodyBytes)
		require.NotContains(t, body, "sk-ant-supersecret")
		require.NotContains(t, body, "bedrock-supersecret")
		require.NotContains(t, body, "api_key")
		require.NotContains(t, body, "bedrock_access_key_secret")

		res2, err := client.Request(ctx, http.MethodGet, "/api/v2/aibridge/providers/secret-leak", nil)
		require.NoError(t, err)
		defer res2.Body.Close()
		require.Equal(t, http.StatusOK, res2.StatusCode)
		bodyBytes, err = io.ReadAll(res2.Body)
		require.NoError(t, err)
		body = string(bodyBytes)
		require.NotContains(t, body, "sk-ant-supersecret")
		require.NotContains(t, body, "bedrock-supersecret")
	})
}
