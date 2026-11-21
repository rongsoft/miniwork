import { TopicService } from "./bindings/rongsoft/miniwork/app/index.js";

/* 拆出来的平台窗口。
 *
 * 这个页面只做四件事：把画面贴出来、把用户的点击/输入投回网页、
 * 按窗口尺寸设视口、以及三个按钮（后退 / 收回 / 关掉）。
 * 它不碰任何业务逻辑——发布、扫码、填表仍然在原来那个 page 上跑，
 * 这里只是换了个地方显示。
 */

const $ = (id) => document.getElementById(id);

// 平台的头像字与配色。和主窗口里那几颗平台药丸是同一套色，
// 用户不用读标题，扫一眼颜色就知道这是哪一家。
const PLAT = {
  xiaohongshu: { name: "小红书", ch: "小", color: "#C81E33", soft: "#FFEDEF" },
  douyin: { name: "抖音", ch: "抖", color: "#1F1F1F", soft: "#F1F0ED" },
  wechat: { name: "公众号", ch: "公", color: "#0B8A4B", soft: "#E7F7EE" },
};

const live = {
  platform: "",
  shown: false,
  frames: 0,
  resizeTimer: 0,
  deadTimer: 0,
  pollTimer: 0,
  closed: false,
};

function host(url) {
  try {
    return new URL(url).host;
  } catch {
    return url || "";
  }
}

function hint(text) {
  const el = $("lwHint");
  el.textContent = text;
  el.hidden = false;
}

/* point 把窗口坐标换成页面里的坐标。
 * 视口就是照着画面区设的，两边一一对应，减掉边框即可，不做二次换算。 */
function point(e) {
  const r = $("lwStage").getBoundingClientRect();
  return { x: Math.round(e.clientX - r.left), y: Math.round(e.clientY - r.top) };
}

function send(ev) {
  if (!live.platform || live.closed) return;
  TopicService.LiveInput(live.platform, ev).catch(() => {
    /* 投递失败不弹提示：用户的下一个动作会带出同样的问题，
       连续弹窗只会挡住他正在看的页面 */
  });
}

function onFrame(ev) {
  const d = ev?.data ?? ev ?? {};
  if (!d.data) return;
  fitViewport(d);
  $("lwShot").src = "data:image/jpeg;base64," + d.data;
  live.frames++;
  if (!live.shown) {
    live.shown = true;
    $("lwHint").hidden = true;
    clearTimeout(live.deadTimer);
  }
}

function viewport() {
  const r = $("lwStage")?.getBoundingClientRect();
  if (!r || r.width < 50) return null;
  return { w: Math.round(r.width), h: Math.round(r.height) };
}

// fitViewport 兜住「帧和这块区域不一样大」：图片是 object-fit: fill，
// 差一点就被拉伸，字既糊又变形。拉窗口时只有最后一次 resize 是准的，
// 中间那几帧都会变形，所以每帧比一次尺寸，最多 300 毫秒纠一次。
let fitAt = 0;
function fitViewport(d) {
  if (!live.platform || !d.w || !d.h) return;
  const now = Date.now();
  if (now - fitAt < 300) return;
  const r = $("lwStage")?.getBoundingClientRect();
  if (!r) return;
  if (Math.abs(r.width - d.w) < 2 && Math.abs(r.height - d.h) < 2) return;
  fitAt = now;
  TopicService.LiveResize(live.platform, Math.round(r.width), Math.round(r.height)).catch(() => {});
}

/* paintStatus 写顶上那条进度；text 为空就整条收起。 */
function paintStatus(text, kind) {
  const box = $("lwStatus");
  if (!box) return;
  if (!text) {
    box.hidden = true;
    return;
  }
  box.hidden = false;
  box.classList.toggle("is-ok", kind === "ok");
  box.classList.toggle("is-warn", kind === "warn");
  $("lwStatusText").textContent = text;
}

function paint(v) {
  if (!v) return;
  const authed = !!v.authed;
  $("lwAuth").hidden = false;
  $("lwAuth").textContent = authed ? "已登录" : "还没登录";
  $("lwAuth").classList.toggle("on", authed);
  if (v.url) $("lwUrl").textContent = host(v.url);
  // 中间那个 · 只在两边都有内容时才出现，否则会留下一个孤零零的点。
  $("lwSep").hidden = !($("lwUrl").textContent && !$("lwAuth").hidden);
}

function pushSize() {
  const vp = viewport();
  if (!vp) return;
  TopicService.LiveResize(live.platform, vp.w, vp.h).catch(() => {});
}

async function boot() {
  const q = new URLSearchParams(location.search);
  live.platform = (q.get("platform") || "").trim();
  const p = PLAT[live.platform] || {};
  $("lwName").textContent = p.name || live.platform || "平台";
  const fav = $("lwFav");
  fav.textContent = p.ch || (p.name || live.platform || "平").slice(0, 1);
  if (p.color) {
    fav.style.color = p.color;
    fav.style.background = p.soft;
  }

  if (!live.platform) {
    hint("没说是哪个平台。关掉这个窗口，回主界面重新打开。");
    return;
  }

  try {
    const rt = await import("/wails/runtime.js");
    rt.Events.On("live:frame", onFrame);
    // 主窗口那边在跑发布脚本时会把进度广播过来。这一屏是用户
    // 正在盯着的那个，进度必须出现在这儿。
    rt.Events.On("live:status", (ev) => {
      const d = ev?.data ?? ev ?? {};
      paintStatus(String(d.text || ""), String(d.kind || ""));
    });
  } catch {
    hint("画面通道没接上。关掉重开一次试试。");
    return;
  }

  // 先量尺寸再要画面：反过来的话会先推几帧按旧尺寸排版的画面，
  // 用户看到的是页面「跳」一下才对齐。
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
  pushSize();

  try {
    paint(await TopicService.LiveState(live.platform));
  } catch {
    /* 读不到状态不影响看画面，等下一轮轮询补上 */
  }

  // 每 2.5 秒同步一次地址和登录态：
  // 用户在这个窗口里扫码、跳页，顶部那行要跟着变，
  // 否则他看到的是「域名一直是登录页」，会以为没登进去。
  live.pollTimer = setInterval(async () => {
    if (live.closed) return;
    try {
      paint(await TopicService.LiveState(live.platform));
    } catch {
      /* 读不到就保持上一次的样子 */
    }
  }, 2500);

  clearTimeout(live.deadTimer);
  live.deadTimer = setTimeout(() => {
    if (live.shown) return;
    hint("还没出画面。点「刷新」试一次；一直这样就是内置浏览器出了问题，回主界面重开这一屏。");
  }, 12000);

  $("lwIme").focus({ preventScroll: true });
}

/* ---------- 交互 ---------- */

$("lwStage").addEventListener("click", (e) => {
  const p = point(e);
  $("lwIme").focus({ preventScroll: true });
  send({ kind: "click", x: p.x, y: p.y });
});

let moveAt = 0;
$("lwStage").addEventListener("mousemove", (e) => {
  const now = Date.now();
  if (now - moveAt < 120) return;
  moveAt = now;
  const p = point(e);
  send({ kind: "move", x: p.x, y: p.y });
});

// 滚轮攒着发：触控板一次滑动会连发几十个事件，逐个投过去页面会抖。
let dy = 0;
let wheelAt = 0;
let wheelPoint = { x: 0, y: 0 };
let wheelTimer = 0;
$("lwStage").addEventListener(
  "wheel",
  (e) => {
    e.preventDefault();
    dy += e.deltaY;
    wheelPoint = point(e);
    const flush = () => {
      if (!dy) return;
      send({ kind: "wheel", x: wheelPoint.x, y: wheelPoint.y, dy: Math.round(dy) });
      dy = 0;
      wheelAt = Date.now();
    };
    if (Date.now() - wheelAt > 50) flush();
    else {
      clearTimeout(wheelTimer);
      wheelTimer = setTimeout(flush, 50);
    }
  },
  { passive: false }
);

/* 键盘只投四个键：回车确认、退格擦掉、Tab 换焦点、Esc 关弹窗。
   全键位映射会把中文输入法搅乱，不值得。 */
$("lwIme").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    send({ kind: "key", key: "escape" });
    return;
  }
  const map = { Enter: "enter", Backspace: "backspace", Tab: "tab" };
  const k = map[e.key];
  if (!k) return;
  e.preventDefault();
  send({ kind: "key", key: k });
});

// 中文输入法要等组合结束再一次性发：中途的拼音会被页面当成正文收进去。
let composing = false;
function flushText() {
  const t = $("lwIme").value;
  if (!t) return;
  $("lwIme").value = "";
  send({ kind: "text", text: t });
}
$("lwIme").addEventListener("compositionstart", () => {
  composing = true;
});
$("lwIme").addEventListener("compositionend", () => {
  composing = false;
  flushText();
});
$("lwIme").addEventListener("input", () => {
  if (composing) return;
  flushText();
});

$("lwBack").addEventListener("click", async () => {
  try {
    await TopicService.LiveBack(live.platform);
  } catch {
    /* 本来就在第一页，退不回去不算错误 */
  }
});

$("lwReload").addEventListener("click", async () => {
  live.shown = false;
  hint("正在重新加载…");
  try {
    await TopicService.LiveReload(live.platform);
  } catch (e) {
    hint("重新加载没成功：" + String(e?.message || e));
  }
});

/* 收回：窗口由 Go 那边关掉，主窗口收到事件后把右栏展开。 */
$("lwDock").addEventListener("click", () => {
  live.closed = true;
  TopicService.LiveAttach();
});

/* 关掉：连投屏一起停，主窗口把整栏收起来。 */
$("lwQuit").addEventListener("click", () => {
  live.closed = true;
  clearInterval(live.pollTimer);
  TopicService.LiveClose();
});

/* 窗口尺寸变了要跟着改视口，否则网页还按旧宽度排版，
   右边露一条空白、点击也开始偏。 */
if (window.ResizeObserver) {
  const ro = new ResizeObserver(() => {
    if (!live.platform || live.closed) return;
    clearTimeout(live.resizeTimer);
    live.resizeTimer = setTimeout(pushSize, 180);
  });
  ro.observe($("lwStage"));
}

window.addEventListener("beforeunload", () => {
  live.closed = true;
  clearInterval(live.pollTimer);
});

boot();
