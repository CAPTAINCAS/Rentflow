import { describe, expect, it } from "vitest";
import { API_BASE, apiUrl } from "@/lib/api";

describe("API base URL", () => {
  it("uses same-origin relative API paths when no API base URL is configured", () => {
    expect(API_BASE).toBe("");
    expect(apiUrl("/dashboard")).toBe("/api/v1/dashboard");
    expect(apiUrl("/reports/arrears.csv")).toBe("/api/v1/reports/arrears.csv");
  });

  it("never duplicates the /api/v1 prefix or trailing slashes", () => {
    expect(apiUrl("payments")).toBe("/api/v1/payments");
    expect(apiUrl("/payments/1/assign")).toBe("/api/v1/payments/1/assign");
  });
});