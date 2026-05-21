import { describe, expect, it } from "vitest";
import { humanBytes, pct, shortHash } from "./format";

describe("format helpers", () => {
  it("humanBytes scales units", () => {
    expect(humanBytes(0)).toBe("0 B");
    expect(humanBytes(1024)).toBe("1.00 KB");
    expect(humanBytes(1024 * 1024 * 5)).toContain("MB");
  });
  it("pct handles nullish", () => {
    expect(pct(null)).toBe("—");
    expect(pct(12.345)).toBe("12.3%");
  });
  it("shortHash truncates", () => {
    expect(shortHash("abcdef1234567890", 8)).toBe("abcdef12…");
    expect(shortHash("short", 12)).toBe("short");
  });
});
