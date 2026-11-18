package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) listProperties(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	if p.Role == "agent" {
		ids, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load properties")
			return
		}
		if len(ids) == 0 {
			writeData(w, 200, []map[string]any{})
			return
		}
		// Build IN clause for assigned property IDs
		args := []any{a}
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			args = append(args, id)
			placeholders[i] = fmt.Sprintf("$%d", i+2)
		}
		query := fmt.Sprintf(`SELECT id,name,address,description,created_at,updated_at FROM properties WHERE account_id=$1 AND id IN (%s) ORDER BY created_at DESC`, strings.Join(placeholders, ","))
		rows(w, r, query, args...)
		return
	}
	rows(w, r, `SELECT id,name,address,description,created_at,updated_at FROM properties WHERE account_id=$1 ORDER BY created_at DESC`, a)
}
func (s *Server) createProperty(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	if p.Role == "agent" {
		writeErr(w, 403, "forbidden", "agents cannot create properties")
		return
	}
	var in struct {
		Name        string `json:"name"`
		Address     string `json:"address"`
		Description string `json:"description"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, 400, "invalid_request", "property name is required")
		return
	}
	a, _ := s.account(r)
	var id string
	e := s.DB.QueryRow(r.Context(), `INSERT INTO properties(account_id,name,address,description) VALUES($1,$2,$3,$4) RETURNING id`, a, strings.TrimSpace(in.Name), in.Address, in.Description).Scan(&id)
	if e != nil {
		writeErr(w, 500, "database_error", "could not create property")
		return
	}
	writeData(w, 201, map[string]any{"id": id, "name": in.Name, "address": in.Address, "description": in.Description})
}
func (s *Server) getProperty(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	propertyID := uuidParam(r)
	if p.Role == "agent" {
		ok, e := s.checkPropertyAccess(r, propertyID)
		if e != nil || !ok {
			writeErr(w, 404, "not_found", "property not found")
			return
		}
	}
	var id, name, address, description string
	var created, updated time.Time
	e = s.DB.QueryRow(r.Context(), `SELECT id,name,address,description,created_at,updated_at FROM properties WHERE account_id=$1 AND id=$2`, a, propertyID).Scan(&id, &name, &address, &description, &created, &updated)
	if e != nil {
		writeErr(w, 404, "not_found", "property not found")
		return
	}
	writeData(w, 200, map[string]any{"id": id, "name": name, "address": address, "description": description, "created_at": created, "updated_at": updated})
}
func (s *Server) patchProperty(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	propertyID := uuidParam(r)
	if p.Role == "agent" {
		ok, e := s.checkPropertyAccess(r, propertyID)
		if e != nil || !ok {
			writeErr(w, 404, "not_found", "property not found")
			return
		}
	}
	var in struct {
		Name        *string `json:"name"`
		Address     *string `json:"address"`
		Description *string `json:"description"`
	}
	if decode(r, &in) != nil {
		writeErr(w, 400, "invalid_request", "invalid property fields")
		return
	}
	tag, e := s.DB.Exec(r.Context(), `UPDATE properties SET name=coalesce($3,name),address=coalesce($4,address),description=coalesce($5,description),updated_at=now() WHERE account_id=$1 AND id=$2`, a, propertyID, in.Name, in.Address, in.Description)
	if e != nil || tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "property not found")
		return
	}
	s.getProperty(w, r)
}
func (s *Server) deleteProperty(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	propertyID := uuidParam(r)
	if p.Role == "agent" {
		ok, e := s.checkPropertyAccess(r, propertyID)
		if e != nil || !ok {
			writeErr(w, 404, "not_found", "property not found")
			return
		}
	}
	tag, e := s.DB.Exec(r.Context(), `DELETE FROM properties WHERE account_id=$1 AND id=$2`, a, propertyID)
	if e != nil {
		writeErr(w, 409, "in_use", "property or its units are still referenced by a lease")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "property not found")
		return
	}
	writeData(w, 200, map[string]string{"deleted": propertyID})
}
func (s *Server) listUnits(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	if p.Role == "agent" {
		ids, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load units")
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
		query := fmt.Sprintf(`SELECT u.id,u.property_id,p.name AS property_name,u.code,u.bedrooms,u.rent_amount,u.status,u.created_at,u.updated_at FROM units u JOIN properties p ON p.id=u.property_id AND p.account_id=u.account_id WHERE u.account_id=$1 AND u.property_id IN (%s) ORDER BY p.name,u.code`, strings.Join(placeholders, ","))
		rows(w, r, query, args...)
		return
	}
	rows(w, r, `SELECT u.id,u.property_id,p.name AS property_name,u.code,u.bedrooms,u.rent_amount,u.status,u.created_at,u.updated_at FROM units u JOIN properties p ON p.id=u.property_id AND p.account_id=u.account_id WHERE u.account_id=$1 ORDER BY p.name,u.code`, a)
}
func (s *Server) createUnit(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	var in struct {
		PropertyID string  `json:"property_id"`
		Code       string  `json:"code"`
		Bedrooms   int     `json:"bedrooms"`
		Rent       float64 `json:"rent_amount"`
		Status     string  `json:"status"`
	}
	if decode(r, &in) != nil || in.PropertyID == "" || strings.TrimSpace(in.Code) == "" || len(strings.TrimSpace(in.Code)) > 12 || in.Rent < 0 {
		writeErr(w, 400, "invalid_request", "property_id, code, and non-negative rent_amount are required")
		return
	}
	if in.Status == "" {
		in.Status = "vacant"
	}
	if in.Status != "vacant" && in.Status != "maintenance" {
		writeErr(w, 400, "invalid_status", "a new unit must be vacant or in maintenance until assigned to a lease")
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	p := principal(r)
	if p.Role == "agent" {
		ok, e := s.checkPropertyAccess(r, in.PropertyID)
		if e != nil || !ok {
			writeErr(w, 403, "forbidden", "property not assigned to this agent")
			return
		}
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database_error", "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	var unitLimit *int
	if e = tx.QueryRow(r.Context(), `SELECT p.unit_limit FROM accounts a JOIN plans p ON p.id=a.plan_id WHERE a.id=$1 FOR UPDATE OF a`, a).Scan(&unitLimit); e != nil {
		writeErr(w, 404, "not_found", "account or plan not found")
		return
	}
	if unitLimit != nil {
		var accountID pgtype.UUID
		if e = accountID.Scan(a); e != nil {
			writeErr(w, 500, "database_error", "could not validate account plan")
			return
		}
		count, countErr := s.Queries.WithTx(tx).CountUnitsByAccount(r.Context(), accountID)
		if countErr != nil {
			writeErr(w, 500, "database_error", "could not validate account unit count")
			return
		}
		if count >= int64(*unitLimit) {
			writeErr(w, 402, "plan_limit", "current plan unit limit reached")
			return
		}
	}
	var id string
	e = tx.QueryRow(r.Context(), `INSERT INTO units(account_id,property_id,code,bedrooms,rent_amount,status) SELECT $1,p.id,$3,$4,$5,$6 FROM properties p WHERE p.id=$2 AND p.account_id=$1 RETURNING id`, a, in.PropertyID, in.Code, in.Bedrooms, in.Rent, in.Status).Scan(&id)
	if e != nil {
		writeErr(w, 400, "invalid_unit", "property is not in your account or unit code already exists")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeErr(w, 500, "database_error", "could not save unit")
		return
	}
	writeData(w, 201, map[string]any{"id": id, "property_id": in.PropertyID, "code": in.Code, "bedrooms": in.Bedrooms, "rent_amount": in.Rent, "status": in.Status})
}
func (s *Server) patchUnit(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	unitID := uuidParam(r)
	if p.Role == "agent" {
		// Resolve the unit's property and check agent assignment
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT property_id FROM units WHERE account_id=$1 AND id=$2`, a, unitID).Scan(&propertyID)
		if e != nil {
			writeErr(w, 404, "not_found", "unit not found")
			return
		}
		ok, e := s.checkPropertyAccess(r, propertyID)
		if e != nil || !ok {
			writeErr(w, 403, "forbidden", "unit not assigned to this agent")
			return
		}
	}
	var in struct {
		Code     string   `json:"code"`
		Bedrooms *int     `json:"bedrooms"`
		Rent     *float64 `json:"rent_amount"`
		Status   *string  `json:"status"`
	}
	if decode(r, &in) != nil {
		writeErr(w, 400, "invalid_request", "invalid unit fields")
		return
	}
	if in.Status != nil {
		var activeLease bool
		if e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM leases WHERE account_id=$1 AND unit_id=$2 AND status='active')`, a, uuidParam(r)).Scan(&activeLease); e != nil {
			writeErr(w, 500, "database_error", "could not validate unit occupancy")
			return
		}
		if (activeLease && *in.Status != "occupied") || (!activeLease && *in.Status == "occupied") {
			writeErr(w, 409, "lease_status_conflict", "unit status must match whether it has an active lease")
			return
		}
	}
	tag, e := s.DB.Exec(r.Context(), `UPDATE units SET code=coalesce($3,code),bedrooms=coalesce($4,bedrooms),rent_amount=coalesce($5,rent_amount),status=coalesce($6,status),updated_at=now() WHERE account_id=$1 AND id=$2`, a, uuidParam(r), in.Code, in.Bedrooms, in.Rent, in.Status)
	if e != nil || tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "unit not found")
		return
	}
	writeData(w, 200, map[string]string{"id": uuidParam(r), "updated": "true"})
}
func (s *Server) deleteUnit(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	unitID := uuidParam(r)
	if p.Role == "agent" {
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT property_id FROM units WHERE account_id=$1 AND id=$2`, a, unitID).Scan(&propertyID)
		if e != nil {
			writeErr(w, 404, "not_found", "unit not found")
			return
		}
		ok, e := s.checkPropertyAccess(r, propertyID)
		if e != nil || !ok {
			writeErr(w, 403, "forbidden", "unit not assigned to this agent")
			return
		}
	}
	tag, e := s.DB.Exec(r.Context(), `DELETE FROM units WHERE account_id=$1 AND id=$2`, a, unitID)
	if e != nil {
		writeErr(w, 409, "in_use", "unit is referenced by a lease")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "unit not found")
		return
	}
	writeData(w, 200, map[string]string{"deleted": uuidParam(r)})
}
func (s *Server) listTenants(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role == "tenant" {
		rows(w, r, `SELECT id,full_name,email,phone,national_id,created_at FROM tenants WHERE id=$1`, p.TenantID)
		return
	}
	if !s.manager(w, r) {
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	if p.Role == "agent" {
		ids, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load tenants")
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
		query := fmt.Sprintf(`SELECT DISTINCT t.id,t.full_name,t.email,t.phone,t.national_id,t.created_at,t.updated_at FROM tenants t JOIN leases l ON l.tenant_id=t.id AND l.account_id=t.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE t.account_id=$1 AND u.property_id IN (%s) ORDER BY t.full_name`, strings.Join(placeholders, ","))
		rows(w, r, query, args...)
		return
	}
	rows(w, r, `SELECT id,full_name,email,phone,national_id,created_at,updated_at FROM tenants WHERE account_id=$1 ORDER BY full_name`, a)
}
func (s *Server) createTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "landlord" && p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "only landlords or super_admins can create tenants")
		return
	}
	if !s.manager(w, r) {
		return
	}
	var in struct {
		Name       string `json:"full_name"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		NationalID string `json:"national_id"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") {
		writeErr(w, 400, "invalid_request", "full_name and valid email are required")
		return
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database_error", "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	var id, uid string
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	e = tx.QueryRow(r.Context(), `INSERT INTO tenants(account_id,full_name,email,phone,national_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, a, in.Name, strings.ToLower(in.Email), in.Phone, in.NationalID).Scan(&id)
	if e == nil {
		// Create user with an unusable password hash; activation sets real password.
		unusable, _ := bcrypt.GenerateFromPassword([]byte("unusable-placeholder-hash"), bcrypt.DefaultCost)
		e = tx.QueryRow(r.Context(), `INSERT INTO users(tenant_id,email,full_name,phone,password_hash,role) VALUES($1,lower($2),$3,$4,$5,'tenant') RETURNING id`, id, in.Email, in.Name, in.Phone, string(unusable)).Scan(&uid)
	}
	if e != nil {
		writeErr(w, 409, "tenant_create_failed", "could not create tenant or linked login (email may already be registered)")
		return
	}
	// Generate single-use activation token.
	raw, terr := randomToken()
	if terr != nil {
		writeErr(w, 500, "internal", "could not generate invitation")
		return
	}
	_, terr = tx.Exec(r.Context(), `INSERT INTO tenant_invitations(account_id,tenant_id,user_id,token_hash,expires_at) VALUES($1,$2,$3,$4,now()+interval '7 days')`, a, id, uid, tokenHash(raw))
	if terr != nil {
		writeErr(w, 500, "internal", "could not store invitation")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeErr(w, 500, "database_error", "could not save tenant")
		return
	}
	link := buildActivationLink(s.C, raw)
	if s.C.Env == "development" {
		s.Log.Info("tenant activation link issued", "email", in.Email, "url", link)
	}
	if s.Email != nil {
		_ = s.Email.Send(in.Email, "Activate your RentFlow account", "Welcome to RentFlow. Activate your account: "+link)
	}
	writeData(w, 201, map[string]any{"id": id, "full_name": in.Name, "email": in.Email, "phone": in.Phone, "national_id": in.NationalID})
}
func (s *Server) resendTenantInvitation(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "landlord" && p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "only landlords or super_admins can resend invitations")
		return
	}
	if !s.manager(w, r) {
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	tenantID := uuidParam(r)
	// verify tenant belongs to account
	var uid, email string
	e = s.DB.QueryRow(r.Context(), `SELECT u.id, u.email FROM users u JOIN tenants t ON t.id=u.tenant_id WHERE t.id=$1 AND t.account_id=$2 AND u.role='tenant'`, tenantID, a).Scan(&uid, &email)
	if e != nil {
		writeErr(w, 404, "not_found", "tenant not found")
		return
	}
	raw, terr := randomToken()
	if terr != nil {
		writeErr(w, 500, "internal", "could not generate invitation")
		return
	}
	_, terr = s.DB.Exec(r.Context(), `INSERT INTO tenant_invitations(account_id,tenant_id,user_id,token_hash,expires_at) VALUES($1,$2,$3,$4,now()+interval '7 days')`, a, tenantID, uid, tokenHash(raw))
	if terr != nil {
		writeErr(w, 500, "internal", "could not store invitation")
		return
	}
	link := buildActivationLink(s.C, raw)
	if s.C.Env == "development" {
		s.Log.Info("tenant activation link reissued", "email", email, "url", link)
	}
	if s.Email != nil {
		_ = s.Email.Send(email, "Activate your RentFlow account", "Activate your account: "+link)
	}
	writeData(w, 200, map[string]string{"message": "invitation sent"})
}
func (s *Server) patchTenant(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	tenantID := uuidParam(r)
	if p.Role == "agent" {
		// Check if tenant has a lease in a property assigned to this agent
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT u.property_id FROM leases l JOIN units u ON u.id=l.unit_id WHERE l.tenant_id=$1 AND l.account_id=$2 AND l.status='active' LIMIT 1`, tenantID, a).Scan(&propertyID)
		if e != nil {
			writeErr(w, 404, "not_found", "tenant not found")
			return
		}
		ok, e := s.checkPropertyAccess(r, propertyID)
		if e != nil || !ok {
			writeErr(w, 403, "forbidden", "tenant not assigned to this agent")
			return
		}
	}
	var in struct {
		Name       *string `json:"full_name"`
		Email      *string `json:"email"`
		Phone      *string `json:"phone"`
		NationalID *string `json:"national_id"`
	}
	if decode(r, &in) != nil {
		writeErr(w, 400, "invalid_request", "invalid tenant fields")
		return
	}
	tag, e := s.DB.Exec(r.Context(), `UPDATE tenants SET full_name=coalesce($3,full_name),email=coalesce($4,email),phone=coalesce($5,phone),national_id=coalesce($6,national_id),updated_at=now() WHERE account_id=$1 AND id=$2`, a, tenantID, in.Name, in.Email, in.Phone, in.NationalID)
	if e != nil || tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "tenant not found")
		return
	}
	writeData(w, 200, map[string]string{"id": uuidParam(r), "updated": "true"})
}
func (s *Server) deleteTenant(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	tenantID := uuidParam(r)
	if p.Role == "agent" {
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT u.property_id FROM leases l JOIN units u ON u.id=l.unit_id WHERE l.tenant_id=$1 AND l.account_id=$2 AND l.status='active' LIMIT 1`, tenantID, a).Scan(&propertyID)
		if e != nil {
			writeErr(w, 404, "not_found", "tenant not found")
			return
		}
		ok, e := s.checkPropertyAccess(r, propertyID)
		if e != nil || !ok {
			writeErr(w, 403, "forbidden", "tenant not assigned to this agent")
			return
		}
	}
	tag, e := s.DB.Exec(r.Context(), `DELETE FROM tenants WHERE account_id=$1 AND id=$2`, a, tenantID)
	if e != nil {
		writeErr(w, 409, "in_use", "tenant has related lease or payment records")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "tenant not found")
		return
	}
	writeData(w, 200, map[string]string{"deleted": uuidParam(r)})
}
func (s *Server) listLeases(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role == "tenant" {
		rows(w, r, `SELECT l.id,l.unit_id,u.code AS unit_code,p.name AS property_name,l.tenant_id,l.start_date,l.end_date,l.rent_amount,l.due_day,l.deposit_amount,l.status FROM leases l JOIN units u ON u.id=l.unit_id JOIN properties p ON p.id=u.property_id WHERE l.tenant_id=$1 AND l.status='active' ORDER BY l.start_date DESC`, p.TenantID)
		return
	}
	if !s.manager(w, r) {
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	if p.Role == "agent" {
		ids, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load leases")
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
		query := fmt.Sprintf(`SELECT l.id,l.unit_id,u.code AS unit_code,p.name AS property_name,l.tenant_id,t.full_name AS tenant_name,l.start_date,l.end_date,l.rent_amount,l.due_day,l.deposit_amount,l.status FROM leases l JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id JOIN properties p ON p.id=u.property_id AND p.account_id=l.account_id JOIN tenants t ON t.id=l.tenant_id AND t.account_id=l.account_id WHERE l.account_id=$1 AND p.id IN (%s) ORDER BY l.start_date DESC`, strings.Join(placeholders, ","))
		rows(w, r, query, args...)
		return
	}
	rows(w, r, `SELECT l.id,l.unit_id,u.code AS unit_code,p.name AS property_name,l.tenant_id,t.full_name AS tenant_name,l.start_date,l.end_date,l.rent_amount,l.due_day,l.deposit_amount,l.status FROM leases l JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id JOIN properties p ON p.id=u.property_id AND p.account_id=l.account_id JOIN tenants t ON t.id=l.tenant_id AND t.account_id=l.account_id WHERE l.account_id=$1 ORDER BY l.start_date DESC`, a)
}
func (s *Server) createLease(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	var in struct {
		UnitID    string  `json:"unit_id"`
		TenantID  string  `json:"tenant_id"`
		StartDate string  `json:"start_date"`
		EndDate   string  `json:"end_date"`
		Rent      float64 `json:"rent_amount"`
		DueDay    int     `json:"due_day"`
		Deposit   float64 `json:"deposit_amount"`
	}
	if decode(r, &in) != nil || in.UnitID == "" || in.TenantID == "" || in.StartDate == "" || in.Rent < 0 {
		writeErr(w, 400, "invalid_request", "unit_id, tenant_id, start_date, and rent_amount are required")
		return
	}
	if in.DueDay == 0 {
		in.DueDay = 1
	}
	if in.DueDay < 1 || in.DueDay > 31 || in.Deposit < 0 {
		writeErr(w, 400, "invalid_request", "due_day must be between 1 and 31 and deposit_amount cannot be negative")
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	p := principal(r)
	if p.Role == "agent" {
		ok, e := s.checkPropertyAccessByUnit(r, in.UnitID, a)
		if e != nil || !ok {
			writeErr(w, 403, "forbidden", "unit not assigned to this agent")
			return
		}
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database_error", "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	e = tx.QueryRow(r.Context(), `INSERT INTO leases(account_id,unit_id,tenant_id,start_date,end_date,rent_amount,due_day,deposit_amount) SELECT $1,u.id,t.id,$4::date,NULLIF($5,'')::date,$6,$7,$8 FROM units u JOIN tenants t ON t.account_id=u.account_id AND t.id=$3 WHERE u.account_id=$1 AND u.id=$2 RETURNING id`, a, in.UnitID, in.TenantID, in.StartDate, in.EndDate, in.Rent, in.DueDay, in.Deposit).Scan(&id)
	if e != nil {
		writeErr(w, 409, "lease_conflict", "unit or tenant not in your account, or unit already leased")
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE units SET status='occupied',updated_at=now() WHERE account_id=$1 AND id=$2`, a, in.UnitID)
	if tx.Commit(r.Context()) != nil {
		writeErr(w, 500, "database_error", "could not save lease")
		return
	}
	writeData(w, 201, map[string]any{"id": id, "unit_id": in.UnitID, "tenant_id": in.TenantID, "start_date": in.StartDate, "end_date": in.EndDate, "rent_amount": in.Rent, "due_day": in.DueDay, "deposit_amount": in.Deposit, "status": "active"})
}
func (s *Server) patchLease(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	p := principal(r)
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	leaseID := uuidParam(r)
	if p.Role == "agent" {
		ok, e := s.checkPropertyAccessByLease(r, leaseID, a)
		if e != nil || !ok {
			writeErr(w, 403, "forbidden", "lease not assigned to this agent")
			return
		}
	}
	var in struct {
		EndDate *string  `json:"end_date"`
		Rent    *float64 `json:"rent_amount"`
		DueDay  *int     `json:"due_day"`
		Deposit *float64 `json:"deposit_amount"`
		Status  *string  `json:"status"`
	}
	if decode(r, &in) != nil || (in.DueDay != nil && (*in.DueDay < 1 || *in.DueDay > 31)) || (in.Deposit != nil && *in.Deposit < 0) || (in.Rent != nil && *in.Rent < 0) {
		writeErr(w, 400, "invalid_request", "invalid lease fields")
		return
	}
	tag, e := s.DB.Exec(r.Context(), `UPDATE leases SET end_date=coalesce($3::date,end_date),rent_amount=coalesce($4,rent_amount),due_day=coalesce($5,due_day),deposit_amount=coalesce($6,deposit_amount),status=coalesce($7,status),updated_at=now() WHERE account_id=$1 AND id=$2`, a, leaseID, in.EndDate, in.Rent, in.DueDay, in.Deposit, in.Status)
	if e != nil || tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "lease not found or invalid update")
		return
	}
	if in.Status != nil && *in.Status == "ended" {
		_, _ = s.DB.Exec(r.Context(), `UPDATE units SET status='vacant',updated_at=now() WHERE account_id=$1 AND id=(SELECT unit_id FROM leases WHERE account_id=$1 AND id=$2)`, a, uuidParam(r))
	}
	writeData(w, 200, map[string]string{"id": uuidParam(r), "updated": "true"})
}
func (s *Server) ensureInvoices(r *http.Request, account string) error {
	return s.ensureInvoicesContext(r.Context(), account)
}
func (s *Server) ensureInvoicesContext(ctx context.Context, account string) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `INSERT INTO invoices(account_id,lease_id,tenant_id,period,due_date,amount) SELECT l.account_id,l.id,l.tenant_id,m.period,make_date(extract(year from m.period)::int,extract(month from m.period)::int,least(l.due_day,extract(day from (m.period + interval '1 month - 1 day'))::int)),l.rent_amount FROM leases l CROSS JOIN LATERAL generate_series(date_trunc('month',l.start_date)::date,date_trunc('month',current_date)::date,interval '1 month') AS m(period) WHERE l.account_id=$1 AND l.start_date<=current_date AND (l.end_date IS NULL OR l.end_date>=m.period) AND make_date(extract(year from m.period)::int,extract(month from m.period)::int,least(l.due_day,extract(day from (m.period + interval '1 month - 1 day'))::int))<=current_date ON CONFLICT(lease_id,period) DO NOTHING`, account)
	if e != nil {
		return e
	}
	credits, ce := tx.Query(ctx, `SELECT tenant_id,balance FROM tenant_credits WHERE account_id=$1 AND balance>0 FOR UPDATE`, account)
	if ce != nil {
		return ce
	}
	type creditRow struct {
		tenant  string
		balance float64
	}
	cs := []creditRow{}
	for credits.Next() {
		var c creditRow
		if ce = credits.Scan(&c.tenant, &c.balance); ce != nil {
			credits.Close()
			return ce
		}
		cs = append(cs, c)
	}
	ce = credits.Err()
	credits.Close()
	if ce != nil {
		return ce
	}
	for _, c := range cs {
		remain := c.balance
		irs, ie := tx.Query(ctx, `SELECT id,amount-paid_amount FROM invoices WHERE account_id=$1 AND tenant_id=$2 AND amount>paid_amount ORDER BY due_date,id FOR UPDATE`, account, c.tenant)
		if ie != nil {
			return ie
		}
		type openInvoice struct {
			id      string
			balance float64
		}
		opens := []openInvoice{}
		for irs.Next() {
			var o openInvoice
			if ie = irs.Scan(&o.id, &o.balance); ie != nil {
				irs.Close()
				return ie
			}
			opens = append(opens, o)
		}
		ie = irs.Err()
		irs.Close()
		if ie != nil {
			return ie
		}
		for _, o := range opens {
			if remain <= 0 {
				break
			}
			applied := remain
			if applied > o.balance {
				applied = o.balance
			}
			if _, ie = tx.Exec(ctx, `UPDATE invoices SET paid_amount=paid_amount+$2,status=CASE WHEN paid_amount+$2>=amount THEN 'paid' ELSE 'partial' END WHERE id=$1`, o.id, applied); ie != nil {
				return ie
			}
			remain -= applied
		}
		if _, ie = tx.Exec(ctx, `UPDATE tenant_credits SET balance=$3,updated_at=now() WHERE tenant_id=$1 AND account_id=$2`, c.tenant, account, remain); ie != nil {
			return ie
		}
	}
	return tx.Commit(ctx)
}
func (s *Server) listInvoices(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role == "tenant" { // tenant can only query their own records
		var account string
		if e := s.DB.QueryRow(r.Context(), `SELECT account_id FROM tenants WHERE id=$1`, p.TenantID).Scan(&account); e == nil {
			if e = s.ensureInvoices(r, account); e != nil {
				writeErr(w, 500, "invoice_generation_failed", "could not create monthly invoices")
				return
			}
		}
		rows(w, r, `SELECT id,lease_id,period,due_date,amount,paid_amount,(amount-paid_amount) AS balance,status FROM invoices WHERE tenant_id=$1 ORDER BY due_date DESC`, p.TenantID)
		return
	}
	if !s.manager(w, r) {
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	if p.Role == "agent" {
		propIDs, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load invoices")
			return
		}
		if len(propIDs) == 0 {
			rows(w, r, `SELECT i.id,i.lease_id,i.tenant_id,t.full_name AS tenant_name,u.code AS unit_code,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,i.status FROM invoices i JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id JOIN properties p ON p.id=u.property_id AND p.account_id=l.account_id WHERE i.account_id=$1 AND false ORDER BY i.due_date DESC`, a)
			return
		}
		placeholders := make([]string, len(propIDs))
		args := make([]any, len(propIDs)+1)
		args[0] = a
		for i, id := range propIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
			args[i+1] = id
		}
		query := fmt.Sprintf(`SELECT i.id,i.lease_id,i.tenant_id,t.full_name AS tenant_name,u.code AS unit_code,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,i.status FROM invoices i JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id JOIN properties p ON p.id=u.property_id AND p.account_id=l.account_id WHERE i.account_id=$1 AND p.id IN (%s) ORDER BY i.due_date DESC`, strings.Join(placeholders, ","))
		rows(w, r, query, args...)
		return
	}
	if e := s.ensureInvoices(r, a); e != nil {
		writeErr(w, 500, "invoice_generation_failed", "could not create monthly invoices")
		return
	}
	rows(w, r, `SELECT i.id,i.lease_id,i.tenant_id,t.full_name AS tenant_name,u.code AS unit_code,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,i.status FROM invoices i JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.account_id=$1 ORDER BY i.due_date DESC`, a)
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role == "tenant" {
		var due, paid float64
		_ = s.DB.QueryRow(r.Context(), `SELECT coalesce(sum(amount-paid_amount),0),coalesce(sum(paid_amount),0) FROM invoices WHERE tenant_id=$1 AND due_date<=current_date`, p.TenantID).Scan(&due, &paid)
		var propertyName, propertyAddress, unitCode, leaseID, leaseStart, leaseEnd, leaseStatus string
		var rentAmount float64
		_ = s.DB.QueryRow(r.Context(), `SELECT p.name, p.address, u.code, l.id, l.start_date, l.end_date, l.rent_amount, l.status FROM leases l JOIN units u ON u.id=l.unit_id JOIN properties p ON p.id=u.property_id WHERE l.tenant_id=$1 AND l.status='active' ORDER BY l.start_date DESC LIMIT 1`, p.TenantID).Scan(&propertyName, &propertyAddress, &unitCode, &leaseID, &leaseStart, &leaseEnd, &rentAmount, &leaseStatus)
		var submitted, pending, inProgress, completed int
		_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE status='submitted'), count(*) FILTER (WHERE status IN ('submitted','under_review','assigned')), count(*) FILTER (WHERE status='in_progress'), count(*) FILTER (WHERE status='resolved') FROM maintenance_requests WHERE tenant_id=$1`, p.TenantID).Scan(&submitted, &pending, &inProgress, &completed)
		writeData(w, 200, map[string]any{
			"outstanding": due, "paid": paid,
			"property_name": propertyName, "property_address": propertyAddress,
			"unit_code": unitCode, "lease_id": leaseID,
			"lease_start": leaseStart, "lease_end": leaseEnd,
			"lease_status": leaseStatus, "rent_amount": rentAmount,
			"maintenance": map[string]any{"submitted": submitted, "pending": pending, "in_progress": inProgress, "completed": completed},
		})
		return
	}
	if !s.manager(w, r) {
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	if p.Role == "agent" {
		propIDs, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		if len(propIDs) == 0 {
			writeData(w, 200, map[string]any{"properties": 0, "units": 0, "occupied_units": 0, "vacant_units": 0, "tenants": 0, "expected_this_month": 0, "collected_this_month": 0, "collection_rate": 0, "arrears_total": 0, "recent_payments": []map[string]any{}})
			return
		}
		placeholders := make([]string, len(propIDs))
		args := make([]any, len(propIDs))
		for i, id := range propIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = id
		}
		propFilter := fmt.Sprintf("IN (%s)", strings.Join(placeholders, ","))
		zone := time.FixedZone("EAT", 3*60*60)
		now := time.Now().In(zone)
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone)
		nextMonth := monthStart.AddDate(0, 1, 0)
		today := now.Format("2006-01-02")
		var props, units, tenants, occupied, vacant int
		var expected, collected, arrears float64
		qProps := fmt.Sprintf(`SELECT count(*) FROM properties WHERE id %s`, propFilter)
		if e := s.DB.QueryRow(r.Context(), qProps, args...).Scan(&props); e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		qUnits := fmt.Sprintf(`SELECT count(*),count(*) FILTER(WHERE status='occupied'),count(*) FILTER(WHERE status='vacant') FROM units WHERE property_id %s`, propFilter)
		if e := s.DB.QueryRow(r.Context(), qUnits, args...).Scan(&units, &occupied, &vacant); e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		qTenants := fmt.Sprintf(`SELECT count(*) FROM tenants t JOIN leases l ON l.tenant_id=t.id AND l.account_id=t.account_id JOIN units u ON u.id=l.unit_id WHERE u.property_id %s`, propFilter)
		if e := s.DB.QueryRow(r.Context(), qTenants, args...).Scan(&tenants); e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		qExpected := fmt.Sprintf(`SELECT coalesce(sum(l.rent_amount),0) FROM leases l JOIN units u ON u.id=l.unit_id WHERE l.status='active' AND l.start_date<$%d::date AND (l.end_date IS NULL OR l.end_date >= $%d::date) AND u.property_id %s`, len(propIDs)+1, len(propIDs)+2, propFilter)
		qExpectedArgs := append([]any{}, args...)
		qExpectedArgs = append(qExpectedArgs, monthStart.Format("2006-01-02"), nextMonth.Format("2006-01-02"))
		if e := s.DB.QueryRow(r.Context(), qExpected, qExpectedArgs...).Scan(&expected); e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		qCollected := fmt.Sprintf(`SELECT coalesce(sum(p.amount),0) FROM payments p JOIN tenants t ON t.id=p.tenant_id AND t.account_id=p.account_id JOIN leases l ON l.id=(SELECT la.id FROM leases la JOIN units uu ON uu.id=la.unit_id WHERE uu.property_id %s AND la.account_id=t.account_id AND la.tenant_id=t.id ORDER BY la.created_at DESC LIMIT 1) WHERE p.status='completed' AND (coalesce(p.transaction_date,p.created_at) AT TIME ZONE 'Africa/Nairobi')::date >= $%d::date AND (coalesce(p.transaction_date,p.created_at) AT TIME ZONE 'Africa/Nairobi')::date < $%d::date`, propFilter, len(propIDs)+1, len(propIDs)+2)
		qCollectedArgs := append([]any{}, args...)
		qCollectedArgs = append(qCollectedArgs, monthStart.Format("2006-01-02"), nextMonth.Format("2006-01-02"))
		if e := s.DB.QueryRow(r.Context(), qCollected, qCollectedArgs...).Scan(&collected); e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		qArrears := fmt.Sprintf(`SELECT coalesce(sum(i.amount-i.paid_amount),0) FROM invoices i JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id WHERE u.property_id %s AND i.due_date<$%d::date AND i.paid_amount<i.amount`, propFilter, len(propIDs)+1)
		qArrearsArgs := append([]any{}, args...)
		qArrearsArgs = append(qArrearsArgs, today)
		if e := s.DB.QueryRow(r.Context(), qArrears, qArrearsArgs...).Scan(&arrears); e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		rate := float64(0)
		if expected > 0 {
			rate = collected / expected * 100
		}
		qRecent := fmt.Sprintf(`SELECT p.id,coalesce(t.full_name,''),coalesce((SELECT u.code FROM payment_allocations pa JOIN invoices i ON i.id=pa.invoice_id AND i.account_id=pa.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE pa.payment_id=p.id AND pa.account_id=p.account_id ORDER BY i.due_date DESC LIMIT 1),''),p.amount,p.method,p.status,coalesce(p.external_ref,p.checkout_request_id,''),coalesce(p.transaction_date,p.created_at) FROM payments p LEFT JOIN tenants t ON t.id=p.tenant_id AND t.account_id=p.account_id WHERE p.status='completed' AND EXISTS(SELECT 1 FROM leases l2 JOIN units u2 ON u2.id=l2.unit_id WHERE l2.tenant_id=p.tenant_id AND l2.account_id=p.account_id AND u2.property_id %s) ORDER BY coalesce(p.transaction_date,p.created_at) DESC LIMIT 6`, propFilter)
		rs, e := s.DB.Query(r.Context(), qRecent, args...)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		recent := []map[string]any{}
		for rs.Next() {
			var id, tenantName, unitCode, method, status, receipt string
			var amount float64
			var paidAt time.Time
			if e = rs.Scan(&id, &tenantName, &unitCode, &amount, &method, &status, &receipt, &paidAt); e != nil {
				rs.Close()
				writeErr(w, 500, "database_error", "could not load dashboard")
				return
			}
			recent = append(recent, map[string]any{"id": id, "tenant_name": tenantName, "unit_code": unitCode, "amount": amount, "method": method, "status": status, "receipt_number": receipt, "payment_date": paidAt})
		}
		if e = rs.Err(); e != nil {
			rs.Close()
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		rs.Close()
		writeData(w, 200, map[string]any{"properties": props, "units": units, "occupied_units": occupied, "vacant_units": vacant, "tenants": tenants, "expected_this_month": expected, "collected_this_month": collected, "collection_rate": rate, "arrears_total": arrears, "recent_payments": recent})
		return
	}
	if e := s.ensureInvoices(r, a); e != nil {
		writeErr(w, 500, "invoice_generation_failed", "could not create monthly invoices")
		return
	}
	zone := time.FixedZone("EAT", 3*60*60)
	now := time.Now().In(zone)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone)
	nextMonth := monthStart.AddDate(0, 1, 0)
	today := now.Format("2006-01-02")
	var props, units, tenants, occupied, vacant int
	var expected, collected, arrears float64
	if e := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM properties WHERE account_id=$1`, a).Scan(&props); e != nil {
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	if e := s.DB.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE status='occupied'),count(*) FILTER(WHERE status='vacant') FROM units WHERE account_id=$1`, a).Scan(&units, &occupied, &vacant); e != nil {
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	if e := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM tenants WHERE account_id=$1`, a).Scan(&tenants); e != nil {
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	if e := s.DB.QueryRow(r.Context(), `SELECT coalesce(sum(l.rent_amount),0) FROM leases l WHERE l.account_id=$1 AND l.status='active' AND l.start_date<$3::date AND (l.end_date IS NULL OR l.end_date >= $2::date)`, a, monthStart.Format("2006-01-02"), nextMonth.Format("2006-01-02")).Scan(&expected); e != nil {
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	if e := s.DB.QueryRow(r.Context(), `SELECT coalesce(sum(p.amount),0) FROM payments p WHERE p.account_id=$1 AND p.status='completed' AND (coalesce(p.transaction_date,p.created_at) AT TIME ZONE 'Africa/Nairobi')::date >= $2::date AND (coalesce(p.transaction_date,p.created_at) AT TIME ZONE 'Africa/Nairobi')::date < $3::date`, a, monthStart.Format("2006-01-02"), nextMonth.Format("2006-01-02")).Scan(&collected); e != nil {
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	if e := s.DB.QueryRow(r.Context(), `SELECT coalesce(sum(amount-paid_amount),0) FROM invoices WHERE account_id=$1 AND due_date<$2::date AND paid_amount<amount`, a, today).Scan(&arrears); e != nil {
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	rate := float64(0)
	if expected > 0 {
		rate = collected / expected * 100
	}
	recent := []map[string]any{}
	rs, e := s.DB.Query(r.Context(), `SELECT p.id,coalesce(t.full_name,''),coalesce((SELECT u.code FROM payment_allocations pa JOIN invoices i ON i.id=pa.invoice_id AND i.account_id=pa.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE pa.payment_id=p.id AND pa.account_id=p.account_id ORDER BY i.due_date DESC LIMIT 1),''),p.amount,p.method,p.status,coalesce(p.external_ref,p.checkout_request_id,''),coalesce(p.transaction_date,p.created_at) FROM payments p LEFT JOIN tenants t ON t.id=p.tenant_id AND t.account_id=p.account_id WHERE p.account_id=$1 AND p.status='completed' ORDER BY coalesce(p.transaction_date,p.created_at) DESC LIMIT 6`, a)
	if e != nil {
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	for rs.Next() {
		var id, tenantName, unitCode, method, status, receipt string
		var amount float64
		var paidAt time.Time
		if e = rs.Scan(&id, &tenantName, &unitCode, &amount, &method, &status, &receipt, &paidAt); e != nil {
			rs.Close()
			writeErr(w, 500, "database_error", "could not load dashboard")
			return
		}
		recent = append(recent, map[string]any{"id": id, "tenant_name": tenantName, "unit_code": unitCode, "amount": amount, "method": method, "status": status, "receipt_number": receipt, "payment_date": paidAt})
	}
	if e = rs.Err(); e != nil {
		rs.Close()
		writeErr(w, 500, "database_error", "could not load dashboard")
		return
	}
	rs.Close()
	writeData(w, 200, map[string]any{"properties": props, "units": units, "occupied_units": occupied, "vacant_units": vacant, "tenants": tenants, "expected_this_month": expected, "collected_this_month": collected, "collection_rate": rate, "arrears_total": arrears, "recent_payments": recent})
}

func validUnitCode(code string) bool { v := strings.TrimSpace(code); return v != "" && len(v) <= 12 }

// listPlatformSubscriptions returns accounts with subscription details.
func (s *Server) listPlatformSubscriptions(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "super_admin access required")
		return
	}
	page, pageSize := 1, 20
	if v := r.URL.Query().Get("page"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 {
			writeErr(w, 400, "invalid_request", "page must be positive")
			return
		}
		page = n
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			writeErr(w, 400, "invalid_request", "page_size 1-100")
			return
		}
		pageSize = n
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	where := ""
	args := []any{}
	conds := []string{}
	if status != "" {
		conds = append(conds, fmt.Sprintf("subscription_status=$%d", len(args)+1))
		args = append(args, status)
	}
	if search != "" {
		conds = append(conds, fmt.Sprintf("name ILIKE $%d", len(args)+1))
		args = append(args, "%"+search+"%")
	}
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	_ = s.DB.QueryRow(r.Context(), "SELECT count(*) FROM accounts "+where, args...).Scan(&total)
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	q := fmt.Sprintf(`SELECT id, name, subscription_status, subscription_plan_id, subscription_started_at, subscription_renews_at, subscription_expires_at, suspended_at FROM accounts %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)
	rows, e := s.DB.Query(r.Context(), q, queryArgs...)
	if e != nil {
		writeErr(w, 500, "database_error", "could not load subscriptions")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, status string
		var planID *string
		var startedAt, renewsAt, expiresAt, suspendedAt interface{}
		if e := rows.Scan(&id, &name, &status, &planID, &startedAt, &renewsAt, &expiresAt, &suspendedAt); e != nil {
			writeErr(w, 500, "database_error", "could not read subscriptions")
			return
		}
		out = append(out, map[string]any{"account_id": id, "account_name": name, "status": status, "plan_id": planID, "started_at": startedAt, "renews_at": renewsAt, "expires_at": expiresAt, "suspended_at": suspendedAt})
	}
	writeData(w, 200, map[string]any{"subscriptions": out, "pagination": map[string]any{"page": page, "page_size": pageSize, "total": total, "total_pages": (total + pageSize - 1) / pageSize}})
}

// listPlatformAuditLogs returns paginated audit logs.
func (s *Server) listPlatformAuditLogs(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "super_admin access required")
		return
	}
	page, pageSize := 1, 20
	if v := r.URL.Query().Get("page"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 {
			writeErr(w, 400, "invalid_request", "page must be positive")
			return
		}
		page = n
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			writeErr(w, 400, "invalid_request", "page_size 1-100")
			return
		}
		pageSize = n
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	resourceType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	where := ""
	args := []any{}
	conds := []string{}
	if action != "" {
		conds = append(conds, fmt.Sprintf("action=$%d", len(args)+1))
		args = append(args, action)
	}
	if resourceType != "" {
		conds = append(conds, fmt.Sprintf("resource_type=$%d", len(args)+1))
		args = append(args, resourceType)
	}
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	_ = s.DB.QueryRow(r.Context(), "SELECT count(*) FROM audit_logs "+where, args...).Scan(&total)
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	q := fmt.Sprintf(`SELECT id, actor_id, actor_role, actor_email, action, resource_type, resource_id, account_id, created_at FROM audit_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)
	rows, e := s.DB.Query(r.Context(), q, queryArgs...)
	if e != nil {
		writeErr(w, 500, "database_error", "could not load audit logs")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, actorRole, actorEmail, action, resourceType, resourceID string
		var actorID, accountID *string
		var createdAt interface{}
		if e := rows.Scan(&id, &actorID, &actorRole, &actorEmail, &action, &resourceType, &resourceID, &accountID, &createdAt); e != nil {
			writeErr(w, 500, "database_error", "could not read audit logs")
			return
		}
		out = append(out, map[string]any{"id": id, "actor_id": actorID, "actor_role": actorRole, "actor_email": actorEmail, "action": action, "resource_type": resourceType, "resource_id": resourceID, "account_id": accountID, "created_at": createdAt})
	}
	writeData(w, 200, map[string]any{"audit_logs": out, "pagination": map[string]any{"page": page, "page_size": pageSize, "total": total, "total_pages": (total + pageSize - 1) / pageSize}})
}

// platformOverview returns platform-wide summary statistics for super_admin.
func (s *Server) platformOverview(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "super_admin access required")
		return
	}
	var totalAccounts, totalProperties, totalUnits, totalTenants int
	var activeSubs, trialingSubs, pastDueSubs, suspendedSubs, cancelledSubs int
	var revenue float64
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM accounts`).Scan(&totalAccounts)
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM properties`).Scan(&totalProperties)
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM units`).Scan(&totalUnits)
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM tenants`).Scan(&totalTenants)
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE subscription_status='active'), count(*) FILTER (WHERE subscription_status='trialing'), count(*) FILTER (WHERE subscription_status='past_due'), count(*) FILTER (WHERE subscription_status='suspended'), count(*) FILTER (WHERE subscription_status='cancelled') FROM accounts`).Scan(&activeSubs, &trialingSubs, &pastDueSubs, &suspendedSubs, &cancelledSubs)
	_ = s.DB.QueryRow(r.Context(), `SELECT coalesce(sum(amount),0) FROM subscription_payments WHERE status='completed'`).Scan(&revenue)
	writeData(w, 200, map[string]any{
		"total_accounts":   totalAccounts,
		"total_properties": totalProperties,
		"total_units":      totalUnits,
		"total_tenants":    totalTenants,
		"subscriptions": map[string]any{
			"active":    activeSubs,
			"trialing":  trialingSubs,
			"past_due":  pastDueSubs,
			"suspended": suspendedSubs,
			"cancelled": cancelledSubs,
		},
		"subscription_revenue": revenue,
	})
}

// listPlatformSubscriptionPayments returns paginated subscription payments.
func (s *Server) listPlatformSubscriptionPayments(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "super_admin access required")
		return
	}
	page, pageSize := 1, 20
	if v := r.URL.Query().Get("page"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 {
			writeErr(w, 400, "invalid_request", "page must be positive")
			return
		}
		page = n
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			writeErr(w, 400, "invalid_request", "page_size 1-100")
			return
		}
		pageSize = n
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	offset := (page - 1) * pageSize
	where := ""
	args := []any{}
	if status != "" {
		where = "WHERE sp.status=$1"
		args = append(args, status)
	}
	var total int
	_ = s.DB.QueryRow(r.Context(), "SELECT count(*) FROM subscription_payments sp "+where, args...).Scan(&total)
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, offset)
	q := fmt.Sprintf(`SELECT sp.id, sp.account_id, a.name, sp.amount, sp.method, sp.status, sp.external_ref, sp.created_at
		FROM subscription_payments sp JOIN accounts a ON a.id=sp.account_id %s ORDER BY sp.created_at DESC LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)
	rows, e := s.DB.Query(r.Context(), q, queryArgs...)
	if e != nil {
		writeErr(w, 500, "database_error", "could not load payments")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, accountID, accountName, method, st string
		var amount float64
		var extRef *string
		var createdAt interface{}
		if e := rows.Scan(&id, &accountID, &accountName, &amount, &method, &st, &extRef, &createdAt); e != nil {
			writeErr(w, 500, "database_error", "could not read payments")
			return
		}
		out = append(out, map[string]any{"id": id, "account_id": accountID, "account_name": accountName, "amount": amount, "method": method, "status": st, "external_ref": extRef, "created_at": createdAt})
	}
	writeData(w, 200, map[string]any{"payments": out, "pagination": map[string]any{"page": page, "page_size": pageSize, "total": total, "total_pages": (total + pageSize - 1) / pageSize}})
}

// platformRevenue aggregates successful subscription payments with optional date filter.
func (s *Server) platformRevenue(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "super_admin access required")
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	where := "WHERE status='completed'"
	args := []any{}
	idx := 1
	if from != "" {
		where += fmt.Sprintf(" AND created_at >= $%d", idx)
		args = append(args, from)
		idx++
	}
	if to != "" {
		where += fmt.Sprintf(" AND created_at <= $%d", idx)
		args = append(args, to)
		idx++
	}
	var total float64
	var count int
	q := "SELECT coalesce(sum(amount),0), count(*) FROM subscription_payments " + where
	if e := s.DB.QueryRow(r.Context(), q, args...).Scan(&total, &count); e != nil {
		writeErr(w, 500, "database_error", "could not compute revenue")
		return
	}
	writeData(w, 200, map[string]any{"total_revenue": total, "completed_payments": count, "from": from, "to": to})
}

// listPlatformAccounts returns a paginated list of accounts/landlords for
// super_admin users only. It never exposes credentials or secrets.
func (s *Server) listPlatformAccounts(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "super_admin access required")
		return
	}

	page := 1
	pageSize := 20
	if v := r.URL.Query().Get("page"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 {
			writeErr(w, 400, "invalid_request", "page must be a positive integer")
			return
		}
		page = n
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			writeErr(w, 400, "invalid_request", "page_size must be between 1 and 100")
			return
		}
		pageSize = n
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	offset := (page - 1) * pageSize

	args := []any{}
	where := ""
	if search != "" {
		where = "WHERE a.name ILIKE $1 OR EXISTS (SELECT 1 FROM users u WHERE u.account_id=a.id AND u.email ILIKE $1)"
		args = append(args, "%"+search+"%")
	}

	// Count total.
	var total int
	countQ := "SELECT count(*) FROM accounts a " + where
	if e := s.DB.QueryRow(r.Context(), countQ, args...).Scan(&total); e != nil {
		writeErr(w, 500, "database_error", "could not count accounts")
		return
	}

	// Fetch page.
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, offset)
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	query := fmt.Sprintf(`SELECT a.id, a.name, a.subscription_status, a.subscription_plan_id, a.created_at, a.updated_at,
		(SELECT count(*) FROM properties p WHERE p.account_id=a.id) AS property_count,
		(SELECT count(*) FROM tenants t WHERE t.account_id=a.id) AS tenant_count
		FROM accounts a %s ORDER BY a.created_at DESC LIMIT $%d OFFSET $%d`, where, limitIdx, offsetIdx)

	rows, e := s.DB.Query(r.Context(), query, queryArgs...)
	if e != nil {
		writeErr(w, 500, "database_error", "could not load accounts")
		return
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var id, name, status string
		var planID *string
		var createdAt, updatedAt interface{}
		var propCount, tenantCount int
		if e := rows.Scan(&id, &name, &status, &planID, &createdAt, &updatedAt, &propCount, &tenantCount); e != nil {
			writeErr(w, 500, "database_error", "could not read accounts")
			return
		}
		out = append(out, map[string]any{
			"id":                   id,
			"name":                 name,
			"subscription_status":  status,
			"subscription_plan_id": planID,
			"property_count":       propCount,
			"tenant_count":         tenantCount,
			"created_at":           createdAt,
			"updated_at":           updatedAt,
		})
	}
	if e := rows.Err(); e != nil {
		writeErr(w, 500, "database_error", "could not read accounts")
		return
	}

	writeData(w, 200, map[string]any{
		"accounts": out,
		"pagination": map[string]any{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
	})
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "landlord" && p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "only the landlord or super_admin can add agents")
		return
	}
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
		// AccountID is required when the caller is super_admin to specify the target landlord account.
		AccountID *string `json:"account_id"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") || validatePassword(in.Password) != nil {
		writeErr(w, 400, "invalid_request", "name, valid email, and password of at least 10 characters are required")
		return
	}
	accountID := p.AccountID
	if p.Role == "super_admin" {
		if in.AccountID == nil || strings.TrimSpace(*in.AccountID) == "" {
			writeErr(w, 400, "invalid_request", "account_id is required when creating an agent as super_admin")
			return
		}
		accountID = *in.AccountID
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if e != nil {
		writeErr(w, 500, "internal", "could not secure password")
		return
	}
	var id string
	e = s.DB.QueryRow(r.Context(), `INSERT INTO users(account_id,email,full_name,phone,password_hash,role) VALUES($1,lower($2),$3,$4,$5,'agent') RETURNING id`, accountID, in.Email, in.Name, in.Phone, string(hash)).Scan(&id)
	if e != nil {
		writeErr(w, 409, "agent_create_failed", "could not add agent (email may already be registered)")
		return
	}
	writeData(w, 201, map[string]any{"id": id, "name": in.Name, "email": strings.ToLower(in.Email), "phone": in.Phone, "role": "agent", "accountId": accountID})
}

func (s *Server) overdueInvoices(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role == "tenant" {
		today := time.Now().In(time.FixedZone("EAT", 3*60*60)).Format("2006-01-02")
		rows(w, r, `SELECT i.id,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,($2::date-i.due_date) AS days_overdue,u.code AS unit_code FROM invoices i JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.tenant_id=$1 AND i.due_date<$2::date AND i.paid_amount<i.amount ORDER BY i.due_date`, p.TenantID, today)
		return
	}
	if !s.manager(w, r) {
		return
	}
	a, e := s.effectiveAccountID(r)
	if e != nil {
		writeErr(w, 500, "database_error", "could not resolve account")
		return
	}
	today := time.Now().In(time.FixedZone("EAT", 3*60*60)).Format("2006-01-02")
	if p.Role == "agent" {
		propIDs, e := s.agentAssignedPropertyIDs(r)
		if e != nil {
			writeErr(w, 500, "database_error", "could not load overdue invoices")
			return
		}
		if len(propIDs) == 0 {
			rows(w, r, `SELECT i.id,i.tenant_id,t.full_name AS tenant_name,i.lease_id,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,($2::date-i.due_date) AS days_overdue,u.code AS unit_code FROM invoices i JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id JOIN properties p ON p.id=u.property_id AND p.account_id=l.account_id WHERE i.account_id=$1 AND false ORDER BY i.due_date`, a, today)
			return
		}
		placeholders := make([]string, len(propIDs))
		args := make([]any, len(propIDs)+2)
		args[0] = a
		args[1] = today
		for i, id := range propIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+3)
			args[i+2] = id
		}
		query := fmt.Sprintf(`SELECT i.id,i.tenant_id,t.full_name AS tenant_name,i.lease_id,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,($2::date-i.due_date) AS days_overdue,u.code AS unit_code FROM invoices i JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id JOIN properties p ON p.id=u.property_id AND p.account_id=l.account_id WHERE i.account_id=$1 AND i.due_date<$2::date AND i.paid_amount<i.amount AND p.id IN (%s) ORDER BY i.due_date`, strings.Join(placeholders, ","))
		rows(w, r, query, args...)
		return
	}
	if e := s.ensureInvoices(r, a); e != nil {
		writeErr(w, 500, "invoice_generation_failed", "could not create monthly invoices")
		return
	}
	today = time.Now().In(time.FixedZone("EAT", 3*60*60)).Format("2006-01-02")
	rows(w, r, `SELECT i.id,i.tenant_id,t.full_name AS tenant_name,i.lease_id,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,($2::date-i.due_date) AS days_overdue,u.code AS unit_code FROM invoices i JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.account_id=$1 AND i.due_date<$2::date AND i.paid_amount<i.amount ORDER BY i.due_date`, a, today)
}
func (s *Server) RunBillingScheduler(ctx context.Context) {
	run := func() {
		if e := s.BillingCycle(ctx); e != nil {
			s.Log.Error("billing cycle failed", "error", e)
		}
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
func (s *Server) BillingCycle(ctx context.Context) error {
	rs, e := s.DB.Query(ctx, `SELECT id FROM accounts ORDER BY id`)
	if e != nil {
		return e
	}
	accounts := []string{}
	for rs.Next() {
		var id string
		if e = rs.Scan(&id); e != nil {
			rs.Close()
			return e
		}
		accounts = append(accounts, id)
	}
	e = rs.Err()
	rs.Close()
	if e != nil {
		return e
	}
	for _, account := range accounts {
		if e = s.ensureInvoicesContext(ctx, account); e != nil {
			return e
		}
		if e = s.sendBillingReminders(ctx, account); e != nil {
			s.Log.Error("billing reminder run failed", "account_id", account, "error", e)
		}
	}
	return nil
}
func (s *Server) sendBillingReminders(ctx context.Context, account string) error {
	type reminder struct {
		id, tenant, phone, unit, kind string
		amount                        float64
		due                           time.Time
	}
	rs, e := s.DB.Query(ctx, `SELECT i.id,t.id,t.phone,u.code,i.amount-i.paid_amount,i.due_date,CASE WHEN i.due_date=current_date+3 THEN 'before_due' WHEN i.due_date=current_date THEN 'due' ELSE 'overdue' END FROM invoices i JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.account_id=$1 AND i.paid_amount<i.amount AND (i.due_date=current_date+3 OR i.due_date=current_date OR i.due_date<current_date)`, account)
	if e != nil {
		return e
	}
	items := []reminder{}
	for rs.Next() {
		var v reminder
		if e = rs.Scan(&v.id, &v.tenant, &v.phone, &v.unit, &v.amount, &v.due, &v.kind); e != nil {
			rs.Close()
			return e
		}
		items = append(items, v)
	}
	e = rs.Err()
	rs.Close()
	if e != nil {
		return e
	}
	for _, v := range items {
		scheduled := time.Now().UTC().Format("2006-01-02")
		var notice string
		e = s.DB.QueryRow(ctx, `INSERT INTO billing_notifications(account_id,tenant_id,lease_id,invoice_id,period,kind,scheduled_for) SELECT $1,i.tenant_id,i.lease_id,i.id,i.period,$5,current_date FROM invoices i WHERE i.account_id=$1 AND i.id=$2 ON CONFLICT(lease_id,period,kind,scheduled_for) DO UPDATE SET status='pending',attempts=billing_notifications.attempts+1,last_error='' WHERE billing_notifications.status='failed' OR (billing_notifications.status='pending' AND billing_notifications.created_at<now()-interval '15 minutes') RETURNING id`, account, v.id, v.tenant, v.due, v.kind).Scan(&notice)
		if e != nil {
			continue
		}
		msg := fmt.Sprintf("RentFlow reminder: KES %.0f for unit %s is due %s. Please contact your landlord if already paid.", v.amount, v.unit, v.due.Format("02 Jan 2006"))
		if e = s.SMS.Send(ctx, []string{normalizePhone(v.phone)}, msg); e != nil {
			_, _ = s.DB.Exec(ctx, `UPDATE billing_notifications SET status='failed',last_error=$2 WHERE id=$1`, notice, e.Error())
			continue
		}
		_, _ = s.DB.Exec(ctx, `UPDATE billing_notifications SET status='sent',sent_at=now(),last_error='' WHERE id=$1`, notice)
		_ = scheduled
	}
	return nil
}
