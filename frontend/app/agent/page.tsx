"use client";
import { useQuery } from "@tanstack/react-query";
import { api, asRecord } from "@/lib/api";
import { Loading, ErrorState } from "@/components/ui";

export default function AgentPage() {
  const q = useQuery({ queryKey: ["dashboard"], queryFn: () => api<unknown>("/dashboard") });
  if (q.isPending) return <main className="main-content"><Loading label="Loading agent overview" /></main>;
  if (q.isError) return <main className="main-content"><ErrorState message="Could not load overview" onRetry={() => q.refetch()} /></main>;
  const data = asRecord(q.data);
  return (
    <main className="main-content">
      <div className="page-header"><h1>Agent overview</h1><p>Assigned properties and activity</p></div>
      <div className="summary-band">
        <div className="metric"><span className="metric-label">Properties</span><span className="metric-value">{String(data?.properties ?? 0)}</span></div>
        <div className="metric"><span className="metric-label">Units</span><span className="metric-value">{String(data?.units ?? 0)}</span></div>
        <div className="metric"><span className="metric-label">Tenants</span><span className="metric-value">{String(data?.tenants ?? 0)}</span></div>
        <div className="metric"><span className="metric-label">Arrears</span><span className="metric-value">{String(data?.arrears_total ?? 0)}</span></div>
      </div>
    </main>
  );
}