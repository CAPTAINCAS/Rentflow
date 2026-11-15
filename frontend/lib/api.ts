export type JsonRecord = Record<string, unknown>;

export class ApiError extends Error {
  readonly status: number;
  readonly details: unknown;

  constructor(message: string, status: number, details?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.details = details;
  }
}

/**
 * Base path of the RentFlow API.
 *
 * - Relative (default) when the browser talks to a same-origin gateway that
 *   serves both the frontend and `/api/v1` (the Docker image, or a platform
 *   rewrite/proxy).
 * - Absolute when the API is deployed separately from the frontend, set with
 *   `NEXT_PUBLIC_API_BASE_URL=https://api.example.com` at build time.
 */
export const API_BASE = (process.env.NEXT_PUBLIC_API_BASE_URL ?? "").replace(/\/+$/, "");

export function apiUrl(path: string): string {
  const suffix = path.startsWith("/") ? path : `/${path}`;
  return `${API_BASE}/api/v1${suffix}`;
}

function csrfCookie(): string | null {
  if (typeof document === "undefined") return null;
  const part = document.cookie.split(";").map(value => value.trim()).find(value => value.startsWith("rf_csrf="));
  if (!part) return null;
  const raw = part.slice("rf_csrf=".length);
  try { return decodeURIComponent(raw); } catch { return raw; }
}

export async function api<T = unknown>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  const isFormData = typeof FormData !== "undefined" && init.body instanceof FormData;
  if (init.body && !isFormData && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  headers.set("Accept", "application/json");
  const method = (init.method ?? "GET").toUpperCase();
  if (["POST", "PUT", "PATCH", "DELETE"].includes(method)) {
    const token = csrfCookie();
    if (token) headers.set("X-CSRF-Token", token);
  }
  const response = await fetch(apiUrl(path), { ...init, headers, credentials: "include", cache: "no-store" });
  const contentType = response.headers.get("content-type") ?? "";
  let body: unknown = null;
  if (response.status !== 204) {
    body = contentType.includes("json")
      ? await response.json().catch(() => null)
      : await response.text().catch(() => "");
  }
  if (!response.ok) {
    const record = asRecord(body);
    const error = asRecord(record?.error);
    const message = stringValue(error?.message ?? record?.message ?? record?.error) || `Request failed (${response.status})`;
    throw new ApiError(message, response.status, body);
  }
  const record = asRecord(body);
  return (record && Object.prototype.hasOwnProperty.call(record, "data") ? record.data : body) as T;
}

export function asRecord(value: unknown): JsonRecord | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as JsonRecord : null;
}

export function stringValue(value: unknown): string {
  if (typeof value === "string" || typeof value === "number") return String(value);
  return "";
}

export function firstValue(record: unknown, keys: string[]): unknown {
  const source = asRecord(record);
  if (!source) return undefined;
  for (const key of keys) {
    const value = source[key];
    if (value !== undefined && value !== null && value !== "") return value;
  }
  return undefined;
}

export function collection(value: unknown, key?: string): JsonRecord[] {
  const asRows = (candidate: unknown): JsonRecord[] => Array.isArray(candidate)
    ? candidate.map(asRecord).filter((row): row is JsonRecord => row !== null)
    : [];
  const direct = asRows(value);
  if (direct.length || Array.isArray(value)) return direct;
  const record = asRecord(value);
  if (!record) return [];
  if (key && Object.prototype.hasOwnProperty.call(record, key)) return asRows(record[key]);
  for (const candidate of [record.data, record.items, record.results]) {
    if (Array.isArray(candidate)) return asRows(candidate);
  }
  return [];
}
