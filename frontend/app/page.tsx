"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useSession } from "@/components/app-shell";
import { Loading } from "@/components/ui";

export default function Home() {
  const q = useSession();
  const router = useRouter();
  useEffect(() => {
    if (q.data) {
      const role = String(q.data.role ?? "").toLowerCase();
      router.replace(role.includes("tenant") ? "/tenant" : "/dashboard");
    }
  }, [q.data, router]);
  if (q.isPending) return <div className="session-loading"><Loading label="Checking your account" /></div>;
  if (q.data) return <div className="session-loading"><Loading label="Opening your workspace" /></div>;

  return (
    <main className="landing">
      <header className="landing-nav">
        <Link href="/" className="brand" aria-label="RentFlow home">
          <svg viewBox="0 0 38 38" aria-hidden="true"><path d="M4 17 19 6l15 11M9 16v17h20V16M14 22h10M14 27h8M25 30l2 3 2-3" /></svg>
          <span>rent<span>flow</span></span>
        </Link>
        <nav className="landing-nav-links" aria-label="Primary">
          <a href="#features">Features</a>
          <a href="#how">How it works</a>
          <a href="#pricing">Pricing</a>
          <a href="#faq">FAQ</a>
        </nav>
        <div className="landing-nav-actions">
          <Link className="button button-outline" href="/login">Sign in</Link>
          <Link className="button button-primary" href="/register">Get started</Link>
        </div>
      </header>

      <section className="landing-hero" style={{ backgroundImage: "url(https://images.unsplash.com/photo-1580587771525-78b9dba3b914?auto=format&fit=crop&w=1600&q=80)", backgroundSize: "cover", backgroundPosition: "center" }}>
        <div className="landing-hero-copy" style={{ background: "rgba(15,23,42,0.55)", padding: "40px", borderRadius: "16px" }}>
          <p className="eyebrow">PROPERTY OPERATIONS · KENYA</p>
          <h1>Every rent entry, in its right place.</h1>
          <p className="landing-hero-sub">RentFlow keeps property, tenant, lease, payment and maintenance records clear, accurate and easy to act on — for landlords, agents and tenants.</p>
          <div className="row-actions">
            <Link className="button button-primary" href="/register">Get started</Link>
            <Link className="button button-outline" href="/login">Sign in</Link>
          </div>
        </div>
        <div className="landing-hero-visual" aria-hidden="true">
          <div className="landing-hero-card">
            <div className="landing-hero-card-bar" />
            <div className="landing-hero-card-body">
              <span className="landing-hero-card-label">Occupancy</span>
              <span className="landing-hero-card-value">87%</span>
            </div>
          </div>
          <div className="landing-hero-card landing-hero-card-2">
            <div className="landing-hero-card-bar" />
            <div className="landing-hero-card-body">
              <span className="landing-hero-card-label">Collected</span>
              <span className="landing-hero-card-value">KES 1.2M</span>
            </div>
          </div>
        </div>
      </section>

      <section id="features" className="landing-section">
        <div className="section-heading"><h2>Everything you need to run rentals</h2><p>One platform for properties, tenants, leases, invoices, payments and maintenance.</p></div>
        <div className="landing-grid">
          <div className="landing-card"><h3>Property management</h3><p>Track properties and units, occupancy and status in one place.</p></div>
          <div className="landing-card"><h3>Tenant management</h3><p>Keep tenant records, leases and contact details organised.</p></div>
          <div className="landing-card"><h3>Rent tracking</h3><p>Generate invoices, record payments and monitor arrears.</p></div>
          <div className="landing-card"><h3>Maintenance</h3><p>Submit, track and resolve maintenance requests with comments and images.</p></div>
        </div>
      </section>

      <section id="how" className="landing-section">
        <div className="section-heading"><h2>How RentFlow works</h2><p>Simple steps from setup to collection.</p></div>
        <ol className="landing-steps">
          <li><strong>Add properties and units</strong> — set up your portfolio.</li>
          <li><strong>Add tenants and leases</strong> — link tenants to units.</li>
          <li><strong>Track rent and payments</strong> — invoices and payments stay in sync.</li>
          <li><strong>Handle maintenance</strong> — requests flow from submission to resolution.</li>
        </ol>
      </section>

      <section id="pricing" className="landing-section">
        <div className="section-heading"><h2>Subscription plans</h2><p>Plans are configured per account. Contact your administrator for plan details.</p></div>
        <p className="muted">Pricing is managed per account and plan. No fixed public prices are advertised.</p>
      </section>

      <section id="faq" className="landing-section">
        <div className="section-heading"><h2>Frequently asked questions</h2></div>
        <div className="landing-faq">
          <details><summary>Who can use RentFlow?</summary><p>Landlords, agents and tenants, each with role-specific access.</p></details>
          <details><summary>Is my data isolated per account?</summary><p>Yes. All records are scoped to your account.</p></details>
          <details><summary>Can tenants submit maintenance requests?</summary><p>Yes, tenants can submit requests with optional images.</p></details>
        </div>
      </section>

      <footer className="landing-footer">
        <div className="brand"><svg viewBox="0 0 38 38" aria-hidden="true"><path d="M4 17 19 6l15 11M9 16v17h20V16M14 22h10M14 27h8M25 30l2 3 2-3" /></svg><span>rent<span>flow</span></span></div>
        <p className="muted">Property operations, kept clear.</p>
      </footer>
    </main>
  );
}
