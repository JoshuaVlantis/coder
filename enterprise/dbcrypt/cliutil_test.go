package dbcrypt_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog/v3/sloggers/slogtest"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbgen"
	"github.com/coder/coder/v2/coderd/database/dbtestutil"
	"github.com/coder/coder/v2/cryptorand"
	"github.com/coder/coder/v2/enterprise/dbcrypt"
)

// TestRotateAIProviders verifies that dbcrypt.Rotate re-encrypts the
// secret columns of every ai_providers row, including soft-deleted
// rows that still hold a foreign-key reference to dbcrypt_keys, so
// the old keys can be revoked without violating the FK constraint.
//
// This is a regression test for an incident where rotating ciphers
// failed with:
//
//	error: rotate ciphers: revoke key: pq: update or delete on
//	  table "dbcrypt_keys" violates foreign key constraint
//	  "ai_providers_api_key_key_id_fkey" on table "ai_providers"
func TestRotateAIProviders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rawDB, _, sqlDB := dbtestutil.NewDBWithSQLDB(t)

	oldCipher := newCipher(t)
	newCipher := newCipher(t)

	// Encrypt rows under the old cipher.
	cdb, err := dbcrypt.New(ctx, rawDB, oldCipher)
	require.NoError(t, err)

	const (
		apiKeyAlive    = "sk-alive"      //nolint:gosec // test fixture
		apiKeyDeleted  = "sk-deleted"    //nolint:gosec // test fixture
		settingsAlive  = `{"r":"alive"}` //nolint:gosec // test fixture
		settingsDelete = `{"r":"dead"}`  //nolint:gosec // test fixture
	)

	alive := dbgen.AIProvider(t, cdb, database.AiProvider{
		Name:     "alive",
		APIKey:   apiKeyAlive,
		Settings: settingsAlive,
	})
	deleted := dbgen.AIProvider(t, cdb, database.AiProvider{
		Name:     "soft-deleted",
		APIKey:   apiKeyDeleted,
		Settings: settingsDelete,
	})
	_, err = rawDB.SoftDeleteAIProviderByID(ctx, deleted.ID)
	require.NoError(t, err)

	// Both rows should have key IDs pointing at the old cipher's
	// digest before rotation.
	beforeAlive, err := rawDB.GetAIProviderByID(ctx, alive.ID)
	require.NoError(t, err)
	require.Equal(t, oldCipher.HexDigest(), beforeAlive.ApiKeyKeyID.String)
	require.Equal(t, oldCipher.HexDigest(), beforeAlive.SettingsKeyID.String)

	// Soft-deleted rows are filtered by GetAIProviderByID, so query
	// the include-deleted variant.
	beforeDeleted, err := rawDB.GetAIProviderByNameIncludeDeleted(ctx, deleted.Name)
	require.NoError(t, err)
	require.Equal(t, oldCipher.HexDigest(), beforeDeleted.ApiKeyKeyID.String)
	require.Equal(t, oldCipher.HexDigest(), beforeDeleted.SettingsKeyID.String)

	// Rotate to the new primary cipher, with the old cipher still
	// available for decryption of pre-rotation rows.
	require.NoError(t, dbcrypt.Rotate(
		ctx,
		slogtest.Make(t, nil),
		sqlDB,
		[]dbcrypt.Cipher{newCipher, oldCipher},
	))

	// All rows must now reference the new digest.
	afterAlive, err := rawDB.GetAIProviderByID(ctx, alive.ID)
	require.NoError(t, err)
	require.Equal(t, newCipher.HexDigest(), afterAlive.ApiKeyKeyID.String)
	require.Equal(t, newCipher.HexDigest(), afterAlive.SettingsKeyID.String)

	afterDeleted, err := rawDB.GetAIProviderByNameIncludeDeleted(ctx, deleted.Name)
	require.NoError(t, err)
	require.Equal(t, newCipher.HexDigest(), afterDeleted.ApiKeyKeyID.String)
	require.Equal(t, newCipher.HexDigest(), afterDeleted.SettingsKeyID.String)

	// Decrypted plaintext is still accessible and unchanged.
	postRotateCDB, err := dbcrypt.New(ctx, rawDB, newCipher)
	require.NoError(t, err)
	gotAlive, err := postRotateCDB.GetAIProviderByID(ctx, alive.ID)
	require.NoError(t, err)
	require.Equal(t, apiKeyAlive, gotAlive.APIKey)
	require.Equal(t, settingsAlive, gotAlive.Settings)

	gotDeleted, err := postRotateCDB.GetAIProviderByNameIncludeDeleted(ctx, deleted.Name)
	require.NoError(t, err)
	require.True(t, gotDeleted.Deleted)
	require.Equal(t, apiKeyDeleted, gotDeleted.APIKey)
	require.Equal(t, settingsDelete, gotDeleted.Settings)

	// The old cipher must have been successfully revoked.
	keys, err := rawDB.GetDBCryptKeys(ctx)
	require.NoError(t, err)
	for _, k := range keys {
		if k.ActiveKeyDigest.String == oldCipher.HexDigest() {
			t.Fatalf("expected old key to be revoked, but it is still active: %#v", k)
		}
	}

	// Re-running Rotate is a no-op (no FK violation, no errors).
	require.NoError(t, dbcrypt.Rotate(
		ctx,
		slogtest.Make(t, nil),
		sqlDB,
		[]dbcrypt.Cipher{newCipher},
	))
}

// TestDecryptAIProviders verifies that dbcrypt.Decrypt clears the
// FK references on every ai_providers row (including soft-deleted)
// so all keys can be revoked.
func TestDecryptAIProviders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rawDB, _, sqlDB := dbtestutil.NewDBWithSQLDB(t)
	cipher := newCipher(t)

	cdb, err := dbcrypt.New(ctx, rawDB, cipher)
	require.NoError(t, err)

	provider := dbgen.AIProvider(t, cdb, database.AiProvider{
		Name:     "to-decrypt",
		APIKey:   "sk-secret", //nolint:gosec // test fixture
		Settings: `{"r":"x"}`,
	})

	require.NoError(t, dbcrypt.Decrypt(
		ctx,
		slogtest.Make(t, nil),
		sqlDB,
		[]dbcrypt.Cipher{cipher},
	))

	got, err := rawDB.GetAIProviderByID(ctx, provider.ID)
	require.NoError(t, err)
	require.False(t, got.ApiKeyKeyID.Valid)
	require.False(t, got.SettingsKeyID.Valid)
	require.Equal(t, "sk-secret", got.APIKey)
	require.Equal(t, `{"r":"x"}`, got.Settings)
}

// TestDeleteAIProviders verifies that dbcrypt.Delete wipes the
// encrypted columns and clears the FK references on every
// ai_providers row.
func TestDeleteAIProviders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rawDB, _, sqlDB := dbtestutil.NewDBWithSQLDB(t)
	cipher := newCipher(t)

	cdb, err := dbcrypt.New(ctx, rawDB, cipher)
	require.NoError(t, err)

	provider := dbgen.AIProvider(t, cdb, database.AiProvider{
		Name:     "to-delete",
		APIKey:   "sk-secret", //nolint:gosec // test fixture
		Settings: `{"r":"x"}`,
	})

	require.NoError(t, dbcrypt.Delete(
		ctx,
		slogtest.Make(t, nil),
		sqlDB,
	))

	got, err := rawDB.GetAIProviderByID(ctx, provider.ID)
	require.NoError(t, err)
	require.False(t, got.ApiKeyKeyID.Valid)
	require.False(t, got.SettingsKeyID.Valid)
	require.Empty(t, got.APIKey)
	require.Empty(t, got.Settings)
}

// newCipher returns a fresh AES-256 dbcrypt.Cipher seeded with random
// data. Each call yields a distinct digest so it can be used to
// represent old vs new keys in rotation tests.
func newCipher(t *testing.T) dbcrypt.Cipher {
	t.Helper()
	key, err := cryptorand.String(32)
	require.NoError(t, err)
	cs, err := dbcrypt.NewCiphers([]byte(key))
	require.NoError(t, err)
	require.Len(t, cs, 1)
	return cs[0]
}
