"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, asRecord } from "@/lib/api";
import { Loading, ErrorState } from "@/components/ui";

export default function PlatformRevenuePage() {
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const q = useQuery({ queryKey: ["platform-revenue", from, to], queryFn: () => api<unknown>(`/platform/revenue?from=${from}&to=${to}`) });
  if (q.isPending) return <main className="main-content"><Loading label="Loading revenue" /></main>;
  if (q.isError) return <main className="main-content"><ErrorState message="Could not load revenue" onRetry={() => q.refetch()} /></main>;
  const data = asRecord(q.data);
  return (
    <main className="main-content">
      <div className="page-header"><h1>Platform revenue</h1><p>Subscription revenue only — excludes tenant rent collections</p></div>
      <div className="row-actions">
        <input className="search-input" type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        <input className="search-input" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
      </div>
      <div className="summary-band">
        <div className="metric"><span className="metric-label">Total revenue</span><span className="metric-value">{String(data?.total_revenue ?? 0)}</span></div>
        <div className="metric"><span className="metric-label">Completed payments</span><span className="metric-value">{String(data?.completed_payments ?? 0)}</span></div>
      </div>
    </main>
  );
}