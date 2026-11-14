import { firstValue, stringValue, type JsonRecord } from "@/lib/api";

function numeric(row: JsonRecord, keys: string[]): number {
  const value = firstValue(row, keys);
  const amount = typeof value === "number" ? value : Number(value);
  return Number.isFinite(amount) ? amount : 0;
}
function timestamp(value: unknown): number {
  const date = new Date(String(value ?? ""));
  return Number.isFinite(date.valueOf()) ? date.valueOf() : 0;
}
function isoDay(value: unknown): string { return String(value ?? "").slice(0, 10); }

export function invoiceOutstanding(row: JsonRecord): number {
  const explicit = firstValue(row, ["balance", "outstanding_amount", "amount_due"]);
  if (explicit !== undefined && explicit !== null) {
    const n = typeof explicit === "number" ? explicit : Number(explicit);
    if (Number.isFinite(n)) return Math.max(0, n);
  }
  return Math.max(0, numeric(row, ["amount", "amount_kes"]) - numeric(row, ["paid_amount", "amount_paid"]));
}

export function buildTenantLedger(invoices: JsonRecord[], payments: JsonRecord[]): JsonRecord[] {
  const names = new Map<string, string>();
  for (const invoice of invoices) {
    const tenantId = stringValue(invoice.tenant_id);
    if (tenantId) names.set(tenantId, stringValue(invoice.tenant_name) || names.get(tenantId) || "Tenant");
  }
  const grouped = new Map<string, JsonRecord[]>();
  const append = (tenantId: string, entry: JsonRecord) => {
    if (!tenantId) return;
    const entries = grouped.get(tenantId) ?? [];
    entries.push(entry);
    grouped.set(tenantId, entries);
  };
  for (const invoice of invoices) {
    const tenantId = stringValue(invoice.tenant_id);
    append(tenantId, { ...invoice, entry_type: "Invoice", _signed: numeric(invoice, ["amount", "amount_kes"]), _date: firstValue(invoice, ["due_date", "period", "created_at"]) });
  }
  for (const payment of payments) {
    if (stringValue(payment.status).toLowerCase() !== "completed") continue;
    const tenantId = stringValue(payment.tenant_id);
    append(tenantId, { ...payment, tenant_name: stringValue(payment.tenant_name) || names.get(tenantId) || "Tenant", entry_type: "Payment", _signed: -Math.abs(numeric(payment, ["amount", "amount_kes"])), _date: firstValue(payment, ["transaction_date", "created_at"]) });
  }
  const rows: JsonRecord[] = [];
  for (const [tenantId, entries] of grouped) {
    entries.sort((a, b) => timestamp(a._date) - timestamp(b._date));
    let running = 0;
    for (const entry of entries) {
      const amount = Number(entry._signed);
      if (Number.isFinite(amount)) running += amount;
      rows.push({ ...entry, tenant_id: tenantId, amount: Math.abs(amount), running_balance: running });
    }
  }
  return rows.sort((a, b) => timestamp(a._date) - timestamp(b._date));
}

export function overdueInvoices(invoices: JsonRecord[], today = new Date()): JsonRecord[] {
  const todayDay = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate()));
  const todayText = todayDay.toISOString().slice(0, 10);
  const overdue: JsonRecord[] = invoices.flatMap(invoice => {
    const due = isoDay(invoice.due_date);
    const balance = invoiceOutstanding(invoice);
    if (!due || due >= todayText || balance <= 0) return [];
    const dueDate = new Date(`${due}T00:00:00Z`);
    const days = Math.floor((todayDay.getTime() - dueDate.getTime()) / 86_400_000);
    return [{ ...invoice, balance, overdue_days: days }];
  });
  return overdue.sort((a, b) => timestamp(a.due_date) - timestamp(b.due_date));
}
