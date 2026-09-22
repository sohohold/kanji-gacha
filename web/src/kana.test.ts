import { describe, it, expect } from "vitest";
import cases from "../../shared/kana-cases.json";
import { validate, normalize, morae, type Input } from "./kana";
describe("shared Go/TypeScript input contract", () => {
  for (const c of cases)
    it(c.name, () => {
      const issue = validate(c.input as Input);
      if (c.valid) {
        expect(issue).toBeUndefined();
        expect(normalize(c.input.reading)).toBe(c.normalized);
        expect(morae(normalize(c.input.reading))).toEqual(c.morae);
      } else {
        expect(issue?.code).toBe(c.code);
        expect(issue?.field).toBe(c.field);
      }
    });
});
