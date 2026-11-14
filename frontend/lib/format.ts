export function formatMoney(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  const amount = typeof value === "number" ? value : Number(value);
  return Number.isFinite(amount)
    ? `KES ${new Intl.NumberFormat("en-KE", { maximumFractionDigits: 2 }).format(amount)}`
    : "—";
}

export function formatDate(value: unknown): string {
  if (!value) return "—";
  const date = new Date(String(value));
  return Number.isNaN(date.valueOf())
    ? String(value)
    : new Intl.DateTimeFormat("en-KE", { day: "numeric", month: "short", year: "numeric" }).format(date);
}
