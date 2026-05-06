package dynamicparameters

import (
	"context"

	"github.com/google/uuid"
	"github.com/hashicorp/hcl/v2"
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/files"
	"github.com/coder/coder/v2/codersdk"
	previewtypes "github.com/coder/preview/types"
)

// CheckSecretRequirements prepares a renderer for the given template version,
// evaluates it against the supplied parameter values, and returns the secret
// requirement statuses produced by the dynamic renderer.
//
// The returned slice mirrors what the dynamic-parameters websocket exposes
// (deduped, with conditional `coder_secret` blocks resolved). It is empty when
// the template version declares no secret requirements, falls back to the
// static renderer, or when the caller is not authorized to read the owner's
// secrets.
//
// Diagnostics surface the same forbidden / fetch-failed cases that
// (*dynamicRenderer).checkSecretRequirements returns. Callers should inspect
// them for DiagCodeSecretValidationForbidden and DiagCodeOwnerSecretsFetchFailed
// before treating the empty slice as "no missing secrets". Other diagnostics
// from the underlying preview render (parameter errors, etc.) are also
// returned so callers can decide whether the result is authoritative.
//
// loaderOpts forwards to Prepare and lets callers reuse already-fetched
// database objects (template version, terraform values, variables) when
// available; pass nil for an unoptimized fetch.
func CheckSecretRequirements(
	ctx context.Context,
	db database.Store,
	cache files.FileAcquirer,
	versionID uuid.UUID,
	ownerID uuid.UUID,
	parameterValues map[string]string,
	loaderOpts ...func(*loader),
) ([]codersdk.SecretRequirementStatus, hcl.Diagnostics, error) {
	renderer, err := Prepare(ctx, db, cache, versionID, loaderOpts...)
	if err != nil {
		return nil, nil, xerrors.Errorf("prepare renderer: %w", err)
	}
	defer renderer.Close()

	result, diags := renderer.Render(ctx, ownerID, parameterValues, IncludeSecretRequirements())
	if result == nil {
		return nil, diags, nil
	}
	return result.SecretRequirements, diags, nil
}

// HasSecretValidationDiagnostic reports whether diags contain a diagnostic
// that signals the secret-requirement evaluation is non-authoritative
// (forbidden caller or owner-secret fetch failure). Callers should not treat
// an empty status slice as "no missing secrets" when this returns true.
func HasSecretValidationDiagnostic(diags hcl.Diagnostics) bool {
	for _, d := range diags {
		extra, ok := d.Extra.(previewtypes.DiagnosticExtra)
		if !ok {
			continue
		}
		if extra.Code == DiagCodeSecretValidationForbidden ||
			extra.Code == DiagCodeOwnerSecretsFetchFailed {
			return true
		}
	}
	return false
}
