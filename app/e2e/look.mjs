// 走查：把这一轮改动逐条在真界面上验一遍，并截图留证。
//
// 只读 + 只在本地造封面，不点任何会真发出去的按钮。
// 用法：先起桥（MW_DB=/tmp/mw-look.db ... go test -tags bridge -run TestServeBridge），
//      再 node e2e/look.mjs
import { chromium } from "playwright-core";
import fs from "node:fs";

const EXE =
  process.env.HOME +
  "/Library/Caches/ms-playwright/chromium-1248/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing";
const BASE = process.env.BASE || "http://127.0.0.1:8732";
const OUT = process.env.OUT || "/tmp/look3";
fs.mkdirSync(OUT, { recursive: true });

const fails = [];
const ok = (cond, msg) => {
  if (!cond) fails.push(msg);
  console.log(`${cond ? "PASS" : "FAIL"} ${msg}`);
};

const b = await chromium.launch({ executablePath: EXE });
const p = await b.newPage({ viewport: { width: 1360, height: 1100 }, deviceScaleFactor: 2 });
const errs = [];
p.on("pageerror", (e) => errs.push("PAGEERROR " + e.message));
p.on("console", (m) => {
  if (m.type() === "error") errs.push("CONSOLE " + m.text().slice(0, 240));
});

await p.goto(`${BASE}/dist/index.html`, { waitUntil: "networkidle" });
await p.waitForTimeout(1500);

// ---- 1. 首页：文案和数字必须对得上 ----
await p.locator('.nav-item[data-view="today"]').first().click();
await p.waitForTimeout(1200);
const hero = await p.evaluate(() => {
  const line = document.getElementById("heroLine")?.innerText.replace(/\s+/g, " ") || "";
  return { line, match: document.getElementById("heroMatch2")?.textContent || "" };
});
console.log("[首页]", JSON.stringify(hero));
if (hero.match === "0") {
  ok(!/高度匹配/.test(hero.line), "匹配数为 0 时不能写「高度匹配」");
  ok(/挑挑看|还没有/.test(hero.line), "匹配数为 0 时要给出下一步，而不是一串数字");
} else {
  ok(/\d+ 条/.test(hero.line), "匹配数非 0 时句子里要有数字");
}
await p.screenshot({ path: `${OUT}/today.png`, fullPage: true });

// ---- 2. 成稿页：主按钮是「发到平台」 ----
await p.locator('.nav-item[data-view="write"]').first().click();
await p.waitForTimeout(1500);
const edVis = await p.locator("#wrEditor").isVisible();
ok(edVis, "成稿页要有稿可编辑（否则后面的发布入口测不到）");
const send = p.locator("#wrSend");
ok((await send.count()) === 1, "成稿页要有「发到平台」按钮");
ok(await send.isVisible(), "「发到平台」要可见");
const sendClass = await send.getAttribute("class");
ok(/btn-primary/.test(sendClass || ""), "「发到平台」必须是主按钮，不能和复制同级");
const copyClass = await p.locator("#wrCopy").getAttribute("class");
ok(!/btn-primary/.test(copyClass || ""), "「复制全部」要降成次要按钮");
await p.screenshot({ path: `${OUT}/write.png`, fullPage: true });

// ---- 3. 点「发到平台」→ 发布面板打开，标题带过来 ----
await send.click();
await p.waitForTimeout(2000);
ok(await p.locator("#autoPop").isVisible(), "点「发到平台」要打开发布面板");
const autoTopic = (await p.locator("#autoTopic").innerText()).trim();
console.log("[面板标题]", autoTopic);
ok(autoTopic.length > 0, "面板上要显示是哪一稿");

// ---- 4. 生成封面 ----
const coverBtn = p.locator("#autoCover");
ok(await coverBtn.isVisible(), "图片那一步要有「生成封面」按钮");
const beforeCount = await p.locator("#autoThumbs .auto-thumb").count();
await coverBtn.click();
await p.waitForTimeout(3000);
const thumbs = await p.evaluate(() => {
  const box = document.getElementById("autoThumbs");
  const figs = [...box.querySelectorAll(".auto-thumb")];
  return figs.map((f) => ({
    gen: f.classList.contains("gen"),
    label: f.querySelector("figcaption")?.textContent || "",
    src: (f.querySelector("img")?.getAttribute("src") || "").slice(0, 30),
    w: f.querySelector("img")?.naturalWidth || 0,
  }));
});
console.log("[缩略图]", JSON.stringify(thumbs));
ok(thumbs.length > beforeCount, "生成封面后要新增一张");
ok(thumbs[0]?.gen === true, "生成的封面要排在第一位并标出来");
ok(thumbs[0]?.src.startsWith("data:image/png"), "封面要有能看见的预览，不能只显示文件名");
ok(thumbs[0]?.w > 100, "预览图要真的解码出来了（naturalWidth>0）");
const note = await p.locator("#autoMediaNote").innerText();
console.log("[素材状态]", note);
await p.screenshot({ path: `${OUT}/autoPop.png`, fullPage: true });

await p.locator("#autoClose").click();
await p.waitForTimeout(600);

// ---- 5. 复盘页：同步按钮在页头 ----
await p.locator('.nav-item[data-view="review"]').first().click();
await p.waitForTimeout(1800);
const sync = await p.evaluate(() => {
  const box = document.getElementById("noteSync");
  if (!box) return { exists: false };
  const head = box.closest(".page-head");
  return {
    exists: true,
    hidden: box.hidden,
    inHead: !!head,
    html: box.innerText.replace(/\s+/g, " ").slice(0, 80),
  };
});
console.log("[同步条]", JSON.stringify(sync));
ok(sync.exists, "复盘页要有同步区");
ok(sync.inHead, "同步按钮要放在页头，不能埋到页面底部");
await p.screenshot({ path: `${OUT}/review.png`, fullPage: true });
// 只截页头，证明它在第一屏
await p.screenshot({ path: `${OUT}/review-head.png`, clip: { x: 0, y: 0, width: 1360, height: 340 } });

// ---- 6. 发布台：页面正常，不报错 ----
await p.locator('.nav-item[data-view="publish"]').first().click();
await p.waitForTimeout(1500);
await p.screenshot({ path: `${OUT}/publish.png`, fullPage: true });

console.log("---- 控制台报错 ----");
console.log(errs.length ? errs.slice(0, 8).join("\n") : "无");
console.log("---- 结论 ----");
console.log(fails.length ? `${fails.length} 条不通过：\n` + fails.join("\n") : "全部通过");

await b.close();
process.exit(fails.length || errs.length ? 1 : 0);
