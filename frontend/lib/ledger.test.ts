import { describe, expect, it } from "vitest";
import { buildTenantLedger, invoiceOutstanding, overdueInvoices } from "@/lib/ledger";

describe("tenant ledger", () => {
  it("calculates independent running balances and subtracts only completed payments once", () => {
    const rows = buildTenantLedger(
      [
        { id: "i-a", tenant_id: "tenant-a", tenant_name: "Amina", amount: 100, paid_amount: 40, due_date: "2025-01-01" },
        { id: "i-b", tenant_id: "tenant-b", tenant_name: "Baraka", amount: 50, paid_amount: 0, due_date: "2025-01-01" },
      ],
      [
        { id: "p-a", tenant_id: "tenant-a", amount: 40, status: "completed", created_at: "2025-01-02" },
        { id: "p-pending", tenant_id: "tenant-b", amount: 20, status: "pending", created_at: "2025-01-03" },
      ],
    );
    expect(rows.filter(row => row.tenant_id === "tenant-a").map(row => row.running_balance)).toEqual([100, 60]);
    expect(rows.filter(row => row.tenant_id === "tenant-b").map(row => row.running_balance)).toEqual([50]);
  });
});

describe("invoice balances and overdue items", () => {
  it("derives partial balances without counting invoice paid_amount twice", () => {
    expect(invoiceOutstanding({ amount: 9000, paid_amount: 2500 })).toBe(6500);
  });
  it("lists only overdue invoices with positive remainder and exact overdue days", () => {
    const rows = overdueInvoices([
      { id: "partial", due_date: "2025-01-08", amount: 1000, paid_amount: 200 },
      { id: "paid", due_date: "2025-01-01", amount: 100, paid_amount: 100 },
      { id: "future", due_date: "2025-01-12", amount: 1000, paid_amount: 0 },
    ], new Date("2025-01-11T12:00:00Z"));
    expect(rows).toHaveLength(1);
    expect(rows[0].id).toBe("partial");
    expect(rows[0].balance).toBe(800);
    expect(rows[0].overdue_days).toBe(3);
  });
});
