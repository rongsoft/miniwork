// 全页面截图：只读，不点任何写操作按钮。
// 用法：先起桥（MW_DB=/tmp/mw-look.db ... go test -tags bridge -run TestServeBridge），再 node e2e/shots.mjs
import { chromium } from "playwright-core";
import fs from "node:fs";

const EXE =
  process.env.HOME +
  "/Library/Caches/ms-playwright/chromium-1248/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing";
const BASE = "http://127.0.0.1:8732";
const OUT = process.env.OUT || "/tmp/look";
fs.mkdirSync(OUT, { recursive: true });

const b = await chromium.launch({ executablePath: EXE });
const p = await b.newPage({ viewport: { width: 1360, height: 1100 }, deviceScaleFactor: 2 });
const errs = [];
p.on("pageerror", (e) => errs.push("PAGEERROR " + e.message));
p.on("console", (m) => {
  if (m.type() === "error") errs.push("CONSOLE " + m.text().slice(0, 240));
});

await p.goto(`${BASE}/dist/index.html`, { waitUntil: "networkidle" });
await p.waitForTimeout(1200);

const views = ["today", "library", "material", "teardown", "title", "write", "agenda", "calendar", "publish", "review", "account", "memory", "settings"];
for (const v of views) {
  const nav = p.locator(`.nav-item[data-view="${v}"]`);
  if ((await nav.count()) === 0) {
    console.log("SKIP 无导航项:", v);
    continue;
  }
  await nav.first().click();
  await p.waitForTimeout(900);
  await p.screenshot({ path: `${OUT}/${v}.png`, fullPage: true });
  const txt = await p.evaluate((n) => (document.getElementById("view-" + n)?.innerText || "").replace(/\s+/g, " ").slice(0, 300), v);
  console.log(`[${v}] ${txt}`);
}

// 发布台三平台的状态文本
await p.locator('.nav-item[data-view="publish"]').first().click();
await p.waitForTimeout(800);
const plats = await p.evaluate(() => {
  const box = document.getElementById("autoPlats");
  return box ? box.innerText.replace(/\s+/g, " ") : "无 autoPlats";
});
console.log("[发布台平台]", plats);

// 设置页 key 区文本
await p.locator('.nav-item[data-view="settings"]').first().click();
await p.waitForTimeout(800);
const st = await p.evaluate(() => (document.getElementById("view-settings")?.innerText || "").replace(/\s+/g, " ").slice(0, 900));
console.log("[设置页]", st);

console.log("---- 报错 ----");
console.log(errs.length ? errs.slice(0, 10).join("\n") : "无");
await b.close();
