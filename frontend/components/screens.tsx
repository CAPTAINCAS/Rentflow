"use client";

import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, type Resolver } from "react-hook-form";
import { api, apiUrl, asRecord, collection, firstValue, stringValue, type JsonRecord } from "@/lib/api";
import { formatDate, formatMoney } from "@/lib/format";
import { buildTenantLedger } from "@/lib/ledger";
import { useSession } from "@/components/app-shell";
import { EmptyState, ErrorState, Field, Loading, StatusPill } from "@/components/ui";

type ScreenKind = "dashboard" | "properties" | "units" | "tenants" | "leases" | "ledger" | "payments" | "review" | "reports" | "tenant" | "tenantPayments" | "maintenance" | "maintenanceDetail" | "maintenanceManage";
type SelectSource = "properties" | "units" | "tenants";
type FieldSpec = { name: string; label: string; type?: string; required?: boolean; hint?: string; minLength?: number; select?: SelectSource | "unitStatus"; editable?: boolean };
type EntitySpec = { title: string; singular: string; endpoint: string; key: string; fields: FieldSpec[]; columns: string[]; editable: boolean; deletable: boolean };
type FormOptions = { properties: JsonRecord[]; units: JsonRecord[]; tenants: JsonRecord[] };

const specs: Record<string, EntitySpec> = {
  properties: {
    title: "Properties", singular: "property", endpoint: "/properties", key: "properties",
    fields: [
      { name: "name", label: "Property name", required: true },
      { name: "address", label: "Address" },
      { name: "description", label: "Notes" },
    ],
    columns: ["name", "address", "created_at"], editable: true, deletable: true,
  },
  units: {
    title: "Units", singular: "unit", endpoint: "/units", key: "units",
    fields: [
      { name: "property_id", label: "Property", required: true, select: "properties", editable: false },
      { name: "code", label: "Unit code", required: true, hint: "Use a short code such as A12; it may be the M-Pesa account reference." },
      { name: "rent_amount", label: "Monthly rent (KES)", type: "number", required: true },
      { name: "status", label: "Status", required: true, select: "unitStatus" },
    ],
    columns: ["code", "property_name", "rent_amount", "status"], editable: true, deletable: true,
  },
  tenants: {
    title: "Tenants", singular: "tenant", endpoint: "/tenants", key: "tenants",
    fields: [
      { name: "full_name", label: "Full name", required: true },
      { name: "email", label: "Email address", type: "email", required: true },
      { name: "phone", label: "Phone", type: "tel" },
      { name: "national_id", label: "National ID" },
      { name: "password", label: "Initial tenant password", type: "password", required: true, minLength: 12, hint: "12–128 characters. Creates a tenant login. Share the initial password securely." },
    ],
    columns: ["full_name", "phone", "email"], editable: true, deletable: true,
  },
  leases: {
    title: "Leases", singular: "lease", endpoint: "/leases", key: "leases",
    fields: [
      { name: "unit_id", label: "Vacant unit", required: true, select: "units", editable: false },
      { name: "tenant_id", label: "Tenant", required: true, select: "tenants", editable: false },
      { name: "start_date", label: "Start date", type: "date", required: true, editable: false },
      { name: "rent_amount", label: "Monthly rent (KES)", type: "number", required: true },
      { name: "due_day", label: "Rent due day (1–31)", type: "number", required: true },
      { name: "deposit_amount", label: "Deposit (KES)", type: "number", required: true },
      { name: "end_date", label: "End date", type: "date" },
    ],
    columns: ["unit_code", "tenant_name", "start_date", "rent_amount", "due_day", "deposit_amount", "status"], editable: true, deletable: false,
  },
};

const labels: Record<string, string> = {
  name: "Name", full_name: "Tenant", email: "Email", phone: "Phone", address: "Address", code: "Unit",
  unit_code: "Unit", property_name: "Property", tenant_name: "Tenant", rent_amount: "Monthly rent",
  amount: "Amount", paid_amount: "Paid", balance: "Balance", status: "Status", start_date: "Start date",
  due_date: "Due date", created_at: "Date", payment_date: "Date", method: "Method", receipt_number: "Receipt",
  deposit_amount: "Deposit", national_id: "ID", period: "Period", description: "Description", id: "Reference",
  running_balance: "Running balance", entry_type: "Entry", due_day: "Due day", days_overdue: "Days overdue",
};

function labelOf(key: string) { return labels[key] ?? key.replaceAll("_", " ").replace(/^./, c => c.toUpperCase()); }
function rowValue(row: JsonRecord, key: string): unknown {
  const aliases: Record<string, string[]> = {
    full_name: ["full_name", "name", "tenant_name"],
    name: ["name", "full_name", "tenant_name", "property_name"],
    tenant_name: ["tenant_name", "full_name", "tenant", "name"],
    unit_code: ["unit_code", "code", "unit_name"],
    rent_amount: ["rent_amount", "monthly_rent"],
    balance: ["balance", "outstanding_amount", "amount_due"],
    amount: ["amount", "amount_kes", "total"],
    paid_amount: ["paid_amount", "amount_paid", "collected"],
    status: ["status", "state"],
    created_at: ["created_at", "date", "created"],
    payment_date: ["payment_date", "transaction_date", "received_at", "created_at", "date"],
    receipt_number: ["receipt_number", "external_ref", "mpesa_receipt", "receipt", "reference"],
  };
  return firstValue(row, aliases[key] ?? [key]);
}
function renderValue(row: JsonRecord, key: string) {
  const value = key === "_date" ? row._date : rowValue(row, key);
  if (value === undefined || value === null || value === "") return "—";
  if (/amount|balance|rent|deposit|paid/.test(key)) return formatMoney(value);
  if (/date|_at$/.test(key)) return formatDate(value);
  if (key === "status") return <StatusPill value={value} />;
  if (key === "days_overdue") return `${stringValue(value)} days`;
  if (typeof value === "object") return stringValue(asRecord(value)?.name) || "—";
  return stringValue(value);
}
function Header({ eyebrow, title, description, action }: { eyebrow: string; title: string; description: string; action?: ReactNode }) {
  return <header className="page-header"><div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1><p>{description}</p></div>{action && <div className="header-actions">{action}</div>}</header>;
}
function Panel({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return <section className="panel"><div className="panel-head"><h2>{title}</h2>{aside}</div>{children}</section>;
}
function Table({ rows, columns, emptyTitle, emptyDetail, actions }: { rows: JsonRecord[]; columns: string[]; emptyTitle: string; emptyDetail: string; actions?: (row: JsonRecord) => ReactNode }) {
  return <div className="table-wrap">{rows.length === 0 ? <EmptyState title={emptyTitle} detail={emptyDetail} /> : <table><thead><tr>{columns.map(column => <th key={column}>{labelOf(column)}</th>)}{actions && <th>Actions</th>}</tr></thead><tbody>{rows.map((row, index) => <tr key={stringValue(row.id) || String(index)}>{columns.map(column => <td key={column}>{renderValue(row, column)}</td>)}{actions && <td>{actions(row)}</td>}</tr>)}</tbody></table>}</div>;
}
function useList(key: string, endpoint: string, resourceKey: string) {
  return useQuery<JsonRecord[]>({ queryKey: [key], queryFn: async () => collection(await api(endpoint), resourceKey) });
}
function QueryError({ error, retry }: { error: unknown; retry: () => void }) {
  return <ErrorState message={(error as Error)?.message || "The API could not return this information."} onRetry={retry} />;
}
function Metric({ label, value, note }: { label: string; value: string; note?: string }) {
  return <div className="metric"><span className="metric-label">{label}</span><span className="metric-value">{value}</span>{note && <span className="metric-note">{note}</span>}</div>;
}
function metricValue(source: JsonRecord, keys: string[]) { return firstValue(source, keys); }

function Dashboard() {
  const query = useQuery<JsonRecord>({ queryKey: ["dashboard"], queryFn: () => api<JsonRecord>("/dashboard") });
  if (query.isPending) return <><Header eyebrow="PROPERTY OPERATIONS" title="Dashboard" description="A live view of rent due, received and still needing attention." /><Loading label="Loading dashboard" /></>;
  if (query.isError) return <><Header eyebrow="PROPERTY OPERATIONS" title="Dashboard" description="A live view of rent due, received and still needing attention." /><QueryError error={query.error} retry={() => query.refetch()} /></>;
  const data = query.data ?? {};
  const expected = metricValue(data, ["expected_this_month"]);
  const collected = metricValue(data, ["collected_this_month"]);
  const rate = metricValue(data, ["collection_rate"]);
  const arrears = metricValue(data, ["arrears_total"]);
  const vacant = metricValue(data, ["vacant_units"]);
  const recent = collection(firstValue(data, ["recent_payments"]), "payments");
  return <>
    <Header eyebrow="PROPERTY OPERATIONS" title="Dashboard" description="A live view of rent due, received and still needing attention." action={<Link className="button button-primary" href="/payments/review">Review unmatched</Link>} />
    <div className="summary-band">
      <Metric label="Expected this month" value={formatMoney(expected)} />
      <Metric label="Collected this month" value={formatMoney(collected)} note="Completed payments recorded this month" />
      <Metric label="Collection rate" value={rate === undefined ? "—" : `${Number(rate).toFixed(0)}%`} />
      <Metric label="Arrears total" value={formatMoney(arrears)} note={`${stringValue(vacant) || "0"} vacant units`} />
    </div>
    <div className="content-grid">
      <div><div className="section-heading"><h2>Recent payments</h2><Link className="text-link" href="/payments">View payment history</Link></div>
        <Panel title="Latest recorded transactions" aside={<span className="muted">Live API records</span>}>
          <Table rows={recent} columns={["payment_date", "tenant_name", "unit_code", "amount", "status", "receipt_number"]} emptyTitle="No payments recorded" emptyDetail="Payments will appear here once recorded by the API." />
        </Panel>
      </div>
      <aside><div className="section-heading"><h2>Work areas</h2></div><div className="quick-links">{[["/properties", "Manage properties"], ["/units", "Check unit occupancy"], ["/tenants", "Tenant directory"], ["/leases", "Lease register"], ["/ledger", "Invoice ledger and arrears"], ["/reports", "Download reports"]].map(([href, text]) => <Link className="quick-link" href={href} key={href}><span>{text}</span><span aria-hidden="true">→</span></Link>)}</div><p className="side-note" style={{ marginTop: 18 }}><strong>Keep the record clear.</strong><br />Payments without a safely matched unit stay in review where they can be assigned within your account.</p></aside>
    </div>
  </>;
}

function formOptions(): Promise<FormOptions> {
  return Promise.all([api("/properties"), api("/units"), api("/tenants")]).then(([properties, units, tenants]) => ({
    properties: collection(properties, "properties"),
    units: collection(units, "units"),
    tenants: collection(tenants, "tenants"),
  }));
}
function optionsFor(field: FieldSpec, options: FormOptions): { value: string; label: string; disabled?: boolean }[] {
  if (field.select === "unitStatus") return [{ value: "vacant", label: "Vacant" }, { value: "maintenance", label: "Maintenance" }, { value: "occupied", label: "Occupied (set by an active lease)", disabled: true }];
  if (field.select === "properties") return options.properties.map(row => ({ value: stringValue(row.id), label: stringValue(row.name) || stringValue(row.id) })).filter(item => item.value);
  if (field.select === "units") return options.units.filter(row => stringValue(row.status).toLowerCase() === "vacant").map(row => ({ value: stringValue(row.id), label: `${stringValue(row.code)} · ${stringValue(row.property_name)}` })).filter(item => item.value);
  if (field.select === "tenants") return options.tenants.map(row => ({ value: stringValue(row.id), label: stringValue(row.full_name) || stringValue(row.id) })).filter(item => item.value);
  return [];
}

function EntityForm({ spec, initial, options, optionsLoading, onCancel, onSaved }: { spec: EntitySpec; initial?: JsonRecord; options: FormOptions; optionsLoading: boolean; onCancel: () => void; onSaved: (values: Record<string, string>) => void }) {
  const fields = useMemo(() => spec.fields.filter(field => !(initial && (field.name === "password" || field.editable === false))), [initial, spec]);
  const schema = useMemo(() => z.object(Object.fromEntries(fields.map(field => [field.name, field.required ? z.string().trim().min(field.minLength ?? 1, field.minLength ? `Use at least ${field.minLength} characters` : `${field.label} is required`) : z.string()]))), [fields]);
  const form = useForm<Record<string, string>>({ resolver: zodResolver(schema) as Resolver<Record<string, string>>, defaultValues: Object.fromEntries(fields.map(field => [field.name, stringValue(initial?.[field.name])])) });
  return <form className="inline-form" onSubmit={form.handleSubmit(onSaved)}><div className="form-grid">
    {fields.map(field => <Field key={field.name} label={field.label} hint={field.hint} error={form.formState.errors[field.name]?.message as string | undefined}>
      {field.select ? <select disabled={optionsLoading} {...form.register(field.name)}><option value="">{optionsLoading ? "Loading choices…" : "Choose an option"}</option>{optionsFor(field, options).map(option => <option key={option.value} value={option.value} disabled={option.disabled}>{option.label}</option>)}</select>
        : <input type={field.type ?? "text"} min={field.type === "number" ? "0" : undefined} max={field.name === "due_day" ? "31" : undefined} step={field.type === "number" ? "any" : undefined} autoComplete={field.type === "password" ? "new-password" : undefined} {...form.register(field.name)} />}
    </Field>)}
  </div><div className="form-actions"><button type="button" className="button button-outline" onClick={onCancel}>Cancel</button><button className="button button-primary" disabled={form.formState.isSubmitting}>{initial ? "Save changes" : `Add ${spec.singular}`}</button></div></form>;
}

function EntityManager({ name }: { name: keyof typeof specs }) {
  const spec = specs[name];
  const query = useList(spec.key, spec.endpoint, spec.key);
  const client = useQueryClient();
  const [editing, setEditing] = useState<JsonRecord | null | "new">(null);
  const [error, setError] = useState("");
  const needsOptions = name === "units" || name === "leases";
  const optionsQuery = useQuery<FormOptions>({ queryKey: ["form-options"], queryFn: formOptions, enabled: needsOptions });
  const options = optionsQuery.data ?? { properties: [], units: [], tenants: [] };
  const save = useMutation({
    mutationFn: ({ values, id }: { values: Record<string, string>; id?: string }) => {
      const body: JsonRecord = {};
      for (const [key, value] of Object.entries(values)) {
        const field = spec.fields.find(item => item.name === key);
        body[key] = value === "" ? null : field?.type === "number" ? Number(value) : value;
      }
      return api(id ? `${spec.endpoint}/${encodeURIComponent(id)}` : spec.endpoint, { method: id ? "PATCH" : "POST", body: JSON.stringify(body) });
    },
    onSuccess: async () => {
      setError(""); setEditing(null);
      await Promise.all([client.invalidateQueries({ queryKey: [spec.key] }), client.invalidateQueries({ queryKey: ["form-options"] }), client.invalidateQueries({ queryKey: ["dashboard"] })]);
    },
    onError: error => setError((error as Error).message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api(`${spec.endpoint}/${encodeURIComponent(id)}`, { method: "DELETE" }),
    onSuccess: async () => { await client.invalidateQueries({ queryKey: [spec.key] }); await client.invalidateQueries({ queryKey: ["form-options"] }); },
    onError: error => setError((error as Error).message),
  });
  const onSaved = (values: Record<string, string>) => {
    const id = editing && editing !== "new" ? stringValue(editing.id) : undefined;
    save.mutate({ values, id });
  };
  const initial = editing && editing !== "new" ? editing : undefined;
  return <>
    <Header eyebrow="RECORDS" title={spec.title} description={`Manage live ${spec.title.toLowerCase()} records. Changes are saved through the API.`} action={<button className="button button-primary" onClick={() => { setError(""); setEditing("new"); }}>＋ Add {spec.singular}</button>} />
    {error && <div className="notice notice-error" role="alert">{error}</div>}
    <Panel title={`${spec.title} register`} aside={<span className="muted">{query.data ? `${query.data.length} records` : "Live records"}</span>}>
      {editing && <EntityForm key={editing === "new" ? "new" : stringValue(editing.id)} spec={spec} initial={initial} options={options} optionsLoading={needsOptions && optionsQuery.isPending} onCancel={() => setEditing(null)} onSaved={onSaved} />}
      {query.isPending ? <Loading label={`Loading ${spec.title.toLowerCase()}`} /> : query.isError ? <QueryError error={query.error} retry={() => query.refetch()} /> :
        <Table rows={query.data ?? []} columns={spec.columns} emptyTitle={`No ${spec.title.toLowerCase()} yet`} emptyDetail={`Create the first ${spec.singular} record when you’re ready. No sample records are shown.`} actions={row => <div className="row-actions">
          {spec.editable && <button className="row-action" onClick={() => { setError(""); setEditing(row); }}>Edit</button>}
          {spec.deletable && <button className="row-action danger" onClick={() => { const id = stringValue(row.id); if (id && window.confirm(`Delete this ${spec.singular}? This action cannot be undone.`)) remove.mutate(id); }}>Delete</button>}
        </div>} />}
    </Panel>
  </>;
}

function Ledger() {
  const invoices = useList("invoices", "/invoices", "invoices");
  const payments = useList("payments", "/payments", "payments");
  const arrears = useList("arrears", "/arrears", "arrears");
  if (invoices.isPending || payments.isPending || arrears.isPending) return <><Header eyebrow="RENT RECORDS" title="Invoice ledger" description="Account-scoped invoices, completed payments, running balances and overdue items." /><Loading label="Loading ledger and arrears" /></>;
  if (invoices.isError) return <><Header eyebrow="RENT RECORDS" title="Invoice ledger" description="Account-scoped invoices and payment history." /><QueryError error={invoices.error} retry={() => invoices.refetch()} /></>;
  if (payments.isError) return <><Header eyebrow="RENT RECORDS" title="Invoice ledger" description="Account-scoped invoices and payment history." /><QueryError error={payments.error} retry={() => payments.refetch()} /></>;
  if (arrears.isError) return <><Header eyebrow="RENT RECORDS" title="Invoice ledger" description="Account-scoped invoices and payment history." /><QueryError error={arrears.error} retry={() => arrears.refetch()} /></>;
  const entries = buildTenantLedger(invoices.data ?? [], payments.data ?? []);
  return <>
    <Header eyebrow="RENT RECORDS" title="Invoice ledger" description="Running tenant balances from invoices and completed payments. Unmatched or pending receipts are not treated as money collected." />
    <Panel title="Ledger & running balances" aside={<span className="muted">Amounts in KES · balances reset per tenant</span>}>
      <Table rows={entries} columns={["_date", "entry_type", "period", "tenant_name", "unit_code", "amount", "running_balance", "status"]} emptyTitle="No ledger entries" emptyDetail="Invoices and completed payments will appear here when returned by the API." />
    </Panel>
    <Panel title="Overdue invoices" aside={<span className="muted">Days past due · partial balances included</span>}>
      <Table rows={arrears.data ?? []} columns={["tenant_name", "unit_code", "period", "due_date", "amount", "paid_amount", "balance", "days_overdue"]} emptyTitle="No overdue invoices" emptyDetail="Invoices with a positive balance past their due date will appear here." />
    </Panel>
  </>;
}

function Payments({ review = false }: { review?: boolean }) {
  const query = useList(review ? "unmatched" : "payments", review ? "/payments/unmatched" : "/payments", "payments");
  const units = useList("units", "/units", "units");
  const tenants = useList("tenants", "/tenants", "tenants");
  const client = useQueryClient();
  const [assign, setAssign] = useState<{ id: string; unit: string } | null>(null);
  const [recordOpen, setRecordOpen] = useState(false);
  const [tenantId, setTenantId] = useState("");
  const [amount, setAmount] = useState("");
  const [method, setMethod] = useState("cash");
  const [notes, setNotes] = useState("");
  const [notice, setNotice] = useState("");
  const refreshRecords = async () => Promise.all([client.invalidateQueries({ queryKey: ["unmatched"] }), client.invalidateQueries({ queryKey: ["payments"] }), client.invalidateQueries({ queryKey: ["invoices"] }), client.invalidateQueries({ queryKey: ["arrears"] }), client.invalidateQueries({ queryKey: ["dashboard"] })]);
  const assignment = useMutation({
    mutationFn: ({ id, unit_id }: { id: string; unit_id: string }) => api(`/payments/${encodeURIComponent(id)}/assign`, { method: "POST", body: JSON.stringify({ unit_id }) }),
    onSuccess: async () => { setNotice("Payment assigned to the unit’s active tenant and allocated by the ledger."); setAssign(null); await refreshRecords(); },
    onError: error => setNotice((error as Error).message),
  });
  const manualPayment = useMutation({
    mutationFn: () => api("/payments", { method: "POST", body: JSON.stringify({ tenant_id: tenantId, amount: Number(amount), method, notes }) }),
    onSuccess: async () => { setNotice("Payment recorded and allocated to the tenant ledger."); setRecordOpen(false); setTenantId(""); setAmount(""); setNotes(""); await refreshRecords(); },
    onError: error => setNotice((error as Error).message),
  });
  const occupiedUnits = (units.data ?? []).filter(unit => stringValue(unit.status).toLowerCase() === "occupied");
  return <>
    <Header eyebrow="COLLECTIONS" title={review ? "Payment review" : "Payments"} description={review ? "Assign a payment only after confirming the matching occupied unit." : "View M-Pesa and manual payment records for this landlord account."} action={review ? <Link className="button button-outline" href="/payments">Payment history</Link> : <><button className="button button-outline" onClick={() => { setNotice(""); setRecordOpen(value => !value); }}>Record manual payment</button><Link className="button button-primary" href="/payments/review">Review unmatched</Link></>} />
    {notice && <div className={`notice ${assignment.isError || manualPayment.isError ? "notice-error" : "notice-success"}`} role="status">{notice}</div>}
    {!review && recordOpen && <Panel title="Record a payment" aside={<span className="muted">Cash, bank or other</span>}>
      <form className="inline-form" onSubmit={event => { event.preventDefault(); manualPayment.mutate(); }}>
        <div className="form-grid">
          <Field label="Tenant"><select required value={tenantId} onChange={event => setTenantId(event.target.value)}><option value="">Choose a tenant</option>{(tenants.data ?? []).map(tenant => <option key={stringValue(tenant.id)} value={stringValue(tenant.id)}>{stringValue(tenant.full_name)} · {stringValue(tenant.phone)}</option>)}</select></Field>
          <Field label="Amount (KES)"><input required type="number" min="0.01" step="0.01" value={amount} onChange={event => setAmount(event.target.value)} /></Field>
          <Field label="Method"><select value={method} onChange={event => setMethod(event.target.value)}><option value="cash">Cash</option><option value="bank">Bank</option><option value="other">Other</option></select></Field>
          <Field label="Notes"><input value={notes} onChange={event => setNotes(event.target.value)} /></Field>
        </div>
        {tenants.isError && <QueryError error={tenants.error} retry={() => tenants.refetch()} />}
        <div className="form-actions"><button type="button" className="button button-outline" onClick={() => setRecordOpen(false)}>Cancel</button><button className="button button-primary" disabled={manualPayment.isPending || !tenantId || !amount}>{manualPayment.isPending ? "Recording…" : "Record payment"}</button></div>
      </form>
    </Panel>}
    <Panel title={review ? "Unmatched queue" : "Payment history"} aside={<span className="muted">{review ? "Needs an account-side unit match" : "Live API records"}</span>}>
      {query.isPending ? <Loading label="Loading payment records" /> : query.isError ? <QueryError error={query.error} retry={() => query.refetch()} /> : <Table rows={query.data ?? []} columns={["payment_date", "tenant_name", "unit_code", "amount", "method", "receipt_number", "status"]} emptyTitle={review ? "No unmatched payments" : "No payment records"} emptyDetail={review ? "There are no account-linked payments waiting for assignment." : "Payment history will appear here when recorded by the API."} actions={review ? row => <button className="row-action" onClick={() => setAssign({ id: stringValue(row.id), unit: "" })}>Assign unit</button> : undefined} />}
    </Panel>
    {assign && <Panel title="Confirm payment allocation" aside={<button className="text-button" onClick={() => setAssign(null)}>Cancel</button>}>
      <form className="inline-form" onSubmit={event => { event.preventDefault(); if (assign.unit) assignment.mutate({ id: assign.id, unit_id: assign.unit }); }}>
        <p>This payment will be assigned to the active tenant of the selected unit, then allocated to outstanding invoices. Any excess remains as tenant credit.</p>
        <Field label="Occupied unit" hint="Only units with an active lease are accepted by the API."><select required value={assign.unit} onChange={event => setAssign({ ...assign, unit: event.target.value })}><option value="">Choose a unit</option>{occupiedUnits.map(unit => <option key={stringValue(unit.id)} value={stringValue(unit.id)}>{stringValue(unit.code)} · {stringValue(unit.property_name)}</option>)}</select></Field>
        {units.isError && <QueryError error={units.error} retry={() => units.refetch()} />}
        <div className="form-actions"><button className="button button-primary" disabled={!assign.unit || assignment.isPending}>{assignment.isPending ? "Assigning…" : "Confirm assignment"}</button></div>
      </form>
    </Panel>}
  </>;
}

function Reports() {
  return <><Header eyebrow="ACCOUNT REPORTS" title="Reports" description="Download account-backed CSV files for a selected collection month and current arrears." /><div className="report-links"><div className="report-card"><strong>Monthly collection</strong><span>Completed collection transactions for the chosen month (defaults to the current month).</span><a href={apiUrl("/reports/collections.csv")} download>Download collections CSV ↗</a></div><div className="report-card"><strong>Arrears register</strong><span>Open invoice balances with the number of days overdue.</span><a href={apiUrl("/reports/arrears.csv")} download>Download arrears CSV ↗</a></div></div><p className="side-note" style={{ marginTop: 18 }}>Report downloads use your account session and include only records in this landlord account.</p></>;
}

function TenantHome({ paymentsOnly = false }: { paymentsOnly?: boolean }) {
  const invoices = useList("invoices", "/invoices", "invoices");
  const payments = useList("payments", "/payments", "payments");
  const dashboard = useQuery({ queryKey: ["tenant-dashboard"], queryFn: () => api("/dashboard") });
  const dash = asRecord(dashboard.data);
  const client = useQueryClient();
  const [form, setForm] = useState(false);
  const [message, setMessage] = useState("");
  const [phone, setPhone] = useState("");
  const [amount, setAmount] = useState("");
  const [invoiceId, setInvoiceId] = useState("");
  const stk = useMutation({
    mutationFn: () => api("/payments/stk-push", { method: "POST", body: JSON.stringify({ phone, amount: Number(amount), invoice_id: invoiceId }) }),
    onSuccess: async () => { setMessage("Payment request sent. Follow the prompt on your phone to complete payment."); setForm(false); await Promise.all([client.invalidateQueries({ queryKey: ["payments"] }), client.invalidateQueries({ queryKey: ["invoices"] })]); },
    onError: error => setMessage((error as Error).message),
  });
  const outstanding = (invoices.data ?? []).reduce((sum, row) => {
    const value = Number(rowValue(row, "balance") ?? (Number(row.amount ?? 0) - Number(row.paid_amount ?? 0)));
    return sum + (Number.isFinite(value) ? Math.max(0, value) : 0);
  }, 0);
  const payableInvoices = (invoices.data ?? []).filter(invoice => Number(rowValue(invoice, "balance") ?? (Number(invoice.amount ?? 0) - Number(invoice.paid_amount ?? 0))) > 0);
  return <>
    <Header eyebrow="TENANT ACCOUNT" title={paymentsOnly ? "Your payments" : "Your rent account"} description={paymentsOnly ? "Your own payment history and receipts." : "Your balance, invoices and payment activity."} action={<button className="button button-primary" onClick={() => { setMessage(""); setForm(value => !value); }}>Pay by M-Pesa</button>} />
    {!paymentsOnly && <Panel title="Your tenancy" aside={<span className="muted">Live API records</span>}>
      {dashboard.isPending ? <Loading label="Loading tenancy" /> : dashboard.isError ? <QueryError error={dashboard.error} retry={() => dashboard.refetch()} /> : <div className="summary-band">
        <Metric label="Property" value={stringValue(dash?.property_name) || "—"} note={stringValue(dash?.property_address)} />
        <Metric label="Unit" value={stringValue(dash?.unit_code) || "—"} />
        <Metric label="Monthly rent" value={dash?.rent_amount != null ? formatMoney(Number(dash.rent_amount)) : "—"} />
        <Metric label="Lease status" value={stringValue(dash?.lease_status) || "—"} note={dash?.lease_end ? `Expires ${stringValue(dash.lease_end)}` : ""} />
      </div>}
    </Panel>}
    {!paymentsOnly && <div className="summary-band"><Metric label="Outstanding balance" value={invoices.isError ? "—" : formatMoney(outstanding)} note="Calculated from your invoice balances" /><Metric label="Invoices" value={invoices.data ? String(invoices.data.length) : "—"} /><Metric label="Payments recorded" value={payments.data ? String(payments.data.length) : "—"} /><Metric label="Maintenance" value={dash?.maintenance ? String((dash.maintenance as { pending?: number }).pending ?? 0) : "—"} note="Pending requests" /></div>}
    {message && <div className={`notice ${stk.isError ? "notice-error" : "notice-success"}`} role="status">{message}</div>}
    {form && <Panel title="M-Pesa payment request" aside={<button className="text-button" onClick={() => setForm(false)}>Close</button>}>
      <form className="inline-form" onSubmit={event => { event.preventDefault(); stk.mutate(); }}>
        <div className="form-grid"><Field label="Phone number" hint="Use the phone number registered with M-Pesa."><input type="tel" required autoComplete="tel" placeholder="07… or 2547…" value={phone} onChange={event => setPhone(event.target.value)} /></Field>
          <Field label="Amount (KES)" hint="Whole shillings only. Any overpayment is carried as account credit."><input type="number" required min="1" step="1" value={amount} onChange={event => setAmount(event.target.value)} /></Field>
          <Field label="Invoice"><select required value={invoiceId} onChange={event => setInvoiceId(event.target.value)}><option value="">Choose an open invoice</option>{payableInvoices.map(invoice => <option key={stringValue(invoice.id)} value={stringValue(invoice.id)}>{stringValue(invoice.period) || stringValue(invoice.id)} · balance {formatMoney(rowValue(invoice, "balance") ?? (Number(invoice.amount ?? 0) - Number(invoice.paid_amount ?? 0)))}</option>)}</select></Field>
        </div>
        {invoices.isError && <QueryError error={invoices.error} retry={() => invoices.refetch()} />}
        <div className="form-actions"><button className="button button-primary" disabled={stk.isPending || invoices.isPending || payableInvoices.length === 0 || !invoiceId}>{stk.isPending ? "Sending request…" : "Request M-Pesa payment"}</button></div>
      </form>
    </Panel>}
    <div className="content-grid"><div><Panel title={paymentsOnly ? "Payment history" : "Invoices & balances"}>{paymentsOnly ? (payments.isPending ? <Loading label="Loading your payments" /> : payments.isError ? <QueryError error={payments.error} retry={() => payments.refetch()} /> : <Table rows={payments.data ?? []} columns={["payment_date", "amount", "method", "receipt_number", "status"]} emptyTitle="No payments recorded" emptyDetail="Your receipts and payment records will appear here when available." />) : (invoices.isPending ? <Loading label="Loading your invoices" /> : invoices.isError ? <QueryError error={invoices.error} retry={() => invoices.refetch()} /> : <Table rows={invoices.data ?? []} columns={["period", "due_date", "amount", "paid_amount", "balance", "status"]} emptyTitle="No invoices available" emptyDetail="Your invoice history will appear here when available from the API." />)}</Panel></div>
      {!paymentsOnly && <div><Panel title="Recent payments">{payments.isPending ? <Loading label="Loading your history" /> : payments.isError ? <QueryError error={payments.error} retry={() => payments.refetch()} /> : <Table rows={(payments.data ?? []).slice(0, 5)} columns={["payment_date", "amount", "receipt_number", "status"]} emptyTitle="No payment history" emptyDetail="Completed payments and receipts will show here." />}<div style={{ padding: "0 18px 17px" }}><Link className="text-link" href="/tenant/payments">Full payment history →</Link></div></Panel></div>}
    </div>
  </>;
}


// ============================================================================
// Maintenance Management Screens
// ============================================================================

const MAINTENANCE_CATEGORIES = [
  { value: "plumbing", label: "Plumbing" },
  { value: "electrical", label: "Electrical" },
  { value: "security", label: "Security" },
  { value: "general_repair", label: "General repair" },
  { value: "other", label: "Other" },
];

const MAINTENANCE_STATUSES = [
  { value: "submitted", label: "Submitted" },
  { value: "under_review", label: "Under review" },
  { value: "assigned", label: "Assigned" },
  { value: "in_progress", label: "In progress" },
  { value: "resolved", label: "Resolved" },
  { value: "rejected", label: "Rejected" },
];

function statusLabel(value: unknown): string {
  const found = MAINTENANCE_STATUSES.find(s => s.value === value);
  return found ? found.label : stringValue(value);
}

function categoryLabel(value: unknown): string {
  const found = MAINTENANCE_CATEGORIES.find(c => c.value === value);
  return found ? found.label : stringValue(value);
}

function TenantMaintenance() {
  const session = useSession();
  const requests = useList("maintenance-requests", "/maintenance/requests", "maintenance-requests");
  const client = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [category, setCategory] = useState("plumbing");
  const [description, setDescription] = useState("");
  const [propertyId, setPropertyId] = useState("");
  const [unitId, setUnitId] = useState("");
  const [leaseId, setLeaseId] = useState("");
  const [imageFile, setImageFile] = useState<File | null>(null);
  const [notice, setNotice] = useState("");

  const properties = useList("maintenance-properties", "/properties", "properties");
  const units = useList("maintenance-units", "/units", "units");

  const submit = useMutation({
    mutationFn: async () => {
      const formData = new FormData();
      formData.append("category", category);
      formData.append("description", description);
      formData.append("property_id", propertyId);
      if (unitId) formData.append("unit_id", unitId);
      if (leaseId) formData.append("lease_id", leaseId);
      if (imageFile) formData.append("image", imageFile);
      return api("/maintenance/requests", { method: "POST", body: formData });
    },
    onSuccess: async () => {
      setNotice("Maintenance request submitted. You’ll receive a reference number.");
      setFormOpen(false);
      setCategory("plumbing"); setDescription(""); setPropertyId(""); setUnitId(""); setLeaseId(""); setImageFile(null);
      await client.invalidateQueries({ queryKey: ["maintenance-requests"] });
    },
    onError: error => setNotice((error as Error).message),
  });

  if (session.isPending) return <Loading label="Checking your session" />;
  if (session.isError) return <QueryError error={session.error} retry={() => session.refetch()} />;

  return <>
    <Header eyebrow="TENANT MAINTENANCE" title="Maintenance requests" description="Report an issue with your unit and track its progress." action={<button className="button button-primary" onClick={() => { setNotice(""); setFormOpen(value => !value); }}>＋ Report an issue</button>} />
    {notice && <div className={`notice ${submitting ? "notice-error" : "notice-success"}`} role="status">{notice}</div>}
    {formOpen && <Panel title="Submit a maintenance request" aside={<button className="text-button" onClick={() => setFormOpen(false)}>Close</button>}>
      <form className="inline-form" onSubmit={event => { event.preventDefault(); setSubmitting(true); submit.mutate(); }}>
        <div className="form-grid">
          <Field label="Category" hint="What kind of issue is this?"><select required value={category} onChange={event => setCategory(event.target.value)}>{MAINTENANCE_CATEGORIES.map(c => <option key={c.value} value={c.value}>{c.label}</option>)}</select></Field>
          <Field label="Description" hint="Describe the issue in detail."><textarea required rows={4} value={description} onChange={event => setDescription(event.target.value)} /></Field>
          <Field label="Property" hint="Select the property this request is about."><select required value={propertyId} onChange={event => setPropertyId(event.target.value)}><option value="">Choose a property</option>{(properties.data ?? []).map(row => <option key={stringValue(row.id)} value={stringValue(row.id)}>{stringValue(row.name)}</option>)}</select></Field>
          <Field label="Unit" hint="Optional: the specific unit."><select value={unitId} onChange={event => setUnitId(event.target.value)}><option value="">None specified</option>{(units.data ?? []).map(row => <option key={stringValue(row.id)} value={stringValue(row.id)}>{stringValue(row.code)}</option>)}</select></Field>
          <Field label="Lease" hint="Optional: your active lease reference."><select value={leaseId} onChange={event => setLeaseId(event.target.value)}><option value="">None specified</option></select></Field>
          <Field label="Photo (optional)" hint="JPEG, PNG or WebP, up to 5 MB."><input type="file" accept="image/jpeg,image/png,image/webp" onChange={event => { const file = event.target.files?.[0] ?? null; setImageFile(file); }} /></Field>
        </div>
        {requests.isError && <QueryError error={requests.error} retry={() => requests.refetch()} />}
        <div className="form-actions"><button type="button" className="button button-outline" onClick={() => setFormOpen(false)}>Cancel</button><button className="button button-primary" disabled={submit.isPending || !description || !propertyId}>{submit.isPending ? "Submitting…" : "Submit request"}</button></div>
      </form>
    </Panel>}
    <Panel title="Your maintenance requests" aside={<span className="muted">{requests.data ? `${requests.data.length} requests` : "Live records"}</span>}>
      {requests.isPending ? <Loading label="Loading your requests" /> : requests.isError ? <QueryError error={requests.error} retry={() => requests.refetch()} /> : <Table rows={requests.data ?? []} columns={["reference", "category", "status", "priority", "created_at"]} emptyTitle="No maintenance requests" emptyDetail="Submit your first maintenance request using the form above." actions={row => <Link className="row-action" href={`/maintenance/${stringValue(row.id)}`}>View</Link>} />}
    </Panel>
  </>;
}

function MaintenanceRequestDetail() {
  // This is a placeholder; the actual detail page uses a route param handled
  // by the Next.js page component which passes the request ID.
  return <><Header eyebrow="MAINTENANCE" title="Request details" description="View the full history, comments and status of a maintenance request." /><Loading label="Loading request details" /></>;
}

function LandlordMaintenance() {
  const requests = useList("landlord-maintenance", "/maintenance/requests", "maintenance-requests");
  const client = useQueryClient();
  const [notice, setNotice] = useState("");

  const updateStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) => api(`/maintenance/requests/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify({ status }) }),
    onSuccess: async () => { setNotice("Request status updated."); await client.invalidateQueries({ queryKey: ["landlord-maintenance"] }); },
    onError: error => setNotice((error as Error).message),
  });

  return <>
    <Header eyebrow="MAINTENANCE MANAGEMENT" title="All maintenance requests" description="View, assign and resolve maintenance requests for your properties." />
    {notice && <div className="notice notice-success" role="status">{notice}</div>}
    <Panel title="Maintenance requests" aside={<span className="muted">{requests.data ? `${requests.data.length} requests` : "Live records"}</span>}>
      {requests.isPending ? <Loading label="Loading requests" /> : requests.isError ? <QueryError error={requests.error} retry={() => requests.refetch()} /> : <Table rows={requests.data ?? []} columns={["reference", "tenant_name", "property_name", "unit_code", "category", "status", "created_at"]} emptyTitle="No maintenance requests" emptyDetail="Tenant maintenance requests will appear here." actions={row => <div className="row-actions"><Link className="row-action" href={`/maintenance/${stringValue(row.id)}`}>View</Link>{stringValue(row.status) === "submitted" && <button className="row-action" onClick={() => { updateStatus.mutate({ id: stringValue(row.id), status: "under_review" }); }}>Review</button>}</div>} />}
    </Panel>
  </>;
}

export function Screen({ kind }: { kind: ScreenKind }) {
  const session = useSession();
  if (session.isPending) return <Loading label="Checking your account role" />;
  if (session.isError) return <QueryError error={session.error} retry={() => session.refetch()} />;
  const role = stringValue(session.data?.role ?? asRecord(session.data?.user)?.role).toLowerCase();
  const tenant = role === "tenant";
  const tenantScreen = kind === "tenant" || kind === "tenantPayments";
  if (tenant && !tenantScreen) return <div className="session-loading"><h1>This view isn’t available for your account.</h1><p className="muted">Tenant accounts can only view their own balance, invoices, payment history and M-Pesa payment form.</p><Link className="button button-outline" href="/tenant">Go to your account</Link></div>;
  if (!tenant && tenantScreen) return <div className="session-loading"><h1>Tenant account view</h1><p className="muted">This page is available to a tenant account.</p><Link className="button button-outline" href="/dashboard">Go to dashboard</Link></div>;
  if (tenant) return <TenantHome paymentsOnly={kind === "tenantPayments"} />;
  switch (kind) {
    case "dashboard": return <Dashboard />;
    case "properties": case "units": case "tenants": case "leases": return <EntityManager name={kind} />;
    case "ledger": return <Ledger />;
    case "payments": return <Payments />;
    case "review": return <Payments review />;
    case "reports": return <Reports />;
    case "maintenance": return <TenantMaintenance />;
    case "maintenanceDetail": return <MaintenanceRequestDetail />;
    case "maintenanceManage": return <LandlordMaintenance />;
    default: return <TenantHome paymentsOnly={kind === "tenantPayments"} />;
  }
}
