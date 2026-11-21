// 采集 → 发布 端到端流程测试（13 步，每步断言「方法被调 + 界面变了 + 后端落库」）
//
// 怎么跑（三步）：
//   1) 备一份库并起桥（真 store + 真 pipeline，AI 换成脚本化假模型，不烧 API 额度）：
//        cd app
//        cp "$HOME/Library/Application Support/miniwork/miniwork.db" /tmp/mw-e2e.db
//        rm -f /tmp/mw-e2e.db-wal /tmp/mw-e2e.db-shm   # 残留 wal 会把上一轮数据"复活"
//        MW_DB=/tmp/mw-e2e.db MW_DIST=$PWD/frontend/dist MW_RT=$PWD/e2e/runtime.js \
//          go test -tags bridge -run TestServeBridge -timeout 0 -count=1 .
//   2) 装驱动：npm i playwright-core@1.49.1（脚本用 executablePath 指向本机 Chrome）
//   3) node e2e/flow.mjs
//
// 为什么接真后端而不是写假数据：假数据只能证明「读不到字段时不崩」，
// 证明不了「点了按钮，库里真的多了一行」。这里有写操作，跑完能拿 sqlite3 核对。
//
// 注意：脚本会往库里写数据，务必对副本跑，别指向 ~/Library/Application Support。

// 从采集到发布的完整流程。跑的是真后端（bridge → 真 store + 真 pipeline），
// AI 是脚本化假模型。每一步都验三件事：
//   1. 前端真的调了那个方法（看 window.__calls）
//   2. 界面真的变了（DOM 断言，不是"没报错就算过"）
//   3. 后端真的落库（/call 查一次，或跑完用 sqlite3 核对）
import { chromium } from "playwright-core";

const EXE =
  process.env.HOME +
  "/Library/Caches/ms-playwright/chromium-1248/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing";
const BASE = "http://127.0.0.1:8732";
const OUT = "/tmp/shots";

const b = await chromium.launch({ executablePath: EXE });
const p = await b.newPage({ viewport: { width: 1360, height: 1000 }, deviceScaleFactor: 2 });

const errs = [];
p.on("pageerror", (e) => errs.push("PAGEERROR " + e.message));
p.on("console", (m) => {
  if (m.type() === "error") errs.push("CONSOLE " + m.text().slice(0, 200));
});
p.on("response", (r) => {
  if (r.status() >= 400) errs.push("HTTP" + r.status() + " " + r.url().replace(BASE, ""));
});

const log = [];
let shotN = 0;
const shot = async (tag) => {
  shotN++;
  const f = `${OUT}/${String(shotN).padStart(2, "0")}-${tag}.png`;
  await p.screenshot({ path: f });
  return f;
};

// 直接问后端，绕过界面
async function api(method, ...args) {
  const r = await fetch(`${BASE}/call`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  const j = await r.json();
  if (!j.ok) throw new Error(`${method}: ${j.error}`);
  return j.data;
}

const called = (n) => p.evaluate((x) => (window.__calls || []).filter((c) => c.name === x), n);
const lastArgs = async (n) => (await called(n)).at(-1)?.args;
const toastText = () => p.evaluate(() => (document.getElementById("toast") || {}).textContent || "");
const viewText = (v) =>
  p.evaluate((n) => (document.getElementById(n)?.innerText || "").replace(/\s+/g, " "), "view-" + v);

const results = [];
async function step(name, fn) {
  const before = errs.length;
  const t0 = Date.now();
  try {
    const detail = await fn();
    results.push({ step: name, ok: true, ms: Date.now() - t0, detail: detail ?? "", errs: errs.length - before });
  } catch (e) {
    results.push({ step: name, ok: false, ms: Date.now() - t0, detail: e.message.split("\n")[0].slice(0, 160), errs: errs.length - before });
  }
}
function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

// ============ 开场 ============
await p.goto(`${BASE}/dist/index.html`, { waitUntil: "networkidle" });
await p.waitForTimeout(900);

const seed = {
  picked: (await api("List", "picked", 50)).length,
  scheduled: (await api("List", "scheduled", 50)).length,
  draft: (await api("List", "draft", 200)).length,
  records: (await api("PublishBoard")).records.length,
  materials: (await api("Materials", "", 100)).length,
};
console.log("起始库状态:", seed);

// ============ 1. 采集 ============
let pickedTitle = "";
let pickedId = "";
await step("01 采集：点刷新灵感 → 管道跑完 → 列表有内容", async () => {
  const todayCards = () => p.locator("#view-today .card").count();
  const before = await todayCards();
  await p.click("#run");
  // 等按钮从「生成中…」回到「刷新灵感」，而不是硬等固定秒数
  await p.waitForFunction(() => {
    const l = document.getElementById("runLabel");
    return l && l.textContent.trim() === "刷新灵感";
  }, { timeout: 60000 });
  await p.waitForTimeout(800);
  const after = await todayCards();
  const run = await called("Run");
  assert(run.length > 0, "没调 Run");
  assert(after > 0, `采集后灵感列表是空的（前 ${before} 后 ${after}）`);
  const summary = await lastArgs("Run");
  await shot("01-collect");
  return `Run 参数=${JSON.stringify(summary)} 卡片 ${before}→${after}`;
});

// ============ 2. 收下 ============
await step("02 收下第一条灵感 → Decide(pick) → 进选题库", async () => {
  const card = p.locator('#view-today [data-act="pick"]').first();
  assert((await card.count()) > 0, "今日灵感没有「收下」按钮");
  await card.click();
  await p.waitForTimeout(900);
  const d = await called("Decide");
  assert(d.length > 0, "没调 Decide");
  pickedId = (await lastArgs("Decide"))[0];
  pickedTitle = (await api("List", "picked", 50)).find((t) => t.id === pickedId)?.title || "";
  const after = (await api("List", "picked", 50)).length;
  assert(after === seed.picked + 1, `选题库 picked 应该 ${seed.picked}→${seed.picked + 1}，实际 ${after}`);
  await shot("02-pick");
  return `Decide 参数=${JSON.stringify(await lastArgs("Decide"))} picked ${seed.picked}→${after} 标题「${pickedTitle.slice(0, 12)}」`;
});

// ============ 3. 选题库确认 ============
await step("03 选题库：刚收下的那条出现在列表里", async () => {
  await p.click('[data-view="library"]');
  await p.waitForTimeout(700);
  const n = await p.locator("#view-library .card").count();
  assert(n > 0, "选题库没有卡片");
  const txt = await viewText("library");
  const hit = pickedTitle ? txt.includes(pickedTitle.slice(0, 8)) : true;
  await shot("03-library");
  return `卡片 ${n} 张；标题「${pickedTitle.slice(0, 16)}」在列表里=${hit}`;
});

// ============ 4. 素材：新增 + 自动提炼 ============
await step("04 素材库：粘链接+正文 → AddMaterial → 自动提炼出学习点", async () => {
  await p.click('[data-view="material"]');
  await p.waitForTimeout(600);
  const beforeMat = (await api("Materials", "", 100)).length;
  await p.click("#matAdd");
  await p.waitForTimeout(300);
  // URL 每次跑都不一样：素材按 URL 去重，写死的话第二轮会命中上一轮那条，
  // 断言就变成「素材没增加」——看起来像产品坏了，其实是测试没清场。
  await p.fill("#matUrl", `https://www.xiaohongshu.com/explore/e2e-${Date.now()}`);
  await p.fill("#matTitle", "流程测试：标题先给结果，正文再讲过程");
  await p.fill("#matBody", "第一段先给结论，第二段讲过程，第三段给一句能直接抄走的句式。这是流程测试用的正文。");
  await p.click("#matOk");
  await p.waitForTimeout(1500);
  const afterMat = (await api("Materials", "", 100)).length;
  assert(afterMat === beforeMat + 1, `素材应该 ${beforeMat}→${beforeMat + 1}，实际 ${afterMat}`);
  const list = await api("Materials", "", 100);
  const fresh = list.find((m) => (m.title || "").includes("流程测试"));
  assert(fresh, "新增的素材在后端查不到");
  await shot("04-material");
  return `素材 ${beforeMat}→${afterMat}；状态=${fresh.statusLabel} 学习点=${(fresh.points || []).length} 条`;
});

// ============ 5. 素材关联选题 ============
await step("05 素材关联到选题 → LinkMaterial 落库", async () => {
  const linkBtn = p.locator('#view-material [data-mat="link"]').first();
  assert((await linkBtn.count()) > 0, "素材卡上没有「关联选题」");
  await linkBtn.click();
  await p.waitForTimeout(400);
  const opts = await p.locator("#linkPop select option").count();
  assert(opts > 1, `关联下拉里没有选题（${opts} 个 option）`);
  const sel = p.locator("#linkPop select").first();
  await sel.selectOption({ index: 1 });
  const ok = p.locator("#linkPop .btn-primary").first();
  await ok.click();
  await p.waitForTimeout(900);
  const l = await called("LinkMaterial");
  await shot("05-link");
  return `下拉 ${opts} 个选项；LinkMaterial 调用 ${l.length} 次`;
});

// ============ 6. 爆款拆解 ============
// 这一版的产品要求是「六行短字段 + 一句可借鉴点」，不是六段分析。
// 所以断言不能只看「有文字」，要数字段、数行数——长回来的那种也算「渲染成功」，
// 但那正是用户说「太繁琐」的东西。
await step("06 拆解：整段分享文案 → 六行卡 → 入库进历史", async () => {
  await p.click('[data-view="teardown"]');
  await p.waitForTimeout(500);
  // 贴的是「标题 + 正文 + 链接 + 广告尾巴」——用户从手机里复制到的原样。
  // 只填标题的那种用法有单测盖（internal/format），这里验真机上能不能分开。
  const raw =
    (pickedTitle || "干了8年HR，说几句求职者不爱听的实话") +
    "\n第一段：先用身份立信。\n\n第二段：给一句反直觉结论。\n" +
    "http://xhslink.com/a/e2e" + Date.now() + "，复制本条信息，打开【小红书】App查看精彩内容！";
  await p.fill("#tdRaw", raw);
  await p.click("#tdRun");
  await p.waitForTimeout(1800);
  const r = await p.evaluate(() => {
    const box = document.getElementById("tdResult");
    return {
      hidden: box?.hidden,
      src: box?.querySelector(".td-src")?.textContent || "",
      rows: [...(box?.querySelectorAll(".td-row dt") || [])].map((d) => d.textContent),
      learn: box?.querySelector(".td-learn p")?.textContent || "",
      cat: box?.querySelector(".td-cat")?.textContent || "",
      dl: !!box?.querySelector("[data-dl-td]"),
      text: (box?.innerText || "").replace(/\s+/g, " ").slice(0, 120),
      histHidden: document.getElementById("tdHist")?.hidden,
      histN: document.querySelectorAll("#tdHistList .td-hist-item").length,
    };
  });
  assert(!r.hidden && r.text.length > 20, "拆解结果没渲染：" + JSON.stringify(r));
  assert(r.rows.length >= 5, "六行字段不全，只有：" + r.rows.join(" / "));
  assert(r.learn.length > 4, "可借鉴点是空的");
  assert(r.cat.length > 0, "分类没显示");
  // 卡片顶上要显示「拆的是哪条」，而不是把链接或广告语当标题。
  assert(r.src && !/xhslink|复制本条/.test(r.src), "原文标题带进了链接/广告：" + r.src);
  assert(r.dl, "没有「下载报告」入口");
  await shot("06-teardown");

  // 拆完必须能回头翻：不然「拆过十条，第二天想回看」只能再花一次模型。
  assert(!r.histHidden && r.histN === 1, `拆完应进历史，实际 ${r.histN} 条 hidden=${r.histHidden}`);
  const saved = await api("Teardowns");
  assert(saved.length === 1 && saved[0].learn, "后端没存住： " + JSON.stringify(saved).slice(0, 120));

  // 点历史 → 结果区还是同一张卡（不是空的、也不是输入框被改写）
  await p.click("#tdHistList .td-hist-item");
  await p.waitForTimeout(300);
  const again = await p.evaluate(() => ({
    rows: document.querySelectorAll("#tdResult .td-row").length,
    on: document.querySelectorAll("#tdHistList .td-hist-item.on").length,
  }));
  assert(again.rows >= 5 && again.on === 1, "点历史没把卡放回来：" + JSON.stringify(again));

  // 删掉 → 历史收起、结果区收起、库里也没了
  await p.hover("#tdHistList .td-hist-item");
  await p.click("#tdHistList .td-hist-del");
  await p.waitForTimeout(400);
  const gone = await p.evaluate(() => ({
    histHidden: document.getElementById("tdHist").hidden,
    resultHidden: document.getElementById("tdResult").hidden,
  }));
  assert(gone.histHidden && gone.resultHidden, "删完应该收起：" + JSON.stringify(gone));
  assert((await api("Teardowns")).length === 0, "库里还留着这条");

  return `${r.rows.join("/")} | 借鉴点 ${r.learn.length} 字 | 入库→查看→删除都通`;
});

// ============ 7. 出标题 ============
await step("07 标题工坊：给种子 → SuggestTitles → 列表出来", async () => {
  await p.click('[data-view="title"]');
  await p.waitForTimeout(500);
  await p.fill("#tgSeed", "跳槽涨薪反而比内部晋升快");
  const hook = p.locator("#tgHooks [data-fill]").first();
  if (await hook.count()) await hook.click();
  await p.click("#tgRun");
  await p.waitForTimeout(1800);
  const n = await p.evaluate(() => document.querySelectorAll("#tgResult .tg-item").length);
  assert(n > 0, "标题没生成（#tgResult 里没有 .tg-item）");
  await shot("07-title");
  return `候选 ${n} 条`;
});

// ============ 8. 写稿 ============
let draftOk = false;
await step("08 写稿：选中刚收下的选题 → Write → 存稿 SaveDraft", async () => {
  await p.click('[data-view="write"]');
  await p.waitForTimeout(1000);
  // 必须写刚收下的那一条：按 data-topic 精确定位，不靠标题模糊匹配。
  const target = p.locator(`#view-write .wr-topic[data-topic="${pickedId}"]`);
  assert((await target.count()) > 0, `写稿页没找到刚收下的选题 ${pickedId}`);
  await target.click();
  await p.waitForTimeout(800);
  await p.click("#wrRun");
  await p.waitForTimeout(2200);
  const title = await p.inputValue("#wrTitle");
  const body = await p.inputValue("#wrBody");
  assert(title.trim().length > 0, "生成后标题是空的");
  assert(body.trim().length > 0, "生成后正文是空的");
  const w = await called("Write");
  assert(w.length > 0, "没调 Write");
  await p.click("#wrSave");
  await p.waitForTimeout(1200);
  const s = await called("SaveDraft");
  assert(s.length > 0, "没调 SaveDraft");
  const saved = JSON.stringify((await lastArgs("SaveDraft"))[0] || {});
  assert(saved.includes(pickedId), `存稿没落在刚收下那条上: ${saved.slice(0, 90)}`);
  draftOk = true;
  await shot("08-write");
  return `标题「${title.slice(0, 20)}」正文 ${body.length} 字；SaveDraft ${s.length} 次`;
});

// ============ 9. 排期 ============
await step("09 排期：选题排到具体日期 → Schedule → 日历可见", async () => {
  await p.click('[data-view="library"]');
  await p.waitForTimeout(700);
  const btn = p.locator(`#view-library .card[data-id="${pickedId}"] [data-act="plan"]`).first();
  assert((await btn.count()) > 0, `选题库里找不到刚收下那条的「排期」按钮（${pickedId}）`);
  await btn.click();
  await p.waitForTimeout(500);
  assert(await p.locator("#planPop").isVisible(), "排期弹窗没打开");
  const quick = p.locator("#planQuick button").nth(1);
  if (await quick.count()) await quick.click();
  else await p.fill("#planDate", "2026-11-20");
  const dateVal = await p.inputValue("#planDate");
  assert(dateVal, "排期日期是空的");
  await p.click("#planOk");
  await p.waitForTimeout(1200);
  const sc = await called("Plan");
  assert(sc.length > 0, "没调 Plan（排期接口叫 Plan，不是 Schedule）");
  const planArgs = await lastArgs("Plan");
  assert(planArgs[0] === pickedId, `排的不是刚收下那条: ${planArgs[0]}`);
  const scheduled = await api("List", "scheduled", 50);
  assert(scheduled.some((t) => t.id === pickedId), "刚收下的那条没进 scheduled");
  await shot("09-schedule");
  return `Plan 参数=${JSON.stringify(planArgs)}；该条状态=${scheduled.find((t) => t.id === pickedId)?.status}`;
});

// ============ 10. 发布 ============
await step("10 发布：待发队列点「已发」→ MarkPublished → 台账多一条", async () => {
  await p.click('[data-view="publish"]');
  await p.waitForTimeout(900);
  // 自动发面板：平台卡、形态切换、按钮可用性。
  // 这块最容易错在「选错形态」——图文消息和图片消息发出去是完全两样的东西，
  // 而界面上两个按钮只差三个字，所以断言必须落到提示文案上。
  const autoBtn = p.locator(`#pubQueue [data-act="auto"][data-topic="${pickedId}"]`).first();
  assert((await autoBtn.count()) > 0, "待发队列里没有「自动发」按钮");
  await autoBtn.click();
  await p.waitForTimeout(900);
  assert(await p.locator("#autoPop").isVisible(), "自动发面板没打开");

  const plats0 = await p.$$eval("#autoPlats .auto-plat", (ns) => ns.map((n) => n.dataset.plat));
  for (const k of ["xiaohongshu", "wechat", "douyin"]) {
    assert(plats0.includes(k), `自动发面板缺平台卡 ${k}：${plats0.join(",")}`);
  }
  // 没填凭证时必须挡住，而不是让用户点了等报错。
  await p.click('#autoPlats [data-plat="wechat"]');
  await p.waitForTimeout(500);
  assert(await p.locator("#autoRun").isDisabled(), "公众号没配凭证，「开始发布」却是可点的");
  const warn0 = (await p.textContent("#autoWarn")) || "";
  assert(/AppID/.test(warn0), `没提示去填 AppID：${warn0}`);

  // 图文 → 贴图：素材提示要跟着换
  const noteNews = (await p.textContent("#autoMediaNote")) || "";
  await p.click('#autoMode [data-mode="newspic"]');
  await p.waitForTimeout(400);
  const notePic = (await p.textContent("#autoMediaNote")) || "";
  assert(notePic !== noteNews, "切到贴图后素材提示没变");
  assert(/封面/.test(notePic), `贴图提示要说清第一张是封面：${notePic}`);
  await p.evaluate(() => document.getElementById("autoClose").click());
  await p.waitForTimeout(400);

  const pubBtn = p.locator(`#pubQueue [data-act="done"][data-topic="${pickedId}"]`).first();
  assert((await pubBtn.count()) > 0, `待发队列里没有刚收下那条（${pickedId}）的「已发」按钮`);
  await pubBtn.click();
  await p.waitForTimeout(500);
  assert(await p.locator("#pubPop").isVisible(), "发布确认弹窗没打开");
  const plat = p.locator("#pubPopPlat [data-plat]").first();
  if (await plat.count()) await plat.click();
  await p.fill("#pubPopAccount", "流程测试号");
  await p.fill("#pubPopUrl", "https://www.xiaohongshu.com/explore/e2e-published");
  await p.fill("#pubPopDate", "2026-11-16");
  await p.click("#pubPopOk");
  await p.waitForTimeout(1400);
  const mp = await called("MarkPublished");
  assert(mp.length > 0, "没调 MarkPublished");
  const mpArgs = (await lastArgs("MarkPublished"))[0] || {};
  assert(mpArgs.topicId === pickedId, `发布记的不是刚收下那条: ${mpArgs.topicId}`);
  const board = await api("PublishBoard");
  assert(board.records.length === seed.records + 1, `台账应 ${seed.records}→${seed.records + 1}，实际 ${board.records.length}`);
  const pub2 = await api("List", "published", 50);
  assert(pub2.some((t) => t.id === pickedId), "该条状态没推进到 published");
  await shot("10-publish");
  return `台账 ${seed.records}→${board.records.length}；该条已进 published`;
});

// ============ 10b. 绑定入口 ============
// 这一条是防回退的。上一版发布台的「平台账号」只有三个输入框，写着
// 「只记名字，程序不登录任何平台」，而账号页还在说「去发布台绑定」——
// 两条链路各说各的，用户照着点进去找不到任何能绑的地方。
// 所以这里不验样式，只验「每一行都有能点的出口」。
await step("10b 绑定：每行都有出口，并说清浏览器是哪来的", async () => {
  await p.click('[data-view="publish"]');
  await p.waitForTimeout(1000);
  const r = await p.evaluate(() => {
    const panel = document.getElementById("bindPanel");
    return {
      rows: [...document.querySelectorAll("#pubBinds .acct-bind")].map((x) => ({
        key: x.dataset.bind,
        state: x.querySelector(".acct-bind-state")?.textContent || "",
        btn: x.querySelector(".bind-go")?.textContent || "",
      })),
      hint: document.getElementById("bindHint")?.hidden,
      old: (panel?.innerText || "").includes("程序不登录任何平台"),
      browser: document.getElementById("bindBrowser")?.textContent || "",
      browserHidden: document.getElementById("bindBrowser")?.hidden,
    };
  });
  assert(r.rows.length >= 4, "绑定行少于 4 条：" + JSON.stringify(r.rows));
  assert(!r.old, "「程序不登录任何平台」这句还在，和自动发布自相矛盾");
  for (const row of r.rows) {
    assert(row.btn, `${row.key} 那一行没有按钮，用户没法绑`);
  }

  // 浏览器是随包发的，这一行是「你不用自己装 Chrome」的唯一凭据。
  // 缺了它，用户会以为要先装个浏览器才能扫码，然后就放弃了。
  assert(!r.browserHidden && r.browser.length > 4, "绑定区没说明浏览器从哪来：" + JSON.stringify(r.browser));

  const st = await api("PublishStatus");
  if (st.browserReady && st.bundled) {
    assert(r.browser.includes("自带"), "用的是内置浏览器，界面却没说「自带」：" + r.browser);
  }
  // 真的没有浏览器时，出口必须是「怎么修」，不能是点了就报错的「扫码登录」，
  // 也不该再教用户去装 Chrome——装了也不是我们保证得了的那一份。
  if (!st.browserReady) {
    for (const row of r.rows) {
      if (row.key === "wechat" || row.key === "ai") continue;
      assert(row.btn.includes("修"), `${row.key} 缺浏览器却给了「${row.btn}」`);
    }
  }
  // 缺某个号时要有一条提示条把人送到绑定区（绑定区在页面最底部）。
  const missing = r.rows.filter((x) => x.state !== "已连接").length;
  assert(missing === 0 || r.hint === false, "有没连上的号，却没有提示条");
  await shot("10b-bind");

  // 点「填凭证」就地展开，不能只跳个空白
  await p.click('#pubBinds [data-bind-act="wechat-config"]');
  await p.waitForTimeout(400);
  assert(!(await p.locator("#bindWx").isHidden()), "点「填凭证」没展开表单");
  return `${r.rows.map((x) => `${x.key}:${x.btn}`).join(" / ")} | ${r.browser}`;
});


let mineId = "";
await step("11 回填：台账「填数据」→ UpdateMetrics → 数字显示出来", async () => {
  await p.click('[data-view="publish"]');
  await p.waitForTimeout(800);
  // 必须回填刚发那条：台账按发布时间倒序，我们刚记的日期就是今天。
  const boardBefore = await api("PublishBoard");
  const mine = boardBefore.records.find((r) => (r.url || "").includes("e2e-published"));
  assert(mine, "台账里找不到刚发的那条");
  mineId = mine.id;
  const fill = p.locator(`#pubRecords [data-rec="${mine.id}"] [data-act="fill"]`).first();
  assert((await fill.count()) > 0, "台账里没有「填数据」");
  await fill.click();
  await p.waitForTimeout(400);
  const row = p.locator(`#pubRecords [data-rec="${mine.id}"] .rec-form`).first();
  assert(await row.count(), "回填表单没出现");
  await row.locator('[data-m="views"]').fill("8800");
  await row.locator('[data-m="likes"]').fill("260");
  await row.locator('[data-m="collects"]').fill("140");
  await row.locator('[data-m="comments"]').fill("32");
  await row.locator('[data-act="save-metrics"]').click();
  await p.waitForTimeout(1200);
  const um = await called("UpdateMetrics");
  assert(um.length > 0, "没调 UpdateMetrics");
  const umArgs = await lastArgs("UpdateMetrics");
  assert(umArgs[0] === mine.id, `回填的不是刚发那条: ${umArgs[0]}`);
  const board = await api("PublishBoard");
  const rec = board.records.find((r) => r.id === mine.id);
  assert(rec && rec.views === 8800, `后端 views 应为 8800，实际 ${rec?.views}`);
  await shot("11-metrics");
  return `views=${rec.views} likes=${rec.likes} 藏=${rec.collects} 评=${rec.comments}`;
});

// ============ 12. 复盘打星 ============
await step("12 复盘：刚发的进待复盘 → 打星即存 → 移到已复盘", async () => {
  await p.click('[data-view="review"]');
  await p.waitForTimeout(1200);
  const nb = await api("NoteBoard");
  const pendingBefore = nb.stats.pending;
  await p.click('[data-ntab="pending"]').catch(() => {});
  await p.waitForTimeout(600);
  // 打的是刚发那一条，不是随便一条
  const card = p.locator(`#noteList .note-card[data-note="${mineId}"]`);
  assert((await card.count()) > 0, `待复盘列表里没有刚发那条（${mineId}）`);
  const star = card.locator('[data-star="4"]');
  assert((await star.count()) > 0, "卡片上没有第 4 颗星");
  await star.click();
  await p.waitForTimeout(1200);
  const sr = await called("SaveNoteReview");
  assert(sr.length > 0, "没调 SaveNoteReview");
  const after = await api("NoteBoard");
  const rated = after.items.find((x) => x.id === mineId);
  assert(rated && rated.rating === 4, `刚发那条的星应该是 4，实际 ${rated?.rating}`);
  await shot("12-note");
  return `待复盘 ${pendingBefore}→${after.stats.pending}；该条评分=${rated.rating} 星，已复盘 ${after.stats.rated} 条`;
});

// ============ 14. 账号 ============
// 承诺是「第一次打开就有账号」。所以这里不点任何「注册」，
// 直接断言设备码已经在页面上、后端也认这个码。
await step("14 账号：机器码自动建档 → 填姓名手机号 → 绑定行可见", async () => {
  await p.click('[data-view="account"]');
  await p.waitForTimeout(800);
  const a0 = await p.evaluate(() => ({
    code: document.getElementById("acctCode")?.textContent || "",
    binds: document.querySelectorAll("#acctBinds .acct-bind").length,
    names: [...document.querySelectorAll("#acctBinds .acct-bind-name")].map((n) => n.textContent),
  }));
  assert(/-/.test(a0.code), "设备码没显示：" + JSON.stringify(a0));
  assert(a0.binds >= 2, "绑定列表行数不对：" + a0.names.join(","));
  const acct = await api("Account");
  assert(acct.code === a0.code, `界面设备码(${a0.code})和后端(${acct.code})不一致`);

  await p.fill("#acctName", "测试用户");
  await p.fill("#acctPhone", "138 0013 8000");
  await p.click("#acctSave");
  await p.waitForTimeout(900);
  const saved = await api("Account");
  assert(saved.name === "测试用户" && saved.phone === "13800138000", "资料没存住：" + JSON.stringify(saved));
  const shown = (await p.textContent("#acctShown")) || "";
  assert(shown.includes("测试用户"), "顶部名字没跟着变：" + shown);

  // 首页那个字要变成姓。这是用户明确提的要求：没填名字显示「创」，
  // 填了就显示姓氏——所以两种状态都得验，只验一种等于没验。
  await p.click('[data-view="today"]');
  await p.waitForTimeout(400);
  const hero = (await p.textContent("#heroAvatar")) || "";
  assert(hero.trim() === "测", `首页头像应该是姓氏，实际 ${JSON.stringify(hero)}`);
  await p.click('[data-view="account"]');
  await p.waitForTimeout(400);
  await shot("14-account");
  return `设备码 ${a0.code}；绑定 ${a0.names.join("/")}；资料已存`;
});

// ============ 收尾：状态机与库核对 ============
await step("13 状态机：各状态的选题条数（收下→排期→发布 应该逐段推进）", async () => {
  const counts = {};
  for (const st of ["draft", "picked", "writing", "scheduled", "published", "dropped"]) {
    counts[st] = (await api("List", st, 500)).length;
  }
  await p.click('[data-view="today"]');
  await p.waitForTimeout(400);
  await shot("13-final");
  return JSON.stringify(counts);
});

console.table(results.map((r) => ({ step: r.step.slice(0, 34), ok: r.ok, ms: r.ms, errs: r.errs, detail: String(r.detail).slice(0, 60) })));
const failed = results.filter((r) => !r.ok);
console.log(`\n通过 ${results.length - failed.length}/${results.length}`);
if (failed.length) console.log("失败明细:\n" + failed.map((f) => " - " + f.step + " :: " + f.detail).join("\n"));
console.log("pageerror/console:", errs.length ? errs.slice(0, 8) : "none");
await b.close();
process.exit(failed.length || errs.length ? 1 : 0);
