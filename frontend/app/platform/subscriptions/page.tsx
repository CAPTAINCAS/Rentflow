"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, asRecord } from "@/lib/api";
import { Loading, ErrorState } from "@/components/ui";

export default function PlatformSubscriptionsPage() {
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const q = useQuery({ queryKey: ["platform-subs", status, page], queryFn: () => api<unknown>(`/platform/subscriptions?status=${status}&page=${page}&page_size=20`) });
  if (q.isPending) return <main className="main-content"><Loading label="Loading subscriptions" /></main>;
  if (q.isError) return <main className="main-content"><ErrorState message="Could not load subscriptions" onRetry={() => q.refetch()} /></main>;
  const data = asRecord(q.data);
  const subs = Array.isArray(data?.subscriptions) ? data.subscriptions : [];
  const pag = asRecord(data?.pagination);
  return (
    <main className="main-content">
      <div className="page-header"><h1>Subscriptions</h1></div>
      <select className="search-input" value={status} onChange={(e) => { setStatus(e.target.value); setPage(1); }}>
        <option value="">All</option><option value="active">Active</option><option value="trialing">Trialing</option><option value="past_due">Past due</option><option value="suspended">Suspended</option><option value="cancelled">Cancelled</option>
      </select>
      <div className="panel"><div className="table-wrap"><table><thead><tr><th>Account</th><th>Status</th><th>Plan</th><th>Renews</th><th>Expires</th></tr></thead><tbody>
        {subs.length === 0 ? <tr><td colSpan={5}>No subscriptions</td></tr> : subs.map((s: any) => (
          <tr key={s.account_id}><td>{s.account_name}</td><td>{s.status}</td><td>{s.plan_id ?? "—"}</td><td>{s.renews_at ?? "—"}</td><td>{s.expires_at ?? "—"}</td></tr>
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