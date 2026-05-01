package coderd_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/coderdtest"
	"github.com/coder/coder/v2/coderd/database/dbtestutil"
	"github.com/coder/coder/v2/codersdk"
	entcoderd "github.com/coder/coder/v2/enterprise/coderd"
	"github.com/coder/coder/v2/enterprise/coderd/coderdenttest"
	"github.com/coder/coder/v2/enterprise/coderd/license"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/serpent"
)

// TestAIProvidersPubsubPublish verifies that mutating an AI Bridge
// provider publishes on the AIBridgeProvidersChangedChannel so each
// replica's RequestBridge pool can invalidate.
func TestAIProvidersPubsubPublish(t *testing.T) {
	t.Parallel()

	db, ps := dbtestutil.NewDB(t)
	dv := coderdtest.DeploymentValues(t)
	dv.AI.BridgeConfig.Enabled = serpent.Bool(true)
	client, _ := coderdenttest.New(t, &coderdenttest.Options{
		Options: &coderdtest.Options{
			DeploymentValues: dv,
			Database:         db,
			Pubsub:           ps,
		},
		LicenseOptions: &coderdenttest.LicenseOptions{
			Features: license.Features{codersdk.FeatureAIBridge: 1},
		},
	})
	ctx := testutil.Context(t, testutil.WaitLong)

	notified := make(chan struct{}, 4)
	cancel, err := ps.Subscribe(entcoderd.AIBridgeProvidersChangedChannel, func(_ context.Context, _ []byte) {
		select {
		case notified <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(cancel)

	// Create publishes.
	//nolint:gocritic // Owner role is the audience for this endpoint.
	created, err := client.CreateAIProvider(ctx, codersdk.CreateAIProviderRequest{
		Type:    codersdk.AIProviderTypeOpenAI,
		Name:    "pubsub-test",
		Enabled: true,
		BaseURL: "https://api.openai.com/v1",
	})
	require.NoError(t, err)
	select {
	case <-notified:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for pubsub notify after create")
	}

	// Update publishes.
	display := "Renamed"
	//nolint:gocritic // Owner role is the audience for this endpoint.
	_, err = client.UpdateAIProvider(ctx, created.Name, codersdk.UpdateAIProviderRequest{
		DisplayName: &display,
	})
	require.NoError(t, err)
	select {
	case <-notified:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for pubsub notify after update")
	}

	// Delete publishes.
	//nolint:gocritic // Owner role is the audience for this endpoint.
	require.NoError(t, client.DeleteAIProvider(ctx, created.Name))
	select {
	case <-notified:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for pubsub notify after delete")
	}
}
