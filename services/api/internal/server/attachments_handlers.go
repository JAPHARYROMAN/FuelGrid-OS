package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/attachments"
	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/expenses"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// Generic per-entity Attachments framework (C.3).
//
// One tenant-scoped table backs file uploads for any business entity, keyed by
// an opaque (entity_type, entity_id) pair. Bytes are stored inline (no object
// store, mirroring the tenant logo) and capped at 5 MiB; the content type is
// restricted to PDF/PNG/JPEG. Rows are append-only plus a soft delete so a
// posted/locked parent record keeps its evidence.
//
// Permissions: attachment.read (list/stream), attachment.manage (upload/remove)
// — both tenant-wide. Writes audit attachment.added / attachment.removed.

// maxAttachmentBytes mirrors attachments.MaxSizeBytes; the whole request body is
// bounded by MaxBytesReader so an oversized upload can't exhaust memory.
const maxAttachmentBytes = attachments.MaxSizeBytes

// attachmentDTO is the JSON metadata shape (never the bytes). The download URL
// is a same-origin path the SDK/BFF resolve.
type attachmentDTO struct {
	ID          string  `json:"id"`
	EntityType  string  `json:"entity_type"`
	EntityID    string  `json:"entity_id"`
	StationID   *string `json:"station_id,omitempty"`
	Filename    string  `json:"filename"`
	ContentType string  `json:"content_type"`
	SizeBytes   int64   `json:"size_bytes"`
	Checksum    string  `json:"checksum"`
	UploadedBy  *string `json:"uploaded_by,omitempty"`
	CreatedAt   string  `json:"created_at"`
	DownloadURL string  `json:"download_url"`
}

func toAttachmentDTO(a *attachments.Attachment) attachmentDTO {
	dto := attachmentDTO{
		ID:          a.ID.String(),
		EntityType:  a.EntityType,
		EntityID:    a.EntityID.String(),
		Filename:    a.Filename,
		ContentType: a.ContentType,
		SizeBytes:   a.SizeBytes,
		Checksum:    a.Checksum,
		CreatedAt:   a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		DownloadURL: "/api/v1/attachments/" + a.ID.String(),
	}
	if a.StationID != nil {
		s := a.StationID.String()
		dto.StationID = &s
	}
	if a.UploadedBy != nil {
		s := a.UploadedBy.String()
		dto.UploadedBy = &s
	}
	return dto
}

// supportedAttachmentEntities is the set of entity_type values an upload may
// target. Keeping it an allowlist (rather than free text) stops a caller from
// stashing files against arbitrary keys, and lets the upload resolve any
// parent-derived context (e.g. the expense's station) consistently.
var supportedAttachmentEntities = map[string]bool{
	"expense": true,
}

// handleUploadAttachment accepts a multipart upload (fields: entity_type,
// entity_id, file) and stores it against the entity. Permission:
// attachment.manage. Audited as attachment.added.
func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Bound the whole body so an oversized upload can't exhaust memory; a body
	// over the cap surfaces as a parse error we map to 413.
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes+(1<<16))
	if err := r.ParseMultipartForm(maxAttachmentBytes); err != nil { //nolint:gosec // body bounded by MaxBytesReader above
		writeError(w, http.StatusRequestEntityTooLarge, "file must be 5 MiB or smaller")
		return
	}

	entityType := r.FormValue("entity_type")
	if !supportedAttachmentEntities[entityType] {
		writeError(w, http.StatusBadRequest, "unsupported entity_type")
		return
	}
	entityID, err := uuid.Parse(r.FormValue("entity_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "entity_id must be a valid id")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "a file field is required")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > maxAttachmentBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file must be 5 MiB or smaller")
		return
	}

	// Read the (already-bounded) bytes and sniff the real content type so a
	// relabelled file can't smuggle a disallowed type past the allowlist.
	data := make([]byte, 0, header.Size)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := file.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			if int64(len(data)) > maxAttachmentBytes {
				writeError(w, http.StatusRequestEntityTooLarge, "file must be 5 MiB or smaller")
				return
			}
		}
		if rerr != nil {
			break
		}
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "the uploaded file is empty")
		return
	}
	contentType := http.DetectContentType(data)
	if !attachments.AllowedContentTypes[contentType] {
		writeError(w, http.StatusBadRequest, "file must be a PDF, PNG, or JPEG")
		return
	}

	ctx := r.Context()

	// Resolve parent context: confirm the parent exists in this tenant (404 on a
	// dangling reference) and carry its station for reporting context.
	stationID, ok, perr := s.resolveAttachmentParent(ctx, actor.TenantID, entityType, entityID)
	if perr != nil {
		s.logger.Error("attachment parent lookup", "error", perr, "entity_type", entityType)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "parent record not found")
		return
	}

	a, err := s.attachments.Create(ctx, attachments.CreateInput{
		TenantID:    actor.TenantID,
		StationID:   stationID,
		EntityType:  entityType,
		EntityID:    entityID,
		Filename:    sanitizeFilename(header.Filename),
		ContentType: contentType,
		Data:        data,
		UploadedBy:  actor.UserID,
	})
	switch {
	case errors.Is(err, attachments.ErrContentType):
		writeError(w, http.StatusBadRequest, "file must be a PDF, PNG, or JPEG")
		return
	case errors.Is(err, attachments.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "file must be 5 MiB or smaller")
		return
	case err != nil:
		s.logger.Error("create attachment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.auditAttachment(r, actor, "attachment.added", "AttachmentAdded", a, nil, toAttachmentDTO(a))
	writeJSON(w, http.StatusCreated, toAttachmentDTO(a))
}

// handleListAttachments lists the live attachments for an entity. Permission:
// attachment.read.
func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	entityType := chi.URLParam(r, "entityType")
	entityID, err := uuid.Parse(chi.URLParam(r, "entityID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "entity id must be a valid id")
		return
	}
	rows, err := s.attachments.ListByEntity(r.Context(), actor.TenantID, entityType, entityID)
	if err != nil {
		s.logger.Error("list attachments", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]attachmentDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toAttachmentDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// handleDownloadAttachment streams one attachment's bytes with its content
// type. Permission: attachment.read.
func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "attachment id must be a valid id")
		return
	}
	data, contentType, filename, err := s.attachments.Stream(r.Context(), actor.TenantID, id)
	if errors.Is(err, attachments.ErrNotFound) {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		s.logger.Error("download attachment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// The content type is from our fixed allowlist (PDF/PNG/JPEG), set
	// explicitly here. nosniff stops the browser from re-interpreting the body,
	// and attachment disposition forces a download rather than rendering an
	// uploaded PDF inline — together these neutralise the stored-XSS vector of
	// serving user-supplied bytes back from the same origin.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(filename)+"\"")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) //nolint:gosec // G705: bytes are served with an explicit allowlisted content type + nosniff + attachment disposition
}

// handleDeleteAttachment soft-deletes an attachment. Permission:
// attachment.manage. Refuses (409) when the parent record is posted/locked
// where determinable. Audited as attachment.removed.
func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "attachment id must be a valid id")
		return
	}

	ctx := r.Context()
	meta, err := s.attachments.GetMeta(ctx, actor.TenantID, id)
	if errors.Is(err, attachments.ErrNotFound) {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		s.logger.Error("delete attachment: load", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Refuse to detach evidence from a parent that is posted/locked, where the
	// parent's state is determinable.
	locked, lerr := s.attachmentParentLocked(ctx, actor.TenantID, meta.EntityType, meta.EntityID)
	if lerr != nil {
		s.logger.Error("delete attachment: parent state", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if locked {
		writeError(w, http.StatusConflict, "the parent record is posted or locked; its attachments cannot be removed")
		return
	}

	deleted, err := s.attachments.SoftDelete(ctx, actor.TenantID, id)
	if errors.Is(err, attachments.ErrNotFound) {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		s.logger.Error("delete attachment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.auditAttachment(r, actor, "attachment.removed", "AttachmentRemoved", deleted, toAttachmentDTO(deleted), nil)
	w.WriteHeader(http.StatusNoContent)
}

// resolveAttachmentParent confirms the parent row exists in the tenant and
// returns its station context (nil when the parent is tenant-wide). ok is false
// when the parent does not exist. An unknown entity_type is treated as having
// no resolvable parent context (ok=true, station nil) — but uploads only allow
// the supportedAttachmentEntities set, so this only matters defensively.
func (s *Server) resolveAttachmentParent(
	ctx context.Context, tenantID uuid.UUID, entityType string, entityID uuid.UUID,
) (stationID *uuid.UUID, ok bool, err error) {
	switch entityType {
	case "expense":
		if s.expenses == nil {
			return nil, true, nil
		}
		exp, gerr := s.expenses.GetExpense(ctx, tenantID, entityID)
		if errors.Is(gerr, expenses.ErrNotFound) {
			return nil, false, nil
		}
		if gerr != nil {
			return nil, false, gerr
		}
		return exp.StationID, true, nil
	default:
		return nil, true, nil
	}
}

// attachmentParentLocked reports whether the parent record is in a
// posted/locked state that should block detaching its evidence. Unknown or
// stateless parents return false (deletion allowed).
func (s *Server) attachmentParentLocked(
	ctx context.Context, tenantID uuid.UUID, entityType string, entityID uuid.UUID,
) (bool, error) {
	switch entityType {
	case "expense":
		if s.expenses == nil {
			return false, nil
		}
		exp, err := s.expenses.GetExpense(ctx, tenantID, entityID)
		if errors.Is(err, expenses.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return exp.Status == "posted", nil
	default:
		return false, nil
	}
}

// sanitizeFilename strips path separators and control characters so a hostile
// filename can't influence the Content-Disposition header or any later
// filesystem use. It never returns empty.
func sanitizeFilename(name string) string {
	out := make([]rune, 0, len(name))
	for _, c := range name {
		switch {
		case c == '/' || c == '\\' || c == '"' || c < 0x20:
			out = append(out, '_')
		default:
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "attachment"
	}
	return string(out)
}

// auditAttachment writes a tenant audit + outbox event in its own transaction.
// Best-effort: a failure is logged but does not fail the request (the
// attachment write already committed via the repo).
func (s *Server) auditAttachment(
	r *http.Request, actor identity.Actor,
	action, eventType string, a *attachments.Attachment, prev, next any,
) {
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		s.logger.Error("audit attachment: begin", "error", err, "action", action)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:      actor.TenantID,
		ActorID:       actor.UserID,
		Action:        action,
		EventType:     eventType,
		EntityType:    "attachment",
		EntityID:      a.ID.String(),
		PreviousValue: prev,
		NewValue:      next,
		IP:            clientIP(r),
		UserAgent:     r.UserAgent(),
		RequestID:     chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("audit attachment: write", "error", err, "action", action)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("audit attachment: commit", "error", err, "action", action)
	}
}
