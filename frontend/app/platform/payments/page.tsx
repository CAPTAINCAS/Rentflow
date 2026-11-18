"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, asRecord } from "@/lib/api";
import { Loading, ErrorState } from "@/components/ui";

export default function PlatformPaymentsPage() {
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const q = useQuery({ queryKey: ["platform-payments", status, page], queryFn: () => api<unknown>(`/platform/subscription-payments?status=${status}&page=${page}&page_size=20`) });
  if (q.isPending) return <main className="main-content"><Loading label="Loading payments" /></main>;
  if (q.isError) return <main className="main-content"><ErrorState message="Could not load payments" onRetry={() => q.refetch()} /></main>;
  const data = asRecord(q.data);
  const payments = Array.isArray(data?.payments) ? data.payments : [];
  const pag = asRecord(data?.pagination);
  return (
    <main className="main-content">
      <div className="page-header"><h1>Subscription payments</h1></div>
      <select className="search-input" value={status} onChange={(e) => { setStatus(e.target.value); setPage(1); }}>
        <option value="">All</option><option value="completed">Completed</option><option value="pending">Pending</option><option value="failed">Failed</option>
      </select>
      <div className="panel"><div className="table-wrap"><table><thead><tr><th>Account</th><th>Amount</th><th>Status</th><th>Reference</th><th>Date</th></tr></thead><tbody>
        {payments.length === 0 ? <tr><td colSpan={5}>No payments</td></tr> : payments.map((p: { id: string; account_name: string; amount: number; status: string; external_ref: string | null; created_at: string }) => (
          <tr key={p.id}><td>{p.account_name}</td><td>{p.amount}</td><td>{p.status}</td><td>{p.external_ref ?? "—"}</td><td>{p.created_at}</td></tr>
        ))}
      </tbody></table></div></div>
      <div className="row-actions" style={{ marginTop: 12 }}>
        <button className="button button-outline" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Prev</button>
        <span>Page {String(pag?.page ?? page)} of {String(pag?.total_pages ?? 1)}</span>
        <button className="button button-outline" disabled={page >= Number(pag?.total_pages ?? 1)} onClick={() => setPage((p) => p + 1)}>Next</button>
      </div>
    </main>
  );
}