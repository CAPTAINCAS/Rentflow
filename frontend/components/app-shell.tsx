"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, asRecord, stringValue, type JsonRecord } from "@/lib/api";
import { Loading } from "@/components/ui";
import type { ReactNode } from "react";

const landlordLinks = [
  ["/dashboard", "Overview", "⌂"],
  ["/properties", "Properties", "▤"], ["/units", "Units", "▦"],
  ["/tenants", "Tenants", "♙"], ["/leases", "Leases", "▱"],
  ["/maintenance", "Maintenance", "🔧"],
  ["/payments", "Payments", "↗"], ["/payments/review", "Review queue", "!"], ["/ledger", "Ledger", "≋"], ["/reports", "Reports", "▥"],
];
const tenantLinks = [["/tenant", "My account", "⌂"], ["/maintenance", "Maintenance", "🔧"], ["/tenant/payments", "Payments", "↗"]];
const superAdminLinks = [["/platform", "Platform", "⌂"], ["/platform/accounts", "Accounts", "♙"], ["/platform/subscriptions", "Subscriptions", "▦"], ["/platform/payments", "Payments", "↗"], ["/platform/revenue", "Revenue", "≋"], ["/platform/audit", "Audit", "▥"]];
const agentLinks = [["/agent", "Overview", "⌂"], ["/properties", "Properties", "▤"], ["/units", "Units", "▦"], ["/tenants", "Tenants", "♙"], ["/leases", "Leases", "▱"], ["/maintenance", "Maintenance", "🔧"], ["/payments", "Payments", "↗"], ["/reports", "Reports", "▥"]];

export function useSession() {
  return useQuery<JsonRecord | null>({
    queryKey: ["session"],
    queryFn: async () => {
      try {
        const data = await api<unknown>("/auth/me");
        const record = asRecord(data);
        if (!record) return null;
        return { ...record, ...(asRecord(record.user) ?? asRecord(record.account) ?? {}) } as JsonRecord;
      } catch (error) {
        if ((error as { status?: number }).status === 401) {
          try {
            await api("/auth/refresh", { method: "POST", body: JSON.stringify({}) });
            const refreshed = asRecord(await api<unknown>("/auth/me"));
            if (!refreshed) return null;
            return { ...refreshed, ...(asRecord(refreshed.user) ?? asRecord(refreshed.account) ?? {}) } as JsonRecord;
          } catch (refreshError) {
            if ((refreshError as { status?: number }).status === 401) return null;
            throw refreshError;
          }
        }
        throw error;
      }
    },
    retry: (count, error) => (error as { status?: number }).status === 401 ? false : count < 1,
  });
}

function roleOf(session: JsonRecord | null | undefined) {
  const role = stringValue(session?.role ?? asRecord(session?.user)?.role).toLowerCase();
  if (role.includes("super_admin") || role.includes("superadmin")) return "super_admin";
  if (role.includes("tenant")) return "tenant";
  if (role.includes("agent")) return "agent";
  if (role) return "landlord";
  return "unknown";
}

function Brand() {
  return <Link href="/" className="brand" aria-label="RentFlow home"><svg viewBox="0 0 38 38" aria-hidden="true"><path d="M4 17 19 6l15 11M9 16v17h20V16M14 22h10M14 27h8M25 30l2 3 2-3" /></svg><span>rent<span>flow</span></span></Link>;
}

export function AppShell({ children }: { children: ReactNode }) {
  const sessionQuery = useSession();
  const pathname = usePathname();
  const router = useRouter();
  const queryClient = useQueryClient();
  const logout = useMutation({
    mutationFn: () => api("/auth/logout", { method: "POST", body: JSON.stringify({}) }),
    onSuccess: async () => { await queryClient.setQueryData(["session"], null); await queryClient.clear(); router.push("/login"); },
  });
  if (pathname === "/" || ["/login", "/register", "/forgot-password", "/reset-password"].includes(pathname)) return <>{children}</>;
  if (sessionQuery.isPending) return <main className="session-loading"><Brand /><Loading label="Checking your session" /></main>;
  if (sessionQuery.isError) return <main className="session-loading"><Brand /><p className="muted">We couldn’t check your account. Check your connection and try again.</p><button className="button button-outline" onClick={() => sessionQuery.refetch()}>Retry</button><Link href="/login" className="text-link">Go to sign in</Link></main>;
  const session = sessionQuery.data;
  if (!session) return <main className="logged-out-shell"><Brand /><div className="logged-out-copy"><p className="eyebrow">PROPERTY OPERATIONS · KENYA</p><h1>Every rent entry,<br />in its right place.</h1><p>Sign in to see live property, lease and payment records. Account data is never replaced with sample figures.</p><div className="row-actions"><Link className="button button-primary" href="/login">Sign in</Link><Link className="button button-outline" href="/register">Create account</Link></div></div><p className="logged-out-note">A clear record of what is owed, received and still needs attention.</p></main>;
  const role = roleOf(session);
  const isTenant = role === "tenant";
  const isSuperAdmin = role === "super_admin";
  const isAgent = role === "agent";
  const allowed = isSuperAdmin ? pathname.startsWith("/platform") || pathname === "/" : isTenant ? pathname.startsWith("/tenant") || pathname === "/" : isAgent ? !pathname.startsWith("/tenant") && !pathname.startsWith("/platform") : !pathname.startsWith("/tenant") && !pathname.startsWith("/platform");
  if (!allowed && pathname !== "/") return <main className="session-loading"><Brand /><h1>This view isn’t available for your account.</h1><p className="muted">You’re signed in with the {role} role.</p><Link className="button button-outline" href={isSuperAdmin ? "/platform" : isTenant ? "/tenant" : isAgent ? "/agent" : "/dashboard"}>Go to your account</Link></main>;
  const links = isSuperAdmin ? superAdminLinks : isTenant ? tenantLinks : isAgent ? agentLinks : landlordLinks;
  const name = stringValue(session.name ?? session.full_name ?? session.email) || "Account";
  return <div className="app-frame">
    <aside className="side-rail"><Brand /><div className="rail-label">WORKSPACE</div><nav aria-label="Primary navigation">{links.map(([href, label, icon]) => <Link key={href} href={href} className={`nav-link ${pathname === href ? "active" : ""}`} aria-current={pathname === href ? "page" : undefined}><span className="nav-icon" aria-hidden="true">{icon}</span><span>{label}</span></Link>)}</nav><div className="rail-bottom"><div className="account-chip"><span className="account-dot" /> <span className="account-name">{name}</span></div><button className="text-button" disabled={logout.isPending} onClick={() => logout.mutate()}>{logout.isPending ? "Signing out…" : "Sign out"}</button></div></aside>
    <div className="workspace"><header className="mobile-header"><Brand /><span className="role-tag">{isTenant ? "TENANT" : "LANDLORD"}</span><button className="mobile-signout" aria-label="Sign out" onClick={() => logout.mutate()}>↗</button></header><main className="main-content">{children}</main><nav className="bottom-nav" aria-label="Mobile navigation">{links.slice(0, 5).map(([href, label, icon]) => <Link key={href} href={href} aria-current={pathname === href ? "page" : undefined} className={`bottom-link ${pathname === href ? "active" : ""}`}><span aria-hidden="true">{icon}</span><small>{label}</small></Link>)}</nav></div>
  </div>;
}
