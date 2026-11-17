"use client";

import { useSession } from "@/components/app-shell";
import { Screen } from "@/components/screens";
import { Loading } from "@/components/ui";
import { asRecord, stringValue } from "@/lib/api";

export default function MaintenancePage() {
  const session = useSession();
  if (session.isPending) return <Loading label="Checking your session" />;
  if (session.isError) return <div className="state-line" role="alert">We couldn’t check your account. Check your connection and try again.</div>;
  const record = asRecord(session.data);
  const role = stringValue(record?.role ?? asRecord(record?.user)?.role).toLowerCase();
  const isTenant = role.includes("tenant");
  return <Screen kind={isTenant ? "maintenance" : "maintenanceManage"} />;
}
