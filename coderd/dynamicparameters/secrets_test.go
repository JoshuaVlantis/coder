package dynamicparameters_test

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/dynamicparameters"
	previewtypes "github.com/coder/preview/types"
)

func TestHasSecretValidationDiagnostic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   hcl.Diagnostics
		want bool
	}{
		{
			name: "Empty",
			in:   hcl.Diagnostics{},
			want: false,
		},
		{
			name: "MissingSecretIsNotBlocking",
			in: hcl.Diagnostics{{
				Severity: hcl.DiagError,
				Summary:  "Missing required secrets",
				Extra: previewtypes.DiagnosticExtra{
					Code: dynamicparameters.DiagCodeMissingSecret,
				},
			}},
			want: false,
		},
		{
			name: "Forbidden",
			in: hcl.Diagnostics{{
				Severity: hcl.DiagWarning,
				Summary:  "Cannot validate secret requirements",
				Extra: previewtypes.DiagnosticExtra{
					Code: dynamicparameters.DiagCodeSecretValidationForbidden,
				},
			}},
			want: true,
		},
		{
			name: "FetchFailed",
			in: hcl.Diagnostics{{
				Severity: hcl.DiagError,
				Summary:  "Failed to fetch owner secrets",
				Extra: previewtypes.DiagnosticExtra{
					Code: dynamicparameters.DiagCodeOwnerSecretsFetchFailed,
				},
			}},
			want: true,
		},
		{
			name: "DiagnosticWithNoExtraIsIgnored",
			in: hcl.Diagnostics{{
				Severity: hcl.DiagError,
				Summary:  "Some other error",
			}},
			want: false,
		},
		{
			name: "MixedKeepsLookingUntilMatch",
			in: hcl.Diagnostics{
				{
					Severity: hcl.DiagError,
					Summary:  "Missing required secrets",
					Extra: previewtypes.DiagnosticExtra{
						Code: dynamicparameters.DiagCodeMissingSecret,
					},
				},
				{
					Severity: hcl.DiagError,
					Summary:  "Failed to fetch owner secrets",
					Extra: previewtypes.DiagnosticExtra{
						Code: dynamicparameters.DiagCodeOwnerSecretsFetchFailed,
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, dynamicparameters.HasSecretValidationDiagnostic(tc.in))
		})
	}
}
