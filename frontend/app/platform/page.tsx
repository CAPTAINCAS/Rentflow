"use client";
import { useQuery } from "@tanstack/react-query";
import { api, asRecord } from "@/lib/api";
import { Loading, ErrorState } from "@/components/ui";

export default function PlatformPage() {
  const q = useQuery({ queryKey: ["platform-overview"], queryFn: () => api<unknown>("/platform/overview") });
  if (q.isPending) return <main className="main-content"><Loading label="Loading platform overview" /></main>;
  if (q.isError) return <main className="main-content"><ErrorState message="Could not load platform overview" onRetry={() => q.refetch()} /></main>;
  const data = asRecord(q.data);
  return (
    <main className="main-content">
      <div className="page-header"><h1>Platform overview</h1><p>RentFlow by TechNova — super admin</p></div>
      <div className="summary-band">
        <div className="metric"><span className="metric-label">Accounts</span><span className="metric-value">{String(data?.total_accounts ?? 0)}</span></div>
        <div className="metric"><span className="metric-label">Properties</span><span className="metric-value">{String(data?.total_properties ?? 0)}</span></div>
        <div className="metric"><span className="metric-label">Units</span><span className="metric-value">{String(data?.total_units ?? 0)}</span></div>
        <div className="metric"><span className="metric-label">Tenants</span><span className="metric-value">{String(data?.total_tenants ?? 0)}</span></div>
      </div>
      <div className="panel"><div className="panel-head"><h2>Subscription revenue</h2></div><div className="panel-body"><p>{String(data?.subscription_revenue ?? 0)}</p></div></div>
    </main>
  );
}