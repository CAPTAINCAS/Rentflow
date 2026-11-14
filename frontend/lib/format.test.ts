import { describe, expect, it } from "vitest";
import { formatDate, formatMoney } from "@/lib/format";

describe("formatMoney", () => {
  it("formats Kenyan shillings with readable grouped digits", () => {
    expect(formatMoney(84500)).toBe("KES 84,500");
  });

  it("keeps decimal amounts and marks missing amounts unavailable", () => {
    expect(formatMoney(1250.5)).toBe("KES 1,250.5");
    expect(formatMoney(null)).toBe("—");
    expect(formatMoney("not-a-number")).toBe("—");
  });
});

describe("formatDate", () => {
  it("renders a valid API date in a concise Kenyan locale", () => {
    expect(formatDate("2024-04-01T12:00:00Z")).toMatch(/1 Apr 2024/);
  });

  it("keeps an unparseable API date visible and marks empty values unavailable", () => {
    expect(formatDate("unconfirmed date")).toBe("unconfirmed date");
    expect(formatDate(null)).toBe("—");
  });
});
