package terraform

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/provisionersdk/proto"
)

func TestProvisionEnv_UserSecrets(t *testing.T) {
	t.Parallel()

	t.Run("EnvSecret", func(t *testing.T) {
		t.Parallel()
		secrets := []*proto.UserSecretValue{
			{EnvName: "MY_TOKEN", Value: []byte("secret-value")},
		}
		env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, secrets)
		require.NoError(t, err)

		want := "CODER_SECRET_ENV_MY_TOKEN=secret-value"
		assert.Contains(t, env, want)
	})

	t.Run("FileSecret", func(t *testing.T) {
		t.Parallel()
		filePath := "~/.ssh/id_rsa"
		secrets := []*proto.UserSecretValue{
			{FilePath: filePath, Value: []byte("key-data")},
		}
		env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, secrets)
		require.NoError(t, err)

		hexPath := hex.EncodeToString([]byte(filePath))
		want := "CODER_SECRET_FILE_" + hexPath + "=key-data"
		assert.Contains(t, env, want)
	})

	t.Run("BothEnvAndFile", func(t *testing.T) {
		t.Parallel()
		filePath := "/tmp/secret.txt"
		secrets := []*proto.UserSecretValue{
			{EnvName: "DUAL", FilePath: filePath, Value: []byte("both-value")},
		}
		env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, secrets)
		require.NoError(t, err)

		wantEnv := "CODER_SECRET_ENV_DUAL=both-value"
		hexPath := hex.EncodeToString([]byte(filePath))
		wantFile := "CODER_SECRET_FILE_" + hexPath + "=both-value"
		assert.Contains(t, env, wantEnv)
		assert.Contains(t, env, wantFile)
	})

	t.Run("NilSecrets", func(t *testing.T) {
		t.Parallel()
		env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, nil)
		require.NoError(t, err)

		for _, e := range env {
			assert.False(t, strings.HasPrefix(e, "CODER_SECRET_"),
				"unexpected secret env var: %s", e)
		}
	})

	t.Run("EmptyEnvAndFile", func(t *testing.T) {
		t.Parallel()
		secrets := []*proto.UserSecretValue{
			{EnvName: "", FilePath: "", Value: []byte("ignored")},
		}
		env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, secrets)
		require.NoError(t, err)

		for _, e := range env {
			assert.False(t, strings.HasPrefix(e, "CODER_SECRET_"),
				"unexpected secret env var: %s", e)
		}
	})
}

// nolint:paralleltest // t.Setenv is incompatible with t.Parallel.
func TestProvisionEnv_HostSecretsStripped(t *testing.T) {
	// Host CODER_* env vars must be stripped by safeEnviron before provisionEnv
	// appends its own entries. If the order of operations in provisionEnv ever
	// changes (e.g. appending before stripping, or adding a post-filter that
	// drops CODER_*), this test catches it. The host var below would otherwise
	// leak into the terraform environment and could be interpreted as a real
	// secret.
	t.Setenv("CODER_SECRET_ENV_PREEXISTING", "host-value")
	env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, nil)
	require.NoError(t, err)

	for _, e := range env {
		assert.False(t, strings.HasPrefix(e, "CODER_SECRET_"),
			"host CODER_SECRET_* var leaked into provisioner env: %s", e)
	}
}

// nolint:paralleltest // t.Setenv is incompatible with t.Parallel.
func TestProvisionEnv_InputSecretsSurviveHostCollision(t *testing.T) {
	// When the host has a CODER_SECRET_ENV_X var set and the caller also passes
	// X in the secrets slice, the caller's value must win. This proves secrets
	// are appended after safeEnviron strips the host's CODER_* vars, not before.
	t.Setenv("CODER_SECRET_ENV_COLLIDE", "host-value-should-not-win")
	secrets := []*proto.UserSecretValue{
		{EnvName: "COLLIDE", Value: []byte("caller-value")},
	}
	env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, secrets)
	require.NoError(t, err)

	assert.Contains(t, env, "CODER_SECRET_ENV_COLLIDE=caller-value",
		"caller-supplied secret must be present")
	assert.NotContains(t, env, "CODER_SECRET_ENV_COLLIDE=host-value-should-not-win",
		"host value must be stripped before secrets are appended")
}

// TestProvisionEnv_WireSizeMatchesSDK is a cross-check between the
// build-time injection format used by provisionEnv and the size formula
// in codersdk.SecretBuildTimeWireSize. The CRUD budget validator,
// provisionerdserver precheck, and any operator-facing reporting all
// rely on that formula being correct. If anyone changes the injection
// format here without updating the formula (or vice versa), this test
// fails. Loud is good.
//
// We measure each CODER_SECRET_* entry as len(entry)+1 because that is
// how the kernel measures envp against ARG_MAX: each string contributes
// its bytes plus one NUL terminator.
func TestProvisionEnv_WireSizeMatchesSDK(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		envName  string
		filePath string
		value    string
	}{
		{name: "EnvOnly", envName: "MY_TOKEN", value: "abc123"},
		{name: "FileOnly", filePath: "~/.ssh/id_rsa", value: "key-data"},
		{name: "BothEnvAndFile", envName: "DUAL", filePath: "/tmp/dual", value: "v"},
		{name: "EmptyValueEnvOnly", envName: "EMPTY", value: ""},
		{name: "LongValueFileOnly", filePath: "/etc/some/cert.pem", value: strings.Repeat("a", 4096)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			secrets := []*proto.UserSecretValue{{
				EnvName:  tc.envName,
				FilePath: tc.filePath,
				Value:    []byte(tc.value),
			}}
			env, err := provisionEnv(&proto.Config{}, &proto.Metadata{}, nil, nil, nil, secrets)
			require.NoError(t, err)

			// Sum the wire size of every CODER_SECRET_* entry that
			// provisionEnv emitted for this secret. Each entry counts
			// its string length plus a NUL terminator.
			var got int
			for _, e := range env {
				if strings.HasPrefix(e, "CODER_SECRET_ENV_") || strings.HasPrefix(e, "CODER_SECRET_FILE_") {
					got += len(e) + 1
				}
			}

			want := codersdk.SecretBuildTimeWireSize(tc.envName, tc.filePath, len(tc.value))
			assert.Equal(t, want, got,
				"codersdk.SecretBuildTimeWireSize must match the actual wire size emitted by provisionEnv. "+
					"if either side changes, update both")
		})
	}
}
