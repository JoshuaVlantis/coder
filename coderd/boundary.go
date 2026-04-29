package coderd

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbauthz"
	"github.com/coder/coder/v2/coderd/httpapi"
	"github.com/coder/coder/v2/coderd/rbac"
	"github.com/coder/coder/v2/coderd/rbac/policy"
	"github.com/coder/coder/v2/codersdk"
)

// @Summary Get boundary session logs
// @ID get-boundary-session-logs
// @Security CoderSessionToken
// @Produce json
// @Tags Boundary
// @Param id path string true "Boundary session ID" format(uuid)
// @Param seq_after query int false "Exclusive lower bound on sequence number"
// @Param seq_before query int false "Exclusive upper bound on sequence number"
// @Param limit query int false "Maximum number of logs to return (default 100)"
// @Success 200 {object} codersdk.BoundarySessionLogsResponse
// @Router /boundary/sessions/{id}/logs [get]
func (api *API) boundarySessionLogs(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !api.Authorize(r, policy.ActionRead, rbac.ResourceBoundaryLog) {
		httpapi.Forbidden(rw)
		return
	}

	idStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(idStr)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: "Invalid session ID.",
			Detail:  err.Error(),
		})
		return
	}

	params := database.ListBoundaryLogsBySessionIDParams{
		SessionID: sessionID,
		SeqAfter:  -1,
		SeqBefore: -1,
		LimitOpt:  0,
	}

	if v := r.URL.Query().Get("seq_after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: "Invalid seq_after parameter.",
				Detail:  err.Error(),
			})
			return
		}
		params.SeqAfter = n
	}

	if v := r.URL.Query().Get("seq_before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: "Invalid seq_before parameter.",
				Detail:  err.Error(),
			})
			return
		}
		params.SeqBefore = n
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: "Invalid limit parameter.",
				Detail:  "limit must be a positive integer",
			})
			return
		}
		// #nosec G115 - Safe conversion as limit is validated above.
		params.LimitOpt = int32(n)
	}

	dbLogs, err := api.Database.ListBoundaryLogsBySessionID(ctx, params)
	if dbauthz.IsNotAuthorizedError(err) {
		httpapi.Forbidden(rw)
		return
	}
	if err != nil {
		httpapi.InternalServerError(rw, err)
		return
	}

	results := make([]codersdk.BoundaryLog, 0, len(dbLogs))
	for _, l := range dbLogs {
		bl := codersdk.BoundaryLog{
			ID:             l.ID,
			SessionID:      l.SessionID,
			SequenceNumber: l.SequenceNumber,
			Allowed:        l.Allowed,
			Time:           l.CreatedAt,
			Proto:          l.Proto,
			Method:         l.Method,
			Detail:         l.Detail,
			CapturedAt:     &l.CapturedAt,
		}
		if l.MatchedRule.Valid {
			bl.MatchedRule = &l.MatchedRule.String
		}
		results = append(results, bl)
	}

	httpapi.Write(ctx, rw, http.StatusOK, codersdk.BoundarySessionLogsResponse{
		Results: results,
	})
}
