package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	maxImageSize = 5 * 1024 * 1024 // 5 MB
	uploadDir    = "backend/uploads/maintenance"
)

// validMaintenanceStatusTransition returns true if the transition from old to
// new status is permitted. Landlords, agents, and super_admins may change
// status; tenants may not change status directly.
func validMaintenanceStatusTransition(old, new string) bool {
	valid := map[string]map[string]bool{
		"submitted":    {"under_review": true, "rejected": true},
		"under_review": {"assigned": true, "in_progress": true, "rejected": true},
		"assigned":     {"in_progress": true, "rejected": true},
		"in_progress":  {"resolved": true, "rejected": true},
		"resolved":     {},
		"rejected":     {},
	}
	allowed, ok := valid[old]
	if !ok {
		return false
	}
	return allowed[new]
}

// validMaintenanceCategory returns true if the category is one of the allowed
// values matching the migration CHECK constraint.
func validMaintenanceCategory(c string) bool {
	switch c {
	case "plumbing", "electrical", "security", "general_repair", "other":
		return true
	}
	return false
}

// maintenanceRequestAccess returns true if the principal can access the given
// maintenance request. Tenants can access their own requests; landlords and
// super_admins can access any request in their account; agents can access
// requests for properties they are assigned to.
func (s *Server) maintenanceRequestAccess(r *http.Request, requestID, accountID string) (bool, error) {
	p := principal(r)
	if p.Role == "landlord" || p.Role == "super_admin" {
		return true, nil
	}
	if p.Role == "agent" {
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT property_id FROM maintenance_requests WHERE id=$1 AND account_id=$2`, requestID, accountID).Scan(&propertyID)
		if e != nil {
			return false, e
		}
		return s.checkPropertyAccess(r, propertyID)
	}
	// tenant: must be the tenant who owns the request
	if p.TenantID == "" {
		return false, nil
	}
	var exists bool
	e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM maintenance_requests WHERE id=$1 AND tenant_id=$2 AND account_id=$3)`, requestID, p.TenantID, accountID).Scan(&exists)
	return exists, e
}

// generateMaintenanceReference produces a human-readable reference like
// MR-2026-0001 using the maintenance_ref_seq sequence.
func (s *Server) generateMaintenanceReference(r *http.Request) (string, error) {
	var n int64
	e := s.DB.QueryRow(r.Context(), `SELECT nextval('maintenance_ref_seq')`).Scan(&n)
	if e != nil {
		return "", e
	}
	year := time.Now().Year()
	return fmt.Sprintf("MR-%d-%04d", year, n), nil
}

// saveUploadedImage stores an uploaded image under backend/uploads/maintenance
// with a unique filename and returns the relative path. It validates the MIME
// type and size. The caller is responsible for closing the file.
func saveUploadedImage(file io.Reader, header *multipart.FileHeader) (string, error) {
	// Validate size first.
	if header.Size > maxImageSize {
		return "", fmt.Errorf("image too large: %d bytes (max %d)", header.Size, maxImageSize)
	}

	// Read a prefix to detect actual content type (do not trust client header).
	buf := make([]byte, 512)
	n, _ := io.ReadFull(file, buf)
	buf = buf[:n]
	detected := http.DetectContentType(buf)
	switch detected {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return "", fmt.Errorf("unsupported image type: %s", detected)
	}

	// Reconstruct reader with the already-read prefix.
	reader := io.MultiReader(bytes.NewReader(buf), file)

	// Derive extension from detected content, never from filename.
	var ext string
	switch detected {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	}

	// Ensure upload directory exists.
	if e := os.MkdirAll(uploadDir, 0o750); e != nil {
		return "", e
	}

	name := fmt.Sprintf("%x%s", time.Now().UnixNano(), ext)
	path := filepath.Join(uploadDir, name)

	dst, e := os.Create(path)
	if e != nil {
		return "", e
	}
	defer dst.Close()

	if _, e = io.Copy(dst, reader); e != nil {
		os.Remove(path)
		return "", e
	}

	return path, nil
}

// serveMaintenanceImage returns the stored image file with appropriate
// content type. Authorization is checked by the caller before invoking this.
func serveMaintenanceImage(w http.ResponseWriter, path string) {
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, nil, path)
}

// ============================================================================
// Handlers
// ============================================================================

func (s *Server) createMaintenanceRequest(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "tenant" {
		writeErr(w, 403, "forbidden", "only tenants can submit maintenance requests")
		return
	}
	if p.TenantID == "" {
		writeErr(w, 403, "forbidden", "tenant not associated with principal")
		return
	}

	// Parse multipart form (supports optional image upload).
	r.Body = http.MaxBytesReader(w, r.Body, maxImageSize+1024)
	if e := r.ParseMultipartForm(maxImageSize); e != nil {
		writeErr(w, 400, "invalid_request", "could not parse request")
		return
	}

	category := strings.TrimSpace(r.FormValue("category"))
	description := strings.TrimSpace(r.FormValue("description"))
	leaseID := strings.TrimSpace(r.FormValue("lease_id"))
	unitID := strings.TrimSpace(r.FormValue("unit_id"))
	propertyID := strings.TrimSpace(r.FormValue("property_id"))

	if !validMaintenanceCategory(category) {
		writeErr(w, 400, "invalid_request", "category must be one of: plumbing, electrical, security, general_repair, other")
		return
	}
	if description == "" {
		writeErr(w, 400, "invalid_request", "description is required")
		return
	}
	if propertyID == "" || !validID(propertyID) {
		writeErr(w, 400, "invalid_request", "valid property_id is required")
		return
	}

	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}

	// Verify the tenant has an active lease on this property (or at least
	// belongs to the property's account).
	var tenantExists bool
	e = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenants WHERE id=$1 AND account_id=$2)`, p.TenantID, a).Scan(&tenantExists)
	if e != nil || !tenantExists {
		writeErr(w, 403, "forbidden", "tenant not in this account")
		return
	}

	// Verify the property belongs to the account.
	var propExists bool
	e = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM properties WHERE id=$1 AND account_id=$2)`, propertyID, a).Scan(&propExists)
	if e != nil || !propExists {
		writeErr(w, 404, "not_found", "property not found")
		return
	}

	// Verify the tenant has an active lease on this property.
	var hasLease bool
	e = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM leases l JOIN units u ON u.id=l.unit_id WHERE l.tenant_id=$1 AND l.account_id=$2 AND u.property_id=$3 AND l.status='active')`, p.TenantID, a, propertyID).Scan(&hasLease)
	if e != nil || !hasLease {
		writeErr(w, 403, "forbidden", "tenant has no active lease on this property")
		return
	}

	// If a unit_id is provided, verify it belongs to the property and account.
	if unitID != "" && !validID(unitID) {
		writeErr(w, 400, "invalid_request", "invalid unit_id")
		return
	}
	if unitID != "" {
		var unitExists bool
		e = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM units WHERE id=$1 AND property_id=$2 AND account_id=$3)`, unitID, propertyID, a).Scan(&unitExists)
		if e != nil || !unitExists {
			writeErr(w, 404, "not_found", "unit not found")
			return
		}
	}

	// If a lease_id is provided, verify it belongs to the tenant and account.
	if leaseID != "" && !validID(leaseID) {
		writeErr(w, 400, "invalid_request", "invalid lease_id")
		return
	}
	if leaseID != "" {
		var leaseExists bool
		e = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM leases WHERE id=$1 AND tenant_id=$2 AND account_id=$3)`, leaseID, p.TenantID, a).Scan(&leaseExists)
		if e != nil || !leaseExists {
			writeErr(w, 404, "not_found", "lease not found")
			return
		}
	}

	reference, e := s.generateMaintenanceReference(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not generate reference")
		return
	}

	// Handle optional image upload.
	var imagePath string
	if f, fh, e := r.FormFile("image"); e == nil {
		defer f.Close()
		imagePath, e = saveUploadedImage(f, fh)
		if e != nil {
			writeErr(w, 400, "invalid_upload", e.Error())
			return
		}
	} else if e != http.ErrMissingFile {
		writeErr(w, 400, "invalid_upload", "could not process image")
		return
	}

	var id string
	e = s.DB.QueryRow(r.Context(), `
		INSERT INTO maintenance_requests(account_id, tenant_id, lease_id, unit_id, property_id, reference, category, description, image_upload_path, status)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'submitted')
		RETURNING id`,
		a, p.TenantID, leaseID, unitID, propertyID, reference, category, description, imagePath,
	).Scan(&id)
	if e != nil {
		writeErr(w, 500, "database_error", "could not create maintenance request")
		return
	}

	writeData(w, 201, map[string]any{
		"id":          id,
		"reference":   reference,
		"status":      "submitted",
		"category":    category,
		"description": description,
		"image_url":   imagePath != "",
		"created_at":  time.Now(),
	})
}

func (s *Server) listMaintenanceRequests(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}

	switch p.Role {
	case "tenant":
		if p.TenantID == "" {
			writeData(w, 200, []map[string]any{})
			return
		}
		rows(w, r, `
			SELECT mr.id, mr.reference, mr.category, mr.description, mr.status, mr.priority,
				   mr.image_upload_path, mr.assigned_to, mr.resolved_at, mr.resolution_notes,
				   mr.created_at, mr.updated_at,
				   p.name AS property_name, u.code AS unit_code
			FROM maintenance_requests mr
			LEFT JOIN properties p ON p.id=mr.property_id
			LEFT JOIN units u ON u.id=mr.unit_id
			WHERE mr.tenant_id=$1 AND mr.account_id=$2
			ORDER BY mr.created_at DESC`, p.TenantID, a)
		return

	case "agent":
		ids, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load requests")
			return
		}
		if len(ids) == 0 {
			writeData(w, 200, []map[string]any{})
			return
		}
		args := []any{a}
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			args = append(args, id)
			placeholders[i] = fmt.Sprintf("$%d", i+2)
		}
		query := fmt.Sprintf(`
			SELECT mr.id, mr.reference, mr.category, mr.description, mr.status, mr.priority,
				   mr.image_upload_path, mr.assigned_to, mr.resolved_at, mr.resolution_notes,
				   mr.created_at, mr.updated_at,
				   p.name AS property_name, u.code AS unit_code, t.full_name AS tenant_name
			FROM maintenance_requests mr
			JOIN properties p ON p.id=mr.property_id AND p.account_id=mr.account_id
			LEFT JOIN units u ON u.id=mr.unit_id
			LEFT JOIN tenants t ON t.id=mr.tenant_id AND t.account_id=mr.account_id
			WHERE mr.account_id=$1 AND p.id IN (%s)
			ORDER BY mr.created_at DESC`, strings.Join(placeholders, ","))
		rows(w, r, query, args...)
		return

	case "super_admin":
		aid, e := s.effectiveAccountID(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not resolve account")
			return
		}
		if aid == "" {
			writeData(w, 200, []map[string]any{})
			return
		}
		rows(w, r, `
			SELECT mr.id, mr.reference, mr.category, mr.description, mr.status, mr.priority,
				   mr.image_upload_path, mr.assigned_to, mr.resolved_at, mr.resolution_notes,
				   mr.created_at, mr.updated_at,
				   p.name AS property_name, u.code AS unit_code, t.full_name AS tenant_name
			FROM maintenance_requests mr
			LEFT JOIN properties p ON p.id=mr.property_id
			LEFT JOIN units u ON u.id=mr.unit_id
			LEFT JOIN tenants t ON t.id=mr.tenant_id AND t.account_id=mr.account_id
			WHERE mr.account_id=$1
			ORDER BY mr.created_at DESC`, aid)
		return

	default: // landlord
		rows(w, r, `
			SELECT mr.id, mr.reference, mr.category, mr.description, mr.status, mr.priority,
				   mr.image_upload_path, mr.assigned_to, mr.resolved_at, mr.resolution_notes,
				   mr.created_at, mr.updated_at,
				   p.name AS property_name, u.code AS unit_code, t.full_name AS tenant_name
			FROM maintenance_requests mr
			LEFT JOIN properties p ON p.id=mr.property_id
			LEFT JOIN units u ON u.id=mr.unit_id
			LEFT JOIN tenants t ON t.id=mr.tenant_id AND t.account_id=mr.account_id
			WHERE mr.account_id=$1
			ORDER BY mr.created_at DESC`, a)
		return
	}
}

func (s *Server) getMaintenanceRequest(w http.ResponseWriter, r *http.Request) {
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	requestID := uuidParam(r)
	if !validID(requestID) {
		writeErr(w, 400, "invalid_request", "invalid request id")
		return
	}

	ok, e := s.maintenanceRequestAccess(r, requestID, a)
	if e != nil || !ok {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	var id, reference, category, description, status, priority string
	var imagePath, assignedTo, resolvedAt, resolutionNotes string
	var createdAt, updatedAt time.Time
	var propertyName, unitCode, tenantName *string
	e = s.DB.QueryRow(r.Context(), `
		SELECT mr.id, mr.reference, mr.category, mr.description, mr.status, mr.priority,
			   mr.image_upload_path, mr.assigned_to, mr.resolved_at, mr.resolution_notes,
			   mr.created_at, mr.updated_at,
			   p.name, u.code, t.full_name
		FROM maintenance_requests mr
		LEFT JOIN properties p ON p.id=mr.property_id
		LEFT JOIN units u ON u.id=mr.unit_id
		LEFT JOIN tenants t ON t.id=mr.tenant_id AND t.account_id=mr.account_id
		WHERE mr.id=$1 AND mr.account_id=$2`, requestID, a).
		Scan(&id, &reference, &category, &description, &status, &priority,
			&imagePath, &assignedTo, &resolvedAt, &resolutionNotes,
			&createdAt, &updatedAt, &propertyName, &unitCode, &tenantName)
	if e != nil {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	writeData(w, 200, map[string]any{
		"id":               id,
		"reference":        reference,
		"category":         category,
		"description":      description,
		"status":           status,
		"priority":         priority,
		"image_url":        imagePath != "",
		"assigned_to":      assignedTo,
		"resolved_at":      resolvedAt,
		"resolution_notes": resolutionNotes,
		"created_at":       createdAt,
		"updated_at":       updatedAt,
		"property_name":    propertyName,
		"unit_code":        unitCode,
		"tenant_name":      tenantName,
	})
}

func (s *Server) patchMaintenanceRequest(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	requestID := uuidParam(r)
	if !validID(requestID) {
		writeErr(w, 400, "invalid_request", "invalid request id")
		return
	}

	ok, e := s.maintenanceRequestAccess(r, requestID, a)
	if e != nil || !ok {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	var in struct {
		Status          *string `json:"status"`
		AssignedTo      *string `json:"assigned_to"`
		ResolutionNotes *string `json:"resolution_notes"`
		Priority        *string `json:"priority"`
		Description     *string `json:"description"`
	}
	if decode(r, &in) != nil {
		writeErr(w, 400, "invalid_request", "invalid request fields")
		return
	}

	// Enforce account suspension for non-super-admins.
	if p.Role != "super_admin" && a != "" {
		var status string
		if e2 := s.DB.QueryRow(r.Context(), `SELECT subscription_status FROM accounts WHERE id=$1`, a).Scan(&status); e2 == nil && status == "suspended" {
			writeErr(w, 403, "forbidden", "account is suspended")
			return
		}
	}

	// Fetch current status for transition validation.
	var currentStatus string
	e = s.DB.QueryRow(r.Context(), `SELECT status FROM maintenance_requests WHERE id=$1 AND account_id=$2`, requestID, a).Scan(&currentStatus)
	if e != nil {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	// Tenants can only update description, and only on submitted requests.
	if p.Role == "tenant" {
		if in.Status != nil || in.AssignedTo != nil || in.ResolutionNotes != nil || in.Priority != nil {
			writeErr(w, 403, "forbidden", "tenants cannot change status, assignment, priority, or resolution notes")
			return
		}
		if in.Description != nil {
			if currentStatus != "submitted" {
				writeErr(w, 409, "conflict", "description can only be updated on submitted requests")
				return
			}
			desc := strings.TrimSpace(*in.Description)
			if desc == "" {
				writeErr(w, 400, "invalid_request", "description cannot be empty")
				return
			}
			_, e = s.DB.Exec(r.Context(), `UPDATE maintenance_requests SET description=$1, updated_at=now() WHERE id=$2 AND account_id=$3`, desc, requestID, a)
			if e != nil {
				writeErr(w, 500, "database_error", "could not update request")
				return
			}
			writeData(w, 200, map[string]string{"id": requestID, "updated": "true"})
			return
		}
		writeData(w, 200, map[string]string{"id": requestID, "updated": "true"})
		return
	}

	// Landlord, agent, super_admin: validate status transition.
	if in.Status != nil && *in.Status != currentStatus {
		if !validMaintenanceStatusTransition(currentStatus, *in.Status) {
			writeErr(w, 409, "conflict", fmt.Sprintf("cannot transition from %s to %s", currentStatus, *in.Status))
			return
		}
	}

	// Validate priority if provided.
	if in.Priority != nil {
		switch *in.Priority {
		case "low", "medium", "high", "urgent":
		default:
			writeErr(w, 400, "invalid_request", "priority must be one of: low, medium, high, urgent")
			return
		}
	}

	// Validate assigned_to if provided (must be a user in the account).
	if in.AssignedTo != nil && *in.AssignedTo != "" {
		var exists bool
		e = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND account_id=$2 AND role='agent')`, *in.AssignedTo, a).Scan(&exists)
		if e != nil || !exists {
			writeErr(w, 400, "invalid_request", "assigned_to must be an agent in this account")
			return
		}
	}

	// Build dynamic update.
	args := []any{requestID, a}
	setClauses := []string{"updated_at=now()"}
	argIdx := 3
	if in.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status=$%d", argIdx))
		args = append(args, *in.Status)
		argIdx++
		if *in.Status == "resolved" {
			setClauses = append(setClauses, fmt.Sprintf("resolved_at=now()"))
		}
	}
	if in.AssignedTo != nil {
		setClauses = append(setClauses, fmt.Sprintf("assigned_to=$%d", argIdx))
		args = append(args, *in.AssignedTo)
		argIdx++
	}
	if in.ResolutionNotes != nil {
		setClauses = append(setClauses, fmt.Sprintf("resolution_notes=$%d", argIdx))
		args = append(args, *in.ResolutionNotes)
		argIdx++
	}
	if in.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority=$%d", argIdx))
		args = append(args, *in.Priority)
		argIdx++
	}
	if in.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description=$%d", argIdx))
		args = append(args, *in.Description)
		argIdx++
	}

	query := fmt.Sprintf(`UPDATE maintenance_requests SET %s WHERE id=$1 AND account_id=$2`, strings.Join(setClauses, ","))
	tag, e := s.DB.Exec(r.Context(), query, args...)
	if e != nil || tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "maintenance request not found or could not update")
		return
	}
	writeData(w, 200, map[string]string{"id": requestID, "updated": "true"})
}

func (s *Server) createMaintenanceComment(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	requestID := uuidParam(r)
	if !validID(requestID) {
		writeErr(w, 400, "invalid_request", "invalid request id")
		return
	}

	ok, e := s.maintenanceRequestAccess(r, requestID, a)
	if e != nil || !ok {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	if p.Role != "super_admin" && a != "" {
		var status string
		if e2 := s.DB.QueryRow(r.Context(), `SELECT subscription_status FROM accounts WHERE id=$1`, a).Scan(&status); e2 == nil && status == "suspended" {
			writeErr(w, 403, "forbidden", "account is suspended")
			return
		}
	}

	var in struct {
		Body string `json:"body"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Body) == "" {
		writeErr(w, 400, "invalid_request", "comment body is required")
		return
	}

	var id string
	e = s.DB.QueryRow(r.Context(), `
		INSERT INTO maintenance_comments(request_id, author_id, body)
		VALUES($1,$2,$3)
		RETURNING id`, requestID, p.ID, in.Body).Scan(&id)
	if e != nil {
		writeErr(w, 500, "database_error", "could not create comment")
		return
	}

	writeData(w, 201, map[string]any{
		"id":          id,
		"request_id":  requestID,
		"author_id":   p.ID,
		"author_name": p.Name,
		"author_role": p.Role,
		"body":        in.Body,
		"created_at":  time.Now(),
	})
}

func (s *Server) listMaintenanceComments(w http.ResponseWriter, r *http.Request) {
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	requestID := uuidParam(r)
	if !validID(requestID) {
		writeErr(w, 400, "invalid_request", "invalid request id")
		return
	}

	ok, e := s.maintenanceRequestAccess(r, requestID, a)
	if e != nil || !ok {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	rows(w, r, `
		SELECT mc.id, mc.author_id, u.full_name AS author_name, u.role AS author_role,
			   mc.body, mc.created_at
		FROM maintenance_comments mc
		JOIN users u ON u.id=mc.author_id
		WHERE mc.request_id=$1
		ORDER BY mc.created_at ASC`, requestID)
}

func (s *Server) uploadMaintenanceImage(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	if p.Role != "super_admin" && a != "" {
		var status string
		if e2 := s.DB.QueryRow(r.Context(), `SELECT subscription_status FROM accounts WHERE id=$1`, a).Scan(&status); e2 == nil && status == "suspended" {
			writeErr(w, 403, "forbidden", "account is suspended")
			return
		}
	}
	requestID := uuidParam(r)
	if !validID(requestID) {
		writeErr(w, 400, "invalid_request", "invalid request id")
		return
	}

	ok, e := s.maintenanceRequestAccess(r, requestID, a)
	if e != nil || !ok {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	// Only the requesting tenant or a manager can upload an image.
	if p.Role != "tenant" && p.Role != "landlord" && p.Role != "super_admin" && p.Role != "agent" {
		writeErr(w, 403, "forbidden", "not authorized to upload images")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImageSize+1024)
	if e := r.ParseMultipartForm(maxImageSize); e != nil {
		writeErr(w, 400, "invalid_request", "could not parse upload")
		return
	}

	f, fh, e := r.FormFile("image")
	if e != nil {
		writeErr(w, 400, "invalid_request", "image file is required")
		return
	}
	defer f.Close()

	imagePath, e := saveUploadedImage(f, fh)
	if e != nil {
		writeErr(w, 400, "invalid_upload", e.Error())
		return
	}

	_, e = s.DB.Exec(r.Context(), `UPDATE maintenance_requests SET image_upload_path=$1, updated_at=now() WHERE id=$2 AND account_id=$3`, imagePath, requestID, a)
	if e != nil {
		os.Remove(imagePath)
		writeErr(w, 500, "database_error", "could not save image reference")
		return
	}

	writeData(w, 200, map[string]any{
		"id":          requestID,
		"image_url":   true,
		"upload_path": imagePath,
	})
}

func (s *Server) getMaintenanceImage(w http.ResponseWriter, r *http.Request) {
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	requestID := uuidParam(r)
	if !validID(requestID) {
		writeErr(w, 400, "invalid_request", "invalid request id")
		return
	}

	ok, e := s.maintenanceRequestAccess(r, requestID, a)
	if e != nil || !ok {
		writeErr(w, 404, "not_found", "maintenance request not found")
		return
	}

	var imagePath string
	e = s.DB.QueryRow(r.Context(), `SELECT image_upload_path FROM maintenance_requests WHERE id=$1 AND account_id=$2`, requestID, a).Scan(&imagePath)
	if e != nil || imagePath == "" {
		writeErr(w, 404, "not_found", "no image attached to this request")
		return
	}

	// Verify the file exists and is within the upload directory.
	absPath, e := filepath.Abs(imagePath)
	if e != nil {
		writeErr(w, 500, "internal", "could not resolve image path")
		return
	}
	absUploadDir, e := filepath.Abs(uploadDir)
	if e != nil {
		writeErr(w, 500, "internal", "could not resolve upload directory")
		return
	}
	if !strings.HasPrefix(absPath, absUploadDir+string(os.PathSeparator)) && absPath != absUploadDir {
		writeErr(w, 403, "forbidden", "invalid image path")
		return
	}
	if _, e := os.Stat(absPath); e != nil {
		writeErr(w, 404, "not_found", "image file not found")
		return
	}

	ct := mime.TypeByExtension(filepath.Ext(absPath))
	if ct == "" || !strings.HasPrefix(ct, "image/") {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, absPath)
}

// registerMaintenanceRoutes mounts all maintenance endpoints on the given router.
func (s *Server) registerMaintenanceRoutes(r chi.Router) {
	r.Route("/maintenance", func(r chi.Router) {
		r.Post("/requests", s.createMaintenanceRequest)
		r.Get("/requests", s.listMaintenanceRequests)
		r.Get("/requests/{id}", s.getMaintenanceRequest)
		r.Patch("/requests/{id}", s.patchMaintenanceRequest)
		r.Post("/requests/{id}/comments", s.createMaintenanceComment)
		r.Get("/requests/{id}/comments", s.listMaintenanceComments)
		r.Post("/requests/{id}/image", s.uploadMaintenanceImage)
		r.Get("/requests/{id}/image", s.getMaintenanceImage)
	})
}
