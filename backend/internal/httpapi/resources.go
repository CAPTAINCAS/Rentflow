package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) listProperties(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	a, _ := s.account(r)
	rows(w, r, `SELECT id,name,address,description,created_at,updated_at FROM properties WHERE account_id=$1 ORDER BY created_at DESC`, a)
}
func (s *Server) createProperty(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
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
	a, _ := s.account(r)
	var id, name, address, description string
	var created, updated time.Time
	e := s.DB.QueryRow(r.Context(), `SELECT id,name,address,description,created_at,updated_at FROM properties WHERE account_id=$1 AND id=$2`, a, uuidParam(r)).Scan(&id, &name, &address, &description, &created, &updated)
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
	var in struct {
		Name        *string `json:"name"`
		Address     *string `json:"address"`
		Description *string `json:"description"`
	}
	if decode(r, &in) != nil {
		writeErr(w, 400, "invalid_request", "invalid property fields")
		return
	}
	a, _ := s.account(r)
	tag, e := s.DB.Exec(r.Context(), `UPDATE properties SET name=coalesce($3,name),address=coalesce($4,address),description=coalesce($5,description),updated_at=now() WHERE account_id=$1 AND id=$2`, a, uuidParam(r), in.Name, in.Address, in.Description)
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
	a, _ := s.account(r)
	tag, e := s.DB.Exec(r.Context(), `DELETE FROM properties WHERE account_id=$1 AND id=$2`, a, uuidParam(r))
	if e != nil {
		writeErr(w, 409, "in_use", "property or its units are still referenced by a lease")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "property not found")
		return
	}
	writeData(w, 200, map[string]string{"deleted": uuidParam(r)})
}
func (s *Server) listUnits(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	a, _ := s.account(r)
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
	a, _ := s.account(r)
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
	var in struct {
		Code     *string  `json:"code"`
		Bedrooms *int     `json:"bedrooms"`
		Rent     *float64 `json:"rent_amount"`
		Status   *string  `json:"status"`
	}
	if decode(r, &in) != nil || (in.Code != nil && !validUnitCode(*in.Code)) || (in.Status != nil && *in.Status != "vacant" && *in.Status != "occupied" && *in.Status != "maintenance") {
		writeErr(w, 400, "invalid_request", "invalid unit fields; unit code must be at most 12 characters")
		return
	}
	a, _ := s.account(r)
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
	a, _ := s.account(r)
	tag, e := s.DB.Exec(r.Context(), `DELETE FROM units WHERE account_id=$1 AND id=$2`, a, uuidParam(r))
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
	rows(w, r, `SELECT id,full_name,email,phone,national_id,created_at,updated_at FROM tenants WHERE account_id=$1 ORDER BY full_name`, p.AccountID)
}
func (s *Server) createTenant(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	var in struct {
		Name       string `json:"full_name"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		NationalID string `json:"national_id"`
		Password   string `json:"password"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") || len(in.Password) < 10 {
		writeErr(w, 400, "invalid_request", "full_name, valid email, and password of at least 10 characters are required")
		return
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database_error", "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	e = tx.QueryRow(r.Context(), `INSERT INTO tenants(account_id,full_name,email,phone,national_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, principal(r).AccountID, in.Name, strings.ToLower(in.Email), in.Phone, in.NationalID).Scan(&id)
	if e == nil {
		var hash []byte
		hash, e = bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO users(tenant_id,email,full_name,phone,password_hash,role) VALUES($1,lower($2),$3,$4,$5,'tenant')`, id, in.Email, in.Name, in.Phone, string(hash))
		}
	}
	if e != nil {
		writeErr(w, 409, "tenant_create_failed", "could not create tenant or linked login (email may already be registered)")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeErr(w, 500, "database_error", "could not save tenant")
		return
	}
	writeData(w, 201, map[string]any{"id": id, "full_name": in.Name, "email": in.Email, "phone": in.Phone, "national_id": in.NationalID})
}
func (s *Server) patchTenant(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
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
	a := principal(r).AccountID
	tag, e := s.DB.Exec(r.Context(), `UPDATE tenants SET full_name=coalesce($3,full_name),email=coalesce($4,email),phone=coalesce($5,phone),national_id=coalesce($6,national_id),updated_at=now() WHERE account_id=$1 AND id=$2`, a, uuidParam(r), in.Name, in.Email, in.Phone, in.NationalID)
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
	a := principal(r).AccountID
	tag, e := s.DB.Exec(r.Context(), `DELETE FROM tenants WHERE account_id=$1 AND id=$2`, a, uuidParam(r))
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
	rows(w, r, `SELECT l.id,l.unit_id,u.code AS unit_code,p.name AS property_name,l.tenant_id,t.full_name AS tenant_name,l.start_date,l.end_date,l.rent_amount,l.due_day,l.deposit_amount,l.status FROM leases l JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id JOIN properties p ON p.id=u.property_id AND p.account_id=l.account_id JOIN tenants t ON t.id=l.tenant_id AND t.account_id=l.account_id WHERE l.account_id=$1 ORDER BY l.start_date DESC`, p.AccountID)
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
	a := principal(r).AccountID
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
	a := principal(r).AccountID
	tag, e := s.DB.Exec(r.Context(), `UPDATE leases SET end_date=coalesce($3::date,end_date),rent_amount=coalesce($4,rent_amount),due_day=coalesce($5,due_day),deposit_amount=coalesce($6,deposit_amount),status=coalesce($7,status),updated_at=now() WHERE account_id=$1 AND id=$2`, a, uuidParam(r), in.EndDate, in.Rent, in.DueDay, in.Deposit, in.Status)
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
	a := p.AccountID
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
		writeData(w, 200, map[string]any{"outstanding": due, "paid": paid})
		return
	}
	if !s.manager(w, r) {
		return
	}
	a := p.AccountID
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

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role != "landlord" {
		writeErr(w, 403, "forbidden", "only the landlord can add agents")
		return
	}
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") || len(in.Password) < 10 {
		writeErr(w, 400, "invalid_request", "name, valid email, and password of at least 10 characters are required")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if e != nil {
		writeErr(w, 500, "internal", "could not secure password")
		return
	}
	var id string
	e = s.DB.QueryRow(r.Context(), `INSERT INTO users(account_id,email,full_name,phone,password_hash,role) VALUES($1,lower($2),$3,$4,$5,'agent') RETURNING id`, p.AccountID, in.Email, in.Name, in.Phone, string(hash)).Scan(&id)
	if e != nil {
		writeErr(w, 409, "agent_create_failed", "could not add agent (email may already be registered)")
		return
	}
	writeData(w, 201, map[string]any{"id": id, "name": in.Name, "email": strings.ToLower(in.Email), "phone": in.Phone, "role": "agent", "accountId": p.AccountID})
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
	if e := s.ensureInvoices(r, p.AccountID); e != nil {
		writeErr(w, 500, "invoice_generation_failed", "could not create monthly invoices")
		return
	}
	today := time.Now().In(time.FixedZone("EAT", 3*60*60)).Format("2006-01-02")
	rows(w, r, `SELECT i.id,i.tenant_id,t.full_name AS tenant_name,i.lease_id,i.period,i.due_date,i.amount,i.paid_amount,(i.amount-i.paid_amount) AS balance,($2::date-i.due_date) AS days_overdue,u.code AS unit_code FROM invoices i JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.account_id=$1 AND i.due_date<$2::date AND i.paid_amount<i.amount ORDER BY i.due_date`, p.AccountID, today)
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
