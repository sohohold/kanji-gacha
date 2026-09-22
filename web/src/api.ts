import type { components } from "./api.generated";
export type Glyph = components["schemas"]["Glyph"];
export type Input = components["schemas"]["Input"];
export class APIError extends Error {
  constructor(public problem: components["schemas"]["Problem"]) {
    super(problem.code);
  }
}
async function result<T>(r: Response): Promise<T> {
  if (!r.ok) {
    let p;
    try {
      p = await r.json();
    } catch {
      p = { code: "network", status: r.status };
    }
    throw new APIError(p);
  }
  return r.json();
}
export async function generate(input: Input): Promise<Glyph> {
  const body = JSON.stringify(input);
  const bytes = new TextEncoder().encode(body);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const hash = [...new Uint8Array(digest)]
    .map((n) => n.toString(16).padStart(2, "0"))
    .join("");
  return result(
    await fetch("/api/v1/glyphs", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "x-amz-content-sha256": hash,
      },
      body,
    }),
  );
}
export async function getGlyph(id: string): Promise<Glyph> {
  return result(await fetch(`/api/v1/glyphs/${encodeURIComponent(id)}`));
}
