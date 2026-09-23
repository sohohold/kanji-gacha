import { test, expect } from "@playwright/test";
test("generate, share, reload the same SVG, and retrieve OGP", async ({
  page,
  request,
}) => {
  await page.goto("/");
  await page.getByRole("tab", { name: "音読み" }).click();
  await page.getByRole("textbox", { name: "読み", exact: true }).fill("ｷｬ");
  await page.getByRole("button", { name: "ガチャを回す" }).click();
  await expect(page.locator(".canvas figcaption")).toHaveText("キャ");
  await expect(
    page.getByRole("button", { name: "共有", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/g\/[0-9a-f]{32}$/);
  const svg = await page.locator(".glyph").innerHTML();
  const href = new URL(await page.locator(".share-url").inputValue()).pathname;
  await page.goto(href!);
  await expect(page.locator(".glyph")).toHaveCount(1);
  expect(await page.locator(".glyph").innerHTML()).toBe(svg);
  expect(
    await page.locator('meta[property="og:image"]').getAttribute("content"),
  ).toContain("/og/");
  const id = href!.split("/").pop();
  const png = await request.get(`/og/${id}.png`);
  expect(png.status()).toBe(200);
  expect(png.headers()["content-type"]).toBe("image/png");
  await page.reload();
  await expect(page.locator(".glyph")).toHaveCount(1);
  expect(await page.locator(".glyph").innerHTML()).toBe(svg);
});
test("reject invalid input and preserve IME composition", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: "音読み" }).click();
  const input = page.getByRole("textbox", { name: "読み", exact: true });
  await input.fill("キョャ");
  await page.getByRole("button", { name: "ガチャを回す" }).click();
  await expect(page.locator("#input-error")).toContainText("小書き");
  await input.fill("トウキョウ");
  await page.getByRole("button", { name: "ガチャを回す" }).click();
  await expect(page.locator("#input-error")).toContainText("2拍");
  await input.dispatchEvent("compositionstart");
  await input.fill("漢字");
  await expect(
    page.getByRole("button", { name: "ガチャを回す" }),
  ).toBeDisabled();
  await expect(page.locator("#input-error")).toBeEmpty();
  await input.fill("せい");
  await input.dispatchEvent("compositionend");
  await expect(
    page.getByRole("button", { name: "ガチャを回す" }),
  ).toBeEnabled();
});
test("kun input on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("tab", { name: "訓読み" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page
    .getByRole("textbox", { name: "読み（語幹）", exact: true })
    .fill("あお");
  await page.getByRole("textbox", { name: "送り仮名", exact: true }).fill("い");
  await page.getByRole("button", { name: "ガチャを回す" }).click();
  await expect(page.locator(".canvas figcaption")).toHaveText("あお（い）");
  await page.goBack();
  await expect(
    page.getByRole("button", { name: "ガチャを回す" }),
  ).toBeEnabled();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});
test("unknown share link has an error", async ({ page }) => {
  const res = await page.goto("/g/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
  expect(res?.status()).toBe(404);
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: "生成結果が見つかりませんでした。" }),
  ).toBeVisible();
});
test("malformed share link has an error", async ({ page }) => {
  const res = await page.goto("/g/no");
  expect(res?.status()).toBe(400);
  await expect(
    page.getByRole("alert").filter({ hasText: "共有URLを確認してください。" }),
  ).toBeVisible();
});
