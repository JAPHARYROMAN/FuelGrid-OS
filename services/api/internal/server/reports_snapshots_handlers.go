package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/reportsnapshots"
)

// Report snapshots & locking (Reports Center Phase 14 — blueprint §15).
//
// A snapshot CAPTURES a report at a point in time: the live structured report is
// re-run to its ReportEnvelope (the SAME builders the Export Center uses —
// reportSpecFor / the spec.build closures — so every money/litre figure is the
// exact decimal string the repos return), canonical-hashed, and stored
// IMMUTABLY. The captured envelope + hash are frozen by a DB trigger (migration
// 0113); a correction never overwrites — it captures the next revision and
// supersedes the prior one.
//
// PERMISSION MODEL: every snapshot endpoint is gated by the SAME permission as
// running the underlying report LIVE (reportSpecFor(key).perm, station-scoped via
// the captured filters when the report is station-scoped). Capturing, listing,
// viewing, signing off and reopening all re-evaluate that exact permission, so a
// signed-off snapshot can never leak data to someone who could not run the report
// live. This mirrors the Export Center's generation-time + delivery-time re-checks.

// snapshotView is the JSON wire shape for a snapshot's metadata (no envelope).
// The list/lock surfaces use this; the single-view endpoint additionally returns
// the stored envelope.
func snapshotView(s *reportsnapshots.Snapshot) map[string]any {
	var supersedes *string
	if s.SupersedesID != nil {
		v := s.SupersedesID.String()
		supersedes = &v
	}
	var signedOffBy *string
	if s.SignedOffBy != nil {
		v := s.SignedOffBy.String()
		signedOffBy = &v
	}
	var signedOffAt *string
	if s.SignedOffAt != nil {
		v := s.SignedOffAt.Format(time.RFC3339)
		signedOffAt = &v
	}
	return map[string]any{
		"id":              s.ID,
		"report_key":      s.ReportKey,
		"filters_used":    s.FiltersUsed,
		"content_hash":    s.ContentHash,
		"captured_by":     s.CapturedBy,
		"captured_at":     s.CapturedAt.Format(time.RFC3339),
		"status":          s.Status,
		"revision":        s.Revision,
		"supersedes_id":   supersedes,
		"signed_off_by":   signedOffBy,
		"signed_off_at":   signedOffAt,
		"correction_note": s.CorrectionNote,
		"created_at":      s.CreatedAt.Format(time.RFC3339),
	}
}

// authorizeSnapshotReport re-evaluates, for the given report key + captured
// filters, the SAME permission as running that report live. It returns the
// resolved spec on success; on failure it has already written the HTTP error
// (404 for an unknown report key, 403 for a forbidden actor/scope, 400 for a
// station-scoped report missing its station_id). This is the single gate every
// snapshot endpoint funnels through, so the snapshot authorization can never
// drift from the live report's.
func (s *Server) authorizeSnapshotReport(
	w http.ResponseWriter, r *http.Request, actor identity.Actor, reportKey string, filters map[string]string,
) (reportSpec, bool) {
	spec, ok := reportSpecFor(reportKey)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown report")
		return reportSpec{}, false
	}
	resource := policy.Resource{}
	if spec.stationScoped {
		sid, perr := uuid.Parse(strings.TrimSpace(filters["station_id"]))
		if perr != nil {
			writeError(w, http.StatusBadRequest, "station_id is required for this report")
			return reportSpec{}, false
		}
		resource = policy.AtStation(sid)
	}
	if cerr := s.policy.Can(r.Context(), actor, spec.perm, resource); cerr != nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return reportSpec{}, false
	}
	return spec, true
}

// captureSnapshotRequest is the POST /reports/{key}/snapshots body. Filters are
// the report inputs (station_id, period, operating_day_id, …) — the same map the
// live report endpoint takes. supersedes_id, when set, ties a correction capture
// to the reopened snapshot it replaces.
type captureSnapshotRequest struct {
	Filters      map[string]string `json:"filters"`
	SupersedesID string            `json:"supersedes_id"`
}

// handleCaptureSnapshot re-runs the report named by {key} to its ReportEnvelope
// under the actor's permission, canonical-hashes it, and stores an IMMUTABLE
// snapshot (status draft). The revision is 1 for a fresh report/scope, or N+1
// superseding a prior chain when correcting a reopened snapshot. Audited +
// outboxed. Gated by the report's OWN permission (resolved from {key}).
func (s *Server) handleCaptureSnapshot(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	reportKey := strings.TrimSpace(chi.URLParam(r, "key"))

	var req captureSnapshotRequest
	if r.ContentLength != 0 {
		if derr := decodeJSON(r, &req); derr != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	if req.Filters == nil {
		req.Filters = map[string]string{}
	}

	spec, ok := s.authorizeSnapshotReport(w, r, actor, reportKey, req.Filters)
	if !ok {
		return
	}

	ctx := r.Context()

	// Resolve a supersedes link when correcting: it must be a real signed-off-then-
	// reopened snapshot of the SAME report (the original stays immutable; this
	// capture is its next revision).
	var supersedesID *uuid.UUID
	if raw := strings.TrimSpace(req.SupersedesID); raw != "" {
		prevID, perr := uuid.Parse(raw)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid supersedes_id")
			return
		}
		prev, gerr := s.reportSnaps.Get(ctx, actor.TenantID, prevID)
		if errors.Is(gerr, reportsnapshots.ErrNotFound) {
			writeError(w, http.StatusNotFound, "superseded snapshot not found")
			return
		}
		if gerr != nil {
			s.logger.Error("snapshot capture: load supersedes", "error", gerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if prev.ReportKey != reportKey {
			writeError(w, http.StatusBadRequest, "superseded snapshot is for a different report")
			return
		}
		if prev.Status != reportsnapshots.StatusReopened {
			writeError(w, http.StatusConflict, "the superseded snapshot must be reopened before a new revision can be captured")
			return
		}
		supersedesID = &prevID
	}

	// Re-run the report to its envelope using the SAME builder the Export Center
	// uses — the figures are identical to the live page.
	env, berr := spec.build(ctx, s, actor, req.Filters)
	if berr != nil {
		s.logger.Error("snapshot capture: build envelope", "error", berr, "report_key", reportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	storage, contentHash, herr := canonicalEnvelopeJSON(env)
	if herr != nil {
		s.logger.Error("snapshot capture: canonical hash", "error", herr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Next revision in the report/scope's chain (1 for a fresh report/scope).
	stationFilter := stationFilterPtr(req.Filters)
	maxRev, merr := s.reportSnaps.MaxRevisionForChain(ctx, actor.TenantID, reportKey, stationFilter)
	if merr != nil {
		s.logger.Error("snapshot capture: max revision", "error", merr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Capture + audit atomically.
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snap, cerr := s.reportSnaps.CaptureTx(ctx, tx, actor.TenantID, reportsnapshots.CaptureInput{
		ReportKey:    reportKey,
		FiltersUsed:  req.Filters,
		Envelope:     storage,
		ContentHash:  contentHash,
		CapturedBy:   actor.UserID,
		Revision:     maxRev + 1,
		SupersedesID: supersedesID,
	})
	if cerr != nil {
		s.logger.Error("snapshot capture: insert", "error", cerr, "report_key", reportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.auditSnapshotTx(ctx, tx, actor, r, "report.snapshot.captured", "ReportSnapshotCaptured", snap, map[string]any{
		"content_hash": snap.ContentHash,
		"revision":     snap.Revision,
	}); err != nil {
		s.logger.Error("snapshot capture: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, snapshotView(snap))
}

// handleListSnapshots lists the snapshots for one report (the revision chain),
// newest first. Gated by the report's OWN permission. For a station-scoped
// report the actor must pass ?station_id so the scope re-check matches the
// captured snapshots.
func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	reportKey := strings.TrimSpace(chi.URLParam(r, "key"))

	// The list re-check uses the query filters (station_id) exactly like the live
	// report's scope gate.
	filters := map[string]string{}
	if sid := strings.TrimSpace(r.URL.Query().Get("station_id")); sid != "" {
		filters["station_id"] = sid
	}
	if _, ok := s.authorizeSnapshotReport(w, r, actor, reportKey, filters); !ok {
		return
	}

	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	snaps, lerr := s.reportSnaps.ListForReport(r.Context(), actor.TenantID, reportKey, limit+1, offset)
	if lerr != nil {
		s.logger.Error("snapshot list", "error", lerr, "report_key", reportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(snaps) > limit
	if hasMore {
		snaps = snaps[:limit]
	}
	out := make([]map[string]any, 0, len(snaps))
	for i := range snaps {
		out = append(out, snapshotView(&snaps[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handleGetSnapshot returns a single snapshot's STORED envelope (a point-in-time
// view, NOT a live re-run) plus its hash + metadata. The actor must hold the
// SAME permission as running the underlying report live (re-checked against the
// snapshot's captured filters), so a signed-off snapshot never leaks data the
// actor could not run live. Tenant-scoped.
func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	snap, ok := s.loadSnapshotAuthorized(w, r, actor)
	if !ok {
		return
	}

	// Return the STORED envelope verbatim (json.RawMessage), never a live re-run.
	out := snapshotView(snap)
	out["envelope"] = snap.Envelope
	writeJSON(w, http.StatusOK, out)
}

// signOffSnapshotRequest is the optional sign-off body (reserved for a future
// note; sign-off records the signer + time regardless).
type signOffSnapshotRequest struct {
	Note string `json:"note"`
}

// handleSignOffSnapshot transitions a draft/reopened snapshot to 'signed_off',
// recording the signer + time. Gated by the report's OWN permission. The signer
// is recorded alongside the original capturer; no policy forces signer != capturer
// here (the blueprint records both — a SoD policy can be layered later), so an
// authorized actor may sign off their own capture while both identities stay on
// the immutable record.
func (s *Server) handleSignOffSnapshot(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	snap, ok := s.loadSnapshotAuthorized(w, r, actor)
	if !ok {
		return
	}
	if snap.Status == reportsnapshots.StatusSignedOff {
		writeError(w, http.StatusConflict, "snapshot is already signed off")
		return
	}

	// Body is optional; a malformed body is rejected but an empty one is fine.
	if r.ContentLength != 0 {
		var req signOffSnapshotRequest
		if derr := decodeJSON(r, &req); derr != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	ctx := r.Context()
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, uerr := s.reportSnaps.SignOffTx(ctx, tx, actor.TenantID, snap.ID, actor.UserID)
	if errors.Is(uerr, reportsnapshots.ErrNotFound) {
		writeError(w, http.StatusConflict, "snapshot cannot be signed off in its current state")
		return
	}
	if uerr != nil {
		s.logger.Error("snapshot sign-off", "error", uerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.auditSnapshotTx(ctx, tx, actor, r, "report.snapshot.signed_off", "ReportSnapshotSignedOff", updated, map[string]any{
		"captured_by": snap.CapturedBy.String(),
		"signed_off":  true,
	}); err != nil {
		s.logger.Error("snapshot sign-off: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, snapshotView(updated))
}

// reopenSnapshotRequest is the reopen body — a correction_note is REQUIRED.
type reopenSnapshotRequest struct {
	CorrectionNote string `json:"correction_note"`
}

// handleReopenSnapshot reopens a SIGNED-OFF snapshot: it requires a correction
// note, marks the snapshot 'reopened', and clears the sign-off stamp — the
// captured payload stays immutable. The corrected figure is captured as the NEXT
// revision (POST /reports/{key}/snapshots with supersedes_id = this snapshot).
// Gated by the report's OWN permission. Audited + outboxed.
func (s *Server) handleReopenSnapshot(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	snap, ok := s.loadSnapshotAuthorized(w, r, actor)
	if !ok {
		return
	}

	var req reopenSnapshotRequest
	if derr := decodeJSON(r, &req); derr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	note := strings.TrimSpace(req.CorrectionNote)
	if note == "" {
		writeError(w, http.StatusBadRequest, "correction_note is required to reopen a snapshot")
		return
	}
	if snap.Status != reportsnapshots.StatusSignedOff {
		writeError(w, http.StatusConflict, "only a signed-off snapshot can be reopened")
		return
	}

	ctx := r.Context()
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, uerr := s.reportSnaps.ReopenTx(ctx, tx, actor.TenantID, snap.ID, note)
	if errors.Is(uerr, reportsnapshots.ErrNotFound) {
		writeError(w, http.StatusConflict, "snapshot cannot be reopened in its current state")
		return
	}
	if uerr != nil {
		s.logger.Error("snapshot reopen", "error", uerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.auditSnapshotTx(ctx, tx, actor, r, "report.snapshot.reopened", "ReportSnapshotReopened", updated, map[string]any{
		"correction_note": note,
	}); err != nil {
		s.logger.Error("snapshot reopen: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, snapshotView(updated))
}

// loadSnapshotAuthorized loads the {id} snapshot for the tenant and re-checks the
// actor's permission to run the UNDERLYING report (using the snapshot's captured
// filters), so a viewer/signer/reopener must hold the same permission as running
// the report live. Returns the snapshot on success; otherwise it has written the
// error (404 absent/cross-tenant, 403 forbidden). Used by view/sign-off/reopen.
func (s *Server) loadSnapshotAuthorized(w http.ResponseWriter, r *http.Request, actor identity.Actor) (*reportsnapshots.Snapshot, bool) {
	id, perr := uuid.Parse(chi.URLParam(r, "id"))
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid snapshot id")
		return nil, false
	}
	snap, gerr := s.reportSnaps.Get(r.Context(), actor.TenantID, id)
	if errors.Is(gerr, reportsnapshots.ErrNotFound) {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return nil, false
	}
	if gerr != nil {
		s.logger.Error("snapshot load", "error", gerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if _, ok := s.authorizeSnapshotReport(w, r, actor, snap.ReportKey, snap.FiltersUsed); !ok {
		return nil, false
	}
	return snap, true
}

// auditSnapshotTx writes the audit_logs + outbox_events for a snapshot state
// change within the supplied transaction (so the state change and its audit
// commit atomically), mirroring the export surface's audit path. extra is merged
// into the audited NewValue alongside the snapshot's identifying fields.
func (s *Server) auditSnapshotTx(
	ctx context.Context, tx pgx.Tx, actor identity.Actor, r *http.Request,
	action, eventType string, snap *reportsnapshots.Snapshot, extra map[string]any,
) error {
	newValue := map[string]any{
		"report_key": snap.ReportKey,
		"revision":   snap.Revision,
		"status":     snap.Status,
	}
	for k, v := range snap.FiltersUsed {
		newValue["filter_"+k] = v
	}
	for k, v := range extra {
		newValue[k] = v
	}
	return audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:   actor.TenantID,
		ActorID:    actor.UserID,
		Action:     action,
		EventType:  eventType,
		EntityType: "report_snapshot",
		EntityID:   snap.ID.String(),
		NewValue:   newValue,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  chimiddleware.GetReqID(ctx),
	})
}

// stationFilterPtr returns a pointer to the station_id filter when present, else
// nil — so the revision chain is keyed per station for station-scoped reports and
// tenant-wide otherwise.
func stationFilterPtr(filters map[string]string) *string {
	if sid := strings.TrimSpace(filters["station_id"]); sid != "" {
		return &sid
	}
	return nil
}
