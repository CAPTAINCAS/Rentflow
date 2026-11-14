"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useSession } from "@/components/app-shell";
import { Loading } from "@/components/ui";
export default function Home(){const q=useSession();const router=useRouter();useEffect(()=>{if(q.data){const role=String(q.data.role??"").toLowerCase();router.replace(role.includes("tenant")?"/tenant":"/dashboard");}},[q.data,router]);if(q.isPending)return <div className="session-loading"><Loading label="Checking your account"/></div>;if(q.data)return <div className="session-loading"><Loading label="Opening your workspace"/></div>;return <main className="logged-out-shell"><Link href="/" className="brand"><svg viewBox="0 0 38 38" aria-hidden="true"><path d="M4 17 19 6l15 11M9 16v17h20V16M14 22h10M14 27h8M25 30l2 3 2-3"/></svg><span>rent<span>flow</span></span></Link><div className="logged-out-copy"><p className="eyebrow">PROPERTY OPERATIONS · KENYA</p><h1>Every rent entry,<br/>in its right place.</h1><p>Clear records for what is owed, received and still needs attention.</p><div className="row-actions"><Link className="button button-primary" href="/login">Sign in</Link><Link className="button button-outline" href="/register">Create account</Link></div></div><p className="logged-out-note">Calm, direct property operations. Live account data only.</p></main>;}
