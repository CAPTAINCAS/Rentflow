import type { ReactNode, ButtonHTMLAttributes } from "react";

export function Loading({ label = "Loading account data" }: { label?: string }) {
  return <div className="state-line" role="status"><span className="spinner" aria-hidden="true" />{label}…</div>;
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return <div className="notice notice-error" role="alert"><span>{message}</span>{onRetry && <button className="text-button" onClick={onRetry}>Try again</button>}</div>;
}

export function EmptyState({ title, detail, children }: { title: string; detail: string; children?: ReactNode }) {
  return <div className="empty-state"><span className="empty-rule" aria-hidden="true" /><h3>{title}</h3><p>{detail}</p>{children}</div>;
}

export function StatusPill({ value }: { value: unknown }) {
  const label = typeof value === "string" ? value.replaceAll("_", " ") : "Unknown";
  const normalized = label.toLowerCase();
  const kind = /paid|active|occupied|complete|collected|settled/.test(normalized) ? "good" : /pending|due|vacant|unmatched|review|partial/.test(normalized) ? "warm" : /fail|overdue|inactive|cancel/.test(normalized) ? "bad" : "neutral";
  return <span className={`status status-${kind}`}>{label}</span>;
}

export function Field({ label, error, children, hint }: { label: string; error?: string; children: ReactNode; hint?: string }) {
  return <label className="field"><span className="field-label">{label}</span>{children}{hint && <span className="field-hint">{hint}</span>}{error && <span className="field-error" role="alert">{error}</span>}</label>;
}

export function Button({ children, variant = "primary", ...props }: { children: ReactNode; variant?: "primary" | "outline" | "ghost" | "danger" } & ButtonHTMLAttributes<HTMLButtonElement>) {
  const cls = variant === "primary" ? "button button-primary" : variant === "outline" ? "button button-outline" : variant === "danger" ? "button button-danger" : "button button-ghost";
  return <button className={cls} {...props}>{children}</button>;
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <section className={`panel ${className}`.trim()}>{children}</section>;
}

export function Badge({ children, tone = "neutral" }: { children: ReactNode; tone?: "good" | "warm" | "bad" | "neutral" }) {
  return <span className={`status status-${tone}`}>{children}</span>;
}
