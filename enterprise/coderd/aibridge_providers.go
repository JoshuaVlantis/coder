package coderd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/coderd/audit"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbauthz"
	"github.com/coder/coder/v2/coderd/httpapi"
	"github.com/coder/coder/v2/codersdk"
)

// aiProviderNameRegex mirrors the CHECK constraint on ai_providers.name.
// Provider names are lowercase alphanumeric with hyphen separators so they
// are safe in URLs.
var aiProviderNameRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedAIProviderNames are paths under /api/v2/aibridge that must not
// collide with provider names. The aibridged catch-all route uses provider
// name as the first path segment, so any name that overlaps with a fixed
// route would shadow it.
var reservedAIProviderNames = map[string]struct{}{
	"providers":     {},
	"proxy":         {},
	"interceptions": {},
	"sessions":      {},
	"models":        {},
	"clients":       {},
}

// aiProviderSettingsBlob is the on-disk JSON shape of ai_providers.settings.
// It is encrypted as a single blob via dbcrypt; this struct is what we
// marshal/unmarshal on either side of that boundary.
type aiProviderSettingsBlob struct {
	BedrockRegion          string `json:"bedrock_region,omitempty"`
	BedrockModel           string `json:"bedrock_model,omitempty"`
	BedrockSmallFastModel  string `json:"bedrock_small_fast_model,omitempty"`
	BedrockAccessKeySecret string `json:"bedrock_access_key_secret,omitempty"`
}

// aiBridgeProvidersHandler registers the CRUD HTTP routes for the
// runtime AI Bridge provider configuration.
func aiBridgeProvidersHandler(api *API, middlewares ...func(http.Handler) http.Handler) func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(middlewares...)
		r.Get("/", api.aiBridgeListProviders)
		r.Post("/", api.aiBridgeCreateProvider)
		r.Get("/{idOrName}", api.aiBridgeGetProvider)
		r.Patch("/{idOrName}", api.aiBridgeUpdateProvider)
		r.Delete("/{idOrName}", api.aiBridgeDeleteProvider)
	}
}

// @Summary List AI Bridge providers
// @ID list-ai-bridge-providers
// @Security CoderSessionToken
// @Produce json
// @Tags AI Bridge
// @Success 200 {array} codersdk.AIProvider
// @Router /aibridge/providers [get]
func (api *API) aiBridgeListProviders(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := api.Database.GetAIProviders(ctx)
	if dbauthz.IsNotAuthorizedError(err) {
		httpapi.Forbidden(rw)
		return
	}
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Internal error listing AI providers.",
			Detail:  err.Error(),
		})
		return
	}

	out := make([]codersdk.AIProvider, 0, len(rows))
	for _, row := range rows {
		sdk, err := dbAIProviderToSDK(row)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Internal error converting AI provider.",
				Detail:  err.Error(),
			})
			return
		}
		out = append(out, sdk)
	}
	httpapi.Write(ctx, rw, http.StatusOK, out)
}

// @Summary Get an AI Bridge provider
// @ID get-an-ai-bridge-provider
// @Security CoderSessionToken
// @Produce json
// @Tags AI Bridge
// @Param idOrName path string true "Provider ID or name"
// @Success 200 {object} codersdk.AIProvider
// @Router /aibridge/providers/{idOrName} [get]
func (api *API) aiBridgeGetProvider(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	row, err := lookupAIProvider(ctx, api.Database, chi.URLParam(r, "idOrName"))
	if err != nil {
		writeAIProviderLookupError(ctx, rw, err)
		return
	}

	sdk, err := dbAIProviderToSDK(row)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Internal error converting AI provider.",
			Detail:  err.Error(),
		})
		return
	}
	httpapi.Write(ctx, rw, http.StatusOK, sdk)
}

// @Summary Create an AI Bridge provider
// @ID create-an-ai-bridge-provider
// @Security CoderSessionToken
// @Accept json
// @Produce json
// @Tags AI Bridge
// @Param request body codersdk.CreateAIProviderRequest true "Create AI provider request"
// @Success 201 {object} codersdk.AIProvider
// @Router /aibridge/providers [post]
func (api *API) aiBridgeCreateProvider(rw http.ResponseWriter, r *http.Request) {
	var (
		ctx               = r.Context()
		auditor           = api.AGPL.Auditor.Load()
		aReq, commitAudit = audit.InitRequest[database.AiProvider](rw, &audit.RequestParams{
			Audit:   *auditor,
			Log:     api.AGPL.Logger,
			Request: r,
			Action:  database.AuditActionCreate,
		})
	)
	defer commitAudit()

	var req codersdk.CreateAIProviderRequest
	if !httpapi.Read(ctx, rw, r, &req) {
		return
	}

	if validations := validateCreateAIProviderRequest(req); len(validations) > 0 {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message:     "Invalid AI provider request.",
			Validations: validations,
		})
		return
	}

	settings, err := buildSettingsBlob(req.Settings, req.BedrockAccessKeySecret)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Internal error encoding settings.",
			Detail:  err.Error(),
		})
		return
	}

	row, err := api.Database.InsertAIProvider(ctx, database.InsertAIProviderParams{
		ID:          uuid.New(),
		Type:        database.AiProviderType(req.Type),
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Enabled:     req.Enabled,
		BaseUrl:     req.BaseURL,
		APIKey:      req.APIKey,
		ApiKeyKeyID: sql.NullString{},
		Settings:    settings,
		// SettingsKeyID is set by the dbcrypt wrapper.
		SettingsKeyID: sql.NullString{},
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			httpapi.Write(ctx, rw, http.StatusConflict, codersdk.Response{
				Message: fmt.Sprintf("AI provider %q already exists.", req.Name),
				Detail:  err.Error(),
			})
			return
		}
		if dbauthz.IsNotAuthorizedError(err) {
			httpapi.Forbidden(rw)
			return
		}
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Internal error creating AI provider.",
			Detail:  err.Error(),
		})
		return
	}
	aReq.New = row

	sdk, err := dbAIProviderToSDK(row)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Internal error converting AI provider.",
			Detail:  err.Error(),
		})
		return
	}
	httpapi.Write(ctx, rw, http.StatusCreated, sdk)
}

// @Summary Update an AI Bridge provider
// @ID update-an-ai-bridge-provider
// @Security CoderSessionToken
// @Accept json
// @Produce json
// @Tags AI Bridge
// @Param idOrName path string true "Provider ID or name"
// @Param request body codersdk.UpdateAIProviderRequest true "Update AI provider request"
// @Success 200 {object} codersdk.AIProvider
// @Router /aibridge/providers/{idOrName} [patch]
func (api *API) aiBridgeUpdateProvider(rw http.ResponseWriter, r *http.Request) {
	var (
		ctx               = r.Context()
		auditor           = api.AGPL.Auditor.Load()
		aReq, commitAudit = audit.InitRequest[database.AiProvider](rw, &audit.RequestParams{
			Audit:   *auditor,
			Log:     api.AGPL.Logger,
			Request: r,
			Action:  database.AuditActionWrite,
		})
	)
	defer commitAudit()

	var req codersdk.UpdateAIProviderRequest
	if !httpapi.Read(ctx, rw, r, &req) {
		return
	}

	if req.DisplayName == nil && req.Enabled == nil && req.BaseURL == nil &&
		req.APIKey == nil && req.Settings == nil && req.BedrockAccessKeySecret == nil {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: "At least one field must be provided.",
		})
		return
	}
	if validations := validateUpdateAIProviderRequest(req); len(validations) > 0 {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message:     "Invalid AI provider request.",
			Validations: validations,
		})
		return
	}

	idOrName := chi.URLParam(r, "idOrName")

	var updated database.AiProvider
	err := api.Database.InTx(func(tx database.Store) error {
		old, err := lookupAIProvider(ctx, tx, idOrName)
		if err != nil {
			return err
		}
		aReq.Old = old

		// Decode the existing settings to merge with the patch. The dbcrypt
		// wrapper has already decrypted the blob for us.
		var blob aiProviderSettingsBlob
		if old.Settings != "" {
			if err := json.Unmarshal([]byte(old.Settings), &blob); err != nil {
				return xerrors.Errorf("decode existing settings: %w", err)
			}
		}
		if req.Settings != nil {
			blob.BedrockRegion = req.Settings.BedrockRegion
			blob.BedrockModel = req.Settings.BedrockModel
			blob.BedrockSmallFastModel = req.Settings.BedrockSmallFastModel
		}
		if req.BedrockAccessKeySecret != nil {
			blob.BedrockAccessKeySecret = *req.BedrockAccessKeySecret
		}
		settings, err := encodeSettingsBlob(blob)
		if err != nil {
			return xerrors.Errorf("encode settings: %w", err)
		}

		params := database.UpdateAIProviderParams{
			ID:            old.ID,
			DisplayName:   strDeref(req.DisplayName, old.DisplayName),
			Enabled:       boolDeref(req.Enabled, old.Enabled),
			BaseUrl:       strDeref(req.BaseURL, old.BaseUrl),
			APIKey:        strDeref(req.APIKey, old.APIKey),
			ApiKeyKeyID:   sql.NullString{},
			Settings:      settings,
			SettingsKeyID: sql.NullString{},
		}

		updated, err = tx.UpdateAIProvider(ctx, params)
		if err != nil {
			return xerrors.Errorf("update ai provider: %w", err)
		}
		aReq.New = updated
		return nil
	}, nil)
	if err != nil {
		writeAIProviderLookupError(ctx, rw, err)
		return
	}

	sdk, err := dbAIProviderToSDK(updated)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Internal error converting AI provider.",
			Detail:  err.Error(),
		})
		return
	}
	httpapi.Write(ctx, rw, http.StatusOK, sdk)
}

// @Summary Delete an AI Bridge provider
// @ID delete-an-ai-bridge-provider
// @Security CoderSessionToken
// @Tags AI Bridge
// @Param idOrName path string true "Provider ID or name"
// @Success 204
// @Router /aibridge/providers/{idOrName} [delete]
func (api *API) aiBridgeDeleteProvider(rw http.ResponseWriter, r *http.Request) {
	var (
		ctx               = r.Context()
		auditor           = api.AGPL.Auditor.Load()
		aReq, commitAudit = audit.InitRequest[database.AiProvider](rw, &audit.RequestParams{
			Audit:   *auditor,
			Log:     api.AGPL.Logger,
			Request: r,
			Action:  database.AuditActionDelete,
		})
	)
	defer commitAudit()

	idOrName := chi.URLParam(r, "idOrName")

	row, err := lookupAIProvider(ctx, api.Database, idOrName)
	if err != nil {
		writeAIProviderLookupError(ctx, rw, err)
		return
	}
	aReq.Old = row

	deleted, err := api.Database.SoftDeleteAIProviderByID(ctx, row.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Already gone; treat as success for idempotency.
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		if dbauthz.IsNotAuthorizedError(err) {
			httpapi.Forbidden(rw)
			return
		}
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Internal error deleting AI provider.",
			Detail:  err.Error(),
		})
		return
	}
	aReq.New = deleted

	rw.WriteHeader(http.StatusNoContent)
}

// lookupAIProvider resolves a UUID-or-name path parameter against a Store.
// Soft-deleted providers are not returned; lookup by name searches active
// rows only so reserved names cannot mask a deleted row's identity.
func lookupAIProvider(ctx context.Context, store database.Store, idOrName string) (database.AiProvider, error) {
	if id, err := uuid.Parse(idOrName); err == nil {
		row, err := store.GetAIProviderByID(ctx, id)
		if err != nil {
			return database.AiProvider{}, err
		}
		return row, nil
	}
	if !aiProviderNameRegex.MatchString(idOrName) {
		// The regex check protects against accidental/malicious lookups
		// against rows that should be impossible to insert.
		return database.AiProvider{}, sql.ErrNoRows
	}
	return store.GetAIProviderByName(ctx, idOrName)
}

// writeAIProviderLookupError translates lookup errors into the right HTTP
// status code.
func writeAIProviderLookupError(ctx context.Context, rw http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.ResourceNotFound(rw)
		return
	}
	if dbauthz.IsNotAuthorizedError(err) {
		httpapi.Forbidden(rw)
		return
	}
	httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
		Message: "Internal error fetching AI provider.",
		Detail:  err.Error(),
	})
}

// validateCreateAIProviderRequest returns the field-level validation errors
// for a create request. An empty slice indicates the request is valid.
func validateCreateAIProviderRequest(req codersdk.CreateAIProviderRequest) []codersdk.ValidationError {
	var validations []codersdk.ValidationError
	switch req.Type {
	case codersdk.AIProviderTypeOpenAI, codersdk.AIProviderTypeAnthropic:
	case "":
		validations = append(validations, codersdk.ValidationError{Field: "type", Detail: "type is required"})
	default:
		validations = append(validations, codersdk.ValidationError{
			Field:  "type",
			Detail: fmt.Sprintf("unsupported provider type %q; expected one of: openai, anthropic", req.Type),
		})
	}
	if errs := validateAIProviderName(req.Name); len(errs) > 0 {
		validations = append(validations, errs...)
	}
	if req.BaseURL == "" {
		validations = append(validations, codersdk.ValidationError{Field: "base_url", Detail: "base_url is required"})
	} else if errs := validateAIProviderBaseURL(req.BaseURL); len(errs) > 0 {
		validations = append(validations, errs...)
	}
	return validations
}

// validateUpdateAIProviderRequest validates only the fields that were
// supplied in the request.
func validateUpdateAIProviderRequest(req codersdk.UpdateAIProviderRequest) []codersdk.ValidationError {
	var validations []codersdk.ValidationError
	if req.BaseURL != nil {
		if *req.BaseURL == "" {
			validations = append(validations, codersdk.ValidationError{Field: "base_url", Detail: "base_url cannot be empty"})
		} else if errs := validateAIProviderBaseURL(*req.BaseURL); len(errs) > 0 {
			validations = append(validations, errs...)
		}
	}
	return validations
}

func validateAIProviderName(name string) []codersdk.ValidationError {
	var validations []codersdk.ValidationError
	switch {
	case name == "":
		validations = append(validations, codersdk.ValidationError{Field: "name", Detail: "name is required"})
	case !aiProviderNameRegex.MatchString(name):
		validations = append(validations, codersdk.ValidationError{
			Field:  "name",
			Detail: "name must match ^[a-z0-9]+(-[a-z0-9]+)*$ (lowercase alphanumeric, hyphens between words)",
		})
	default:
		if _, ok := reservedAIProviderNames[strings.ToLower(name)]; ok {
			validations = append(validations, codersdk.ValidationError{
				Field:  "name",
				Detail: fmt.Sprintf("%q is a reserved provider name", name),
			})
		}
	}
	return validations
}

func validateAIProviderBaseURL(raw string) []codersdk.ValidationError {
	var validations []codersdk.ValidationError
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		validations = append(validations, codersdk.ValidationError{
			Field:  "base_url",
			Detail: "base_url must be an absolute URL (e.g. https://api.example.com/)",
		})
		return validations
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		validations = append(validations, codersdk.ValidationError{
			Field:  "base_url",
			Detail: fmt.Sprintf("base_url scheme must be http or https, got %q", parsed.Scheme),
		})
	}
	return validations
}

// dbAIProviderToSDK converts a database row into the codersdk type. The
// caller is responsible for ensuring the row has been decrypted (i.e.
// fetched through the dbcrypt-wrapped store).
func dbAIProviderToSDK(row database.AiProvider) (codersdk.AIProvider, error) {
	display := row.DisplayName
	if display == "" {
		display = row.Name
	}
	out := codersdk.AIProvider{
		ID:          row.ID,
		Type:        codersdk.AIProviderType(row.Type),
		Name:        row.Name,
		DisplayName: display,
		Enabled:     row.Enabled,
		BaseURL:     row.BaseUrl,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.Settings != "" {
		var blob aiProviderSettingsBlob
		if err := json.Unmarshal([]byte(row.Settings), &blob); err != nil {
			return codersdk.AIProvider{}, xerrors.Errorf("decode settings: %w", err)
		}
		out.Settings = codersdk.AIProviderSettings{
			BedrockRegion:         blob.BedrockRegion,
			BedrockModel:          blob.BedrockModel,
			BedrockSmallFastModel: blob.BedrockSmallFastModel,
			// BedrockAccessKeySecret is intentionally omitted from
			// responses.
		}
	}
	return out, nil
}

// buildSettingsBlob serializes settings + secret for an insert, returning
// "" when the blob is empty so the row keeps an empty plaintext.
func buildSettingsBlob(s codersdk.AIProviderSettings, bedrockSecret string) (string, error) {
	blob := aiProviderSettingsBlob{
		BedrockRegion:          s.BedrockRegion,
		BedrockModel:           s.BedrockModel,
		BedrockSmallFastModel:  s.BedrockSmallFastModel,
		BedrockAccessKeySecret: bedrockSecret,
	}
	return encodeSettingsBlob(blob)
}

func encodeSettingsBlob(blob aiProviderSettingsBlob) (string, error) {
	if blob == (aiProviderSettingsBlob{}) {
		return "", nil
	}
	out, err := json.Marshal(blob)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func strDeref(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func boolDeref(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}
