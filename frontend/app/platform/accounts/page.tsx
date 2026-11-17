"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, asRecord } from "@/lib/api";
import { Loading, ErrorState } from "@/components/ui";

export default function PlatformAccountsPage() {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const q = useQuery({ queryKey: ["platform-accounts", search, page], queryFn: () => api<unknown>(`/platform/accounts?search=${encodeURIComponent(search)}&page=${page}&page_size=20`) });
  if (q.isPending) return <main className="main-content"><Loading label="Loading accounts" /></main>;
  if (q.isError) return <main className="main-content"><ErrorState message="Could not load accounts" onRetry={() => q.refetch()} /></main>;
  const data = asRecord(q.data);
  const accounts = Array.isArray(data?.accounts) ? data.accounts : [];
  const pag = asRecord(data?.pagination);
  return (
    <main className="main-content">
      <div className="page-header"><h1>Accounts</h1><p>Platform landlords and accounts</p></div>
      <input className="search-input" placeholder="Search accounts" value={search} onChange={(e) => { setSearch(e.target.value); setPage(1); }} />
      <div className="panel"><div className="table-wrap"><table><thead><tr><th>Name</th><th>Status</th><th>Properties</th><th>Tenants</th></tr></thead><tbody>
        {accounts.length === 0 ? <tr><td colSpan={4}>No accounts found</td></tr> : accounts.map((a: any) => (
          <tr key={a.id}><td>{a.name}</td><td>{a.subscription_status}</td><td>{a.property_count}</td><td>{a.tenant_count}</td></tr>
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