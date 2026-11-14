package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type stkResult struct {
	CheckoutRequestID   string `json:"CheckoutRequestID"`
	CustomerMessage     string `json:"CustomerMessage"`
	ResponseCode        string `json:"ResponseCode"`
	ResponseDescription string `json:"ResponseDescription"`
}

func (s *Server) darajaToken(ctx context.Context) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.darajaTokenValue != "" && time.Until(s.darajaTokenExpiry) > time.Minute {
		return s.darajaTokenValue, nil
	}
	if s.C.DarajaKey == "" || s.C.DarajaSecret == "" {
		return "", fmt.Errorf("Daraja sandbox credentials are not configured")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", s.C.DarajaBaseURL+"/oauth/v1/generate?grant_type=client_credentials", nil)
	if e != nil {
		return "", e
	}
	req.SetBasicAuth(s.C.DarajaKey, s.C.DarajaSecret)
	resp, e := s.HTTP.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	var v struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&v) != nil || v.AccessToken == "" {
		return "", fmt.Errorf("Daraja OAuth rejected request")
	}
	ttl, _ := strconv.Atoi(v.ExpiresIn)
	if ttl <= 0 {
		ttl = 3600
	}
	s.darajaTokenValue = v.AccessToken
	s.darajaTokenExpiry = time.Now().Add(time.Duration(ttl) * time.Second)
	return v.AccessToken, nil
}
func normalizePhone(v string) string {
	v = strings.TrimSpace(v)
	v = strings.NewReplacer("+", "", " ", "", "-", "", "(", "", ")", "").Replace(v)
	if strings.HasPrefix(v, "0") {
		return "254" + v[1:]
	}
	if strings.HasPrefix(v, "7") || strings.HasPrefix(v, "1") {
		return "254" + v
	}
	if strings.HasPrefix(v, "254") {
		return v
	}
	return v
}
func (s *Server) stkPush(w http.ResponseWriter, r *http.Request) {
	var in struct {
		InvoiceID string  `json:"invoice_id"`
		LeaseID   string  `json:"lease_id"`
		Phone     string  `json:"phone"`
		Amount    float64 `json:"amount"`
	}
	if decode(r, &in) != nil || in.Amount <= 0 || in.Amount != float64(int64(in.Amount)) {
		writeErr(w, 400, "invalid_request", "invoice_id, phone, and a positive whole-number amount are required")
		return
	}
	p := principal(r)
	phone := normalizePhone(in.Phone)
	var account, tenant, unitCode, leaseID string
	var rent float64
	if in.InvoiceID != "" {
		var e error
		if p.Role == "tenant" {
			e = s.DB.QueryRow(r.Context(), `SELECT i.account_id,i.tenant_id,u.code,l.id,l.rent_amount FROM invoices i JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.id=$1 AND i.tenant_id=$2`, in.InvoiceID, p.TenantID).Scan(&account, &tenant, &unitCode, &leaseID, &rent)
		} else {
			if !s.manager(w, r) {
				return
			}
			e = s.DB.QueryRow(r.Context(), `SELECT i.account_id,i.tenant_id,u.code,l.id,l.rent_amount FROM invoices i JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.id=$1 AND i.account_id=$2`, in.InvoiceID, p.AccountID).Scan(&account, &tenant, &unitCode, &leaseID, &rent)
		}
		if e != nil {
			writeErr(w, 404, "invoice_not_found", "invoice is not in your account")
			return
		}
	} else if in.LeaseID != "" {
		var e error
		if p.Role == "tenant" {
			e = s.DB.QueryRow(r.Context(), `SELECT l.account_id,l.tenant_id,u.code,l.id,l.rent_amount FROM leases l JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE l.tenant_id=$1 AND l.status='active' AND l.id=$2`, p.TenantID, in.LeaseID).Scan(&account, &tenant, &unitCode, &leaseID, &rent)
		} else {
			if !s.manager(w, r) {
				return
			}
			e = s.DB.QueryRow(r.Context(), `SELECT l.account_id,l.tenant_id,u.code,l.id,l.rent_amount FROM leases l JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE l.account_id=$1 AND l.status='active' AND l.id=$2`, p.AccountID, in.LeaseID).Scan(&account, &tenant, &unitCode, &leaseID, &rent)
		}
		if e != nil {
			writeErr(w, 404, "lease_not_found", "lease is not in your account")
			return
		}
	} else {
		writeErr(w, 400, "invalid_request", "invoice_id is required")
		return
	}
	if !validUnitCode(unitCode) {
		writeErr(w, 400, "invalid_unit_reference", "unit code for Daraja AccountReference must be at most 12 characters")
		return
	}
	if s.C.DarajaShortCode == "" || s.C.DarajaPasskey == "" || s.C.DarajaCallbackURL == "" {
		writeErr(w, 503, "payments_unconfigured", "Daraja sandbox is not fully configured")
		return
	}
	now := time.Now().In(time.FixedZone("EAT", 3*3600)).Format("20060102150405")
	password := base64.StdEncoding.EncodeToString([]byte(s.C.DarajaShortCode + s.C.DarajaPasskey + now))
	token, e := s.darajaToken(r.Context())
	if e != nil {
		writeErr(w, 502, "daraja_unavailable", "could not obtain sandbox access token")
		return
	}
	body := map[string]any{"BusinessShortCode": s.C.DarajaShortCode, "Password": password, "Timestamp": now, "TransactionType": "CustomerPayBillOnline", "Amount": int64(in.Amount), "PartyA": phone, "PartyB": s.C.DarajaShortCode, "PhoneNumber": phone, "CallBackURL": s.C.DarajaCallbackURL, "AccountReference": unitCode, "TransactionDesc": "Rent payment"}
	raw, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(r.Context(), "POST", s.C.DarajaBaseURL+"/mpesa/stkpush/v1/processrequest", bytes.NewReader(raw))
	if e != nil {
		writeErr(w, 500, "internal", "could not prepare payment")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := s.HTTP.Do(req)
	if e != nil {
		writeErr(w, 502, "daraja_unavailable", "sandbox payment request failed")
		return
	}
	defer resp.Body.Close()
	var out stkResult
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || json.NewDecoder(resp.Body).Decode(&out) != nil || out.CheckoutRequestID == "" {
		writeErr(w, 502, "daraja_rejected", "sandbox did not accept the STK push")
		return
	}
	var id string
	e = s.DB.QueryRow(r.Context(), `INSERT INTO payments(account_id,tenant_id,invoice_id,amount,method,status,phone,checkout_request_id,notes) VALUES($1,$2,NULLIF($3,'')::uuid,$4,'mpesa','pending',$5,$6,'STK push initiated') RETURNING id`, account, tenant, in.InvoiceID, in.Amount, phone, out.CheckoutRequestID).Scan(&id)
	if e != nil {
		writeErr(w, 500, "database_error", "could not record pending STK request")
		return
	}
	writeData(w, 202, map[string]any{"id": id, "status": "pending", "checkout_request_id": out.CheckoutRequestID, "message": out.CustomerMessage})
}

func (s *Server) stkCallback(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeDarajaCallback(w, r) {
		return
	}
	var body struct {
		Body struct {
			StkCallback struct {
				CheckoutRequestID string `json:"CheckoutRequestID"`
				ResultCode        int    `json:"ResultCode"`
				ResultDesc        string `json:"ResultDesc"`
				CallbackMetadata  struct {
					Item []struct {
						Name  string `json:"Name"`
						Value any    `json:"Value"`
					} `json:"Item"`
				} `json:"CallbackMetadata"`
			} `json:"stkCallback"`
		} `json:"Body"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSON(w, 400, map[string]string{"Result": "Invalid JSON"})
		return
	}
	c := body.Body.StkCallback
	values := map[string]any{}
	for _, v := range c.CallbackMetadata.Item {
		values[v.Name] = v.Value
	}
	if c.ResultCode == 0 {
		receipt := fmt.Sprint(values["MpesaReceiptNumber"])
		amount, _ := number(values["Amount"])
		phone := normalizePhone(fmt.Sprint(values["PhoneNumber"]))
		date := fmt.Sprint(values["TransactionDate"])
		if c.CheckoutRequestID == "" || receipt == "" || receipt == "<nil>" || amount <= 0 || phone == "" || phone == "<nil>" {
			s.Log.Warn("ignored incomplete successful STK callback", "checkout_request_id_present", c.CheckoutRequestID != "", "receipt_present", receipt != "" && receipt != "<nil>")
			writeJSON(w, 400, map[string]any{"ResultCode": 1, "ResultDesc": "Incomplete successful callback"})
			return
		}
		txdate := parseDarajaDate(date)
		tx, e := s.DB.Begin(r.Context())
		if e != nil {
			s.Log.Error("STK callback transaction could not start", "error", e)
			writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
			return
		}
		defer tx.Rollback(r.Context())
		var id, tenant, account, expectedPhone string
		var expectedAmount float64
		newReceipt := false
		e = tx.QueryRow(r.Context(), `SELECT id,coalesce(tenant_id::text,''),coalesce(account_id::text,''),amount,phone FROM payments WHERE checkout_request_id=$1 FOR UPDATE`, c.CheckoutRequestID).Scan(&id, &tenant, &account, &expectedAmount, &expectedPhone)
		if errors.Is(e, pgx.ErrNoRows) {
			s.Log.Warn("ignored STK callback with no pending checkout request")
			writeJSON(w, 200, map[string]any{"ResultCode": 0, "ResultDesc": "Unknown checkout request ignored"})
			return
		} else if e != nil {
			s.Log.Error("STK callback lookup failed", "error", e)
			writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
			return
		} else if amount != expectedAmount || phone != normalizePhone(expectedPhone) {
			s.Log.Warn("ignored STK callback with metadata that did not match its pending request")
			writeJSON(w, 200, map[string]any{"ResultCode": 1, "ResultDesc": "Payment details did not match the pending request"})
			return
		} else { // repeated success callbacks are harmless
			tag, upErr := tx.Exec(r.Context(), `UPDATE payments SET status='completed',external_ref=$2,transaction_date=$3,phone=$4,notes=$5 WHERE id=$1 AND status='pending' AND NOT EXISTS(SELECT 1 FROM payments WHERE external_ref=$2)`, id, receipt, txdate, phone, c.ResultDesc)
			if upErr != nil {
				s.Log.Error("STK payment could not be completed", "error", upErr)
				writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
				return
			}
			if tag.RowsAffected() == 1 && tenant != "" {
				if upErr = allocateWithin(r.Context(), tx, id, account, tenant, amount); upErr != nil {
					s.Log.Error("STK payment allocation failed", "error", upErr)
					writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
					return
				}
				newReceipt = true
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			s.Log.Error("STK callback could not commit", "error", e)
			writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
			return
		}
		if newReceipt {
			s.sendPaymentReceipt(r.Context(), account, tenant, amount, receipt)
		}
	} else {
		_, _ = s.DB.Exec(r.Context(), `UPDATE payments SET status='failed',notes=$2 WHERE checkout_request_id=$1 AND status='pending'`, c.CheckoutRequestID, c.ResultDesc)
	}
	writeJSON(w, 200, map[string]any{"ResultCode": 0, "ResultDesc": "Accepted"})
}
func number(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, fmt.Errorf("not a number")
	}
}
func parseDarajaDate(v string) time.Time {
	if t, e := time.ParseInLocation("20060102150405", v, time.FixedZone("EAT", 3*3600)); e == nil {
		return t
	}
	return time.Now()
}
func (s *Server) matchesDarajaShortCode(value any) bool {
	configured := strings.TrimSpace(s.C.DarajaShortCode)
	received := strings.TrimSpace(fmt.Sprint(value))
	return configured != "" && received != "<nil>" && received == configured
}

func (s *Server) c2bValidation(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeDarajaCallback(w, r) {
		return
	}
	var in struct {
		BusinessShortCode any `json:"BusinessShortCode"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || !s.matchesDarajaShortCode(in.BusinessShortCode) {
		writeJSON(w, 200, map[string]any{"ResultCode": 1, "ResultDesc": "Business shortcode is invalid"})
		return
	}
	writeJSON(w, 200, map[string]any{"ResultCode": 0, "ResultDesc": "Accepted"})
}
func (s *Server) c2bConfirmation(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeDarajaCallback(w, r) {
		return
	}
	var in struct {
		TransID           string `json:"TransID"`
		TransAmount       string `json:"TransAmount"`
		BillRefNumber     string `json:"BillRefNumber"`
		MSISDN            string `json:"MSISDN"`
		TransactionDate   string `json:"TransactionDate"`
		BusinessShortCode any    `json:"BusinessShortCode"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.TransID) == "" || !s.matchesDarajaShortCode(in.BusinessShortCode) {
		writeJSON(w, 200, map[string]any{"ResultCode": 1, "ResultDesc": "Invalid callback or business shortcode"})
		return
	}
	amount, amountErr := strconv.ParseFloat(strings.TrimSpace(in.TransAmount), 64)
	if amountErr != nil || amount <= 0 {
		writeJSON(w, 200, map[string]any{"ResultCode": 1, "ResultDesc": "Invalid transaction amount"})
		return
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
		return
	}
	defer tx.Rollback(r.Context())
	var exists string
	e = tx.QueryRow(r.Context(), `SELECT id FROM payments WHERE external_ref=$1`, in.TransID).Scan(&exists)
	if e == nil {
		if e = tx.Commit(r.Context()); e != nil {
			writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
			return
		}
		writeJSON(w, 200, map[string]any{"ResultCode": 0, "ResultDesc": "Duplicate accepted"})
		return
	} else if !errors.Is(e, pgx.ErrNoRows) {
		s.Log.Error("C2B duplicate check failed", "error", e)
		writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
		return
	}
	phone := normalizePhone(in.MSISDN)
	var account, tenant, storedPhone string
	_ = tx.QueryRow(r.Context(), `SELECT l.account_id,l.tenant_id,t.phone FROM leases l JOIN units u ON u.id=l.unit_id JOIN tenants t ON t.id=l.tenant_id WHERE u.code=$1 AND l.status='active' AND (SELECT count(*) FROM leases l2 JOIN units u2 ON u2.id=l2.unit_id WHERE u2.code=$1 AND l2.status='active')=1 ORDER BY l.created_at DESC LIMIT 1`, in.BillRefNumber).Scan(&account, &tenant, &storedPhone)
	matchedTenant := tenant != ""
	if !matchedTenant {
		tenant = ""
	}
	var id string
	status := "unmatched"
	note := "C2B transaction requires manual review"
	if matchedTenant {
		status = "completed"
		note = "C2B confirmation"
	}
	if account != "" {
		e = tx.QueryRow(r.Context(), `INSERT INTO payments(account_id,tenant_id,amount,method,status,external_ref,phone,transaction_date,notes) VALUES($1,NULLIF($2,'')::uuid,$3,'mpesa',$4,$5,$6,$7,$8) ON CONFLICT(external_ref) DO NOTHING RETURNING id`, account, tenant, amount, status, in.TransID, phone, parseDarajaDate(in.TransactionDate), note).Scan(&id)
		if e == nil && matchedTenant {
			e = allocateWithin(r.Context(), tx, id, account, tenant, amount)
		}
	} else {
		_, e = tx.Exec(r.Context(), `INSERT INTO payments(amount,method,status,external_ref,phone,transaction_date,notes) VALUES($1,'mpesa','unmatched',$2,$3,$4,'C2B transaction requires manual review') ON CONFLICT(external_ref) DO NOTHING`, amount, in.TransID, phone, parseDarajaDate(in.TransactionDate))
	}
	if e != nil {
		s.Log.Error("C2B payment could not be stored", "error", e)
		writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		s.Log.Error("C2B payment could not commit", "error", e)
		writeJSON(w, 503, map[string]any{"ResultCode": 1, "ResultDesc": "Temporary processing failure"})
		return
	}
	if matchedTenant && id != "" {
		s.sendPaymentReceipt(r.Context(), account, tenant, amount, in.TransID)
	}
	writeJSON(w, 200, map[string]any{"ResultCode": 0, "ResultDesc": "Accepted"})
}
func allocateWithin(ctx context.Context, tx pgx.Tx, payment, account, tenant string, amount float64) error {
	rows, e := tx.Query(ctx, `SELECT id,amount-paid_amount FROM invoices WHERE account_id=$1 AND tenant_id=$2 AND amount>paid_amount ORDER BY due_date,id FOR UPDATE`, account, tenant)
	if e != nil {
		return e
	}
	type dueInvoice struct {
		id      string
		balance float64
	}
	due := []dueInvoice{}
	for rows.Next() {
		var d dueInvoice
		if e = rows.Scan(&d.id, &d.balance); e != nil {
			rows.Close()
			return e
		}
		due = append(due, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	remaining := amount
	for _, d := range due {
		if remaining <= 0 {
			break
		}
		applied := remaining
		if applied > d.balance {
			applied = d.balance
		}
		if _, e = tx.Exec(ctx, `UPDATE invoices SET paid_amount=paid_amount+$2,status=CASE WHEN paid_amount+$2>=amount THEN 'paid' ELSE 'partial' END WHERE id=$1`, d.id, applied); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO payment_allocations(account_id,payment_id,invoice_id,amount) VALUES($1,$2,$3,$4) ON CONFLICT(account_id,payment_id,invoice_id) DO NOTHING`, account, payment, d.id, applied); e != nil {
			return e
		}
		remaining -= applied
	}
	if remaining > 0 {
		_, e = tx.Exec(ctx, `INSERT INTO tenant_credits(tenant_id,account_id,balance) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO UPDATE SET balance=tenant_credits.balance+EXCLUDED.balance,updated_at=now()`, tenant, account, remaining)
	}
	return e
}
func (s *Server) listPayments(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Role == "tenant" {
		rows(w, r, `SELECT p.id,p.tenant_id,coalesce(t.full_name,'') AS tenant_name,coalesce((SELECT u.code FROM payment_allocations pa JOIN invoices i ON i.id=pa.invoice_id AND i.account_id=pa.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE pa.payment_id=p.id AND pa.account_id=(SELECT account_id FROM tenants WHERE id=$1) ORDER BY i.due_date DESC LIMIT 1),'') AS unit_code,p.amount,p.method,p.status,coalesce(p.external_ref,p.checkout_request_id,'') AS receipt_number,coalesce(p.transaction_date,p.created_at) AS payment_date FROM payments p LEFT JOIN tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 ORDER BY coalesce(p.transaction_date,p.created_at) DESC`, p.TenantID)
		return
	}
	if !s.manager(w, r) {
		return
	}
	rows(w, r, `SELECT p.id,p.tenant_id,coalesce(t.full_name,'') AS tenant_name,coalesce((SELECT u.code FROM payment_allocations pa JOIN invoices i ON i.id=pa.invoice_id AND i.account_id=pa.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE pa.payment_id=p.id AND pa.account_id=p.account_id ORDER BY i.due_date DESC LIMIT 1),'') AS unit_code,p.amount,p.method,p.status,coalesce(p.external_ref,p.checkout_request_id,'') AS receipt_number,coalesce(p.transaction_date,p.created_at) AS payment_date FROM payments p LEFT JOIN tenants t ON t.id=p.tenant_id AND t.account_id=p.account_id WHERE p.account_id=$1 ORDER BY coalesce(p.transaction_date,p.created_at) DESC`, p.AccountID)
}
func (s *Server) unmatched(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	rows(w, r, `SELECT p.id,p.tenant_id,coalesce(t.full_name,'') AS tenant_name,'' AS unit_code,p.amount,p.method,p.status,coalesce(p.external_ref,p.checkout_request_id,'') AS receipt_number,p.phone,coalesce(p.transaction_date,p.created_at) AS payment_date,p.notes FROM payments p LEFT JOIN tenants t ON t.id=p.tenant_id AND t.account_id=p.account_id WHERE p.account_id=$1 AND p.status='unmatched' ORDER BY coalesce(p.transaction_date,p.created_at) DESC`, principal(r).AccountID)
}
func (s *Server) assignPayment(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	var in struct {
		UnitID string `json:"unit_id"`
	}
	if decode(r, &in) != nil || in.UnitID == "" {
		writeErr(w, 400, "invalid_request", "unit_id is required")
		return
	}
	account := principal(r).AccountID
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database_error", "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	var tenant string
	var amount float64
	e = tx.QueryRow(r.Context(), `SELECT coalesce(tenant_id::text,''),amount FROM payments WHERE account_id=$1 AND id=$2 AND status='unmatched' FOR UPDATE`, account, chi.URLParam(r, "id")).Scan(&tenant, &amount)
	if e != nil {
		writeErr(w, 404, "not_found", "unmatched payment not found")
		return
	}
	var targetTenant string
	e = tx.QueryRow(r.Context(), `SELECT tenant_id FROM leases WHERE account_id=$1 AND unit_id=$2 AND status='active'`, account, in.UnitID).Scan(&targetTenant)
	if e != nil {
		writeErr(w, 404, "active_lease_not_found", "selected unit has no active lease in this account")
		return
	}
	if tenant != "" && targetTenant != tenant {
		writeErr(w, 400, "tenant_mismatch", "selected unit belongs to a different tenant")
		return
	}
	_, e = tx.Exec(r.Context(), `UPDATE payments SET tenant_id=$3,status='completed',notes='Manually assigned after review' WHERE account_id=$1 AND id=$2`, account, chi.URLParam(r, "id"), targetTenant)
	if e == nil {
		e = allocateWithin(r.Context(), tx, chi.URLParam(r, "id"), account, targetTenant, amount)
	}
	if e != nil || tx.Commit(r.Context()) != nil {
		writeErr(w, 500, "allocation_failed", "could not assign payment")
		return
	}
	s.sendPaymentReceipt(r.Context(), account, targetTenant, amount, chi.URLParam(r, "id"))
	writeData(w, 200, map[string]string{"id": chi.URLParam(r, "id"), "status": "completed", "tenant_id": targetTenant, "unit_id": in.UnitID})
}
func (s *Server) collectionsCSV(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().In(time.FixedZone("EAT", 3*3600)).Format("2006-01")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_month", "month must use YYYY-MM")
		return
	}
	account := principal(r).AccountID
	rs, err := s.DB.Query(r.Context(), `SELECT to_char(COALESCE(p.transaction_date,p.created_at) AT TIME ZONE 'Africa/Nairobi','YYYY-MM-DD'),coalesce(t.full_name,''),p.amount::text,p.method,coalesce(p.external_ref,'') FROM payments p LEFT JOIN tenants t ON t.id=p.tenant_id AND t.account_id=p.account_id WHERE p.account_id=$1 AND p.status='completed' AND (COALESCE(p.transaction_date,p.created_at) AT TIME ZONE 'Africa/Nairobi')::date >= $2::date AND (COALESCE(p.transaction_date,p.created_at) AT TIME ZONE 'Africa/Nairobi')::date < ($2::date+interval '1 month') ORDER BY COALESCE(p.transaction_date,p.created_at)`, account, month+"-01")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "database_error", "could not generate report")
		return
	}
	defer rs.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=collections-"+month+".csv")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "tenant", "amount_kes", "method", "mpesa_reference"})
	for rs.Next() {
		var date, tenant, amount, method, ref string
		if err = rs.Scan(&date, &tenant, &amount, &method, &ref); err != nil {
			s.Log.Error("collections export row failed", "error", err)
			break
		}
		if err = cw.Write([]string{date, tenant, amount, method, ref}); err != nil {
			s.Log.Error("collections export write failed", "error", err)
			break
		}
	}
	if err = rs.Err(); err != nil {
		s.Log.Error("collections export query failed", "error", err)
	}
	cw.Flush()
	if err = cw.Error(); err != nil {
		s.Log.Error("collections export flush failed", "error", err)
	}
}

func (s *Server) arrearsCSV(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	account := principal(r).AccountID
	rs, err := s.DB.Query(r.Context(), `SELECT t.full_name,t.phone,u.code,i.period::text,i.due_date::text,i.amount::text,i.paid_amount::text,(i.amount-i.paid_amount)::text,(current_date-i.due_date)::text FROM invoices i JOIN tenants t ON t.id=i.tenant_id AND t.account_id=i.account_id JOIN leases l ON l.id=i.lease_id AND l.account_id=i.account_id JOIN units u ON u.id=l.unit_id AND u.account_id=l.account_id WHERE i.account_id=$1 AND i.due_date<current_date AND i.paid_amount<i.amount ORDER BY i.due_date`, account)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "database_error", "could not generate report")
		return
	}
	defer rs.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=arrears.csv")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"tenant", "phone", "unit", "period", "due_date", "amount_kes", "paid_kes", "balance_kes", "days_overdue"})
	for rs.Next() {
		var tenant, phone, unit, period, due, amount, paid, balance, days string
		if err = rs.Scan(&tenant, &phone, &unit, &period, &due, &amount, &paid, &balance, &days); err != nil {
			s.Log.Error("arrears export row failed", "error", err)
			break
		}
		if err = cw.Write([]string{tenant, phone, unit, period, due, amount, paid, balance, days}); err != nil {
			s.Log.Error("arrears export write failed", "error", err)
			break
		}
	}
	if err = rs.Err(); err != nil {
		s.Log.Error("arrears export query failed", "error", err)
	}
	cw.Flush()
	if err = cw.Error(); err != nil {
		s.Log.Error("arrears export flush failed", "error", err)
	}
}

func (s *Server) registerC2B(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	if s.C.DarajaShortCode == "" || s.C.DarajaValidationURL == "" || s.C.DarajaConfirmationURL == "" {
		writeErr(w, 503, "payments_unconfigured", "sandbox C2B URLs and shortcode must be configured")
		return
	}
	token, e := s.darajaToken(r.Context())
	if e != nil {
		writeErr(w, 502, "daraja_unavailable", "could not obtain sandbox access token")
		return
	}
	payload := map[string]string{"ShortCode": s.C.DarajaShortCode, "ResponseType": "Completed", "ConfirmationURL": s.C.DarajaConfirmationURL, "ValidationURL": s.C.DarajaValidationURL}
	raw, _ := json.Marshal(payload)
	req, e := http.NewRequestWithContext(r.Context(), "POST", s.C.DarajaBaseURL+"/mpesa/c2b/v1/registerurl", bytes.NewReader(raw))
	if e != nil {
		writeErr(w, 500, "internal", "could not prepare URL registration")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := s.HTTP.Do(req)
	if e != nil {
		writeErr(w, 502, "daraja_unavailable", "sandbox URL registration request failed")
		return
	}
	defer resp.Body.Close()
	var result map[string]any
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || json.NewDecoder(resp.Body).Decode(&result) != nil {
		writeErr(w, 502, "daraja_rejected", "sandbox rejected C2B URL registration")
		return
	}
	writeData(w, 200, result)
}

func (s *Server) recordPayment(w http.ResponseWriter, r *http.Request) {
	if !s.manager(w, r) {
		return
	}
	var in struct {
		TenantID string  `json:"tenant_id"`
		Amount   float64 `json:"amount"`
		Method   string  `json:"method"`
		Notes    string  `json:"notes"`
	}
	if decode(r, &in) != nil || in.TenantID == "" || in.Amount <= 0 {
		writeErr(w, 400, "invalid_request", "tenant_id and positive amount are required")
		return
	}
	in.Method = strings.ToLower(in.Method)
	if in.Method == "" {
		in.Method = "cash"
	}
	if in.Method != "cash" && in.Method != "bank" && in.Method != "other" {
		writeErr(w, 400, "invalid_method", "manual payment method must be cash, bank, or other")
		return
	}
	account := principal(r).AccountID
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database_error", "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	e = tx.QueryRow(r.Context(), `INSERT INTO payments(account_id,tenant_id,amount,method,status,notes) SELECT account_id,id,$3,$4,'completed',$5 FROM tenants WHERE account_id=$1 AND id=$2 RETURNING id`, account, in.TenantID, in.Amount, in.Method, in.Notes).Scan(&id)
	if e != nil {
		writeErr(w, 404, "tenant_not_found", "tenant is not part of your account")
		return
	}
	if e = allocateWithin(r.Context(), tx, id, account, in.TenantID, in.Amount); e != nil {
		writeErr(w, 500, "allocation_failed", "could not allocate payment")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeErr(w, 500, "database_error", "could not record payment")
		return
	}
	s.sendPaymentReceipt(r.Context(), account, in.TenantID, in.Amount, id)
	writeData(w, 201, map[string]any{"id": id, "tenant_id": in.TenantID, "amount": in.Amount, "method": in.Method, "status": "completed"})
}

func (s *Server) sendPaymentReceipt(ctx context.Context, account, tenant string, amount float64, reference string) {
	var phone string
	if err := s.DB.QueryRow(ctx, `SELECT phone FROM tenants WHERE account_id=$1 AND id=$2`, account, tenant).Scan(&phone); err != nil || phone == "" {
		return
	}
	message := fmt.Sprintf("RentFlow receipt: KES %.2f received. Reference: %s.", amount, reference)
	if err := s.SMS.Send(ctx, []string{normalizePhone(phone)}, message); err != nil {
		s.Log.Warn("payment receipt SMS could not be sent", "account_id", account, "tenant_id", tenant, "error", err)
	}
}
