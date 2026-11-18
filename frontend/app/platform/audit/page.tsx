"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, asRecord } from "@/lib/api";
import { Loading, ErrorState } from "@/components/ui";

export default function PlatformAuditPage() {
  const [action, setAction] = useState("");
  const [resourceType, setResourceType] = useState("");
  const [page, setPage] = useState(1);
  const q = useQuery({ queryKey: ["platform-audit", action, resourceType, page], queryFn: () => api<unknown>(`/platform/audit-logs?action=${action}&resource_type=${resourceType}&page=${page}&page_size=20`) });
  if (q.isPending) return <main className="main-content"><Loading label="Loading audit logs" /></main>;
  if (q.isError) return <main className="main-content"><ErrorState message="Could not load audit logs" onRetry={() => q.refetch()} /></main>;
  const data = asRecord(q.data);
  const logs = Array.isArray(data?.audit_logs) ? data.audit_logs : [];
  const pag = asRecord(data?.pagination);
  return (
    <main className="main-content">
      <div className="page-header"><h1>Audit logs</h1></div>
      <div className="row-actions">
        <input className="search-input" placeholder="Action" value={action} onChange={(e) => { setAction(e.target.value); setPage(1); }} />
        <input className="search-input" placeholder="Resource type" value={resourceType} onChange={(e) => { setResourceType(e.target.value); setPage(1); }} />
      </div>
      <div className="panel"><div className="table-wrap"><table><thead><tr><th>Action</th><th>Resource</th><th>Actor</th><th>Account</th><th>Date</th></tr></thead><tbody>
        {logs.length === 0 ? <tr><td colSpan={5}>No audit logs</td></tr> : logs.map((l: { id: string; action: string; resource_type: string; actor_email: string; account_id: string | null; created_at: string }) => (
          <tr key={l.id}><td>{l.action}</td><td>{l.resource_type}</td><td>{l.actor_email}</td><td>{l.account_id ?? "—"}</td><td>{l.created_at}</td></tr>
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