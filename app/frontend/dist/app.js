import { TopicService } from "./bindings/rongsoft/miniwork/app/index.js";

const $ = (id) => document.getElementById(id);

const el = {
  list: $("list"),
  empty: $("empty"),
  meta: $("meta"),
  niche: $("niche"),
  run: $("run"),
  run2: $("run2"),
  runLabel: $("runLabel"),
  progress: $("progress"),
  progressBar: $("progressBar"),
  progressMsg: $("progressMsg"),

  navLib: $("navLib"),
  navTodo: $("navTodo"),
  navQueue: $("navQueue"),
  navTodo2: $("navTodo2"),
  heroHi: $("heroHi"),
  heroLine: $("heroLine"),
  heroNew2: $("heroNew2"),
  heroMatch2: $("heroMatch2"),
  heroPicked: $("heroPicked"),
  kwInput: $("kwInput"),
  kwRun: $("kwRun"),
  kwHint: $("kwHint"),
  run3: $("run3"),
  todayDate: $("todayDate"),
  source: $("source"),
  srcDot: $("srcDot"),
  srcLine: $("srcLine"),
  memCount: $("memCount"),
  modelBadge: $("modelBadge"),
  modelPill: $("modelPill"),

  agendaCounts: $("agendaCounts"),
  agendaList: $("agendaList"),

  libList: $("libList"),
  libEmpty: $("libEmpty"),

  tdRaw: $("tdRaw"),
  tdRun: $("tdRun"),
  tdResult: $("tdResult"),
  tdEmpty: $("tdEmpty"),
  tdHint: $("tdHint"),
  tdHist: $("tdHist"),
  tdHistList: $("tdHistList"),

  tgSeed: $("tgSeed"),
  tgHooks: $("tgHooks"),
  tgRun: $("tgRun"),
  tgResult: $("tgResult"),
  tgEmpty: $("tgEmpty"),
  tgHint: $("tgHint"),

  calMonth: $("calMonth"),
  calPrev: $("calPrev"),
  calNext: $("calNext"),
  calSummary: $("calSummary"),
  calGrid: $("calGrid"),
  pendCount: $("pendCount"),
  pendList: $("pendList"),
  calDay: $("calDay"),
  calDayTitle: $("calDayTitle"),
  calDayClose: $("calDayClose"),
  calDayList: $("calDayList"),

  revSeg: $("revSeg"),
  revBody: $("revBody"),

  memSave: $("memSave"),
  memStats: $("memStats"),
  pfPositioning: $("pfPositioning"),
  pfAudience: $("pfAudience"),
  pfTone: $("pfTone"),
  pfAvoid: $("pfAvoid"),
  learntNote: $("learntNote"),
  learntChips: $("learntChips"),

  schEnabled: $("schEnabled"),
  schTime: $("schTime"),
  schState: $("schState"),
  schLast: $("schLast"),
  schNext: $("schNext"),

  setSource: $("setSource"),
  setAI: $("setAI"),
  setDB: $("setDB"),

  logbox: $("logbox"),
  logBody: $("logBody"),

  onboard: $("onboard"),
  obDots: $("obDots"),
  obBody: $("obBody"),
  obSkip: $("obSkip"),
  obNext: $("obNext"),

  planPop: $("planPop"),
  planTopic: $("planTopic"),
  planQuick: $("planQuick"),
  planDate: $("planDate"),
  planOk: $("planOk"),
  planClear: $("planClear"),

  matStats: $("matStats"),
  matSeg: $("matSeg"),
  matList: $("matList"),
  matAdd: $("matAdd"),
  matPop: $("matPop"),
  matPopTitle: $("matPopTitle"),
  matUrl: $("matUrl"),
  matPlatform: $("matPlatform"),
  matAuthor: $("matAuthor"),
  matTitle: $("matTitle"),
  matBody: $("matBody"),
  matLikes: $("matLikes"),
  matCollects: $("matCollects"),
  matCancel: $("matCancel"),
  matOk: $("matOk"),
  linkPop: $("linkPop"),
  linkMatTitle: $("linkMatTitle"),
  linkTopic: $("linkTopic"),
  linkOk: $("linkOk"),
  linkClear: $("linkClear"),
  navMat: $("navMat"),

  toast: $("toast"),
};

const state = {
  all: [],          // 全部待定，统计口径用这个
  topics: [],       // 当前筛选后展示的
  niche: "职场",
  plat: "",
  view: "today",
  running: false,
  lastDecide: 0,
  niches: [],
  suppressUntil: 0,

  today: null,          // 今日待办
  calMonth: "",         // 日历当前月，空表示本月
  calData: null,
  revDays: 7,
  plan: { id: "", title: "", date: "" },

  matPlat: "",        // 素材库当前平台筛选，空表示全部
  mat: [],            // 当前列表
  matStat: null,
  matEditing: null,   // 正在编辑的素材 id，空表示新增
  linkMat: "",        // 正在关联选题的素材 id

  account: null,      // 本机账号（机器码 + 姓名手机号 + 绑定状态）

  tdHist: [],         // 拆过的记录，最新在前
  tdCurrent: null,    // 结果区正在看的那条
  tdID: "",           // 它的 id，用来标历史列表里的选中态
};

/* 引导关闭后的一小段静默期。
   第三屏「开始用」按钮的位置，正好压在列表卡片的「收下」上——引导一消失，
   这次点击的剩余事件会落到下面的卡片上，用户还没看清今天的选题，
   就已经莫名其妙收下了一条。这是真机上一定会发生的，不是测试环境特例。 */
const AFTER_ONBOARD_QUIET = 450;

/* 相邻两张卡片的「收下」在同一位置，连点会误伤下一条。
   240ms 是整个动作动画的长度，冷却比它稍长一点。 */
const DECIDE_COOLDOWN = 320;

/* ---------- 小工具 ---------- */

/* 标题来自外部数据，一律转义后再进 innerHTML。 */
function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

const pad2 = (n) => String(n).padStart(2, "0");

function fmtDate(d) {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

function todayStr() { return fmtDate(new Date()); }

function addDays(d, n) {
  const x = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  x.setDate(x.getDate() + n);
  return x;
}

/* 11-15 这种短日期，和 Go 侧 format.PlanDate 保持一致。 */
function shortDate(s) { return s && s.length >= 10 ? s.slice(5) : s || ""; }

function scoreClass(n) {
  if (n >= 70) return "high";
  if (n >= 60) return "mid";
  return "";
}

function toast(msg) {
  el.toast.textContent = msg;
  el.toast.classList.add("on");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => el.toast.classList.remove("on"), 2600);
}

function humanError(e) {
  const raw = e?.message || String(e || "");
  if (raw.includes("正在生成")) return "正在生成中，稍等一下";
  return raw.replace(/^.*?(error|Error)[:\s]+/, "").slice(0, 140) || "操作失败";
}

/* ---------- 选题卡片 ---------- */

/* 四项维度条。
   值域 0-100，宽度用百分比；最小给 2% 是为了「有这一项但分数极低」时
   还能看见一条细线，不然和「这一项没有」长得一样。 */
function dimsHtml(t) {
  if (!t.dims || !t.dims.length) return "";
  return `<div class="dims">${t.dims
    .map((d) => {
      const v = Math.max(0, Math.min(100, d.value | 0));
      const w = Math.max(2, v);
      return `<span class="dim" title="${esc(d.note || "")}">
        <span class="dim-k">${esc(d.name)}</span>
        <span class="dim-track"><i style="width:${w}%"></i></span>
        <span class="dim-v">${v}</span>
      </span>`;
    })
    .join("")}</div>`;
}

/* 「为什么推荐你写」。
   只展示 2 条：这一屏用户是在快速筛，不是读。三条理由加一行维度条，
   卡片会涨到 150px 以上，一屏只剩一张半——为了「多给一点信息」牺牲掉
   整屏的比较能力，是亏的。剩下的收成「还有 N 条」，仍然在卡片上给个交代。 */
function whyHtml(t) {
  if (!t.reasons || !t.reasons.length) return "";
  return `<ul class="why">${t.reasons
    .slice(0, 2)
    .map((r) => `<li>${esc(r)}</li>`)
    .join("")}</ul>`;
}

/* 阶段进度轴。只在「已收下」之后显示：还在待定池里的选题谈进度是虚的，
   用户还没决定写不写。 */
function flowHtml(t) {
  if (!t.steps || !t.steps.length || !t.done) return "";
  const steps = t.steps
    .map((s, i) => {
      const n = i + 1;
      const cls = n < t.done ? "done" : n === t.done ? "cur" : "todo";
      return `<span class="step ${cls}">${esc(s)}</span>`;
    })
    .join(`<span class="step-line"></span>`);
  return `<div class="flow">${steps}<span class="flow-n">第 ${t.done}/${t.steps.length} 步</span></div>`;
}

function cardHtml(t, i, mode) {
  // aiScore >= 0 说明这条经过模型按「你」的口味复核过，标出来，
  // 用户才知道那些分数不是凭空算的。
  const aiTag =
    t.aiScore >= 0 ? `<span class="tag ai" title="模型契合度 ${t.aiScore}">AI</span>` : "";
  const planTag = t.planLabel
    ? `<span class="tag done">排 ${esc(t.planLabel)}</span>` : "";

  // 有维度条时不再重复热度和时间：它们是同一批数据的另一种说法
  // （「12.3万」就是「热度 96」，「3小时前」就是「新颖 92」）。
  // 同样的信息在卡片上出现两次，用户会花时间去确认「这两个是不是一个东西」。
  // 没有维度条（老数据）时退回原来的标签，信息一条都不少。
  const heatTags = t.dims && t.dims.length
    ? ""
    : `<span class="tag">${esc(t.heatLabel)}</span>
        <span class="tag">${esc(t.ageLabel)}</span>`;

  let actions;
  if (mode === "library") {
    // 收了但还没排期的，最该做的是给它定个日子，所以「排期」放主按钮。
    actions = `<button class="btn btn-ghost" data-act="drop">丢掉</button>
       <button class="btn btn-ghost" data-act="restore">退回</button>
       <button class="btn btn-primary" data-act="plan">排期</button>`;
  } else if (mode === "calendar") {
    actions = `<button class="btn btn-ghost" data-act="unschedule">取消排期</button>
       <button class="btn btn-ghost" data-act="plan">改期</button>
       ${t.status === "scheduled" ? `<button class="btn btn-primary" data-act="publish">已发</button>` : ""}`;
  } else {
    actions = `<button class="btn btn-ghost" data-act="drop">丢掉</button>
       <button class="btn btn-primary" data-act="pick">收下</button>`;
  }

  // 待定池里的卡片只显示理由和维度，不显示进度轴；收下之后才谈走到哪一步。
  const flow = mode === "today" ? "" : flowHtml(t);

  // 非今天的条目要带一个日期标签。没有它，用户在一屏里分不清
  // 「这是昨天刷出来的」和「这是刚刷出来的」，只能得出「刷新没反应」。
  const dayTag = t.dayLabel ? `<span class="tag day">${esc(t.dayLabel)}</span>` : "";

  return `<article class="card" data-id="${esc(t.id)}" style="animation-delay:${Math.min(i * 26, 200)}ms">
    <div class="score ${scoreClass(t.score)}">${t.score}</div>
    <div class="card-body">
      <div class="card-title" title="${esc(t.title)}">${esc(t.title)}</div>
      ${whyHtml(t)}
      ${dimsHtml(t)}
      ${flow}
      <div class="tags">
        <span class="tag plat">${esc(t.platformName)}</span>
        ${dayTag}
        ${heatTags}
        ${planTag}
        ${aiTag}
      </div>
    </div>
    <div class="actions">${actions}</div>
  </article>`;
}

function render() {
  // 今天这批在前、更早的在后，中间插一条分隔。
  // 不加分隔的话，列表就是一张按分数排的大杂烩：昨天 82 分的挤在今天 76 分前面，
  // 用户刷新完扫一眼，看到的还是昨天那几张熟脸。
  const older = state.topics.filter((t) => !t.today).length;
  const parts = [];
  let split = false;
  state.topics.forEach((t, i) => {
    if (!t.today && !split) {
      split = true;
      parts.push(`<div class="list-sep"><span>更早抓的 ${older} 条 · 还没处理</span></div>`);
    }
    parts.push(cardHtml(t, i, "today"));
  });
  el.list.innerHTML = parts.join("");
  el.empty.hidden = state.topics.length > 0;
  updateMeta();
}

function updateMeta() {
  const today = state.topics.filter((t) => t.today).length;
  if (!state.topics.length) {
    el.meta.textContent = "";
    return;
  }
  // 「今天 6 条 · 共 26 条待定」比「26 条待你决定」有用：
  // 前者能回答「刷新到底有没有效果」，后者只报了个总量。
  el.meta.textContent = today
    ? `今天 ${today} 条 · 共 ${state.topics.length} 条待定`
    : `${state.topics.length} 条待定（今天还没有新的）`;
}

/* 收下/丢掉一张卡之后，分隔条上的数字要跟着变。
   不重画整个列表是为了保住其他卡片的入场动画和滚动位置；
   但数字不跟着变，用户会看到「更早抓的 12 条」下面只剩 10 张，
   这种对不上的小账最容易让人不信整页数据。 */
function refreshSep() {
  const sep = el.list.querySelector(".list-sep");
  if (!sep) return;
  const older = state.topics.filter((t) => !t.today).length;
  const today = state.topics.filter((t) => t.today).length;
  // 两边有一边空了，分隔就没有意义了。
  if (!older || !today) {
    sep.remove();
    return;
  }
  const label = sep.querySelector("span");
  if (label) label.textContent = `更早抓的 ${older} 条 · 还没处理`;
}

/* ---------- 今日待办 ----------
   每天打开这一屏，用户第一个问题是「今天干什么」而不是「有什么新东西」。
   所以待办排在选题流上面，且按「逾期 > 今天 > 卡住 > 待定」排序。 */

const TODO_ACT = {
  overdue: { label: "标记已发", act: "publish" },
  due:     { label: "标记已发", act: "publish" },
  stuck:   { label: "排期",     act: "plan" },
  decide:  { label: "收下",     act: "pick" },
};

function todoHtml(it, i) {
  const a = TODO_ACT[it.kind] || { label: "查看", act: "plan" };
  return `<div class="todo k-${esc(it.kind)}" data-id="${esc(it.topicId)}"
      style="animation-delay:${Math.min(i * 24, 180)}ms">
    <span class="todo-bar"></span>
    <div class="todo-main">
      <div class="todo-title" title="${esc(it.title)}">${esc(it.title)}</div>
      <div class="todo-hint">${esc(it.hint)}</div>
    </div>
    <button class="todo-act" data-act="${a.act}">${a.label}</button>
  </div>`;
}

function renderToday(d) {
  state.today = d;
  const total = d?.total || 0;
  for (const badge of [el.navTodo, el.navTodo2]) {
    if (!badge) continue;
    badge.textContent = total;
    badge.hidden = total === 0;
  }

  if (!total) {
    el.agendaList.innerHTML = `<div class="agenda-empty">
      <span class="agenda-ok">
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
          stroke-width="3.4" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6L9 17l-5-5"></path></svg>
      </span>
      今天没有非做不可的事。下面的新选题可以先看着，不急。
    </div>`;
    return;
  }
  el.agendaList.innerHTML = d.items.map(todoHtml).join("");
}

async function loadToday() {
  try {
    renderToday(await TopicService.Today());
  } catch {
    /* 待办是增强项，读不到不影响看选题 */
  }
}

// 统计口径用全量，不受平台筛选影响——否则切一下平台，这个数字会跳。
// 只报「待定」和「已收」：待办区块本身就是今天的事，再写一次「今天 5」是重复。
function paintCounts(s) {
  el.navLib.textContent = s?.picked ?? 0;
  const parts = [];
  if (s?.draft) parts.push(`待定 ${s.draft}`);
  if (s?.picked) parts.push(`已收 ${s.picked}`);
  el.agendaCounts.textContent = parts.join(" · ");
  paintHero(s);
}

/* 问候卡上那三个数。
   口径必须和列表一致：高匹配用「今天这批里 70 分以上的条数」，
   「今日新增」用后端算好的 today，不拿全部待定数顶替——拿总数当新增，
   数字会一天比一天大，而列表并没有变长，用户迟早发现这个数在骗人。 */
function paintHero(s) {
  const today = s?.today ?? 0;
  const picked = s?.picked ?? 0;
  const older = (state.all || []).filter((t) => !t.today).length;
  const match = (state.all || []).filter((t) => t.today && t.score >= 70).length;
  if (el.heroLine) el.heroLine.innerHTML = heroLine(today, match, older);
  el.heroNew2.textContent = today;
  el.heroMatch2.textContent = match;
  el.heroPicked.textContent = picked;
}

/* 这一句必须跟着真实数字变。
   上一版是写死的一句「今天找到 N 条，其中 M 条高度匹配」：
   当 M 是 0 而下面照样摆着两张 68 分的卡时，这句话在自打嘴巴——
   用户的第一反应不是"阈值设得严"，而是"这数字是编的"，
   一旦起了这个念头，后面所有统计都不作数了。 */
function heroLine(n, m, older = 0) {
  const tail = ` <span class="sparkle">✨</span>`;
  if (n === 0) {
    // 今天没抓到新的、但旧待定还在时，别装作列表是空的——
    // 用户眼睛看得见下面那堆卡，说「还没有灵感」只会显得这行字没读数据。
    if (older > 0) {
      return `今天还没有新的，下面是之前没处理的 <b>${older}</b> 条，先消化也好${tail}`;
    }
    return `今天还没有新灵感，下面挑个方向让我去找一批${tail}`;
  }
  if (m === 0) {
    return `今天为你找到了 <b>${n}</b> 条新灵感，还没有特别贴合你方向的，先挑挑看${tail}`;
  }
  if (m === n) {
    return `今天为你找到了 <b>${n}</b> 条新灵感，全都和你常写的方向接近${tail}`;
  }
  return `今天为你找到了 <b>${n}</b> 条新灵感，其中 <b>${m}</b> 条和你常写的方向接近${tail}`;
}

/* ---------- 排期弹层 ---------- */

function quickDates() {
  const now = new Date();
  const out = [
    { label: "今天", date: fmtDate(now) },
    { label: "明天", date: fmtDate(addDays(now, 1)) },
    { label: "后天", date: fmtDate(addDays(now, 2)) },
  ];
  // 下周一：创作者常在周末排下周的活，这个快捷项最常用。
  const wd = now.getDay();
  const toMon = wd === 1 ? 7 : (8 - wd) % 7 || 7;
  out.push({ label: "下周一", date: fmtDate(addDays(now, toMon)) });
  return out;
}

function openPlan(id, title, current) {
  state.plan = { id, title, date: current || "" };
  el.planTopic.textContent = title || "";
  el.planDate.value = current || todayStr();
  // 已排期的可以撤销；没排期的这个按钮只是关闭，文字必须跟着变，
  // 否则用户会以为自己点掉了什么。
  el.planClear.textContent = current ? "取消排期" : "关闭";
  el.planClear.dataset.mode = current ? "clear" : "close";
  paintQuick();
  el.planPop.hidden = false;
}

function paintQuick() {
  const cur = el.planDate.value;
  el.planQuick.innerHTML = quickDates()
    .map((o) => `<button data-date="${o.date}" class="${o.date === cur ? "on" : ""}">${o.label}</button>`)
    .join("");
}

function closePlan() { el.planPop.hidden = true; }

/* 排期是从弹窗提交的，不走 handleAction，所以得自己按当前视图重画。
   不重画的话：选题库里那条还挂着「排期」按钮、日历里还留着已取消的条目，
   界面和库对不上，用户会以为没点成功。 */
async function refreshAfterPlan() {
  await refresh();
  if (state.view === "calendar") await loadCalendar(state.calMonth);
  else if (state.view === "library") await loadLibrary();
}

async function submitPlan() {
  const date = el.planDate.value;
  if (!date) { toast("先选个日子"); return; }
  el.planOk.disabled = true;
  try {
    await TopicService.Plan(state.plan.id, date);
    closePlan();
    toast(`排到 ${shortDate(date)} 了`);
    await refreshAfterPlan();
  } catch (e) {
    toast(humanError(e));
  } finally {
    el.planOk.disabled = false;
  }
}

el.planOk.addEventListener("click", submitPlan);
el.planDate.addEventListener("change", paintQuick);
el.planQuick.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-date]");
  if (!b) return;
  el.planDate.value = b.dataset.date;
  paintQuick();
});
el.planClear.addEventListener("click", async () => {
  if (el.planClear.dataset.mode === "close") { closePlan(); return; }
  try {
    await TopicService.Plan(state.plan.id, "");
    closePlan();
    toast("已取消排期");
    await refreshAfterPlan();
  } catch (e) {
    toast(humanError(e));
  }
});
el.planPop.addEventListener("click", (ev) => {
  // 点遮罩关闭：弹层本身不大，误触概率低，但能关总比不能关好。
  if (ev.target === el.planPop) closePlan();
});
// Esc 关掉当前打开的弹层。
//
// 三个弹层以前只有「排期」那个认 Esc，另外两个只能点遮罩或取消按钮——
// 同一个界面里有的能按 Esc 有的不能，用户按一次没反应就会以为卡住了。
// 弹层是叠加的（素材弹窗可能压着关联弹窗），所以关最上面那个就走。
document.addEventListener("keydown", (ev) => {
  if (ev.key !== "Escape") return;
  if (el.linkPop && !el.linkPop.hidden) { el.linkPop.hidden = true; return; }
  if (el.matPop && !el.matPop.hidden) { closeMatPop(); return; }
  if (!el.planPop.hidden) closePlan();
});

/* ---------- 进展与日志 ---------- */

function setBusy(busy) {
  el.run.disabled = busy;
  el.run2.disabled = busy;
  el.run3.disabled = busy;
  el.kwRun.disabled = busy;
  el.kwInput.disabled = busy;
  el.runLabel.textContent = busy ? "生成中…" : "刷新灵感";
}

function showProgress(pct, msg) {
  el.progress.classList.add("on");
  el.progressBar.style.width = `${Math.max(0, Math.min(100, pct))}%`;
  el.progressMsg.textContent = msg || "";
}

function hideProgress() {
  el.progressBar.style.width = "100%";
  setTimeout(() => {
    el.progress.classList.remove("on");
    el.progressBar.style.width = "0%";
    el.progressMsg.textContent = "";
  }, 500);
}

/* 工作台日志：每一行都来自流水线真实的进度事件。
   用户看得见机器在干什么，就不会觉得「点了没反应」。 */
let logTimer;
function log(msg, dim) {
  if (!msg) return;
  el.logbox.hidden = false;
  const t = new Date().toTimeString().slice(0, 8);
  const line = document.createElement("div");
  line.className = "log-line" + (dim ? " dim" : "");
  line.innerHTML = `<span class="t">${t}</span><span>${esc(msg)}</span>`;
  el.logBody.appendChild(line);
  while (el.logBody.children.length > 60) el.logBody.removeChild(el.logBody.firstChild);
  el.logBody.scrollTop = el.logBody.scrollHeight;
}

function logDone(msg) {
  log(msg);
  clearTimeout(logTimer);
  logTimer = setTimeout(() => {
    // 收起来而不是清空：用户想回头看这轮做了什么都还在。
    el.logbox.hidden = true;
  }, 4200);
}

/* ---------- 视图切换 ---------- */
const VIEWS = ["today", "library", "material", "teardown", "title", "write", "agenda", "calendar", "publish", "review", "account", "memory", "settings"];

function switchView(name) {
  if (!VIEWS.includes(name)) return;
  state.view = name;
  for (const v of VIEWS) $("view-" + v).hidden = v !== name;
  document.querySelectorAll(".nav-item").forEach((b) => {
    b.classList.toggle("on", b.dataset.view === name);
  });
  // 日志是「今日」这一页的东西，切走就收起，别飘在别的页面上。
  if (name !== "today") el.logbox.hidden = true;
  if (name === "library") loadLibrary();
  if (name === "material") loadMaterial(state.matPlat);
  if (name === "agenda") loadToday();
  if (name === "teardown") resetTeardown();
  if (name === "write") loadWrite();
  if (name === "publish") loadPublish();
  if (name === "calendar") loadCalendar(state.calMonth);
  if (name === "review") {
    loadReview(state.revDays);
    loadNotes();
  }
  if (name === "account") loadAccount();
  if (name === "memory") loadMemoryForm();
  if (name === "settings") loadSettings();
}

document.querySelectorAll(".nav-item").forEach((b) => {
  b.addEventListener("click", () => switchView(b.dataset.view));
});

// 左上角头像 = 账号入口。放在这里而不是新加一个菜单项：
// 用户已经习惯「点自己的头像看账号」，多一个「账号」按钮只是重复。
$("brandBtn")?.addEventListener("click", () => switchView("account"));

/* 切到某一页，并滚到具体那一块。
   账号页说「去发布台」「去设置」，光切视图是把用户丢在页首——
   他还要自己在十几个面板里找那一块，找不到就等于没入口。 */
function goToPanel(view, panelId) {
  switchView(view);
  const jump = () => {
    const node = panelId && $(panelId);
    if (!node) return;
    node.scrollIntoView({ behavior: "smooth", block: "center" });
    node.classList.remove("flash");
    // 读一次 offsetWidth 强制回流，否则同一个类连着加两次不会重播动画。
    void node.offsetWidth;
    node.classList.add("flash");
    setTimeout(() => node.classList.remove("flash"), 1800);
  };
  // 视图里的数据是异步拉的（发布台要探浏览器），等一帧再滚，
  // 否则滚到的是空容器，内容一填回来位置就跑了。
  setTimeout(jump, 260);
}

/* ---------- 数据 ---------- */
function applyFilter() {
  // 今日灵感只展示当前赛道的待定卡：state.all 保留全量给统计用，
  // 只在「展示」这一层按赛道 + 平台过滤。
  //
  // 不在这里过滤的话，切到「美食」下拉框后列表里还混着职场的旧卡，
  // 用户会以为赛道选择器是摆设。没有 niche 字段的老数据保留，别让升级上来的
  // 旧条目凭空消失（它们本来就属于「尚未分类」）。
  let list = state.all.filter((t) => !t.niche || t.niche === state.niche);
  if (state.plat) list = list.filter((t) => t.platform === state.plat);
  state.topics = list;
  render();
}

async function refresh() {
  const [topics, stats] = await Promise.all([
    TopicService.List("draft", 50),
    TopicService.Stats(),
  ]);
  state.all = topics || [];
  applyFilter();
  await loadToday();
  paintCounts(stats);
}

async function loadLibrary() {
  try {
    const [list, stats] = await Promise.all([
      TopicService.List("picked", 100),
      TopicService.Stats(),
    ]);
    const items = list || [];
    el.libList.innerHTML = items.map((t, i) => cardHtml(t, i, "library")).join("");
    el.libEmpty.hidden = items.length > 0;
    paintCounts(stats);
  } catch (e) {
    toast(humanError(e));
  }
}

/* 数据来源必须写在界面上。用户以为在看真榜单、其实在看三天前的缓存，
   这比少给两条选题严重得多——他会拿过期热点去写今天的稿子。 */
const SOURCE_LABEL = { remote: "线上数据", cache: "本地缓存", mock: "示例数据" };

// 「示例数据」是没配服务端时的降级态，属于开发期的东西，不该一直挂在成品界面
// 左下角——用户看到的每一屏都自带这四个字，像没做完。真实来源（线上/缓存）
// 照常显示，因为那时候用户确实需要知道自己在看什么。
//
// 判断只有这一处。后端 SourceName() 给的是文案（「示例数据」「示例数据（未配置…）」），
// 摘要里给的是 key（mock/remote/cache），两条路都归到这里 —— 分成两份判断的后果
// 是某天只改了其中一处，左下角又冒出「示例数据」。
function paintSourceLine(label, key) {
  const demo = !label || label.startsWith("示例");
  el.srcLine.hidden = demo;
  // 示例数据下首页要有一句解释。左下角那个角标可以藏（它是给开发者看的），
  // 但用户问的「为什么刷不出新东西」必须有人回答，否则他会以为按钮坏了。
  const note = $("demoNote");
  if (note) note.hidden = !demo;
  if (demo) return;
  el.source.textContent = label;
  el.srcDot.className = "dot " + (key === "cache" ? "cache" : "live");
}

function applySource(summary) {
  const key = summary?.source;
  const label = SOURCE_LABEL[key];
  if (label) paintSourceLine(label, key);
  if (summary?.stale) toast("数据源暂时连不上，这次用的是本地缓存");
}

async function run() {
  if (state.running) return;
  state.running = true;
  setBusy(true);
  el.logBody.innerHTML = "";
  log(`开始取数：${state.niche}`);
  showProgress(0, "开始");
  try {
    const summary = await TopicService.Run(state.niche, "");
    await refresh();
    applySource(summary);
    log(`取到 ${summary.fetched} 条，新增 ${summary.newHotspot} 条`, true);
    // 「入选 N 条」原来把重算旧条目也算进去，用户看到「入选 18 条」
    // 再回列表却一条没多，只能怀疑按钮坏了。分开报，两个数都对得上界面。
    log(
      `新入池 ${summary.saved} 条，重算 ${summary.updated} 条，低于阈值 ${summary.skipped} 条`,
      true
    );
    if (summary.aiScored > 0) log(`模型复核 ${summary.aiScored} 条`, true);
    logDone("完成");
    if (!summary.saved && summary.updated > 0) {
      toast(
        summary.source === "mock"
          ? "这批都是之前抓过的：示例数据抽完了，接上数据源才会每天不一样"
          : "这批内容和之前重复，没有新的入池"
      );
    }
    if (summary?.aiNote) toast(summary.aiNote);
  } catch (e) {
    log("失败：" + humanError(e));
    toast(humanError(e));
  } finally {
    state.running = false;
    setBusy(false);
    hideProgress();
  }
}

/* 切到拆解页时清掉上一次的结果。
   不保留是因为拆解结果只对「那一条」有意义，留着会让用户以为
   当前输入框里的内容已经被拆过了。历史记录每次进来重读一遍：
   它可能在另一台设备/上一次运行里被加过，前台缓存不能当真。 */
function resetTeardown() {
  loadTdHist();
  paintTdSample();
  if (el.tdResult && !el.tdResult.hidden) return; // 正在看结果就不要清
  if (el.tdEmpty) el.tdEmpty.hidden = false;
}

/* 关键词主动挖掘。
   和「刷新灵感」共用同一条流水线，区别只在传了 keyword：
   关键词搜索是跨赛道的，而且失败时不降级到赛道缓存——
   用户搜「裁员」却拿到一堆「职场」，等于答非所问。 */
async function digKeyword() {
  const kw = el.kwInput.value.trim();
  if (!kw) {
    toast("先输个关键词，例如「裁员」");
    el.kwInput.focus();
    return;
  }
  if (state.running) return;
  state.running = true;
  setBusy(true);
  el.logBody.innerHTML = "";
  el.kwHint.className = "come-hint";
  el.kwHint.textContent = `正在跨赛道找「${kw}」…`;
  log(`关键词挖掘：${kw}`);
  showProgress(0, "开始挖掘");
  try {
    const summary = await TopicService.Run(state.niche, kw);
    await refresh();
    applySource(summary);
    const n = (summary.topics || []).length;
    el.kwHint.className = "come-hint is-ok";
    el.kwHint.textContent = n
      ? `找到 ${n} 条和「${kw}」相关的内容，已按匹配度排序`
      : `没有和「${kw}」相关的内容，换个说法试试`;
    log(`「${kw}」命中 ${n} 条`, true);
    logDone("挖掘完成");
    if (summary?.aiNote) toast(summary.aiNote);
  } catch (e) {
    el.kwHint.className = "come-hint is-warn";
    el.kwHint.textContent = humanError(e);
    log("挖掘失败：" + humanError(e));
    toast(humanError(e));
  } finally {
    state.running = false;
    setBusy(false);
    hideProgress();
  }
}

async function decide(id, act, card) {
  card.classList.add(act === "pick" ? "leaving-right" : "leaving-left");
  try {
    await TopicService.Decide(id, act);
    state.all = state.all.filter((t) => t.id !== id);
    state.topics = state.topics.filter((t) => t.id !== id);
    setTimeout(async () => {
      card.remove();
      refreshSep();
      el.empty.hidden = state.topics.length > 0;
      updateMeta();
      await loadToday();
      paintCounts(await TopicService.Stats());
    }, 240);
  } catch (e) {
    card.classList.remove("leaving-right", "leaving-left");
    toast(humanError(e));
  }
}

/* 所有动作统一走这里：卡片、待办、日历里的按钮都带 data-act，
   各自的外层容器只是不同的 class。一处判断，避免三个地方各写一套。 */
async function handleAction(btn, card, mode) {
  const id = card.dataset.id;
  const act = btn.dataset.act;
  const title = card.querySelector(".card-title, .todo-title, .pend-title")?.textContent || "";

  if (act === "plan") {
    const t = findTopic(id, mode);
    openPlan(id, title, t?.planDate || "");
    return;
  }

  // 选题流里的收下/丢掉走带滑出动画的那条路：卡片整块飞走，
  // 比「它突然消失」更让人确认自己点对了。
  if (mode === "today" && card.classList.contains("card") && (act === "pick" || act === "drop")) {
    await decide(id, act, card);
    return;
  }

  try {
    await TopicService.Decide(id, act);
  } catch (e) {
    toast(humanError(e));
    return;
  }

  await loadToday();
  if (mode === "today") {
    // 待办里点掉一条，整块重新拉：状态一变，它可能从「今天要发」跳到别的类。
    await refresh();
    return;
  }
  if (mode === "library") { await loadLibrary(); return; }
  if (mode === "calendar") {
    await loadCalendar(state.calMonth);
    if (!el.calDay.hidden) showDay(el.calDay.dataset.date);
  }
}

/* 按 id 在已有数据里找回排期日期，省一次往返。 */
function findTopic(id, mode) {
  if (mode === "today") {
    const it = state.today?.items?.find((x) => x.topicId === id);
    if (it) return { planDate: it.planDate || "" };
  }
  const pools = [state.all, state.calData?.pending || []];
  if (state.calData?.cells) {
    for (const c of state.calData.cells) pools.push(c.items || []);
  }
  for (const p of pools) {
    const hit = p.find((t) => t.id === id);
    if (hit) return hit;
  }
  return null;
}

function bindActions(container, mode) {
  container.addEventListener("click", async (ev) => {
    const btn = ev.target.closest("[data-act]");
    if (!btn) return;
    if (Date.now() < state.suppressUntil) return;

    const now = Date.now();
    if (now - state.lastDecide < DECIDE_COOLDOWN) return;
    state.lastDecide = now;

    const card = btn.closest(".card, .todo, .pend");
    if (!card) return;
    await handleAction(btn, card, mode);
  });
}

bindActions(el.list, "today");
bindActions(el.libList, "library");
bindActions(el.agendaList, "today");
bindActions(el.calDayList, "calendar");
bindActions(el.pendList, "library");

/* ---------- 顶部控件 ---------- */
el.niche.addEventListener("change", () => {
  state.niche = el.niche.value;
  run();
});

el.run.addEventListener("click", run);
el.run2.addEventListener("click", run);
el.run3.addEventListener("click", run);
el.kwRun.addEventListener("click", digKeyword);
el.kwInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") digKeyword();
});

el.modelPill.addEventListener("click", () => switchView("settings"));

document.querySelectorAll(".chip[data-plat]").forEach((c) => {
  c.addEventListener("click", () => {
    state.plat = c.dataset.plat;
    document.querySelectorAll(".chip[data-plat]").forEach((x) => {
      x.classList.toggle("on", x === c);
    });
    applyFilter();
  });
});

/* ---------- 日历 ---------- */

function isOverdue(t) {
  return t.status === "scheduled" && t.planDate && t.planDate < todayStr();
}

function cellHtml(c) {
  if (!c.date) return `<div class="cell blank"></div>`;

  const items = c.items || [];
  // 每条占两行，格子里最多放两条，多了给 +N。
  const shown = items.slice(0, 2);
  const body = shown.map((t) => {
    const cls = t.status === "published" ? "published" : (isOverdue(t) ? "overdue" : "");
    return `<div class="cell-item ${cls}" title="${esc(t.title)}">${esc(t.title)}</div>`;
  }).join("");
  const more = items.length > shown.length
    ? `<div class="cell-more">+${items.length - shown.length}</div>` : "";

  const late = items.filter(isOverdue).length;
  const warn = late ? `<span class="cell-warn">逾期 ${late}</span>` : "";

  return `<div class="cell${c.weekend ? " weekend" : ""}${c.today ? " today" : ""}"
      data-date="${c.date}">
    <div class="cell-num"><span>${c.day}</span>${warn}</div>
    ${body}${more}
  </div>`;
}

function pendHtml(t, i) {
  return `<div class="pend" data-id="${esc(t.id)}">
    <div class="pend-title" title="${esc(t.title)}">${esc(t.title)}</div>
    <div class="pend-foot">
      <span class="pend-meta">${esc(t.platformName)} · ${t.score} 分</span>
      <button class="btn btn-ghost btn-mini" data-act="plan">排期</button>
    </div>
  </div>`;
}

function renderCalendar(v) {
  el.calMonth.textContent = v.title;

  const chips = [`<span class="cal-chip">本月排了<b>${v.planned}</b>条</span>`];
  if (v.published) chips.push(`<span class="cal-chip">已发<b>${v.published}</b>条</span>`);
  if (v.overdue) {
    chips.push(`<span class="cal-chip warn">逾期<b>${v.overdue}</b>条</span>`);
  }
  if (!v.planned && !v.pending.length) {
    chips.push(`<span class="cal-chip">日历还是空的，从右边挑一条排个日子</span>`);
  }
  el.calSummary.innerHTML = chips.join("");

  el.calGrid.innerHTML = v.cells.map(cellHtml).join("");

  el.pendCount.textContent = v.pending.length;
  el.pendList.innerHTML = v.pending.length
    ? v.pending.map(pendHtml).join("")
    : `<p class="rev-empty">收下的都排上了</p>`;

  el.calDay.hidden = true;
}

async function loadCalendar(month) {
  try {
    const v = await TopicService.Calendar(month || "");
    state.calMonth = v.month;
    state.calData = v;
    renderCalendar(v);
  } catch (e) {
    toast(humanError(e));
  }
}

/* 点某一格，在下面展开当天的全部条目。
   不用弹窗：日历占了大半屏，弹窗会盖住用户刚要看的上下文。 */
function showDay(date) {
  const cell = state.calData?.cells?.find((c) => c.date === date);
  const items = cell?.items || [];
  el.calDay.dataset.date = date;
  el.calDayTitle.textContent = items.length
    ? `${shortDate(date)} · ${items.length} 条`
    : `${shortDate(date)} · 这天还空着`;
  el.calDayList.innerHTML = items.length
    ? items.map((t, i) => cardHtml(t, i, "calendar")).join("")
    : `<p class="rev-empty">把右边「收了还没排期」里的条目排到这天，它就会出现在格子里。</p>`;
  el.calDay.hidden = false;
}

el.calGrid.addEventListener("click", (ev) => {
  const cell = ev.target.closest(".cell[data-date]");
  if (cell) showDay(cell.dataset.date);
});

el.calDayClose.addEventListener("click", () => { el.calDay.hidden = true; });

/* 月份翻页：以当前的 calMonth 为基准，不依赖「今天」，
   否则用户翻到三个月前再点下一月会跳回本月。 */
function shiftMonth(delta) {
  const base = state.calMonth || todayStr().slice(0, 7);
  const [y, m] = base.split("-").map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  loadCalendar(`${d.getFullYear()}-${pad2(d.getMonth() + 1)}`);
}

el.calPrev.addEventListener("click", () => shiftMonth(-1));
el.calNext.addEventListener("click", () => shiftMonth(1));

/* ---------- 复盘 ---------- */

function heroHtml(r) {
  const notes = (r.notes || []).map((n) => `<li>${esc(n)}</li>`).join("");
  return `<div class="rev-hero">
    <h2>${esc(r.headline)}</h2>
    <p class="rev-window">${esc(r.window?.label || "")} · ${esc(r.window?.from || "")} 到 ${esc(r.window?.to || "")}</p>
    ${notes ? `<ul class="rev-notes">${notes}</ul>` : ""}
  </div>`;
}

function funnelHtml(r) {
  const rows = (r.funnel || []).map((s) => `
    <div class="funnel-row${s.count === 0 ? " zero" : ""}">
      <div class="funnel-top">
        <span>${esc(s.label)} <i>${s.count} 条</i></span>
        <b>${s.pct}%</b>
      </div>
      <div class="funnel-bar"><i style="width:${Math.max(s.pct, s.count > 0 ? 3 : 0)}%"></i></div>
    </div>`).join("");
  return `<div class="rev-panel"><h3>从选题到发出去</h3>${rows}</div>`;
}

function aiHtml(r) {
  const a = r.ai || {};
  if (!a.available || !a.highTotal) {
    return `<div class="rev-panel"><h3>模型和你合不合拍</h3>
      <p class="rev-verdict">${esc(a.verdict || "还没有数据")}</p></div>`;
  }
  // 两个孤立的大数字看不出关系，补一条比例和一句换算过的话。
  const rate = Math.round((a.highKept * 100) / a.highTotal);
  return `<div class="rev-panel">
    <h3>模型和你合不合拍</h3>
    <p class="rev-verdict">${esc(a.verdict || "")}</p>
    <div class="ai-nums">
      <div class="ai-num"><b>${a.highTotal}</b><span>模型给高分</span></div>
      <div class="ai-num"><b>${a.highKept}</b><span>你收下了</span></div>
    </div>
    <div class="ai-bar"><i style="width:${Math.max(rate, a.highKept ? 3 : 0)}%"></i></div>
    <div class="ai-foot"><span>认可了 ${rate}% 的高分选题</span><span>共 ${a.highTotal} 条</span></div>
  </div>`;
}

function sparkHtml(r) {
  const trend = r.trend || [];
  if (!trend.length) return "";
  const maxNew = Math.max(1, ...trend.map((p) => p.new));
  const maxAll = Math.max(maxNew, ...trend.map((p) => Math.max(p.picked, p.published)));

  const bars = trend.map((p) => {
    const h = Math.round((p.new / maxAll) * 100);
    const cls = p.published > 0 ? "pub" : (p.picked > 0 ? "on" : "");
    return `<i class="${cls}" style="height:${Math.max(h, 2)}%"
      title="${esc(p.day)}：新增 ${p.new}，收下 ${p.picked}，发出 ${p.published}"></i>`;
  }).join("");

  return `<div class="rev-panel full">
    <h3>每天的节奏</h3>
    <div class="spark">${bars}</div>
    <div class="spark-axis">
      <span>${esc(trend[0].day)}</span>
      <span>${esc(trend[trend.length - 1].day)}</span>
    </div>
  </div>`;
}

function regretHtml(r) {
  const list = r.regrets || [];
  const body = list.length
    ? list.map((t) => `<div class="list-row">
        <span class="row-score">${t.score}</span>
        <span class="row-title" title="${esc(t.title)}">${esc(t.title)}</span>
        <span class="row-days">${t.days ? `${t.days} 天前丢的` : "刚丢"}</span>
      </div>`).join("")
    : `<p class="rev-empty">这段时间没有丢掉高分选题，判断挺稳。</p>`;
  return `<div class="rev-panel">
    <h3>你扔掉的，但当时分很高</h3>
    ${body}
    ${list.length ? `<p class="rev-foot">这一栏不是让你后悔，是让你确认：那会儿丢得对，还是又在回避某个方向。</p>` : ""}
  </div>`;
}

function stuckHtml(r) {
  const list = r.stuck || [];
  const body = list.length
    ? list.map((t) => `<div class="list-row">
        <span class="row-days">${t.days} 天</span>
        <span class="row-title" title="${esc(t.title)}">${esc(t.title)}</span>
        <span class="row-days">${esc(t.statusLabel)}</span>
      </div>`).join("")
    : `<p class="rev-empty">没有卡住的，都在动。</p>`;
  return `<div class="rev-panel">
    <h3>卡着没动的</h3>
    ${body}
  </div>`;
}

function renderReview(r) {
  el.revBody.innerHTML = heroHtml(r) + `<div class="rev-grid">
    ${funnelHtml(r)}
    ${aiHtml(r)}
    ${sparkHtml(r)}
    ${regretHtml(r)}
    ${stuckHtml(r)}
  </div>
  <p class="rev-foot">这些数字只反映你自己的判断，不含流量。流量在下面的「笔记复盘」里——可以直接从平台拉。</p>`;
}

async function loadReview(days) {
  state.revDays = days || 7;
  paintSeg();
  try {
    const r = await TopicService.Review(state.revDays);
    renderReview(r);
  } catch (e) {
    el.revBody.innerHTML = `<p class="rev-empty">读不出来：${esc(humanError(e))}</p>`;
  }
}

function paintSeg() {
  el.revSeg.innerHTML = [7, 30, 90]
    .map((d) => `<button data-days="${d}" class="${d === state.revDays ? "on" : ""}">${d} 天</button>`)
    .join("");
}

el.revSeg.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-days]");
  if (b) loadReview(Number(b.dataset.days));
});

/* ---------- 笔记复盘 ----------
   上面那块（判断）不用填任何东西，所以永远有数据；这一块必须用户自己填，
   所以它天生容易空着。为了不让它变成「第二个台账」，做了三件事：

   1. 没填数据的记录不进这个列表——那是发布台还没记完的账，摆到这里
      只会让一屏全是待办，人看一眼就关了。
   2. 点星就立刻存，不等「保存」。用户点完星很可能直接关窗口，
      那时候再弹「有未保存的修改」已经晚了。
   3. 打星只刷统计、不重画列表；点「存」才整列刷新。否则用户刚打完星，
      卡片就从他鼠标底下消失（从「待复盘」跳到「已复盘」），
      他会以为点坏了。 */

const nb = {
  stats: $("noteStats"),
  insight: $("noteInsight"),
  tabs: $("noteTabs"),
  list: $("noteList"),
  note: $("noteNote"),
  sync: $("noteSync"),
};

let nbBoard = null;
let nbTab = "pending"; // pending / done / lesson

async function loadNotes() {
  // 同步条先渲染，不等列表：用户进这个页最常见的情况恰恰是
  // 「台账有记录、数据一片空白」，那时最需要看到的是一键同步入口。
  paintNoteSync();
  try {
    nbBoard = await TopicService.NoteBoard();
  } catch (e) {
    nb.list.innerHTML = `<p class="note-empty">读不出来：${esc(humanError(e))}</p>`;
    return;
  }
  const s = nbBoard.stats || {};
  const items = nbBoard.items || [];

  if (!items.length) {
    nb.note.textContent = "";
    nb.stats.innerHTML = "";
    nb.insight.hidden = true;
    nb.tabs.innerHTML = "";
    nb.list.innerHTML = `<p class="note-empty">${
      s.total
        ? `台账里记了 ${s.total} 条，但数据都还没回填。先去「发布台」把播放、点赞填上，这里才有可复盘的东西。`
        : "还没发过笔记。发出去之后在「发布台」记一笔，过两天回来填上数据，就能开始复盘了。"
    }</p>`;
    return;
  }

  paintNoteStats(s);
  paintNoteInsight(nbBoard.insight);
  paintNoteTabs();
  paintNoteList();
}

/* 从平台一键拉数据。

   为什么值得做这一块：手抄这四个数的失败方式不是「抄错」，是「不抄」。
   发完笔记就去忙别的，三天后要么懒得翻回去抄，要么记不清哪条是哪条——
   于是复盘页常年空着，而它正是整个工具里唯一能用真实结果校准直觉的地方。

   两条铁律写在界面上，不写在注释里：
   1. 拉不到就说拉不到。绝不填 0 顶上去——0 是「真的没人看」。
   2. 对不上的不硬凑。挂错人比没数据难查得多。 */
async function paintNoteSync() {
  if (!nb.sync) return;
  let list = [];
  try {
    list = (await TopicService.MetricsPlatforms()) || [];
  } catch {
    // 取不到平台能力就不显示这一块：宁可没有按钮，
    // 也不要给一个点了没反应的按钮。
    nb.sync.hidden = true;
    return;
  }
  if (!list.length) {
    nb.sync.hidden = true;
    return;
  }
  const canAuto = list.filter((p) => p.auto);
  const noAuto = list.filter((p) => !p.auto);
  nb.sync.hidden = false;
  if (!canAuto.length) {
    // 一个都取不了时别摆一排灰卡片占页头：压成一句话。
    // 必须说「为什么 + 怎么办」，只说「取不到」等于告诉用户这功能没用——
    // 而多半只是还没登录，扫码一次就能用了。
    const why = noAuto.map((p) => p.reason).filter(Boolean);
    nb.sync.innerHTML = `<span class="head-sync-off">${esc(
      why[0] || "这些平台的数据拿不到，只能手填"
    )}</span>`;
    return;
  }
  nb.sync.innerHTML = canAuto
    .map(
      (p) => `<button class="note-sync-btn" data-sync="${esc(p.platform)}" title="用已经登录的浏览器把数据拉下来">
        <b>${esc(p.platformName)}</b>
        <span>从平台拉取 · 台账 ${p.count} 条</span>
      </button>`
    )
    .join("");
}

async function syncMetrics(btn) {
  const platform = btn.dataset.sync;
  const name = btn.querySelector("b")?.textContent || "平台";
  const orig = btn.innerHTML;
  btn.disabled = true;
  // 会弹出一个真浏览器窗口，先说清楚。用户看到窗口自己跳出来
  // 又不知道在干什么，第一反应是关掉它。
  btn.innerHTML = `<b>${esc(name)}</b><span>正在打开浏览器取数…</span>`;
  try {
    const r = await TopicService.SyncNoteMetrics(platform);
    let msg = `取到 ${r.fetched} 条，对上 ${r.matched} 条`;
    // 没对上的必须报出来。悄悄跳过会让用户以为「同步成功了」，
    // 而实际上他一半的笔记还是没有数据——这种「看起来成功」最费时间。
    if (r.unmatched > 0) msg += `，另有 ${r.unmatched} 条在台账里找不到`;
    btn.innerHTML = `<b>${esc(name)}</b><span>${esc(msg)}</span>`;
    toast(`${name}：${msg}`);
    await loadNotes();
  } catch (e) {
    btn.innerHTML = orig;
    btn.disabled = false;
    toast(humanError(e));
  }
}

function paintNoteStats(s) {
  const avg = s.avgStar > 0 ? s.avgStar.toFixed(1) : "—";
  // 没回填的那部分要写在标题旁边：只有 3 条有数据就报「平均 4.3 星」，
  // 是拿一部分当全部，而用户会拿这个数当结论。
  // 后面必须标「全部时间」：页头那个 7/30/90 只管上面那块（判断），
  // 这一块不跟窗口走——把三个月前的笔记藏起来，恰好藏掉了最该补复盘的那批。
  nb.note.textContent = s.unrated > 0
    ? `另有 ${s.unrated} 条还没填数据`
    : `${s.total || 0} 条记录 · 全部时间`;
  nb.stats.innerHTML = `
    <div class="stat-mini${s.pending > 0 ? " accent" : ""}"><b>${s.pending || 0}</b><span>待复盘</span></div>
    <div class="stat-mini"><b>${s.rated || 0}</b><span>已复盘</span></div>
    <div class="stat-mini"><b>${avg}</b><span>平均星</span></div>
    <div class="stat-mini"><b>${s.lessons || 0}</b><span>可复用经验</span></div>`;
}

function paintNoteInsight(ins) {
  if (!ins || !ins.verdict) {
    nb.insight.hidden = true;
    return;
  }
  nb.insight.hidden = false;
  nb.insight.className = "note-insight" + (ins.available ? " on" : "");
  nb.insight.textContent = ins.verdict;
}

function noteCounts() {
  const items = nbBoard?.items || [];
  return {
    pending: items.filter((x) => !x.reviewed).length,
    done: items.filter((x) => x.reviewed).length,
    lesson: (nbBoard?.lessons || []).length,
  };
}

function paintNoteTabs() {
  const c = noteCounts();
  const tabs = [
    { id: "pending", label: "待复盘", n: c.pending },
    { id: "done", label: "已复盘", n: c.done },
    { id: "lesson", label: "可复用经验", n: c.lesson },
  ];
  nb.tabs.innerHTML = tabs
    .map(
      (t) =>
        `<button class="note-tab${t.id === nbTab ? " on" : ""}" data-ntab="${t.id}">${t.label}<b>${t.n}</b></button>`
    )
    .join("");
}

function starsHtml(n) {
  return [1, 2, 3, 4, 5]
    .map((v) => `<button class="star${v <= n ? " on" : ""}" data-star="${v}" title="点一下打 ${v} 星，再点一次撤销">★</button>`)
    .join("");
}

function noteCard(it) {
  // 数据来源要标出来：平台拉的和手抄的，摆在一起长得一样，
  // 但一个可以直接当结论用，另一个得先怀疑一下。
  const src =
    it.metricsSrc === "auto"
      ? `<span class="note-src auto">平台同步${it.metricsAt ? " " + esc(it.metricsAt) : ""}</span>`
      : it.metricsSrc === "manual"
        ? `<span class="note-src">手动填的</span>`
        : "";
  const nums = it.filled
    ? `<div class="note-nums">
         <span><i>播</i>${fmtNum(it.views)}</span>
         <span><i>赞</i>${fmtNum(it.likes)}</span>
         <span><i>藏</i>${fmtNum(it.collects)}</span>
         <span><i>评</i>${fmtNum(it.comments)}</span>
         <span class="note-rate${it.rate < 0 ? " off" : ""}">互动 ${it.rate < 0 ? "—" : it.rate.toFixed(1) + "%"}</span>
         ${src}
       </div>`
    : `<div class="note-nums"><span class="note-nodata">数据还没填，回发布台补齐，或者在上面点一下「从平台拉取」</span></div>`;
  const age = [it.platformName || "未标平台", it.date, it.ageLabel].filter(Boolean).join(" · ");
  const link = it.url
    ? `<a class="link-btn" href="${esc(it.url)}" target="_blank" rel="noreferrer">看原文</a>`
    : "";
  return `<div class="note-card" data-note="${esc(it.id)}" data-rating="${it.rating || 0}">
    <div class="note-head">
      <div class="note-title" title="${esc(it.title)}">${esc(it.title || "（选题已不在库里）")}</div>
      <div class="note-meta">${esc(age)}</div>
    </div>
    ${nums}
    <div class="note-stars" data-stars>${starsHtml(it.rating || 0)}</div>
    <input class="note-lesson" data-lesson maxlength="200" autocomplete="off"
      value="${esc(it.lesson || "")}" placeholder="一句话：这条学到的，下次能直接复用" />
    <textarea class="note-analysis" data-analysis rows="2"
      placeholder="为什么好 / 为什么差（选填，写给自己看的）">${esc(it.analysis || "")}</textarea>
    <div class="note-foot">
      ${link}
      <button class="btn btn-primary btn-mini" data-act="save-note">存</button>
    </div>
  </div>`;
}

function lessonHtml(l) {
  const stars = l.rating ? `<span class="lesson-stars">${"★".repeat(l.rating)}</span>` : "";
  const src = [stars, l.platformName || "未标平台", l.date].filter(Boolean).join(" · ");
  return `<div class="lesson-item">
    <p class="lesson-text">${esc(l.lesson)}</p>
    <div class="lesson-src">${src}${l.title ? " · " + esc(l.title) : ""}</div>
  </div>`;
}

function paintNoteList() {
  const items = nbBoard?.items || [];
  if (nbTab === "lesson") {
    const list = nbBoard?.lessons || [];
    nb.list.innerHTML = list.length
      ? `<div class="lesson-list">${list.map(lessonHtml).join("")}</div>`
      : `<p class="note-empty">还没有沉淀下来的经验。打完星，在那句「一句话」里写下能复用的结论，就会收在这里。</p>`;
    return;
  }
  const want = nbTab === "done";
  const list = items.filter((x) => x.reviewed === want);
  if (!list.length) {
    nb.list.innerHTML = `<p class="note-empty">${
      want
        ? "还没有复盘过的笔记。切到「待复盘」，给发出去的那几条打个星。"
        : "都复盘完了。切到「已复盘」看看那些星和数据对不对得上。"
    }</p>`;
    return;
  }
  nb.list.innerHTML = list.map(noteCard).join("");
}

// 存一条。quiet 表示「顺手存的」，不弹提示——打星时每次弹一下很吵。
async function saveNote(card, quiet) {
  const v = {
    id: card.dataset.note,
    rating: Number(card.dataset.rating || 0),
    analysis: card.querySelector("[data-analysis]").value.trim(),
    lesson: card.querySelector("[data-lesson]").value.trim(),
  };
  try {
    await TopicService.SaveNoteReview(v);
    if (!quiet) toast("存下了");
    const b = await TopicService.NoteBoard();
    nbBoard = b;
    paintNoteStats(b.stats || {});
    paintNoteInsight(b.insight);
    paintNoteTabs();
    // 打星时只重画星级和统计，不动列表：卡片此时若跳走，
    // 用户接着要写的分析就没有落脚点了。
    if (!quiet) paintNoteList();
  } catch (e) {
    toast(humanError(e));
  }
}

nb.sync?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-sync]");
  if (b) syncMetrics(b);
});

nb.tabs?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-ntab]");
  if (!b) return;
  nbTab = b.dataset.ntab;
  paintNoteTabs();
  paintNoteList();
});

nb.list?.addEventListener("click", (ev) => {
  const star = ev.target.closest("[data-star]");
  if (star) {
    const card = star.closest(".note-card");
    const v = Number(star.dataset.star);
    // 再点一次同一颗星就是撤销：不给撤销的话，点错了只能把星级改成 1 星，
    // 那条假评分会一直混在平均分里。
    const next = Number(card.dataset.rating || 0) === v ? 0 : v;
    card.dataset.rating = String(next);
    card.querySelector("[data-stars]").innerHTML = starsHtml(next);
    // 打完星这条其实已经挪到「已复盘」了。列表不重画（否则卡片会从鼠标底下
    // 跳走，接着要写的分析就没有落脚点），但这件事得看得见——不标出来的话，
    // 页签上的数字会比列表少一条，看着像丢了一条记录。
    card.classList.toggle("moved", nbTab === "pending" && next > 0);
    saveNote(card, true);
    return;
  }
  const b = ev.target.closest("[data-act]");
  if (b && b.dataset.act === "save-note") saveNote(b.closest(".note-card"), false);
});

/* ---------- 长期记忆 ----------
   三层：人设（这里手填）、偏好（收下/丢掉自动积累）、产出（下一版）。
   只有第一层需要用户动手，所以做得越轻越好。 */
async function loadMemory() {
  try {
    const m = await TopicService.Memory();
    const total = (m.picked || 0) + (m.dropped || 0);
    el.memCount.textContent = total;
    const on = m.aiName && !m.aiName.includes("关闭");
    el.modelBadge.textContent = on ? m.aiName : "未接入";
    el.modelBadge.className = "badge" + (on ? "" : " off");
    return m;
  } catch {
    return null;
  }
}

async function loadMemoryForm() {
  const m = await loadMemory();
  if (!m) {
    toast("读不到记忆数据");
    return;
  }
  const p = m.profile || {};
  // 人设页里存的赛道也要同步回顶部：用户在设置页把「职场」改成「美食」，
  // 刷新就该按美食抓，不能还用旧的 state.niche。
  if (p.niche && state.niches.includes(p.niche)) {
    state.niche = p.niche;
    el.niche.value = p.niche;
  }
  el.pfPositioning.value = p.positioning || "";
  el.pfAudience.value = p.audience || "";
  el.pfTone.value = p.tone || "";
  el.pfAvoid.value = p.avoid || "";
  el.memStats.textContent = `已记住 ${m.picked} 条收下、${m.dropped} 条丢掉 · 模型 ${m.aiName}`;
  paintLearnt(m.learnt, m.blocked || []);
}

/* 渲染「它自己学到的」。

   这一块存在的理由只有一个：让用户能当场证伪。
   学到的词是机器从标题里数出来的，会猜错。用户看见「汇报」被列出来，
   第一反应要么是「对，我确实爱写这个」，要么是「胡说，我只是那几天撞上了」——
   后者必须能一键点掉，否则记忆就变成了他无法反驳的偏见。 */
function paintLearnt(l, blocked) {
  l = l || { words: [] };
  const words = l.words || [];
  blocked = blocked || [];

  el.learntNote.textContent = l.note || "";

  if (!words.length && !blocked.length) {
    el.learntChips.innerHTML = "";
    return;
  }

  // 证据条数直接写在词后面。「6 条」是用户判断该不该信的依据，
  // 只给一个词不给理由，他会觉得这是随机挑的。
  const on = words.map((w) => `
    <span class="lchip">
      <b>${esc(w.text)}</b>
      <i title="出现在你收下的 ${w.picked} 条标题里">${w.picked} 条</i>
      <button class="lchip-x" data-block="${esc(w.text)}"
              title="不再按「${esc(w.text)}」加分" aria-label="删掉 ${esc(w.text)}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"
             stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"></path></svg>
      </button>
    </span>`).join("");

  // 屏蔽掉的词留在原地而不是消失：用户点掉之后需要看见「它确实没了」，
  // 以及「反悔了还能拿回来」。直接消失会让他怀疑刚才那一下是不是没生效。
  const off = blocked.map((w) => `
    <span class="lchip off">
      <b>${esc(w)}</b>
      <button class="lchip-back" data-unblock="${esc(w)}">恢复</button>
    </span>`).join("");

  el.learntChips.innerHTML = on + off;
}

el.learntChips.addEventListener("click", async (ev) => {
  const block = ev.target.closest("[data-block]");
  const unblock = ev.target.closest("[data-unblock]");
  if (!block && !unblock) return;

  const btn = block || unblock;
  btn.disabled = true;
  try {
    if (block) {
      await TopicService.BlockLearntWord(block.dataset.block);
      toast("不再按这个词加分 · 下次刷新重新排序");
    } else {
      await TopicService.UnblockLearntWord(unblock.dataset.unblock);
      toast("已恢复");
    }
    await loadMemoryForm();
  } catch (e) {
    btn.disabled = false;
    toast(humanError(e));
  }
});

/* 路径太长时保留头尾、中间省略。
   不用 CSS 的 direction:rtl 硬截——那会把斜杠和字符顺序一起翻掉，
   真机上看过一次显示成「..brary/…/miniwork.db/」，比截断还糟。 */
function shortPath(p) {
  if (!p || p.length <= 46) return p || "—";
  const parts = p.split("/").filter(Boolean);
  if (parts.length <= 3) return p;
  return "/" + parts[0] + "/…/" + parts.slice(-2).join("/");
}

/* ---------- 设置 ---------- */

async function loadSchedule() {
  try {
    const s = await TopicService.Schedule();
    el.schEnabled.checked = !!s.enabled;
    el.schTime.value = s.time || "08:30";
    el.schState.textContent = s.enabled ? "已开启" : "已关闭";
    el.schLast.textContent = s.lastRunLabel || "—";
    el.schNext.textContent = s.enabled ? (s.nextLabel || "—") : "—";
  } catch {
    el.schState.textContent = "读不出来";
  }
}

async function saveSchedule() {
  const enabled = el.schEnabled.checked;
  const time = el.schTime.value || "08:30";
  try {
    await TopicService.SaveSchedule({ enabled, time });
    await loadSchedule();
    toast(enabled ? `已开启，每天 ${time} 自动刷新` : "已关闭自动刷新");
  } catch (e) {
    toast(humanError(e));
    await loadSchedule(); // 存失败就回滚界面，别让开关骗人
  }
}

el.schEnabled.addEventListener("change", saveSchedule);
el.schTime.addEventListener("change", saveSchedule);

/* 后台运行：关窗驻留 + 开机自启。
   这两个开关合在一块，是因为它们回答的是同一个问题：「我没盯着它的时候，它还在吗」。
   定时刷新写在上面那一格里，但上面那格能不能兑现，取决于这里。 */
const shEl = {
  resident: $("shResident"),
  residentState: $("shResidentState"),
  autoStart: $("shAutoStart"),
  autoStartState: $("shAutoStartState"),
  autoStartHint: $("shAutoStartHint"),
  autoStartPath: $("shAutoStartPath"),
};

// 驻留区在 macOS 是菜单栏（右上角），在 Windows 是托盘（右下角）。
// 默认按 macOS 写，boot 里拿到平台后改。
let trayName = "菜单栏";

async function loadShell() {
  if (!shEl.resident) return;
  try {
    paintShell(await TopicService.AppShell());
  } catch {
    shEl.residentState.textContent = "读不出来";
    shEl.autoStartState.textContent = "读不出来";
  }
}

function paintShell(s) {
  shEl.resident.checked = !!s.resident;
  shEl.residentState.textContent = s.resident
    ? `关窗后留在${trayName}，定时刷新照常跑`
    : "关窗就退出，定时刷新也停";

  // 自启的真实状态来自系统，不是我们自己的配置。所以不支持、开发态运行
  // 这几种情况会带着原因回来，这里要把原因说出来——用户点不开开关时
  // 最想知道的就是「为什么」。
  const a = s.autoStart || {};
  const can = !!a.supported && !a.reason;
  shEl.autoStart.checked = !!a.enabled;
  shEl.autoStart.disabled = !can;
  shEl.autoStartState.textContent = a.enabled
    ? "开机后自动在后台跑"
    : can
      ? "开机不会自动启动"
      : "当前不可用";
  shEl.autoStartHint.textContent = a.reason || "";
  shEl.autoStartPath.textContent = a.location ? shortPath(a.location) : "—";
  shEl.autoStartPath.title = a.location || "系统里没有自启项";
}

async function saveResident() {
  const on = shEl.resident.checked;
  try {
    await TopicService.SetResident(on);
    await loadShell();
    toast(on ? `关窗后会留在${trayName}，点图标叫回来` : "关窗就会退出，定时刷新也会停");
  } catch (e) {
    toast(humanError(e));
    await loadShell(); // 存失败就回滚界面，别让开关骗人
  }
}

async function saveAutoStart() {
  const on = shEl.autoStart.checked;
  // 开自启会顺带打开驻留，所以这一下可能要动两个开关；
  // 点完统一重读一遍，界面不会出现「自启开了、驻留还显示关着」。
  try {
    const st = await TopicService.SetAutoStart(on);
    await loadShell();
    if (on && !(st && st.enabled)) {
      toast((st && st.reason) || "没能设置开机自启");
    } else {
      toast(on ? "开机后会自动跑起来" : "已取消开机自启");
    }
  } catch (e) {
    toast(humanError(e));
    await loadShell();
  }
}

shEl.resident?.addEventListener("change", saveResident);
shEl.autoStart?.addEventListener("change", saveAutoStart);

async function loadSettings() {
  const [src, m] = await Promise.all([
    TopicService.SourceName().catch(() => "—"),
    TopicService.Memory().catch(() => null),
  ]);
  el.setSource.textContent = src || "—";
  el.setAI.textContent = m?.aiName || "—";
  try {
    const p = await TopicService.DBPath();
    el.setDB.textContent = shortPath(p);
    el.setDB.title = p; // 悬停能看到完整路径，方便他手工备份
  } catch {
    el.setDB.textContent = "用户目录 / miniwork.db";
  }
  // 三块独立加载：任一块读失败都不该让另外两块空白。
  // 之前是顺序 await，定时刷新一抛错，模型那一栏就永远是「读不到」。
  await loadAIKey().catch(() => {});
  await loadAIStack();
  await loadShell().catch(() => {});
  await loadSchedule().catch(() => {});
}

/* 顶部的「模型」那行角标。改完 Key 之后要跟着变，
   否则保存成功了、上面还写着「关闭」，用户会以为没生效。 */
async function refreshAIBadge() {
  const m = await TopicService.Memory().catch(() => null);
  el.setAI.textContent = m?.aiName || "—";
}

/* 模型分工。这一块的存在理由是：配了多个模型之后，「到底谁在干活」
   是用户唯一会怀疑的事。所以场景、模型名、用在哪，全部摊开写。 */
const aiEl = {
  summary: $("aiSummary"),
  scenes: $("aiScenes"),
  models: $("aiModels"),
  hint: $("aiHint"),
  path: $("aiConfigPath"),
};

const SCENE_LABEL = { score: "批量打分", craft: "写稿与拆解" };

/* 填 Key 的表单。
   这块以前没有——文案让用户「到设置里填一个 API Key」，设置页却只有一个
   只读的分工表，用户照着找必然卡住。 */
const aiKeyEl = {
  state: $("aiKeyState"),
  base: $("aiBase"),
  model: $("aiModel"),
  key: $("aiKey"),
  save: $("aiSave"),
  test: $("aiTest"),
  out: $("aiTestOut"),
  path: $("aiKeyPath"),
};

async function loadAIKey() {
  if (!aiKeyEl.state) return;
  try {
    const v = await TopicService.AIConfig();
    aiKeyEl.base.value = v.baseUrl || "";
    aiKeyEl.model.value = v.model || "";
    // 明文不可能回填，只能靠占位符告诉用户「已经存过一个了」。
    aiKeyEl.key.value = "";
    aiKeyEl.key.placeholder = v.hasKey ? `已保存 ${v.keyMasked}，留空不改` : "sk-…";
    if (aiKeyEl.path) aiKeyEl.path.textContent = shortPath(v.path || "");
    paintAIKeyState(v);
  } catch (e) {
    aiKeyEl.state.textContent = "读不到模型配置";
  }
}

function paintAIKeyState(v) {
  if (v.ready) {
    // 说「已配置」不说「已连通」：保存只保证地址、Key、模型名都填了，
    // Key 对不对要点了「测试连接」才知道。写成已连通会让人以为验过了。
    aiKeyEl.state.textContent = `已配置 · ${v.model || "模型"}（点「测试连接」确认能不能用）`;
    aiKeyEl.state.classList.add("ok");
  } else {
    aiKeyEl.state.textContent = "还没配置。填完就能用拆解、写稿、素材提炼";
    aiKeyEl.state.classList.remove("ok");
  }
  // 多模型优先于这一栏：配了 models 就会按场景分工，单个表单改了也不生效，
  // 不说清楚用户会以为是保存没成。
  if (v.multi > 0) {
    aiKeyEl.state.textContent += `（配置文件里另外配了 ${v.multi} 个模型，按场景分工优先）`;
  }
}

async function saveAIKey() {
  const base = aiKeyEl.base.value.trim();
  const model = aiKeyEl.model.value.trim();
  const key = aiKeyEl.key.value.trim();
  aiKeyEl.save.disabled = true;
  aiKeyEl.save.textContent = "保存中…";
  try {
    const v = await TopicService.SaveAIKey({ baseUrl: base, apiKey: key, model });
    aiKeyEl.key.value = "";
    aiKeyEl.key.placeholder = v.hasKey ? `已保存 ${v.keyMasked}，留空不改` : "sk-…";
    paintAIKeyState(v);
    toast("已保存，现在就能用");
    // 分工表和顶部角标都依赖客户端状态，跟着刷一遍，不留「保存了但到处还是关闭」。
    await loadAIStack().catch(() => {});
    await refreshAIBadge().catch(() => {});
  } catch (e) {
    toast(humanError(e));
  } finally {
    aiKeyEl.save.disabled = false;
    aiKeyEl.save.textContent = "保存并启用";
  }
}

async function testAI() {
  aiKeyEl.test.disabled = true;
  aiKeyEl.out.textContent = "正在连…";
  aiKeyEl.out.className = "key-out";
  try {
    const out = await TopicService.TestAI();
    aiKeyEl.out.textContent = `连上了：${out || "有返回"}`;
    aiKeyEl.out.classList.add("ok");
  } catch (e) {
    aiKeyEl.out.textContent = humanError(e);
    aiKeyEl.out.classList.add("bad");
  } finally {
    aiKeyEl.test.disabled = false;
  }
}

aiKeyEl.save?.addEventListener("click", saveAIKey);
aiKeyEl.test?.addEventListener("click", testAI);
aiKeyEl.key?.addEventListener("keydown", (e) => {
  if (e.key === "Enter") saveAIKey();
});


async function loadAIStack() {
  if (!aiEl.summary) return;
  try {
    const v = await TopicService.AIStack();
    paintAIStack(v || {});
  } catch {
    aiEl.summary.textContent = "读不到模型配置";
  }
}

function paintAIStack(v) {
  aiEl.summary.textContent = v.enabled
    ? `${v.mode === "relay" ? "服务端中转" : "自带 Key"} · ${v.summary || "已接入"}`
    : "关闭中，只用规则打分。填一个模型就能用";

  const scenes = v.scenes || [];
  aiEl.scenes.innerHTML = scenes
    .map(
      (s) => `<div class="ai-scene">
        <span class="ai-scene-name">${esc(s.label)}</span>
        <span class="ai-scene-arrow">→</span>
        <span class="ai-scene-model">${esc(s.model || "关闭")}</span>
        <span class="ai-scene-why">${esc(s.why || "")}</span>
      </div>`
    )
    .join("");

  const models = v.models || [];
  aiEl.models.innerHTML = models
    .map((m) => {
      const used = m.scenes || [];
      const tags = used.length
        ? used.map((k) => `<span class="ai-tag">${esc(SCENE_LABEL[k] || k)}</span>`).join("")
        : `<span class="ai-tag off">当前没用上</span>`;
      const meta = [m.model, m.baseUrl, m.keyHint]
        .filter(Boolean)
        .map((x) => `<code>${esc(x)}</code>`)
        .join("");
      return `<div class="ai-model">
        <div class="ai-model-main">
          <div class="ai-model-label">${esc(m.label || m.id || "未命名")}</div>
          <div class="ai-model-meta">${meta}${m.note ? `<span>${esc(m.note)}</span>` : ""}</div>
        </div>
        <div class="ai-model-tags">${tags}</div>
      </div>`;
    })
    .join("");

  aiEl.hint.textContent = v.hint || "";
  if (aiEl.path) {
    aiEl.path.textContent = shortPath(v.configPath || "—");
    aiEl.path.title = v.configPath || "";
  }
}

async function saveProfile() {
  el.memSave.disabled = true;
  try {
    await TopicService.SaveProfile({
      niche: state.niche,
      positioning: el.pfPositioning.value.trim(),
      audience: el.pfAudience.value.trim(),
      tone: el.pfTone.value.trim(),
      avoid: el.pfAvoid.value.trim(),
    });
    await loadMemory();
    toast("记下了，下次生成就按这个来判断");
    // 改完人设当场重判一次：让「它因为你这几句话改了判断」看得见。
    await rescore();
  } catch (e) {
    toast(humanError(e));
  } finally {
    el.memSave.disabled = false;
  }
}

el.memSave.addEventListener("click", saveProfile);

async function rescore() {
  showProgress(0, "正在按你的人设重判");
  el.logBody.innerHTML = "";
  log("按当前人设重判已有选题");
  try {
    const r = await TopicService.Rescore(state.niche);
    await refresh();
    if (r.aiScored > 0) log(`模型复核 ${r.aiScored} 条`, true);
    logDone("完成");
    if (r.aiScored > 0) toast(`已按你的人设重判了 ${r.aiScored} 条`);
    else if (r.aiNote) toast(r.aiNote);
    else toast("已记住，下次生成会直接用上");
  } catch (e) {
    log("失败：" + humanError(e));
    toast(humanError(e));
  } finally {
    hideProgress();
  }
}

/* ---------- 平台页面（内嵌） ----------

   以前点「扫码登录」会弹出一个 Chromium 窗口，用户第一反应是「这软件在动我的
   浏览器」，观感像脚本。现在网页渲染在我们自己的窗口里，但内核没换——
   扫码、传图、自动填表这些能力一步没少，只是不再让用户看见浏览器本身。

   这一层只做四件事：把画面贴出来、把点击/滚轮/键盘投回去、跟着容器改视口、
   关掉。所有关于平台的知识都在 Go 那边，界面不碰。 */

const live = {
  platform: "",
  frames: 0,
  shown: false,
  deadTimer: 0,
  resizeTimer: 0,
  // detached 表示这一屏已经拆到独立窗口里了。
  //
  // 拆出去之后，画面由那个窗口负责显示；这边既不能再画（画了会和它
  // 抢同一路帧，白费带宽），也不能再报尺寸（报过去会把视口改成这边
  // 那块已经隐藏的区域的大小，用户的页面会突然变形）。
  detached: false,
};

// liveHost 把地址缩成域名，顶部只显示这一截。
// 完整 URL 又长又带参数，用户看的是「我是不是还在小红书上」，域名就够。
function liveHost(url) {
  try {
    return new URL(url).host;
  } catch {
    return url || "";
  }
}

// 平台名的兜底表。
//
// 正常情况下从 PublishStatus 里取，但那一份要等第一次发布面板打开才有；
// 而从「账号页点扫码登录」进来时它可能还是空的，标题就会显示成
// xiaohongshu 这种内部 key。用户看到的是「这是哪个平台」，不该是标识符。
const LIVE_NAMES = {
  xiaohongshu: "小红书",
  douyin: "抖音",
  wechat: "公众号",
};

// livePoint 把界面坐标换成页面里的坐标。
//
// 不用「图片像素 / 显示尺寸」那套换算：视口尺寸是照着这块区域的 CSS 尺寸设的，
// 两边一一对应，减掉边框就是答案。多一层换算就多一处会算错的地方。
function livePoint(e) {
  const r = $("liveStage").getBoundingClientRect();
  return { x: Math.round(e.clientX - r.left), y: Math.round(e.clientY - r.top) };
}

function liveSend(ev) {
  if (!live.platform) return;
  TopicService.LiveInput(live.platform, ev).catch(() => {
    /* 投递失败不弹窗：用户的下一个动作会带出同样的问题，
       连着弹几次红字只会挡住他正在看的页面 */
  });
}

// onLiveFrame 收到一帧就换图。
//
// 直接用 data URL 而不是 createObjectURL：每一帧都是一次性的，
// 用 blob URL 就必须自己回收，漏一次就白白占着几十 KB。
function onLiveFrame(ev) {
  const d = ev?.data ?? ev ?? {};
  if (!d.data) return;
  // 拆出去时这一屏不画：画面归那个窗口，这里画了也看不见，
  // 反而每帧多一次解码。
  if (live.detached) return;
  const img = $("liveShot");
  if (!img) return;
  liveFitViewport(d);
  img.src = "data:image/jpeg;base64," + d.data;
  live.frames++;
  if (!live.shown) {
    live.shown = true;
    $("liveHint").hidden = true;
    clearTimeout(live.deadTimer);
  }
}

// liveFitViewport 兜住「帧和显示区域不一样大」这件事。
//
// 图片是 object-fit: fill，视口只要和这块区域差一点，画面就被拉伸：
// 字发虚、比例也不对。ResizeObserver 有 180 毫秒防抖，拆栏动画和
// 拖分隔条的那几帧都是拉伸过的——用户看到的「模糊」多半出在这儿。
// 所以每帧顺手比一次尺寸，最多 300 毫秒纠一次，别每帧都去设。
let liveVpFixAt = 0;
function liveFitViewport(d) {
  if (live.detached || !live.platform || !d.w || !d.h) return;
  const stage = $("liveStage");
  if (!stage) return;
  const now = Date.now();
  if (now - liveVpFixAt < 300) return;
  const r = stage.getBoundingClientRect();
  if (Math.abs(r.width - d.w) < 2 && Math.abs(r.height - d.h) < 2) return;
  liveVpFixAt = now;
  TopicService.LiveResize(live.platform, Math.round(r.width), Math.round(r.height)).catch(() => {});
}

// nextPaint 等两帧，但最多等 200 毫秒。
//
// 为什么要兜底：窗口被别的窗口盖住、或最小化到任务栏时，浏览器会暂停
// requestAnimationFrame。只等 rAF 就等于「这一步永远不结束」，
// 表现是点了登录之后界面一直停在「正在接上…」，一个请求都没发出去。
function nextPaint(timeout = 200) {
  return new Promise((resolve) => {
    let done = false;
    const fin = () => {
      if (done) return;
      done = true;
      resolve();
    };
    setTimeout(fin, timeout);
    requestAnimationFrame(() => requestAnimationFrame(fin));
  });
}

// liveViewport 把当前容器尺寸报给后端。
function liveViewport() {
  const r = $("liveStage")?.getBoundingClientRect();
  if (!r || r.width < 50) return null;
  return { w: Math.round(r.width), h: Math.round(r.height) };
}

function paintLiveHead(v) {
  const auth = $("liveAuth");
  if (!v) {
    auth.hidden = true;
    return;
  }
  auth.hidden = false;
  auth.textContent = v.authed ? "已登录" : "还没登录";
  auth.classList.toggle("on", !!v.authed);
  if (v.url) $("liveUrl").textContent = liveHost(v.url);
}

// liveWidth / setLiveWidth 读写右边那一栏的宽度。
//
// 宽度只存一个 CSS 变量：拖动分隔条时改这一个值，布局自己重排，
// 不用去算每一块的尺寸，也就不会出现「拖完和算出来的差几像素」。
function liveWidth() {
  const v = getComputedStyle(document.documentElement).getPropertyValue("--live-w").trim();
  const n = parseFloat(v);
  return Number.isFinite(n) && n > 0 ? n : 480;
}
function setLiveWidth(px) {
  document.documentElement.style.setProperty("--live-w", Math.round(px) + "px");
}

// 分栏时侧栏的宽度，必须和 CSS 里 .app.live-open 的 --side-w 一致。
// 两处对不上，算出来的右栏就比实际的宽，中间那块会被挤过头。
const LIVE_SIDE = 196;
// 中间那栏的底线。再窄下去，选题卡片的标题会折成三行、正文一列字，
// 用户会以为应用坏了——与其压成那样，不如让右栏小一点。
const LIVE_MAIN_MIN = 440;

// openLiveSplit 让右边那一栏出现在布局里。
//
// 它和普通弹层的区别就在这两行：加 class 让 grid 多出一列，
// 而不是浮在界面上。左边导航仍然能点，用户一边看平台一边看选题。
function openLiveSplit() {
  const app = document.querySelector(".app");
  if (!app) return;
  // 先加 class 再加宽：分栏会把侧栏收窄一档，宽度得按分栏后的布局算。
  app.classList.add("live-open");
  const avail = window.innerWidth - LIVE_SIDE - 5;
  const want = Math.round(avail * 0.46);
  const cap = Math.max(380, avail - LIVE_MAIN_MIN);
  setLiveWidth(Math.max(380, Math.min(want, cap)));
  $("liveSplit").hidden = false;
}

function closeLiveSplit() {
  $("liveSplit").hidden = true;
  document.querySelector(".app")?.classList.remove("live-open");
}

/* 分隔条：拖左变宽、拖右变窄。 */
let splitDrag = null;
$("liveSplit")?.addEventListener("mousedown", (e) => {
  e.preventDefault();
  splitDrag = { x: e.clientX, w: liveWidth() };
  $("liveSplit").classList.add("dragging");
  document.body.style.userSelect = "none";
});
window.addEventListener("mousemove", (e) => {
  if (!splitDrag) return;
  // 左边永远给导航和主内容留够位置，右边不小于 380
  const max = Math.max(380, window.innerWidth - LIVE_SIDE - LIVE_MAIN_MIN - 5);
  const w = Math.min(max, Math.max(380, splitDrag.w + (splitDrag.x - e.clientX)));
  setLiveWidth(w);
});
window.addEventListener("mouseup", () => {
  if (!splitDrag) return;
  splitDrag = null;
  $("liveSplit")?.classList.remove("dragging");
  document.body.style.userSelect = "";
});

/**
 * openLive 把某个平台的页面接进右边那一栏。
 *
 * opts.url 为空表示「接上画面，页面停在上次那一页」。
 */
async function openLive(platform, opts = {}) {
  if (!platform) return;
  const name = autoPlatOf(platform)?.name || LIVE_NAMES[platform] || platform;

  if (live.platform && live.platform !== platform) {
    await TopicService.LiveClose().catch(() => {});
  }
  live.platform = platform;
  live.frames = 0;
  live.shown = false;
  live.detached = false;

  $("liveTitle").textContent = name;
  $("liveUrl").textContent = opts.url ? liveHost(opts.url) : "";
  $("liveAuth").hidden = true;
  $("liveShot").removeAttribute("src");
  $("liveHint").textContent = `正在接上${name}的页面…`;
  $("liveHint").hidden = false;

  // 先把窗口撑开再分栏：三栏并排在 1040 宽的窗口里塞不下，
  // 右栏会被压成一条，平台页面在窄条里没法用。
  try {
    await TopicService.WidenForLive();
  } catch {
    /* 撑不开就按现在的宽度分，右栏窄一点但不影响能不能发 */
  }
  // 等窗口真的变宽：SetSize 是异步的，立刻量到的还是旧宽度，
  // 于是视口按窄的设、画面按宽的显示，字就被拉糊了。
  await new Promise((r) => setTimeout(r, 260));

  openLiveSplit();
  $("liveWin").hidden = false;

  // 等一帧：面板刚显示时量不到真实尺寸，报上去的视口会比实际小一圈，
  // 网页按小尺寸排版、我们按大尺寸显示，字就被拉糊了。
  //
  // 但 requestAnimationFrame 在窗口被遮住或最小化时根本不触发。
  // 只等 rAF 的话，这一步会把整条登录流程挂在这里——用户看到的是
  // 「点了登录，然后什么都没有」。所以另加一个 200ms 的兜底。
  await nextPaint();
  const vp = liveViewport() || { w: 1000, h: 700 };

  try {
    const v = await TopicService.LiveOpen({
      platform,
      url: opts.url || "",
      w: vp.w,
      h: vp.h,
    });
    paintLiveHead(v);
  } catch (e) {
    $("liveHint").hidden = false;
    $("liveHint").textContent =
      "这个平台的页面没接上：" + humanError(e) + "。可以先关掉，" +
      "到成稿页用「复制全部」手工发一次。";
    return;
  }

  $("liveIme").focus({ preventScroll: true });

  // 桌面版登录页比这一栏宽得多：抖音的登录弹窗按 1000 像素排版，
  // 塞在四百多的分栏里，二维码会被右边裁掉一半——「扫不了」的根源就在这儿。
  // 窄了就自动挪到独立窗口，那边 1024 宽，和普通浏览器一致。
  if (vp.w < 900) {
    await detachLive();
    toast("这一页要宽一点才扫得动，已经放进独立窗口");
    return;
  }

  // 十秒还没出画面，说明浏览器那边卡住了（最常见是安装包里的浏览器被删了）。
  // 与其让用户盯着白屏，不如说清楚点一下刷新。
  clearTimeout(live.deadTimer);
  live.deadTimer = setTimeout(() => {
    if (live.shown) return;
    $("liveHint").textContent =
      "页面还没出画面。点右上角「刷新」试一次；一直这样就是内置浏览器出了问题，" +
      "重装一次应用能修好。";
  }, 10000);
}

function closeLive() {
  clearTimeout(live.deadTimer);
  clearTimeout(live.resizeTimer);
  // 公众号扫码靠轮询推进：面板一关，那个页面已经不在了，
  // 再轮询只会一直显示「等扫码中」，看起来像卡死。
  if (live.platform === "wechat") {
    stopWxScan();
    if (pub.bindWxState) pub.bindWxState.textContent = "面板已关，再点「打开后台扫码」继续";
  }
  live.platform = "";
  live.shown = false;
  live.frames = 0;
  live.detached = false;
  $("liveWin").hidden = true;
  $("liveShot").removeAttribute("src");
  closeLiveSplit();
  paintLiveStatus("", "");
  $("liveMark").hidden = true;
  // 这一步同时会把拆出去的窗口关掉——关掉就是关掉，
  // 不能留一个还在投屏的窗口在屏幕上。
  TopicService.LiveClose().catch(() => {});
}

/* ---------- 拆出 / 收回 ----------
   微信点开一篇文章，那一页是独立窗口：可以挪到另一块屏、可以只关它
   不影响聊天。这里照同一个思路：平台页面能从右栏拆出去，也能收回。
   拆的只是「画面显示在哪」，浏览器会话一份都不多——登录态、正在填的
   表单、扫码进度全在原来那个 page 上。 */

async function detachLive() {
  if (!live.platform) return;
  if (live.detached) {
    // 已经拆出去了：点一下等于把它叫到前面，不要又开一个
    TopicService.LiveDetach(live.platform).catch(() => {});
    return;
  }
  try {
    await TopicService.LiveDetach(live.platform);
  } catch (e) {
    toast("拆不出去：" + humanError(e));
    return;
  }
  live.detached = true;
  // 右栏整块收掉：画面已经在另一个窗口里，这里再留一块空白
  // 只会白白吃掉中间那栏的宽度。
  $("liveShot").removeAttribute("src");
  $("liveWin").hidden = true;
  closeLiveSplit();
  toast("画面已拆到独立窗口");
}

/* reembedLive 把画面接回右栏（用户在那个窗口里点了「收回」或者按了 X）。 */
async function reembedLive() {
  if (!live.platform) return;
  live.detached = false;
  live.shown = false;
  live.frames = 0;
  $("liveShot").removeAttribute("src");
  $("liveHint").hidden = false;
  $("liveHint").textContent = "正在接上画面…";
  $("liveWin").hidden = false;
  openLiveSplit();

  await nextPaint();
  const vp = liveViewport() || { w: 900, h: 700 };
  TopicService.LiveResize(live.platform, vp.w, vp.h).catch(() => {});
  TopicService.LiveState(live.platform)
    .then((v) => {
      paintLiveHead(v);
      $("liveUrl").textContent = liveHost(v?.url || "");
    })
    .catch(() => {});
}

// paintLiveStatus 写画面顶上那条进度。text 为空就把整条收起来。
//
// 三种颜色对应三种结局：跑着（蓝）、成了（绿）、要你接手（橙）。
// 用颜色而不是图标：这条最高频的读法是「扫一眼，出结果没有」。
function paintLiveStatus(text, kind) {
  const box = $("liveStatus");
  if (!box) return;
  if (!text) {
    box.hidden = true;
    return;
  }
  box.hidden = false;
  box.classList.toggle("is-ok", kind === "ok");
  box.classList.toggle("is-warn", kind === "warn");
  $("liveStatusText").textContent = text;
  // 画面拆出去之后，进度条留在主窗口里就没人看得见了。
  // 事件是广播的，那边收到就画在自己那条上——两处显示同一句话。
  try {
    wailsRT?.Events?.Emit("live:status", { text, kind })?.catch?.(() => {});
  } catch {
    /* 发不出去不影响主窗口显示 */
  }
}

/* 画面上的操作：点、悬停、滚。 */
$("liveStage")?.addEventListener("click", (e) => {
  const p = livePoint(e);
  $("liveIme").focus({ preventScroll: true });
  liveSend({ kind: "click", x: p.x, y: p.y });
});

let liveMoveAt = 0;
$("liveStage")?.addEventListener("mousemove", (e) => {
  // 悬停只用来让下拉菜单展开，不需要跟着每次移动都投一次
  const now = Date.now();
  if (now - liveMoveAt < 120) return;
  liveMoveAt = now;
  const p = livePoint(e);
  liveSend({ kind: "move", x: p.x, y: p.y });
});

// 滚轮要自己攒着发：触控板一次滑动会连发几十个事件，
// 一个一个投过去，页面会像被人反复拽着抖。
let wheelDY = 0;
let wheelAt = 0;
let wheelPoint = { x: 0, y: 0 };
let wheelTimer = 0;
$("liveStage")?.addEventListener(
  "wheel",
  (e) => {
    e.preventDefault();
    wheelDY += e.deltaY;
    wheelPoint = livePoint(e);
    const now = Date.now();
    const flush = () => {
      if (!wheelDY) return;
      liveSend({ kind: "wheel", x: wheelPoint.x, y: wheelPoint.y, dy: Math.round(wheelDY) });
      wheelDY = 0;
      wheelAt = Date.now();
    };
    if (now - wheelAt > 50) flush();
    else {
      clearTimeout(wheelTimer);
      wheelTimer = setTimeout(flush, 50);
    }
  },
  { passive: false }
);

/* 键盘。只有四个键真的必须：回车确认、退格擦掉、Tab 换焦点、Esc 关弹窗。 */
$("liveIme")?.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    liveSend({ kind: "key", key: "escape" });
    return;
  }
  const map = { Enter: "enter", Backspace: "backspace", Tab: "tab" };
  const k = map[e.key];
  if (!k) return;
  e.preventDefault();
  liveSend({ kind: "key", key: k });
});

// 中文输入法的组合过程不能逐字投：中途的拼音会被页面当成正文收进去。
// 等 compositionend 再一次性发最终那几个字。
let liveComposing = false;
function flushLiveText() {
  const ime = $("liveIme");
  const t = ime.value;
  if (!t) return;
  ime.value = "";
  liveSend({ kind: "text", text: t });
}
$("liveIme")?.addEventListener("compositionstart", () => {
  liveComposing = true;
});
$("liveIme")?.addEventListener("compositionend", () => {
  liveComposing = false;
  flushLiveText();
});
$("liveIme")?.addEventListener("input", () => {
  if (liveComposing) return;
  flushLiveText();
});

$("liveClose")?.addEventListener("click", closeLive);
$("livePop")?.addEventListener("click", detachLive);
$("liveMark")?.addEventListener("click", () => markRecord($("liveMark")));
$("liveReload")?.addEventListener("click", async () => {
  if (!live.platform) return;
  $("liveHint").hidden = false;
  $("liveHint").textContent = "正在重新加载…";
  live.shown = false;
  try {
    await TopicService.LiveReload(live.platform);
  } catch (e) {
    $("liveHint").textContent = humanError(e);
  }
});
$("liveBack")?.addEventListener("click", async () => {
  if (!live.platform) return;
  try {
    await TopicService.LiveBack(live.platform);
  } catch {
    /* 退不回去（比如本来就在第一页）不算错误 */
  }
});

/* 窗口大小变了要跟着改视口，否则网页还按旧宽度排版，
   右边露出一条空白、点击也开始偏移。 */
if (window.ResizeObserver) {
  const ro = new ResizeObserver(() => {
    if (!live.platform || live.detached) return;
    clearTimeout(live.resizeTimer);
    live.resizeTimer = setTimeout(() => {
      const vp = liveViewport();
      if (vp) TopicService.LiveResize(live.platform, vp.w, vp.h).catch(() => {});
    }, 180);
  });
  const stage = $("liveStage");
  if (stage) ro.observe(stage);
}

/* ---------- 事件订阅 ---------- */
// wailsRT 存一份运行时引用：除了订阅事件，还要往外发「进度」这种
// 只在拆出窗口时才有用的事件（见 paintLiveStatus）。
let wailsRT = null;

async function subscribeEvents() {
  try {
    const rt = await import("/wails/runtime.js");
    wailsRT = rt;
    rt.Events.On("pipeline:progress", (ev) => {
      const d = (ev && ev.data) || ev || {};
      showProgress(Number(d.pct ?? 0), String(d.msg ?? ""));
      log(String(d.msg ?? ""), true);
    });

    // 定时器在后台跑完，用户可能正在看别的页。
    // 不静默刷新：他要是正好在改人设，列表突然变了会莫名其妙。
    rt.Events.On("schedule:ran", async () => {
      log("定时刷新完成");
      if (state.view === "today") await refresh();
      toast("定时刷新完成，今天的选题已经到了");
    });

    rt.Events.On("schedule:fail", (ev) => {
      const d = (ev && ev.data) || ev || {};
      log("定时刷新失败：" + String(d.err || ""), true);
      toast("定时刷新没成功：" + String(d.err || "").slice(0, 60));
    });

    // 托盘菜单里也能改「关窗驻留」，改完要把设置页同步过来，
    // 否则用户会看到「菜单里勾着、设置页关着」这种自相矛盾的状态。
    rt.Events.On("shell:changed", () => {
      const v = $("view-settings");
      if (v && !v.hidden) loadShell();
    });

    // 平台页面的每一帧。不走绑定调用：画面是「后端主动推」，
    // 绑定是「前端问一次答一次」，方向对不上。
    rt.Events.On("live:frame", onLiveFrame);

    // 拆出去的窗口被关掉时由 Go 那边发出来。两种结局要分开处理：
    // 收回（closed=false）把画面接回右栏；关掉（closed=true）连整栏一起收。
    rt.Events.On("live:detached", (ev) => {
      const d = (ev && ev.data) || ev || {};
      if (d.open) return;
      if (!live.platform) return;
      if (d.closed) {
        live.detached = false;
        closeLive();
        return;
      }
      reembedLive();
    });
  } catch {
    /* 事件不是核心路径，拿不到就静默降级 */
  }
}

/* ---------- 首启动三问 ----------
   为什么必须做：一套「长期记忆」如果第一天是空的，它第 1 天和第 30 天一样笨。
   用户不会等到第 30 天，所以冷启动必须靠这三句话，而不是靠慢慢学。

   为什么只问三件：定位、读者、禁忌——这三项决定了模型判断「适不适合你」，
   其余都能从行为里学，问了反而增加放弃率。

   为什么给可点示例：让一个创作者「描述你的目标读者」，大多数人写不出来；
   给三个范例让他改，完成率完全不是一个量级。 */
const ONBOARD = [
  {
    field: "positioning",
    q: "你主要做什么内容？",
    hint: "先说方向，再用一句话说清你站在什么位置。「职场博主」太泛，模型只能给你通用选题；「二线城市的 HR，专写职场潜规则」它就知道该挑什么。",
    label: "一句话说清你是谁",
    placeholder: "例：二线城市的 HR，专写职场潜规则",
    samples: [
      "一线大厂程序员，写裁员和跳槽",
      "两个孩子的妈妈，写省钱和育儿踩坑",
      "开了 5 年小饭馆，写餐饮的真实成本",
    ],
  },
  {
    field: "audience",
    q: "写给谁看？",
    hint: "越具体越准。写「所有人」等于没写，模型会退回按赛道泛泛打分，跟你自己刷热榜没区别。",
    label: "你的读者是谁",
    placeholder: "例：工作 3–8 年、想跳槽又不敢的白领",
    samples: ["和我一样的同行", "刚入行的新人", "踩过坑的普通人", "想转型的中年人"],
  },
  {
    field: "avoid",
    q: "有哪些你不想碰？",
    hint: "写下来它就不会再推给你。这一项比「你喜欢什么」更值钱——喜欢的东西模型能学，不能碰的踩一次就掉粉。",
    label: "不碰的话题",
    placeholder: "例：不碰裁员赔偿的法律细节",
    samples: ["法律、医疗这类专业结论", "需要真人露脸的", "需要内部消息的", "暂时没有"],
  },
];

const ob = { i: 0, niche: "", positioning: "", audience: "", avoid: "" };

function renderOb() {
  const s = ONBOARD[ob.i];
  el.obDots.innerHTML = ONBOARD.map(
    (_, k) => `<span class="ob-dot${k <= ob.i ? " on" : ""}"></span>`
  ).join("");

  let html = `<p class="ob-q">${esc(s.q)}</p><p class="ob-hint">${esc(s.hint)}</p>`;

  if (ob.i === 0) {
    html += `<div class="ob-niches">` +
      state.niches.map(
        (n) => `<button class="ob-niche${n === ob.niche ? " on" : ""}" data-niche="${esc(n)}">${esc(n)}</button>`
      ).join("") +
      `</div>`;
  }

  html += `<label class="ob-label">${esc(s.label)}</label>
    <div class="ob-input-wrap">
      <input class="ob-input" id="obInput" autocomplete="off"
        placeholder="${esc(s.placeholder)}" value="${esc(ob[s.field] || "")}">
    </div>
    <div class="ob-samples">` +
    s.samples.map((x) => `<button class="ob-sample" data-fill="${esc(x)}">${esc(x)}</button>`).join("") +
    `</div>`;

  el.obBody.innerHTML = html;
  el.obNext.textContent = ob.i === ONBOARD.length - 1 ? "开始用" : "下一步";

  const input = $("obInput");
  input.focus();
  input.setSelectionRange?.(input.value.length, input.value.length);
}

function stashOb() {
  const input = $("obInput");
  if (input) ob[ONBOARD[ob.i].field] = input.value.trim();
}

el.obBody.addEventListener("click", (ev) => {
  const chip = ev.target.closest("[data-niche]");
  if (chip) {
    ob.niche = chip.dataset.niche;
    el.obBody.querySelectorAll("[data-niche]").forEach((b) => {
      b.classList.toggle("on", b.dataset.niche === ob.niche);
    });
    return;
  }
  const fill = ev.target.closest("[data-fill]");
  if (fill) {
    const input = $("obInput");
    const text = fill.dataset.fill;
    // 「暂时没有」这类答案是清空，不是把这句话当成禁忌写进去。
    input.value = text === "暂时没有" ? "" : text;
    input.focus();
  }
});

el.obNext.addEventListener("click", async () => {
  stashOb();
  if (ob.i < ONBOARD.length - 1) {
    ob.i++;
    renderOb();
    return;
  }
  await finishOb(false);
});

el.obSkip.addEventListener("click", () => finishOb(true));

document.addEventListener("keydown", (ev) => {
  if (el.onboard.hidden) return;
  if (ev.key === "Enter") {
    ev.preventDefault();
    el.obNext.click();
  }
});

/* 等后台那次启动抓取跑完。
   引导和抓取是并行的，用户在填表时列表已经在生成；填完再抢着改分数，
   两边会同时写 topic，分数会跳。 */
async function waitIdle(timeout = 25000) {
  const t0 = Date.now();
  while (state.running && Date.now() - t0 < timeout) {
    await new Promise((r) => setTimeout(r, 180));
  }
}

async function finishOb(skip) {
  el.obNext.disabled = true;
  el.obSkip.disabled = true;
  stashOb();
  try {
    if (!skip) {
      await TopicService.SaveProfile({
        niche: ob.niche || state.niche,
        positioning: ob.positioning,
        audience: ob.audience,
        tone: "",
        avoid: ob.avoid,
      });
    }
    await TopicService.FinishOnboarding();
    el.onboard.hidden = true;
    state.suppressUntil = Date.now() + AFTER_ONBOARD_QUIET;

    if (skip) return;

    const niche = ob.niche || state.niche;
    const changed = niche !== state.niche;
    state.niche = niche;
    el.niche.value = niche;

    await waitIdle();
    await loadMemory();

    if (changed) {
      await run();
    } else {
      await rescore();
    }
  } catch (e) {
    toast(humanError(e));
  } finally {
    el.obNext.disabled = false;
    el.obSkip.disabled = false;
  }
}

/* ---------- 启动 ---------- */
function paintHeader() {
  const d = new Date();
  const week = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"][d.getDay()];
  el.todayDate.textContent = `${d.getMonth() + 1}.${d.getDate()} ${week}`;
  const h = d.getHours();
  const word = h < 5 ? "夜深了" : h < 11 ? "早上好" : h < 14 ? "中午好" : h < 18 ? "下午好" : "晚上好";
  el.heroHi.textContent = `${word}，创作者 👋`;
}

/* ============================================================
   账号：机器码即身份
   ------------------------------------------------------------
   没有登录页——第一次打开时后端已经按机器码建好档了。
   这块只做两件事：让用户把名字留下来，以及一眼看到
   「哪些外部账号还没绑、绑到哪一步」。所有凭证都是用户自己的。
   ============================================================ */

const acct = {
  shown: $("acctShown"),
  avatar: $("acctAvatar"),
  created: $("acctCreated"),
  code: $("acctCode"),
  copy: $("acctCopy"),
  name: $("acctName"),
  phone: $("acctPhone"),
  save: $("acctSave"),
  out: $("acctOut"),
  binds: $("acctBinds"),
  dot: $("navAcctDot"),
  hero: $("heroAvatar"),
};

/* 首页那个字取用户的姓，没填名字才显示「创」。
   为什么是姓不是全名：那块只有 56px，放两个汉字就挤成一条，
   而首屏左上角这个字的作用是「这是你的工作台」，一个姓就够认。 */
function surName(name) {
  const n = String(name || "").trim();
  if (!n) return "创";
  const first = [...n][0] || "创";
  return first.toUpperCase();
}

const BIND_WHERE = { settings: "去设置", publish: "去发布台" };

function paintAccount(v) {
  if (!v) return;
  state.account = v;
  acct.shown.textContent = v.name || "还没填名字";
  // 头像用名字第一个字。没名字时显示一个中性符号，不要显示「未」——
  // 那个字看着像错误提示。
  acct.avatar.textContent = (v.name || "·").trim().slice(0, 1) || "·";
  acct.created.textContent = v.createdLabel || "—";
  acct.code.textContent = v.code || "—";
  acct.name.value = v.name || "";
  acct.phone.value = v.phone || "";
  if (acct.dot) acct.dot.hidden = !(v.complete === false || v.unbound > 0);
  if (acct.hero) acct.hero.textContent = surName(v.name);

  // 左上角那个头像：一个姓就够认出「这是我」，名字太长会把这栏撑破，
  // 所以头像只放一个字，全名放在下面一行、超长自动截断。
  const sideAvatar = $("sideAvatar");
  if (sideAvatar) sideAvatar.textContent = surName(v.name);
  const sideName = $("sideName");
  if (sideName) sideName.textContent = String(v.name || "").trim() || "Miniwork";

  const list = v.bindings || [];
  acct.binds.innerHTML = list.length
    ? bindRowsHtml(list, "nav")
    : `<p class="acct-bind-empty">发布和模型都还没配。配好之后这里会显示状态</p>`;
}

async function loadAccount() {
  try {
    paintAccount(await TopicService.Account());
  } catch (e) {
    toast("读不到账号信息：" + humanError(e));
  }
}

acct.save?.addEventListener("click", async () => {
  acct.save.disabled = true;
  const old = acct.save.textContent;
  acct.save.textContent = "保存中…";
  try {
    // 不本地校验手机号格式：后端挡明显不是号码的输入就够了，
    // 界面上再加一套规则，两边迟早不一致。
    paintAccount(await TopicService.UpdateAccount(acct.name.value, acct.phone.value));
    acct.out.textContent = "已保存";
    setTimeout(() => {
      if (acct.out.textContent === "已保存") acct.out.textContent = "";
    }, 2200);
  } catch (e) {
    acct.out.textContent = humanError(e);
  } finally {
    acct.save.disabled = false;
    acct.save.textContent = old;
  }
});

acct.copy?.addEventListener("click", () => {
  const code = state.account?.code;
  if (!code) return toast("还没读到设备码");
  copyText(code);
});

/* 绑定那一行点哪儿都一样：整行都是入口，跳去能绑的地方。
   不在这里直接弹表单——模型的 Key、公众号的凭证各有各的规矩，
   在账号页再实现一遍，两处就会各写一套校验。 */
acct.binds?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-bind]");
  if (!b) return;
  if (b.dataset.where === "publish") {
    // 已连上：带他去能发的地方（待发区，那里有「发一篇」），
    // 别再滚到绑定面板——那儿只有状态，没有可点的动作。
    if (b.dataset.ready === "1") return goToPanel("publish", "pubQueue");
    return goToPanel("publish", "bindPanel");
  }
  goToPanel("settings", "aiPanel");
});

async function boot() {
  // 平台差异两件事，一次问清：
  // 1) Windows / Linux 有系统标题栏，页面顶部那条 46px（为 macOS 红绿灯留的
  //    可拖拽区）要去掉，否则标题栏下面多一条空白，看着像没对齐；
  // 2) 那块图标区在 macOS 叫「菜单栏」，在 Windows 叫「托盘」。文案跟平台走，
  //    否则设置页写着「留在菜单栏」，Windows 用户得先猜「菜单栏在哪」。
  // 不 await：这个判断值一帧，不能拖慢首屏。
  TopicService.Platform()
    .then((p) => {
      if (!p || p === "darwin") return;
      document.body.classList.add("native-titlebar");
      trayName = "托盘";
    })
    .catch(() => {});

  paintHeader();
  paintSeg();

  // 启动时先把赛道对齐到人设里存的那一个。
  //
  // 以前这里写死 "职场"，或者只在引导弹窗里设一次：老用户填完「我做餐饮」
  // 关掉引导，下次启动又变回职场，刷新照着职场抓——用户看到的就是
  // 「我明明写的是餐饮，怎么全是升职跳槽」。profile.niche 是唯一事实来源，
  // 下拉框和刷新参数都得跟它走。
  let mem0 = null;
  try {
    state.niches = (await TopicService.Niches()) || [];
    mem0 = await TopicService.Memory();
    const savedNiche = mem0?.profile?.niche;
    if (savedNiche && state.niches.includes(savedNiche)) {
      state.niche = savedNiche;
    }
    el.niche.innerHTML = state.niches
      .map((n) => `<option${n === state.niche ? " selected" : ""}>${esc(n)}</option>`)
      .join("");
    el.niche.value = state.niche;
    const src = await TopicService.SourceName();
    paintSourceLine(src);
  } catch (e) {
    toast("初始化失败：" + humanError(e));
  }

  await subscribeEvents();

  // 引导的判断要放在 refresh/run 之前：让用户一进来就看到这三问，
  // 但抓取不能等它——下面那句 run() 照常发车，用户在填表时列表已经在长了。
  try {
    const o = await TopicService.Onboarding();
    if (o?.need) {
      ob.niche = o.niche || state.niches[0] || "职场";
      state.niche = ob.niche;
      el.niche.value = ob.niche;
      el.onboard.hidden = false;
      renderOb();
    }
  } catch {
    /* 引导状态读不到就不弹，绝不能因为它拦住用户用产品 */
  }

  await loadMemory();

  // 账号只用来点导航上的小圆点（资料没填全 / 还有没绑的号），
  // 不 await：它不该拖慢首屏，更不该因为读失败让整个界面停住。
  loadAccount().catch(() => {});

  try {
    await refresh();
  } catch (e) {
    toast(humanError(e));
  }

  // 创作工具能不能用取决于有没有模型，启动时问一次就够。
  paintCraftStatus();

  // 首启动：库里没有待定选题就自动跑一次，保证 90 秒内看到东西。
  if (state.all.length === 0) run();
}

/* ============================================================
   成稿
   ------------------------------------------------------------
   这一页的取舍：所有输入放上面一条，稿子占满下面整块。
   不做左右分栏——左边一栏输入会把正文挤到 400px 宽，
   而正文是要一行一行读、一句一句改的东西，宽度就是可读性。
   ============================================================ */

const wr = {
  topics: $("wrTopics"),
  plats: $("wrPlats"),
  format: $("wrFormat"),
  matToggle: $("wrMatToggle"),
  matBox: $("wrMatBox"),
  material: $("wrMaterial"),
  skel: $("wrSkel"),
  hint: $("wrHint"),
  run: $("wrRun"),
  empty: $("wrEmpty"),
  emptyTitle: $("wrEmptyTitle"),
  emptySub: $("wrEmptySub"),
  editor: $("wrEditor"),
  title: $("wrTitle"),
  titleCount: $("wrTitleCount"),
  issues: $("wrIssues"),
  body: $("wrBody"),
  bodyCount: $("wrBodyCount"),
  edTools: $("edTools"),
  note: $("wrNote"),
  tags: $("wrTags"),
  shots: $("wrShots"),
  where: $("wrWhere"),
  save: $("wrSave"),
  copyTitle: $("wrCopyTitle"),
  copy: $("wrCopy"),
  del: $("wrDel"),
  publish: $("wrPublish"),
  send: $("wrSend"),
};

const write = {
  platforms: null,   // 平台规格只拉一次，之后本地用
  topics: [],
  topicId: "",
  pendingTopic: "",
  platform: "xiaohongshu",
  format: "long",
  skeleton: null,
  skeletonFrom: "",
  draft: null,
  dirty: false,
};

/* 按字符数，不按字节：Go 侧的 []rune 和这里的 Array.from 口径一致。
   用 .length 会把 emoji 和部分中文标点数成两个，用户看到的字数
   和平台看到的对不上。 */
const rlen = (s) => Array.from(String(s ?? "")).length;

/* 平台规格只在这几个函数里取，别在别处再写一份 find：

   · currentSpec() 是工具栏选中的那个，只管「下一篇写什么」；
   · activeSpec() 是编辑区正在呈现的那一篇，字数上限、口播布局都按它算。

   两者可以不一致（用户先开了一篇小红书稿，又把工具栏换成公众号）。
   不一致时必须以稿子为准——否则正文还是小红书那一千字的长度，
   却按公众号的规则判「没超」，复制出去才发现被平台截了。 */
const SPEC_FALLBACK = { key: "xiaohongshu", name: "小红书", titleMax: 20, bodyMax: 1000, tagMax: 10, format: "long" };

/* 形态的中文名。后端 draft.FormatLabel 是同一套说法，改成两样用户会看糊涂。 */
const FMT_LABEL = { long: "图文", short: "口播稿" };
function fmtLabel(f) { return FMT_LABEL[f] || "图文"; }

function specOf(key) {
  return (write.platforms || []).find((p) => p.key === key) || SPEC_FALLBACK;
}

function currentSpec() { return specOf(write.platform); }

function draftPlatform() { return (write.draft && write.draft.platform) || write.platform; }
function draftFormat() { return (write.draft && write.draft.format) || write.format; }
function activeSpec() { return specOf(draftPlatform()); }

async function loadWrite() {
  try {
    if (!write.platforms) {
      write.platforms = (await TopicService.Platforms()) || [];
      paintPlats();
    }
    await loadWriteTopics();
    // 没有指定目标选题时，默认落在第一条上。
    //
    // 不这么做的话，进这个页面看到的是「还没有可以写的选题，先去今日灵感收下一条」——
    // 而上面明明列着三条。那句文案只在真的没选题时才成立，用户会被这两句话夹在中间，
    // 以为列表是坏的。自动选一条同时把已有稿子带出来，打开就能接着改。
    const want =
      write.pendingTopic || write.topicId || (write.topics[0] && write.topics[0].id) || "";
    write.pendingTopic = "";
    paintSkeleton();
    // 有目标选题就切过去：从发布台点「去写」时，用户期望直接落在那一篇上。
    if (want) await selectTopic(want);
  } catch (e) {
    toast(humanError(e));
  }
}

/* 能写的是「已收下」与「已排期」两类。
   待定的也能写，但那等于跳过了「这条我要不要写」这个决定，
   先把决定做掉再写字，否则写出来的是别人觉得好的东西。 */
async function loadWriteTopics() {
  const [picked, scheduled] = await Promise.all([
    TopicService.List("picked", 100),
    TopicService.List("scheduled", 100),
  ]);
  const seen = new Set();
  write.topics = [...(picked || []), ...(scheduled || [])].filter((t) => {
    if (seen.has(t.id)) return false;
    seen.add(t.id);
    return true;
  });
  paintTopics();
}

function paintTopics() {
  if (!write.topics.length) {
    wr.topics.innerHTML = "";
    wr.empty.hidden = false;
    wr.emptyTitle.textContent = "还没有可以写的选题";
    wr.emptySub.textContent = "先去「今日灵感」收下一条。收下之后它就在这儿等你。";
    wr.editor.hidden = true;
    wr.run.disabled = true;
    wr.hint.textContent = "先挑一条选题";
    return;
  }
  wr.topics.innerHTML = write.topics
    .map(
      (t) => `<button class="wr-topic${t.id === write.topicId ? " on" : ""}" data-topic="${esc(t.id)}">
        <span>${esc(t.title)}</span>${t.planLabel ? `<em>${esc(t.planLabel)}</em>` : ""}
      </button>`
    )
    .join("");
  wr.run.disabled = false;
  if (!write.draft) wr.hint.textContent = "选好平台，点右边生成第一稿";
}

function paintPlats() {
  wr.plats.innerHTML = (write.platforms || [])
    .map((p) => `<button class="plat-pick${p.key === write.platform ? " on" : ""}" data-plat="${esc(p.key)}">${esc(p.name)}</button>`)
    .join("");
}

function paintFormat() {
  wr.format.querySelectorAll("button").forEach((b) => {
    b.classList.toggle("on", b.dataset.format === write.format);
  });
  // 布局跟着「正在编辑的那一篇」走，不是跟着工具栏：开了口播稿再切到图文，
  // 编辑区仍然要给分镜留位置，否则分镜整块被挤没，用户以为稿子丢了。
  wr.editor.classList.toggle("is-short", draftFormat() === "short");
}

function paintSkeleton() {
  const has = Array.isArray(write.skeleton) && write.skeleton.length > 0;
  wr.skel.hidden = !has;
  if (!has) return;
  // 骨架条长在「我的素材」框里面，那个框默认是收着的。
  // 不顺手展开的话，用户点完「照这个结构写一篇」只看到页面一跳，
  // 没有任何东西告诉他结构带过来了——会以为没生效，再点一次。
  wr.matBox.hidden = false;
  // 拆解带过来的是一句公式（一条），手工搭骨架是多条，两种都要说人话。
  const tail = ` <button class="link-btn" data-skel-clear="1">清掉</button>`;
  const from = esc(write.skeletonFrom || "拆解");
  wr.skel.innerHTML =
    write.skeleton.length === 1
      ? `已带上${from}的结构：<b class="skel-line">${esc(write.skeleton[0])}</b>${tail}`
      : `已带上${from}的 ${write.skeleton.length} 步骨架${tail}`;
}

async function selectTopic(id) {
  write.topicId = id;
  write.dirty = false;
  paintTopics();
  wr.empty.hidden = true;
  wr.editor.hidden = true;
  try {
    const list = (await TopicService.Drafts(id)) || [];
    if (list.length) {
      setDraft(list[0]);
      // 注意是 wr（节点表）不是 write（状态对象）。写成 write.hint 会抛
      // 「Cannot set properties of undefined」——而它发生在 setDraft 之后，
      // 稿子已经填好了，所以除了弹一句英文报错，看起来一切正常，最难查。
      wr.hint.textContent = `这条已有 ${list.length} 稿，最新一稿是 ${list[0].createdAt}`;
    } else {
      write.draft = null;
      wr.empty.hidden = false;
      wr.emptyTitle.textContent = "这条还没有稿";
      wr.emptySub.textContent = "选好平台和形态，点上面的「生成成稿」。";
      wr.hint.textContent = "这条还没写";
    }
  } catch (e) {
    toast(humanError(e));
  }
}

function setDraft(d) {
  const prev = write.draft;
  write.draft = d || null;
  if (!d) {
    wr.editor.hidden = true;
    return;
  }
  wr.editor.hidden = false;
  wr.empty.hidden = true;
  // 只有「换了一稿」才跟着它的平台走。同一稿保存后回传时不动工具栏：
  // 用户可能已经把工具栏切到别的平台、准备写下一篇了，这时候把选择弹回去
  // 等于把他刚做的选择抹掉，而且下一次生成又会写回原来的平台。
  if (!prev || prev.id !== d.id) {
    write.platform = d.platform || write.platform;
    write.format = d.format || "long";
  }
  paintPlats();
  paintFormat();

  wr.title.value = d.title || "";
  bodySet(d.body || "");
  wr.note.textContent = d.note || "";
  // 这句是「不想用自动发布时」的退路，措辞必须把位置摆正：
  // 写「复制后：…」会让人以为复制粘贴才是主流程，
  // 而现在旁边就摆着一个「发到平台」。
  wr.where.textContent = d.where ? `手动发：${d.where}` : "";
  paintTags();
  paintShots();
  paintWriteCounts();
}

function paintWriteCounts() {
  const sp = activeSpec();
  const short = draftFormat() === "short";
  const tl = rlen(wr.title.value);
  const bl = rlen(bodyText());
  const tags = write.draft?.tags || [];

  markCount(wr.titleCount, tl, sp.titleMax);

  /* 口播稿的正文是台词，它的限制是时长不是字数：
     中文口播每秒约 4.5 个字，80 秒是甜区，超过 90 秒就得二倍速。
     显示「217 字 ≈ 48 秒」比显示「217/1000」有用得多——
     后者会让人以为还有 800 字可以写。 */
  const sec = Math.round(bl / 4.5);
  if (short) {
    wr.bodyCount.textContent = bl ? `${bl} 字 ≈ ${sec} 秒` : "0 字";
    wr.bodyCount.classList.toggle("over", bl > 0 && (sec > 92 || sec < 58));
  } else {
    markCount(wr.bodyCount, bl, sp.bodyMax);
  }

  /* 越界提醒在本地算，不等保存后回传：用户每敲一个字都该看到
     「再写两个字就看不见了」，而不是打完一整段再被告知白写了。 */
  const out = [];

  /* 工具栏选的和手上这稿不是一回事时，明说一句。
     不说的话，用户会以为刚点的「公众号」已经作用到这篇稿子上了，
     然后按公众号的规则检查一遍，复制出去才发现被截。 */
  if (write.draft && (draftPlatform() !== write.platform || draftFormat() !== write.format)) {
    const want = specOf(write.platform);
    out.push({
      block: false,
      text: `当前编辑的是${sp.name}·${fmtLabel(draftFormat())}，工具栏选的是${want.name}·${fmtLabel(write.format)}。换平台只影响下一篇，点「生成成稿」会另写一篇`,
    });
  }

  if (sp.titleMax && tl > sp.titleMax) {
    out.push({ block: true, text: `标题 ${tl} 字，${sp.name}只显示前 ${sp.titleMax} 字，第 ${sp.titleMax + 1} 个字开始看不见` });
  }
  if (short && bl > 380) {
    out.push({ block: true, text: `台词 ${bl} 字，约 ${sec} 秒，超过 90 秒了。念不完的部分会被剪掉` });
  } else if (short && bl > 0 && bl < 260) {
    out.push({ block: false, text: `台词只有 ${bl} 字，约 ${sec} 秒，撑不到 60 秒` });
  } else if (!short && sp.bodyMax && bl > sp.bodyMax) {
    out.push({ block: true, text: `正文 ${bl} 字，超过${sp.name} ${sp.bodyMax} 字上限，超出部分发不出去` });
  }
  if (sp.tagMax && tags.length > sp.tagMax) {
    out.push({ block: false, text: `标签 ${tags.length} 个，${sp.name}最多 ${sp.tagMax} 个，多的会被丢掉` });
  }
  wr.issues.innerHTML = out
    .map((i) => `<p class="issue${i.block ? " is-block" : ""}">${esc(i.text)}</p>`)
    .join("");
}

function markCount(node, len, max) {
  if (!node) return;
  node.textContent = max ? `${len}/${max}` : String(len);
  node.classList.toggle("over", !!max && len > max);
}

function paintTags() {
  const tags = write.draft?.tags || [];
  wr.tags.innerHTML =
    tags
      .map(
        (t, i) => `<span class="tag-chip">${esc(t)}<button data-tag-del="${i}" title="删掉这个标签">×</button></span>`
      )
      .join("") + `<input class="tag-add" id="wrTagAdd" placeholder="+ 标签" maxlength="20" autocomplete="off">`;
}

function paintShots() {
  const segs = write.draft?.segments || [];
  if (!segs.length) {
    wr.shots.hidden = true;
    wr.shots.innerHTML = "";
    return;
  }
  wr.shots.hidden = false;
  wr.shots.innerHTML =
    `<p class="shot-cap">分镜（照着拍。台词以正文为准，这里改不了）</p>` +
    segs
      .map(
        (s) => `<div class="shot">
          <span class="shot-at">${esc(s.at || "")}</span>
          <div class="shot-main">
            <p class="shot-shot">${esc(s.shot || "")}</p>
            <p class="shot-line">${esc(s.line || "")}</p>
          </div>
        </div>`
      )
      .join("");
}

function markDirty() {
  write.dirty = true;
  paintWriteCounts();
}

/* ---------- 正文编辑器 ----------
   正文原来是 <textarea>。小标题、列表、引用在平台正文里都只是「空行 + 符号」，
   纯文本框等于让用户自己记符号、自己数空行。这里换成 contenteditable：
   写的时候是排好版的样子，存下去仍是纯文本。

   所以这一整块只有一个真正的约束：DOM ↔ 纯文本要能来回转换而不丢信息。
   所有格式都选了「纯文本里也有写法」的那几种（列表用 · / 1.、分割线用 ———），
   不做加粗斜体——那两个在纯文本里活不下来，做了等于骗用户。 */

const ED_BLOCK_TAGS = new Set(["P", "DIV", "H1", "H2", "H3", "H4", "H5", "H6", "BLOCKQUOTE", "PRE"]);
const ED_BULLET_RE = /^[·•]\s+/;
const ED_NUM_RE = /^\d{1,2}[.、)）]\s+/;
const ED_DIVIDER_RE = /^(?:—{3,}|-{3,}|_{3,}|={3,})$/;

/* bodySet 纯文本 → DOM。按空行切段，段内单换行保留成 <br>：
   口播稿的台词是逐行的，把它并成一段会让节奏全丢。 */
function bodySet(v) {
  const root = wr.body;
  if (!root) return;
  root.innerHTML = "";
  const text = String(v || "").replace(/\r\n?/g, "\n");
  for (const raw of text.split(/\n{2,}/)) {
    const b = raw.replace(/^\n+|\n+$/g, "");
    if (!b.trim()) continue;
    root.appendChild(edBuild(b));
  }
  edSync();
}

/* bodyText DOM → 纯文本。块之间是空行，块内换行是单换行。
   列表带上 · / 1. 前缀，分割线写成 ———：这三个符号在任何平台的正文框里
   都能原样显示，读者看到的就是稿子里的样子。 */
function bodyText() {
  const root = wr.body;
  if (!root) return "";
  const blocks = [];
  let cur = "";

  const flush = () => {
    blocks.push(cur.replace(/[ \t\u00a0]+$/gm, ""));
    cur = "";
  };

  const walk = (node) => {
    for (const n of node.childNodes) {
      if (n.nodeType === 3) {
        cur += n.nodeValue;
        continue;
      }
      if (n.nodeType !== 1) continue;
      const t = n.tagName;
      if (t === "BR") {
        cur += "\n";
        continue;
      }
      if (t === "HR") {
        flush();
        blocks.push("———");
        continue;
      }
      if (t === "UL" || t === "OL") {
        // 整张列表算一个块：每项各占一块的话，块之间会用空行隔开，
        // 存下去再读回来就变成「每行一个单条列表」，间距全散了。
        flush();
        const items = [];
        let i = 1;
        for (const li of n.children) {
          cur = t === "OL" ? `${i++}. ` : "· ";
          walk(li);
          items.push(cur.replace(/[ \t\u00a0]+$/gm, ""));
          cur = "";
        }
        if (items.length) blocks.push(items.join("\n"));
        continue;
      }
      if (ED_BLOCK_TAGS.has(t)) {
        walk(n);
        flush();
        continue;
      }
      walk(n);
    }
  };

  walk(root);
  flush();

  return blocks
    .map((b) => b.replace(/\n{3,}/g, "\n\n").trim())
    .filter(Boolean)
    .join("\n\n");
}

/* 纯文本的一段 → 一个块。识别顺序有讲究：先判整块是不是列表
   （每行都带前缀才算），否则「· 先说结论」这种单行会误判成列表。 */
function edBuild(text) {
  const lines = text.split("\n");
  if (lines.length === 1 && ED_DIVIDER_RE.test(lines[0].trim())) {
    return document.createElement("hr");
  }
  if (lines.every((l) => ED_BULLET_RE.test(l))) {
    const ul = document.createElement("ul");
    for (const l of lines) {
      const li = document.createElement("li");
      li.textContent = l.replace(ED_BULLET_RE, "");
      ul.appendChild(li);
    }
    return ul;
  }
  if (lines.every((l) => ED_NUM_RE.test(l))) {
    const ol = document.createElement("ol");
    for (const l of lines) {
      const li = document.createElement("li");
      li.textContent = l.replace(ED_NUM_RE, "");
      ol.appendChild(li);
    }
    return ol;
  }
  // AI 偶尔仍给 markdown 标题；去掉 # 当小标题，省得用户手动清
  if (lines.length === 1 && /^#{1,3}\s+/.test(lines[0])) {
    const h = document.createElement("h3");
    h.textContent = lines[0].replace(/^#{1,3}\s+/, "");
    return h;
  }
  const p = document.createElement("p");
  lines.forEach((l, i) => {
    if (i) p.appendChild(document.createElement("br"));
    if (l) p.appendChild(document.createTextNode(l));
  });
  if (!p.firstChild) p.appendChild(document.createElement("br"));
  return p;
}

/* 选区只在编辑器内部时才返回：工具栏点击、页面别处选中文字都要能区分开，
   否则 queryCommandState 会把外面的状态读成正文的。 */
function edSelection() {
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount) return null;
  const r = sel.getRangeAt(0);
  if (!wr.body || !wr.body.contains(r.commonAncestorContainer)) return null;
  return { sel, r };
}

function edBlockOf(node) {
  let n = node && node.nodeType === 1 ? node : node?.parentElement;
  while (n && n !== wr.body) {
    if (ED_BLOCK_TAGS.has(n.tagName) || n.tagName === "LI") return n;
    n = n.parentElement;
  }
  return null;
}

function edCaret(node, offset = 0) {
  if (!node) return;
  try {
    const r = document.createRange();
    r.setStart(node, offset);
    r.collapse(true);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(r);
    wr.body?.focus();
  } catch {
    /* 节点刚被替换掉的情况，光标留在原处也能继续写，不值得报错 */
  }
}

/* 工具栏高亮：光标停在标题里时 H 亮着，用户不用回头确认这一段是什么。 */
function edSync() {
  if (!wr.edTools) return;
  let block = "";
  let ul = false;
  let ol = false;
  try {
    block = String(document.queryCommandValue("formatBlock") || "").toLowerCase();
    ul = document.queryCommandState("insertUnorderedList");
    ol = document.queryCommandState("insertOrderedList");
  } catch {
    /* 选区不在编辑器里时这些命令会抛，按「什么都没选中」处理 */
  }
  const state = { h3: /^h[1-6]$/.test(block), p: block === "p" || block === "div", quote: block === "blockquote", ul, ol, focus: edFocus.on };
  wr.edTools.querySelectorAll("[data-ed]").forEach((b) => {
    const k = b.dataset.ed;
    if (k in state) b.classList.toggle("on", !!state[k]);
  });
}

const edFocus = {
  on: false,
  set(v) {
    this.on = !!v;
    wr.editor?.classList.toggle("is-focus", this.on);
    if (this.on) wr.body?.focus();
    edSync();
  },
  toggle() {
    this.set(!this.on);
  },
};

/* 标题、引用里回车：浏览器会继续同一个标签，一句标题一回车，
   整篇就都成了标题。这里改成回车另起正文段落。 */
function edSplitToParagraph(blk, sel) {
  const r = sel.r;
  const tail = document.createRange();
  tail.setStart(r.endContainer, r.endOffset);
  tail.setEndAfter(blk.lastChild || blk);
  let frag = null;
  try {
    frag = tail.extractContents();
  } catch {
    frag = null;
  }
  const p = document.createElement("p");
  if (frag) p.appendChild(frag);
  if (!p.textContent.trim() && !p.querySelector("br,img")) p.appendChild(document.createElement("br"));
  blk.after(p);
  edCaret(p, 0);
}

/* Chrome 在段落里执行列表命令时，会生成 <p><ul>…</ul></p> 这种非法嵌套：
   浏览器照样渲染，但缩进、间距全歪，而且再执行一次命令会套得更深。
   这里把里层的块提到段落外面，段落该拆就拆。 */
function edHoist() {
  const root = wr.body;
  if (!root) return;
  const SEL = "p > ul, p > ol, p > blockquote, p > hr, p > h1, p > h2, p > h3, p > h4";
  for (let guard = 0; guard < 40; guard++) {
    const inner = root.querySelector(SEL);
    if (!inner) break;
    const para = inner.parentElement;
    const tail = para.cloneNode(false);
    while (inner.nextSibling) tail.appendChild(inner.nextSibling);
    para.after(inner);
    if (tail.childNodes.length) inner.after(tail);
    const hasText = para.textContent.trim() !== "" || para.querySelector("br,img");
    if (!hasText) para.remove();
  }
}

function edInsertDivider(blk) {
  const p = document.createElement("p");
  p.appendChild(document.createElement("br"));
  if (blk && blk.parentNode) {
    blk.replaceWith(document.createElement("hr"), p);
  } else {
    wr.body.append(document.createElement("hr"), p);
  }
  edCaret(p, 0);
}

function edTextBeforeCaret(block) {
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount) return "";
  const r = sel.getRangeAt(0);
  const head = document.createRange();
  head.setStart(block, 0);
  head.setEnd(r.startContainer, r.startOffset);
  return head.toString();
}

/* 行首符号 + 空格 → 变成对应格式，markdown 的手感。
   只在段首、且只认最常见的几个；认太多反而会误触（比如正文里写「1. 2. 3.」）。 */
function edSpaceShortcut(ev) {
  const s = edSelection();
  if (!s) return;
  const blk = edBlockOf(s.r.startContainer);
  if (!blk || !/^(P|DIV|H[1-6]|BLOCKQUOTE)$/.test(blk.tagName)) return;
  const before = edTextBeforeCaret(blk);
  let kind = "";
  if (before === "#" || before === "##" || before === "###") kind = "h3";
  else if (before === ">") kind = "quote";
  else if (before === "-" || before === "*") kind = "ul";
  else if (before === "1." || before === "1、") kind = "ol";
  else if (before === "---") kind = "hr";
  if (!kind) return;
  ev.preventDefault();
  // 先把标记本身删掉：留着的话「# 小标题」里的 # 会跟着发出去
  const head = s.r.cloneRange();
  head.setStart(blk, 0);
  head.deleteContents();
  if (kind === "hr") {
    edInsertDivider(blk);
  } else if (kind === "ul" || kind === "ol") {
    document.execCommand(kind === "ul" ? "insertUnorderedList" : "insertOrderedList");
  } else {
    document.execCommand("formatBlock", false, kind === "h3" ? "h3" : "blockquote");
  }
  edHoist();
  markDirty();
  edSync();
}

/* 粘贴只吃纯文本：从网页或 Word 粘进来会带一层外来样式（字号、行距、颜色、
   甚至整段 <div style>），那些样式在平台输入框里活不下来，留在编辑器里
   只会让稿子看起来和发出去的不一样。 */
function edInsertPlain(txt) {
  const lines = txt.split("\n");
  lines.forEach((l, i) => {
    if (i) document.execCommand("insertLineBreak");
    if (l) document.execCommand("insertText", false, l);
  });
}

function edPasteText(raw) {
  const text = String(raw || "").replace(/\r\n?/g, "\n").replace(/\u00a0/g, " ");
  if (!text.trim()) return;
  const blocks = text
    .split(/\n{2,}/)
    .map((s) => s.replace(/^\n+|\n+$/g, ""))
    .filter((s) => s.trim());

  if (blocks.length <= 1) {
    edInsertPlain(blocks[0] || text);
  } else {
    // 多段就另起块接在后面，不拆当前段：拆错一次，用户得自己手动修段落
    const s = edSelection();
    const blk = s && edBlockOf(s.r.startContainer);
    const nodes = blocks.map(edBuild);
    if (blk) {
      // 注意是 blk.after，不是 wr.body.after：后者会把段落插到编辑器外面去
      let anchor = blk;
      for (const node of nodes) {
        anchor.after(node);
        anchor = node;
      }
      edCaret(anchor, anchor.childNodes.length);
    } else {
      wr.body.append(...nodes);
      edCaret(nodes[nodes.length - 1], nodes[nodes.length - 1].childNodes.length);
    }
  }
  edHoist();
  markDirty();
  edSync();
}

const ED_CMD = {
  undo: () => document.execCommand("undo"),
  redo: () => document.execCommand("redo"),
  h3: () => document.execCommand("formatBlock", false, "h3"),
  p: () => document.execCommand("formatBlock", false, "p"),
  ul: () => document.execCommand("insertUnorderedList"),
  ol: () => document.execCommand("insertOrderedList"),
  quote: () => document.execCommand("formatBlock", false, "blockquote"),
  hr: () => {
    document.execCommand("insertHorizontalRule");
    const s = edSelection();
    if (s && s.r.startContainer.nodeType === 1 && s.r.startContainer.tagName === "HR") {
      const p = document.createElement("p");
      p.appendChild(document.createElement("br"));
      s.r.startContainer.after(p);
      edCaret(p, 0);
    }
  },
  clear: () => {
    document.execCommand("removeFormat");
    document.execCommand("formatBlock", false, "p");
  },
  focus: () => edFocus.toggle(),
};

/* 工具栏用 mousedown 拦一下：按下按钮时选区会移到按钮上，
   等到 click 再执行命令，Range 已经没了，格式作用不到刚选的那几个字。 */
wr.edTools?.addEventListener("mousedown", (ev) => {
  if (ev.target.closest("[data-ed]")) ev.preventDefault();
});
wr.edTools?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-ed]");
  if (!b) return;
  const fn = ED_CMD[b.dataset.ed];
  if (!fn) return;
  fn();
  if (b.dataset.ed !== "focus") {
    edHoist();
    markDirty();
  }
  edSync();
});

wr.body?.addEventListener("input", () => {
  markDirty();
  edSync();
});

wr.body?.addEventListener("keydown", (ev) => {
  // Ctrl+S 直接在编辑器里存，不用挪到下面点按钮
  if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "s") {
    ev.preventDefault();
    saveDraft(false).catch((e) => toast(humanError(e)));
    return;
  }
  if (ev.key === "Escape" && edFocus.on) {
    edFocus.set(false);
    return;
  }
  if (ev.key === "Enter" && !ev.shiftKey) {
    const s = edSelection();
    const blk = s && edBlockOf(s.r.startContainer);
    if (blk && blk.tagName === "P" && blk.textContent.trim() === "---") {
      ev.preventDefault();
      edInsertDivider(blk);
      markDirty();
      return;
    }
    if (blk && /^(H[1-6]|BLOCKQUOTE)$/.test(blk.tagName)) {
      ev.preventDefault();
      if (!blk.textContent.trim()) document.execCommand("formatBlock", false, "p");
      else edSplitToParagraph(blk, s);
      markDirty();
      return;
    }
  }
  if (ev.key === " " && !ev.ctrlKey && !ev.metaKey && !ev.altKey) edSpaceShortcut(ev);
});

wr.body?.addEventListener("paste", (ev) => {
  const t = ev.clipboardData?.getData("text/plain");
  if (!t) return; // 没文本就交给浏览器（例如图片，按普通粘贴处理）
  ev.preventDefault();
  edPasteText(t);
});

wr.body?.addEventListener("drop", (ev) => {
  // 拖进一段文字也按粘贴走；拖进图片/文件一律不接——
  // 正文最终是纯文本，图片在这个通道里根本发不出去
  const t = ev.dataTransfer?.getData("text/plain");
  ev.preventDefault();
  if (t) edPasteText(t);
});

document.addEventListener("selectionchange", () => {
  if (edSelection()) edSync();
});

// 回车默认切成 <p> 而不是 <div>：div 在纯文本里没有段落语义，
// 序列化时看不出和 p 的差别，但粘贴进平台会变成挤在一起的一整段
try {
  document.execCommand("defaultParagraphSeparator", false, "p");
} catch {
  /* 老内核不支持就算了，bodyText 里 div 和 p 是同一套处理 */
}

async function runWrite() {
  if (!write.topicId) {
    toast("先挑一条选题");
    return;
  }
  // 生成新稿会覆盖编辑区，先把手上这篇存了。
  // 不存的话，用户改了两段、点「生成成稿」、对新稿不满意再退回，
  // 刚才的修改已经不在了——而且没有任何提示。
  if (write.draft && write.dirty) {
    try {
      await saveDraft(true);
    } catch (e) {
      toast("上一篇没存住，先不生成新的：" + humanError(e));
      return;
    }
  }
  wr.run.disabled = true;
  const old = wr.run.textContent;
  wr.run.textContent = "正在写…";
  // 素材是这一稿唯一别人没有的东西。没填的时候不拦，但必须说清楚：
  // 之前什么都不提示，用户拿到一稿全是通用场景，会以为是模型不行，
  // 实际是它手里真的只有选题标题这一句。
  const mat = (wr.material.value || "").trim();
  wr.hint.textContent = mat
    ? "写一篇通常 20-60 秒，别关窗口"
    : "没填素材：这一稿只能拿「大多数人的共同经历」来写，具体的东西一件都不会有";
  try {
    const d = await TopicService.Write(
      write.topicId,
      write.platform,
      write.format,
      wr.material.value,
      write.skeleton || []
    );
    write.dirty = false;
    setDraft(d);
    wr.hint.textContent = mat
      ? "写好了。改成你的语气再发，别直接用"
      : "写好了，但没用到你的素材。往「我的素材」里补三句真事，再点一次会强很多";
    toast("第一稿写好了");
  } catch (e) {
    wr.hint.textContent = "这次没写成，可以直接再点一次";
    toast(humanError(e));
  } finally {
    wr.run.disabled = false;
    wr.run.textContent = old;
  }
}

async function saveDraft(silent) {
  const d = write.draft;
  if (!d) return null;
  // 平台和形态用**这一稿自己的**，不用工具栏上的。
  // 用工具栏的话，用户开着一篇小红书稿、把工具栏换成公众号、再点保存，
  // 这篇稿子的规格就被悄悄改成公众号了：正文没重写，校验和标签拼法却换了，
  // 最后拼出来的东西按公众号的规矩加顿号，长度却还是小红书那套。
  const v = await TopicService.SaveDraft({
    id: d.id,
    topicId: d.topicId,
    platform: d.platform || write.platform,
    format: d.format || write.format,
    title: wr.title.value,
    body: bodyText(),
    tags: d.tags || [],
    segments: d.segments || [],
    note: d.note || "",
  });
  write.dirty = false;
  setDraft(v);
  if (!silent) toast("已保存");
  if (!d.edited) {
    // 第一次人改过之后，这条选题才算「真在写」。
    // 状态不动：状态是人的决定，保存不等于决定。
    try { await TopicService.Decide(d.topicId, "pick"); } catch { /* 已经是 picked 就不管 */ }
  }
  return v;
}

async function copyAll() {
  if (!write.draft) return;
  try {
    if (write.dirty) await saveDraft(true);
    const pack = await TopicService.CopyPack(write.draft.id);
    const n = rlen(pack.text);
    toast(`已复制 ${n} 字，去${currentSpec().name}粘贴`);
  } catch (e) {
    toast(humanError(e));
  }
}

async function deleteDraft() {
  if (!write.draft) return;
  if (!confirm("删掉这一稿？台账里的发布记录不会删。")) return;
  try {
    await TopicService.DeleteDraft(write.draft.id);
    setDraft(null);
    write.draft = null;
    await selectTopic(write.topicId);
    toast("删掉了");
  } catch (e) {
    toast(humanError(e));
  }
}

/* ---------- 成稿页事件 ---------- */

wr.topics?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-topic]");
  if (!b) return;
  if (write.dirty && !confirm("当前稿子有未保存的修改，切换会丢掉。继续？")) return;
  selectTopic(b.dataset.topic);
});

wr.plats?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-plat]");
  if (!b) return;
  write.platform = b.dataset.plat;
  // 换平台就跟着它的默认形态走（抖音默认口播），用户之后还能手改。
  const sp = currentSpec();
  write.format = sp.format || "long";
  paintPlats();
  paintFormat();
  paintWriteCounts();
});

wr.format?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-format]");
  if (!b) return;
  write.format = b.dataset.format;
  paintFormat();
  paintWriteCounts();
});

wr.matToggle?.addEventListener("click", () => {
  wr.matBox.hidden = !wr.matBox.hidden;
  if (!wr.matBox.hidden) wr.material.focus();
});

wr.skel?.addEventListener("click", (ev) => {
  if (!ev.target.closest("[data-skel-clear]")) return;
  write.skeleton = null;
  write.skeletonFrom = "";
  paintSkeleton();
});

wr.tags?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-tag-del]");
  if (!b || !write.draft) return;
  write.draft.tags = (write.draft.tags || []).filter((_, i) => i !== Number(b.dataset.tagDel));
  write.dirty = true;
  paintTags();
  paintWriteCounts();
});

wr.tags?.addEventListener("keydown", (ev) => {
  if (!ev.target.classList?.contains("tag-add")) return;
  if (ev.key !== "Enter" && ev.key !== ",") return;
  ev.preventDefault();
  const v = ev.target.value.trim().replace(/^[#＃]+/, "");
  if (!v || !write.draft) return;
  if ((write.draft.tags || []).includes(v)) {
    ev.target.value = "";
    return;
  }
  write.draft.tags = [...(write.draft.tags || []), v];
  write.dirty = true;
  paintTags();
  paintWriteCounts();
  wr.tags.querySelector(".tag-add")?.focus();
});

wr.title?.addEventListener("input", markDirty);
wr.run?.addEventListener("click", runWrite);
wr.save?.addEventListener("click", () => saveDraft(false).catch((e) => toast(humanError(e))));
wr.copy?.addEventListener("click", copyAll);
wr.copyTitle?.addEventListener("click", () => copyText(wr.title.value));
wr.del?.addEventListener("click", deleteDraft);
wr.publish?.addEventListener("click", () => {
  if (!write.draft) return toast("还没有稿");
  openPubPop({
    topicId: write.draft.topicId,
    draftId: write.draft.id,
    // 记哪一稿的平台，不是工具栏上的：台账上一旦记错平台，
    // 按平台统计的那几个数就全歪了，而且事后没法分辨。
    platform: write.draft.platform || write.platform,
    title: wr.title.value,
  });
});

/* 「发到平台」＝从写直接跨到发。
   先存再发是硬要求：界面上的改动没落库就发，台账会记着另一个版本，
   事后复盘时看到的标题和当时实际发出去的能对不上。 */
wr.send?.addEventListener("click", async () => {
  if (!write.draft) return toast("还没有稿");
  if (write.dirty) {
    try {
      await saveDraft(true);
    } catch (e) {
      return toast("先存下来才能发：" + humanError(e));
    }
  }
  const d = write.draft;
  if (!d) return toast("还没有稿");
  openAutoPop({
    topicId: d.topicId,
    draftId: d.id,
    title: wr.title.value.trim() || d.title || "（无标题）",
    platform: d.platform || write.platform,
  });
});

/* ============================================================
   发布台
   ------------------------------------------------------------
   「半自动」的全部意思就在这一页：
   程序不替你点发布（那得模拟浏览器，会被风控且随改版失效），
   但它把「发什么、发到哪、发完回来记一笔」串成一条不会断的线。
   ============================================================ */

const pub = {
  queue: $("pubQueue"),
  records: $("pubRecords"),
  stats: $("pubStats"),
  queueNote: $("pubQueueNote"),
  recNote: $("pubRecNote"),
  // 绑定区。名字取 bind*，和账号页的 acct* 区分开：
  // 两边渲染的是同一份数据（TopicService.Bindings），但动作按钮各在各的页面上。
  binds: $("pubBinds"),
  bindNote: $("bindNote"),
  bindBrowser: $("bindBrowser"),
  bindRecheck: $("bindRecheck"),
  bindWx: $("bindWx"),
  bindWxAppID: $("bindWxAppID"),
  bindWxSecret: $("bindWxSecret"),
  bindWxSave: $("bindWxSave"),
  bindWxTest: $("bindWxTest"),
  bindWxCancel: $("bindWxCancel"),
  bindWxOut: $("bindWxOut"),
  bindWxScan: $("bindWxScan"),
  bindWxState: $("bindWxState"),
  bindWxIp: $("bindWxIp"),
  bindHint: $("bindHint"),
  bindHintText: $("bindHintText"),
  tasks: $("taskList"),
  taskNote: $("taskNote"),
  taskClear: $("taskClear"),
  taskNew: $("taskNew"),
};

let pubBoard = null;
let pubAccountsCache = {};

function fmtNum(n) {
  if (n === null || n === undefined || n < 0) return "—";
  if (n >= 10000) return (n / 10000).toFixed(1).replace(/\.0$/, "") + "万";
  return String(n);
}

async function loadPublish() {
  try {
    const b = await TopicService.PublishBoard();
    pubBoard = b;
    pubAccountsCache = b.accounts || {};
    renderPubStats(b.stats || {});
    renderTasks(b.tasks || []);
    renderQueue(b.queue || []);
    renderRecords(b.records || []);
    loadBindings();
    paintQueueBadge((b.queue || []).length);
  } catch (e) {
    toast(humanError(e));
  }
}

function paintQueueBadge(n) {
  if (!el.navQueue) return;
  el.navQueue.hidden = !n;
  el.navQueue.textContent = String(n);
}

function renderPubStats(st) {
  const filled = st.filled || 0;
  const total = st.total || 0;
  pub.stats.innerHTML = `
    <div class="stat-mini"><b>${total}</b><span>记过的发布</span></div>
    <div class="stat-mini"><b>${fmtNum(st.views)}</b><span>总播放</span></div>
    <div class="stat-mini"><b>${fmtNum((st.likes || 0) + (st.collects || 0))}</b><span>赞 + 藏</span></div>
    <div class="stat-mini${filled < total ? " accent" : ""}"><b>${filled}/${total}</b><span>回填了数据</span></div>`;
}

/* ---------- 发布任务（三段式的产物） ---------- */

function renderTasks(list) {
  pub.taskNote.textContent = list.length ? `${list.length} 条还没发` : "";
  pub.taskClear.hidden = !list.length;
  if (!list.length) {
    pub.tasks.innerHTML = `<p class="pub-empty">还没有任务。点「新建发布」：选类型 → 选稿子和平台 → 生成一张清单，之后一条条划掉。</p>`;
    return;
  }
  pub.tasks.innerHTML = list
    .map(
      (t, i) => `<div class="task-item${t.draftGone ? " is-gone" : ""}">
      <span class="task-no">${i + 1}</span>
      <div class="task-main">
        <div class="task-title">${esc(t.title || "（无标题）")}</div>
        <div class="task-sub">${esc(t.platformName)} · ${esc(t.formatName)}${
        t.words ? ` · ${t.words} 字` : ""
      }${t.account ? ` · ${esc(t.account)}` : " · 没记账号"}${t.draftGone ? " · 稿子已删" : ""}</div>
      </div>
      <div class="task-acts">
        ${
          t.draftGone
            ? ""
            : `<button class="btn btn-ghost btn-mini" data-tact="go" data-id="${esc(t.id)}" data-topic="${esc(t.topicId)}" data-draft="${esc(t.draftId)}" data-plat="${esc(t.platform)}" data-title="${esc(t.title)}">去发</button>`
        }
        <button class="btn btn-primary btn-mini" data-tact="done" data-id="${esc(t.id)}" data-topic="${esc(
        t.topicId
      )}" data-draft="${esc(t.draftId)}" data-plat="${esc(t.platform)}" data-title="${esc(t.title)}">已发</button>
        <button class="link-btn" data-tact="drop" data-id="${esc(t.id)}">删</button>
      </div>
    </div>`
    )
    .join("");
}

pub.tasks?.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-tact]");
  if (!b) return;
  const act = b.dataset.tact;
  if (act === "drop") {
    try {
      await TopicService.DropPublishTask(b.dataset.id);
      await loadPublish();
    } catch (e) {
      toast(humanError(e));
    }
    return;
  }
  if (act === "go") {
    openAutoPop({
      topicId: b.dataset.topic,
      draftId: b.dataset.draft,
      platform: b.dataset.plat,
      title: b.dataset.title,
    });
    return;
  }
  if (act === "done") {
    // 先记账再划掉任务（openPubPop 里记账成功后才回调）。
    // 反过来的话，记账失败时任务已经没了，等于凭空丢一次发布。
    openPubPop({
      topicId: b.dataset.topic,
      draftId: b.dataset.draft,
      platform: b.dataset.plat,
      title: b.dataset.title,
      onDone: async () => {
        try {
          await TopicService.CompletePublishTask(b.dataset.id);
          await loadPublish();
        } catch (e) {
          toast(humanError(e));
        }
      },
    });
  }
});

pub.taskClear?.addEventListener("click", async () => {
  if (!confirm("清空所有还没发的任务？稿子本身不会删。")) return;
  try {
    await TopicService.ClearPublishTasks();
    await loadPublish();
    toast("清空了");
  } catch (e) {
    toast(humanError(e));
  }
});

/* ---------- 新建发布：三段式 ----------
   1 选类型 → 2 选素材和账号 → 3 确认生成。
   为什么分三步而不是一个表单：这三件事有依赖（类型决定能选哪些稿），
   塞进一屏会变成一堆互相牵连的下拉框，改一个全都要重算。 */

const tz = {
  root: $("taskPop"),
  progress: $("tzProgress"),
  title: $("tzTitle"),
  drafts: $("tzDrafts"),
  plats: $("tzPlats"),
  draftCount: $("tzDraftCount"),
  platCount: $("tzPlatCount"),
  summary: $("tzSummary"),
  plan: $("tzPlan"),
  hint: $("tzHint"),
  back: $("tzBack"),
  next: $("tzNext"),
  step: 1,
  format: "",
  picked: new Set(),
  targets: new Set(),
  draftsData: [],
  accounts: {},
  binds: [],
  busy: false,
};

const TZ_TITLE = { 1: "选发布类型", 2: "选素材和账号", 3: "确认生成" };

async function openTaskWizard() {
  tz.step = 1;
  tz.format = "";
  tz.picked = new Set();
  tz.targets = new Set();
  tz.root.hidden = false;
  paintTz();
  try {
    const [drafts, accounts, binds, plats] = await Promise.all([
      TopicService.RecentDrafts(60),
      TopicService.Accounts(),
      TopicService.Bindings(),
      (write.platforms || []).length ? Promise.resolve(write.platforms) : TopicService.Platforms(),
    ]);
    tz.draftsData = drafts || [];
    tz.accounts = accounts || {};
    tz.binds = binds || [];
    write.platforms = plats || [];
  } catch (e) {
    toast(humanError(e));
  }
  paintTz();
}

function tzDraftsOfFormat() {
  return tz.draftsData.filter((d) => (d.format || "long") === tz.format);
}

function tzPlatsOfFormat() {
  return (write.platforms || []).filter((p) => (p.format || "long") === tz.format);
}

function paintTz() {
  tz.progress.textContent = `${tz.step} / 3`;
  tz.title.textContent = TZ_TITLE[tz.step] || "";
  tz.root.querySelectorAll(".tz-pane").forEach((p) => {
    p.hidden = p.dataset.pane !== String(tz.step);
  });
  tz.root.querySelectorAll(".tz-type").forEach((b) => {
    b.classList.toggle("on", b.dataset.fmt === tz.format);
  });

  if (tz.step === 2) {
    const list = tzDraftsOfFormat();
    tz.drafts.innerHTML = list.length
      ? list
          .map(
            (d) => `<button class="tz-item${tz.picked.has(d.id) ? " on" : ""}" data-draft="${esc(d.id)}">
          <span class="tz-tick"></span>
          <span class="tz-item-main">
            <span class="tz-item-title">${esc(d.title || d.topicTitle || "未命名")}</span>
            <span class="tz-item-sub">${esc(d.platformName)} · ${d.bodyLen || 0} 字</span>
          </span>
        </button>`
          )
          .join("")
      : `<p class="tz-empty">还没有${fmtLabel(tz.format)}的稿子。先去「成稿」写一篇，再回来发。</p>`;

    const plats = tzPlatsOfFormat();
    tz.plats.innerHTML = plats.length
      ? plats
          .map((p) => {
            const b = tz.binds.find((x) => x.key === p.key);
            const ok = b && b.ready;
            return `<button class="tz-item${tz.targets.has(p.key) ? " on" : ""}" data-plat="${esc(p.key)}">
          <span class="tz-tick"></span>
          <span class="tz-item-main">
            <span class="tz-item-title">${esc(p.name)}</span>
            <span class="tz-item-sub">${ok ? "已连上" : "还没登录"}${tz.accounts[p.key] ? " · " + esc(tz.accounts[p.key]) : ""}</span>
          </span>
        </button>`;
          })
          .join("")
      : `<p class="tz-empty">这个形态还没有可发的平台。</p>`;
  }

  if (tz.step === 3) {
    const n = tz.picked.size * tz.targets.size;
    const pickedDrafts = tzDraftsOfFormat().filter((d) => tz.picked.has(d.id));
    tz.summary.textContent = `${tz.picked.size} 篇 × ${tz.targets.size} 个平台 = ${n} 条任务`;
    tz.plan.innerHTML =
      pickedDrafts
        .map(
          (d) =>
            `<div class="tz-plan-row"><span>${esc(d.title || d.topicTitle || "未命名")}</span><em>${[...tz.targets]
              .map((k) => esc(platformNameOf(k)))
              .join(" / ")}</em></div>`
        )
        .join("") + (n > 12 ? `<p class="tz-plan-more">清单较长，先生成，之后一条条发。</p>` : "");
    tz.hint.textContent = n > 20 ? "一次最多 20 条，超出的会被截掉" : "";
  } else {
    tz.hint.textContent =
      tz.step === 1 ? "" : tz.picked.size && tz.targets.size ? "" : "素材和平台各选至少一个";
  }

  tz.draftCount.textContent = `已选 ${tz.picked.size} 篇`;
  tz.platCount.textContent = `已选 ${tz.targets.size} 个`;
  tz.back.hidden = tz.step === 1;

  const ready = tz.step === 1 ? !!tz.format : tz.step === 2 ? tz.picked.size > 0 && tz.targets.size > 0 : true;
  tz.next.disabled = !ready || tz.busy;
  tz.next.textContent = tz.step === 3 ? `生成 ${tz.picked.size * tz.targets.size} 条任务` : "下一步";
}

function platformNameOf(key) {
  return (write.platforms || []).find((p) => p.key === key)?.name || key;
}

tz.root?.querySelectorAll(".tz-type").forEach((b) => {
  b.addEventListener("click", () => {
    tz.format = b.dataset.fmt;
    // 换类型要把已选清掉：图文稿和口播稿不是一批东西，
    // 留着上次勾的会在下一步凭空多出几条看不懂的任务。
    tz.picked = new Set();
    tz.targets = new Set();
    paintTz();
  });
});

tz.drafts?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-draft]");
  if (!b) return;
  const id = b.dataset.draft;
  if (tz.picked.has(id)) tz.picked.delete(id);
  else tz.picked.add(id);
  paintTz();
});

tz.plats?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-plat]");
  if (!b) return;
  const k = b.dataset.plat;
  if (tz.targets.has(k)) tz.targets.delete(k);
  else tz.targets.add(k);
  paintTz();
});

tz.back?.addEventListener("click", () => {
  if (tz.step > 1) {
    tz.step--;
    paintTz();
  }
});

tz.close?.addEventListener("click", () => {
  tz.root.hidden = true;
});

tz.root?.addEventListener("click", (ev) => {
  if (ev.target === tz.root) tz.root.hidden = true;
});

tz.next?.addEventListener("click", async () => {
  if (tz.step < 3) {
    tz.step++;
    paintTz();
    return;
  }
  if (tz.busy) return;
  tz.busy = true;
  tz.next.disabled = true;
  const old = tz.next.textContent;
  tz.next.textContent = "生成中…";
  try {
    const n = await TopicService.CreatePublishTasks({
      draftIds: [...tz.picked],
      platforms: [...tz.targets],
      accounts: tz.accounts,
    });
    tz.root.hidden = true;
    toast(`生成 ${n} 条任务，一条条发就行`);
    await loadPublish();
  } catch (e) {
    toast(humanError(e));
  } finally {
    tz.busy = false;
    tz.next.textContent = old;
    paintTz();
  }
});

pub.taskNew?.addEventListener("click", () => openTaskWizard());

function renderQueue(list) {
  pub.queueNote.textContent = list.length ? `${list.length} 条排了日子还没发` : "";
  if (!list.length) {
    pub.queue.innerHTML = `<p class="pub-empty">没有排期的。点右上角「发一篇」，挑一份写好的稿直接发；想按日子排就先去「发布日历」。</p>`;
    return;
  }
  pub.queue.innerHTML = list
    .map((it) => {
      const drafted = it.draftId
        ? `<button class="btn btn-ghost btn-mini" data-act="copy" data-draft="${esc(it.draftId)}">复制</button>
           <button class="btn btn-ghost btn-mini" data-act="open" data-topic="${esc(it.topicId)}">打开</button>
           <button class="btn btn-ghost btn-mini" data-act="auto" data-topic="${esc(it.topicId)}" data-draft="${esc(it.draftId)}" data-title="${esc(it.title)}" data-plat="${esc(it.platform || "")}">自动发</button>`
        : `<button class="btn btn-ghost btn-mini" data-act="open" data-topic="${esc(it.topicId)}">去写</button>`;
      const sub = [
        it.platformName || "未标平台",
        it.planLabel || "",
        it.draftId ? `已有 ${it.draftCount} 稿` : "还没稿",
      ]
        .filter(Boolean)
        .join(" · ");
      return `<div class="queue-item">
        <span class="q-due ${esc(it.due || "later")}">${esc(it.dueLabel || it.planLabel || "")}</span>
        <div class="q-main">
          <div class="q-title" title="${esc(it.title)}">${esc(it.title)}</div>
          <div class="q-sub">${esc(sub)}</div>
        </div>
        <div class="q-acts">
          ${drafted}
          <button class="btn btn-primary btn-mini" data-act="done" data-topic="${esc(it.topicId)}" data-draft="${esc(it.draftId || "")}" data-title="${esc(it.title)}" data-plat="${esc(it.platform || "")}">已发</button>
        </div>
      </div>`;
    })
    .join("");
}

function renderRecords(list) {
  pub.recNote.textContent = list.length ? `${list.length} 条记录` : "";
  if (!list.length) {
    pub.records.innerHTML = `<p class="pub-empty">还没有发过。发完回来点「已发」，这里就开始有账了。</p>`;
    return;
  }
  pub.records.innerHTML = list
    .map((r) => {
      const nums = r.filled
        ? `<div class="rec-nums">
             <span><i>播</i>${fmtNum(r.views)}</span>
             <span><i>赞</i>${fmtNum(r.likes)}</span>
             <span><i>藏</i>${fmtNum(r.collects)}</span>
             <span><i>评</i>${fmtNum(r.comments)}</span>
           </div>`
        : `<button class="btn btn-ghost btn-mini" data-act="fill" data-id="${esc(r.id)}">填数据</button>`;
      const link = r.url
        ? `<a href="${esc(r.url)}" target="_blank" rel="noreferrer">链接</a>`
        : "";
      return `<div class="rec" data-rec="${esc(r.id)}">
        <div class="rec-main">
          <div class="rec-title" title="${esc(r.title)}">${esc(r.title || "（选题已不在库里）")}</div>
          <div class="rec-sub">
            <span>${esc(r.platformName || "未标平台")}</span>
            <span>${esc(r.date)}</span>
            ${r.account ? `<span>${esc(r.account)}</span>` : ""}
            ${link}
            ${r.filled ? `<button class="link-btn" data-act="fill" data-id="${esc(r.id)}">改</button>` : ""}
          </div>
        </div>
        ${nums}
      </div>`;
    })
    .join("");
}

/* 绑定这一行的渲染。账号页和发布台共用一份 HTML，只有按钮不同：
   账号页整行是「跳过去」，发布台整行是「当场做」。
   两处各写一套的话，早晚出现一边能绑、另一边还写着「未绑定」的鬼话。 */
const BIND_BTN = {
  "setup-ai": "去填 Key",
  "wechat-scan": "扫码绑定",
  "wechat-config": "改凭证",
  login: "登录",
  relogin: "重新登录",
  "install-browser": "怎么修",
};

/* 账号页那一侧到底该写什么。
   连上了还写「去发布台」，点过去只看到一排已经连好的状态——
   用户会以为入口坏了（他确实这么说过）。连上就该直接说「去发布」。 */
function bindGoLabel(b) {
  if (b.ready && b.where === "publish") return "去发布";
  return BIND_WHERE[b.where] || "去绑定";
}

function bindRowsHtml(list, mode) {
  return (list || [])
    .map((b) => {
      const btn = BIND_BTN[b.action] || "";
      const tail =
        mode === "act" && btn
          ? `<button class="btn btn-ghost btn-mini bind-go" data-bind-act="${esc(b.action)}" data-key="${esc(b.key)}">${esc(btn)}</button>`
          : `<span class="acct-bind-go">${esc(mode === "act" ? "" : bindGoLabel(b))}</span>`;
      return `<div class="acct-bind${b.ready ? " on" : ""}" data-bind="${esc(b.key)}" data-where="${esc(b.where)}" data-ready="${b.ready ? "1" : "0"}" role="button" tabindex="0">
        <span class="acct-bind-main">
          <span class="acct-bind-name">${esc(b.name)}</span>
          <span class="acct-bind-desc">${esc(b.detail || b.desc)}</span>
        </span>
        <span class="acct-bind-state${b.ready ? " ok" : ""}">${esc(b.status)}</span>
        ${tail}
      </div>`;
    })
    .join("");
}

async function loadBindings() {
  if (!pub.binds) return;
  try {
    const list = (await TopicService.Bindings()) || [];
    pub.binds.innerHTML = bindRowsHtml(list, "act");
    const missing = list.filter((b) => !b.ready).length;
    if (pub.bindNote) {
      pub.bindNote.textContent = missing
        ? `还有 ${missing} 个没连上，点右边的按钮`
        : "四个都连上了，可以直接发";
    }
    if (pub.bindHint) {
      pub.bindHint.hidden = missing === 0;
      if (missing) {
        // 说清「缺哪个」，而不是只说「没配好」——用户才知道要不要装浏览器。
        const need = list.filter((b) => !b.ready).map((b) => b.name).join("、");
        pub.bindHintText.textContent = `${need} 还没连上`;
      }
    }
    const wx = list.find((b) => b.key === "wechat");
    if (wx && pub.bindWxAppID) {
      const cfg = await TopicService.WeChatConfig().catch(() => null);
      if (cfg && !pub.bindWxAppID.value) pub.bindWxAppID.value = cfg.appId || "";
    }
    paintBrowserLine();
  } catch (e) {
    pub.binds.innerHTML = `<p class="acct-bind-empty">读不到绑定状态：${esc(humanError(e))}</p>`;
  }
}

/* 绑定区底部一行小字：说明「浏览器不用你自己装」。
   以前这块是空白，用户以为要先装 Chrome 才能扫码，就直接放弃了。
   有内置就报版本（证明真的带着），没有就说一句实话并给出下一步。 */
async function paintBrowserLine() {
  if (!pub.bindBrowser) return;
  let info = null;
  try {
    info = await TopicService.BrowserInfo();
  } catch {
    info = null;
  }
  const line = pub.bindBrowser;
  if (!info || !info.ready) {
    line.hidden = false;
    line.className = "bind-browser bad";
    line.textContent = "内置浏览器没找到，扫码登录用不了：重新解压或重装一次应用。也可以先用「复制全部」手动发。";
    return;
  }
  line.hidden = false;
  line.className = "bind-browser";
  line.textContent = info.bundled
    ? `自带浏览器 ${info.version || ""}，不用你自己装 Chrome`.trim()
    : "用你本机装的 Chrome 打开登录页";
}

function openFillForm(id) {
  const row = pub.records.querySelector(`[data-rec="${id}"]`);
  if (!row || row.querySelector(".rec-form")) return;
  const r = (pubBoard?.records || []).find((x) => x.id === id);
  if (!r) return;
  const val = (n) => (n >= 0 ? String(n) : "");
  const form = document.createElement("div");
  form.className = "rec-form";
  form.innerHTML = `
    <label>播放</label><input type="number" min="0" data-m="views" value="${val(r.views)}">
    <label>赞</label><input type="number" min="0" data-m="likes" value="${val(r.likes)}">
    <label>藏</label><input type="number" min="0" data-m="collects" value="${val(r.collects)}">
    <label>评</label><input type="number" min="0" data-m="comments" value="${val(r.comments)}">
    <button class="btn btn-primary btn-mini" data-act="save-metrics" data-id="${esc(id)}">存</button>
    <button class="btn btn-ghost btn-mini" data-act="cancel-metrics">取消</button>`;
  row.querySelector(".rec-main").appendChild(form);
}

async function saveMetrics(id, form) {
  const read = (k) => {
    const v = form.querySelector(`[data-m="${k}"]`).value.trim();
    // 空着就传 -1：这是「这一项我还没看」，不是 0。
    // 混成 0 会让均值被一堆假零拉垮，而这种错看不出来。
    return v === "" ? -1 : Math.max(0, parseInt(v, 10) || 0);
  };
  try {
    await TopicService.UpdateMetrics(id, read("views"), read("likes"), read("collects"), read("comments"));
    toast("记下了");
    await loadPublish();
  } catch (e) {
    toast(humanError(e));
  }
}

pub.queue?.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-act]");
  if (!b) return;
  const act = b.dataset.act;
  if (act === "open") {
    gotoWrite(b.dataset.topic);
    return;
  }
  if (act === "copy") {
    try {
      const pack = await TopicService.CopyPack(b.dataset.draft);
      toast(`已复制 ${rlen(pack.text)} 字`);
    } catch (e) {
      toast(humanError(e));
    }
    return;
  }
  if (act === "done") {
    openPubPop({
      topicId: b.dataset.topic,
      draftId: b.dataset.draft,
      platform: b.dataset.plat,
      title: b.dataset.title,
    });
    return;
  }
  if (act === "auto") {
    openAutoPop({
      topicId: b.dataset.topic,
      draftId: b.dataset.draft,
      platform: b.dataset.plat,
      title: b.dataset.title,
    });
  }
});

pub.records?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-act]");
  if (!b) return;
  if (b.dataset.act === "fill") openFillForm(b.dataset.id);
  if (b.dataset.act === "cancel-metrics") b.closest(".rec-form")?.remove();
  if (b.dataset.act === "save-metrics") {
    saveMetrics(b.dataset.id, b.closest(".rec-form"));
  }
});

/* 发布台的绑定按钮。四个动作分得很清楚，因为它们的失败方式完全不同：
   填 Key 失败是打错字、扫码失败是没扫成、没浏览器是没有程序可驱动——
   混成一个「绑定」按钮，用户就分不清该改哪里。 */
pub.binds?.addEventListener("click", async (ev) => {
  const btn = ev.target.closest("[data-bind-act]");
  if (!btn) {
    // 点行本身：公众号行直接展开凭证表单，其余行滚到按钮上不做事。
    const row = ev.target.closest("[data-bind]");
    if (row?.dataset.bind === "wechat") openWxForm();
    return;
  }
  const act = btn.dataset.bindAct;
  const key = btn.dataset.key;

  if (act === "setup-ai") return goToPanel("settings", "aiPanel");
  if (act === "wechat-config" || act === "wechat-scan") return openWxForm();
  if (act === "install-browser") {
    // 浏览器是随包发的，走到这里等于包里那份不见了。
    // 让用户去装 Chrome 是错的建议——装了也不是我们保证得了的那一份。
    toast("内置浏览器不见了：重新解压或重装一次应用就能修好");
    return;
  }

  // 点「扫码登录」前先问一次状态。已经登录的人点了这个按钮，
  // 窗口里会直接是他自己的创作者首页——他会以为程序没认出他来，
  // 于是又扫一次。「重新登录」是明确要换号，所以那条不拦。
  if (act === "login") {
    const list = await TopicService.Bindings().catch(() => null);
    const row = list?.find((b) => b.key === key);
    if (row?.ready) {
      toast(`${row.name}已经登录了，不用再扫一次`);
      await loadBindings();
      return;
    }
  }

  // 扫码登录：浏览器窗口会停在该平台登录页。用户扫完就把手机放下、
  // 继续看别的了，不会主动回来点「重新检测」——以前界面就一直停在
  // 「未登录」，他以为没登录成功，于是又扫一次。现在打开窗口后自己盯着。
  btn.disabled = true;
  const old = btn.textContent;
  btn.textContent = "打开中…";
  try {
    // 页面从右边滑出来，不再是另一个浏览器窗口。
    const url = await TopicService.LiveLoginURL(key).catch(() => "");
    await openLive(key, { url });
    toast("页面在右边，用手机扫一下；扫完这儿会自动变成已连接");
    startLoginWatch(key);
  } catch (e) {
    toast(humanError(e));
  } finally {
    btn.disabled = false;
    btn.textContent = old;
  }
});

/* 扫码之后盯一段时间，登录一成功就把界面改过来。
   每 2 秒问一次后端：cookie 落在 profile 里就算登录了。
   最多盯 3 分钟——用户可能去泡了杯茶，不该无限轮询。 */
let loginWatch = null; // { key, timer, started }

function stopLoginWatch() {
  if (loginWatch?.timer) clearTimeout(loginWatch.timer);
  loginWatch = null;
}

function startLoginWatch(key) {
  stopLoginWatch();
  loginWatch = { key, timer: null, started: Date.now() };
  const tick = async () => {
    if (!loginWatch || loginWatch.key !== key) return;
    const list = await TopicService.Bindings().catch(() => null);
    const row = list?.find((b) => b.key === key);
    if (row?.ready) {
      const name = row.name;
      stopLoginWatch();
      await loadBindings();
      await refreshAutoStatus();
      // 登录成功后页面通常停在登录页。右边那一屏既然开着，就直接带到发布页——
      // 否则用户扫完码看到的还是那张登录表单，会以为没生效。
      if (live.platform === key) {
        const url = await TopicService.LivePublishURL(key).catch(() => "");
        if (url) TopicService.LiveNavigate(key, url).catch(() => {});
      }
      toast(`${name}登录好了，可以直接发`);
      return;
    }
    if (Date.now() - loginWatch.started > 3 * 60 * 1000) {
      stopLoginWatch();
      toast("还没检测到登录。扫完码可以点「重新检测」");
      return;
    }
    loginWatch.timer = setTimeout(tick, 2000);
  };
  loginWatch.timer = setTimeout(tick, 2000);
  paintLoginWaiting(key);
}

/* 盯着的这段时间，把那一行按钮改成「等待扫码…」。
   不改的话用户会以为按钮没反应，反复点。 */
function paintLoginWaiting(key) {
  const row = pub.binds?.querySelector(`[data-bind="${key}"]`);
  const btn = row?.querySelector("[data-bind-act]");
  if (!btn) return;
  btn.disabled = true;
  btn.textContent = "等待扫码…";
}

function openWxForm() {
  if (!pub.bindWx) return;
  pub.bindWx.hidden = false;
  pub.bindWxOut.textContent = "";
  // 一打开就去开后台：用户点「扫码绑定」的意思就是「我不想自己找 AppID」，
  // 再让他点第二个按钮才算开始，等于把麻烦还给他。
  startWxScan();
}

/* 公众号扫码：开着轮询看「登录了没、AppID 读到没」。
   和登录小红色那套不同，这里没有"扫码成功"这个瞬间可监听——
   微信登录后是整页跳转，只有轮询页面状态才看得见。 */
let wxScanTimer = 0;
// 轮询次数：只为了一件事——登录了却读不到 AppID 时别一直转。
let wxScanTries = 0;

async function startWxScan() {
  if (wxScanTimer) return; // 已经在跑了，别叠一个
  wxScanTries = 0;
  pub.bindWxState.textContent = "正在打开后台…";
  try {
    const st = await TopicService.WeChatScanStart();
    paintWxScan(st);
    await openLive("wechat");
  } catch (e) {
    pub.bindWxState.textContent = humanError(e);
    return;
  }
  wxScanTimer = setInterval(async () => {
    wxScanTries++;
    try {
      const st = await TopicService.WeChatScanStatus();
      paintWxScan(st);
      if (st.appId) {
        // AppID 只在用户没填过时自动填：填过的可能正手改，
        // 这时候覆盖他等于「我帮你改了但我没说」。
        if (pub.bindWxAppID && !pub.bindWxAppID.value.trim()) {
          pub.bindWxAppID.value = st.appId;
        }
        stopWxScan();
        pub.bindWxState.textContent = "AppID 已读到，填好下面的 AppSecret 就能用";
        return;
      }
      // 登录了却一直读不到 AppID：多半是这个号没认证、进不去「基本配置」。
      // 别无限轮询，把话说清楚，让人自己去复制。
      if (st.authed && wxScanTries > 10) stopWxScan();
    } catch {
      /* 轮询失败不弹错：后台页面加载慢或浏览器刚起来，下一轮就好了 */
    }
  }, 2000);
}

function stopWxScan() {
  clearInterval(wxScanTimer);
  wxScanTimer = 0;
  wxScanTries = 0;
}

function paintWxScan(st) {
  if (!pub.bindWxState) return;
  pub.bindWxState.textContent = st.note || (st.authed ? "已登录" : "等扫码中…");
  pub.bindWxState.classList.toggle("on", !!st.authed);
  if (st.egress && pub.bindWxIp) {
    pub.bindWxIp.hidden = false;
    pub.bindWxIp.innerHTML = `本机出口 IP：<b>${esc(st.egress)}</b>　如果以后接口报 40164，就到后台「IP 白名单」里加上它`;
  }
}

pub.bindWxScan?.addEventListener("click", () => {
  // 手动再点一次 = 重新打开后台（用户可能关掉过右侧面板）
  stopWxScan();
  startWxScan();
});

pub.bindWxCancel?.addEventListener("click", () => {
  pub.bindWx.hidden = true;
  stopWxScan();
});

pub.bindWxSave?.addEventListener("click", async () => {
  pub.bindWxSave.disabled = true;
  pub.bindWxOut.textContent = "保存中…";
  try {
    await TopicService.SaveWeChatConfig(pub.bindWxAppID.value.trim(), pub.bindWxSecret.value.trim());
    pub.bindWxSecret.value = "";
    stopWxScan();
    pub.bindWxState.textContent = "已保存";
    pub.bindWxOut.textContent = "已保存。点「测试连接」确认凭证和白名单都对。";
    await loadBindings();
  } catch (e) {
    pub.bindWxOut.textContent = humanError(e);
  } finally {
    pub.bindWxSave.disabled = false;
  }
});

pub.bindWxTest?.addEventListener("click", async () => {
  pub.bindWxTest.disabled = true;
  pub.bindWxOut.textContent = "正在问微信…";
  try {
    pub.bindWxOut.textContent = (await TopicService.TestWeChat()) || "通了";
    await loadBindings();
  } catch (e) {
    // 40164 这类错误的后半句是「把 IP 加进白名单」，原样给用户看最有用于是
    pub.bindWxOut.textContent = humanError(e);
  } finally {
    pub.bindWxTest.disabled = false;
  }
});

pub.bindRecheck?.addEventListener("click", async () => {
  pub.bindRecheck.disabled = true;
  await loadBindings();
  pub.bindRecheck.disabled = false;
  toast("重新检测过了");
});

pub.bindHint?.addEventListener("click", () => goToPanel("publish", "bindPanel"));

function gotoWrite(topicId) {
  write.pendingTopic = topicId || "";
  switchView("write");
}

/* ============================================================
   发布确认浮层（写稿页与发布台共用）
   ============================================================ */

const pp = {
  root: $("pubPop"),
  topic: $("pubPopTopic"),
  plat: $("pubPopPlat"),
  account: $("pubPopAccount"),
  url: $("pubPopUrl"),
  date: $("pubPopDate"),
  ok: $("pubPopOk"),
  cancel: $("pubPopCancel"),
};

const ppForm = { topicId: "", draftId: "", platform: "", title: "" };

async function openPubPop(o) {
  // 平台列表原本只在成稿页拉过；这个浮层两边共用，缺了就自己补一次。
  // 不补的话从发布台点进来时平台那排是空的，用户会以为没得选。
  if (!(write.platforms || []).length) {
    try {
      write.platforms = (await TopicService.Platforms()) || [];
    } catch {
      write.platforms = [];
    }
  }
  ppForm.topicId = o.topicId || "";
  ppForm.draftId = o.draftId || "";
  ppForm.title = o.title || "";
  ppForm.platform = o.platform || write.platform || "xiaohongshu";
  ppForm.onDone = o.onDone || null;

  pp.topic.textContent = o.title || "";
  renderPopPlats();
  pp.date.value = todayStr();
  pp.url.value = "";

  // 账号是选填的便利项：有记过就填上，省得每次手打。
  if (!Object.keys(pubAccountsCache).length) {
    try {
      pubAccountsCache = (await TopicService.Accounts()) || {};
    } catch { /* 读不到不影响记账 */ }
  }
  pp.account.value = pubAccountsCache[ppForm.platform] || "";

  pp.root.hidden = false;
  pp.account.focus();
}

function renderPopPlats() {
  pp.plat.innerHTML = (write.platforms || [])
    .map((p) => `<button class="plat-pick${p.key === ppForm.platform ? " on" : ""}" data-plat="${esc(p.key)}">${esc(p.name)}</button>`)
    .join("");
}

pp.plat?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-plat]");
  if (!b) return;
  ppForm.platform = b.dataset.plat;
  renderPopPlats();
  pp.account.value = pubAccountsCache[ppForm.platform] || "";
});

pp.cancel?.addEventListener("click", () => { pp.root.hidden = true; });
pp.root?.addEventListener("click", (ev) => {
  // 点遮罩关闭，点卡片不关。判据是「点到的就是遮罩本身」。
  if (ev.target === pp.root) pp.root.hidden = true;
});

pp.ok?.addEventListener("click", async () => {
  pp.ok.disabled = true;
  try {
    await TopicService.MarkPublished({
      topicId: ppForm.topicId,
      draftId: ppForm.draftId,
      platform: ppForm.platform,
      account: pp.account.value.trim(),
      url: pp.url.value.trim(),
      date: pp.date.value,
      note: "",
    });
    pp.root.hidden = true;
    toast("记下了。数据过两天回来填");
    if (ppForm.onDone) {
      const fn = ppForm.onDone;
      ppForm.onDone = null;
      try {
        await fn();
      } catch (e) {
        toast(humanError(e));
      }
    }
    if (write.draft && write.draft.topicId === ppForm.topicId) {
      // 这条已经被发掉了，留在写作台上没意义，回到列表。
      setDraft(null);
      write.draft = null;
      await loadWrite();
    }
    if (state.view === "publish") await loadPublish();
  } catch (e) {
    toast(humanError(e));
  } finally {
    pp.ok.disabled = false;
  }
});


/* ============================================================
   自动发布面板
   ------------------------------------------------------------
   替用户干的是「开页面、传图、填标题正文」这些机械活；
   最后那个发布按钮默认留给他自己按——那一步不可撤回，
   平台对无人值守的连续发布也最敏感。
   ============================================================ */

const ap = {
  root: $("autoPop"),  topic: $("autoTopic"),
  plats: $("autoPlats"),
  wx: $("autoWx"),
  wxAppID: $("autoWxAppID"),
  wxSecret: $("autoWxSecret"),
  wxTest: $("autoWxTest"),
  wxSave: $("autoWxSave"),
  wxNote: $("autoWxNote"),
  mode: $("autoMode"),
  media: $("autoMedia"),
  mediaLabel: $("autoMediaLabel"),
  mediaNote: $("autoMediaNote"),
  pick: $("autoPick"),
  cover: $("autoCover"),
  thumbs: $("autoThumbs"),
  warn: $("autoWarn"),
  submit: $("autoSubmit"),
  logs: $("autoLogs"),
  close: $("autoClose"),
  mark: $("autoMark"),
  run: $("autoRun"),
};

/* 选稿弹窗。发布台顶部「发一篇」先问「发哪一篇」，
   因为自动发布必须落在一份具体的稿上（标题、正文、话题都从稿里取）。 */
const pick = {
  root: $("pickPop"),
  list: $("pickList"),
  cancel: $("pickCancel"),
  compose: $("pubCompose"),
};

async function openPickPop() {
  pick.root.hidden = false;
  pick.list.innerHTML = `<p class="pick-empty">正在读稿…</p>`;
  let list = [];
  try {
    list = (await TopicService.RecentDrafts(30)) || [];
  } catch (e) {
    pick.list.innerHTML = `<p class="pick-empty">读不到稿子：${esc(humanError(e))}</p>`;
    return;
  }
  if (!list.length) {
    pick.list.innerHTML = `<p class="pick-empty">还没有成稿。先去「成稿」页写一篇，回来就能发。</p>`;
    return;
  }
  pick.list.innerHTML = list
    .map((d) => {
      const title = d.title || d.topicTitle || "（无标题）";
      const sub = [d.platformName || "", d.formatLabel || "", d.createdAt || ""]
        .filter(Boolean)
        .join(" · ");
      return `<div class="pick-row" data-draft="${esc(d.id)}" data-topic="${esc(d.topicId)}" data-title="${esc(title)}" data-plat="${esc(d.platform || "")}">
        <span class="pick-main">
          <span class="pick-title">${esc(title)}</span>
          <span class="pick-sub">${esc(sub)}</span>
        </span>
        <span class="pick-go">发 →</span>
      </div>`;
    })
    .join("");
}

pick.compose?.addEventListener("click", openPickPop);
pick.cancel?.addEventListener("click", () => {
  pick.root.hidden = true;
});
pick.root?.addEventListener("click", (ev) => {
  if (ev.target === pick.root) pick.root.hidden = true;
  const row = ev.target.closest("[data-draft]");
  if (!row) return;
  pick.root.hidden = true;
  openAutoPop({
    topicId: row.dataset.topic,
    draftId: row.dataset.draft,
    title: row.dataset.title,
    // 稿子上标的平台就是它适合发的平台，带过去省一次选择。
    platform: row.dataset.plat,
  });
});

const auto = {
  topicId: "",
  draftId: "",
  title: "",
  platform: "",
  images: [],
  video: "",
  mode: "news",
  status: null,
  timer: 0,
  running: false,
  // 生成的封面单独记一份路径：它在 images 里也有一项，
  // 界面要标出「这张是封面」，不然用户分不清哪张是自己选的、哪张是画的。
  genCover: "",
  thumbKey: "",
  thumbs: [],
};

async function openAutoPop(o) {
  auto.topicId = o.topicId || "";
  auto.draftId = o.draftId || "";
  auto.title = o.title || "";
  auto.platform = o.platform || "";
  auto.images = [];
  auto.video = "";
  auto.running = false;
  auto.status = null;
  auto.genCover = "";
  auto.thumbKey = "";
  auto.thumbs = [];

  ap.topic.textContent = auto.title;
  ap.logs.hidden = true;
  ap.logs.innerHTML = "";
  ap.warn.hidden = true;
  ap.mark.hidden = true;
  ap.run.disabled = true;
  ap.run.textContent = "开始发布";
  ap.root.hidden = false;

  await refreshAutoStatus();
}

function showAutoWarn(msg) {
  ap.warn.textContent = msg || "";
  ap.warn.hidden = !msg;
}

async function refreshAutoStatus() {
  try {
    auto.status = await TopicService.PublishStatus();
  } catch (e) {
    auto.status = null;
    showAutoWarn(humanError(e));
    return;
  }
  const st = auto.status;
  if (!st?.browserReady) {
    showAutoWarn(st?.reason || "本机找不到 Chrome 或 Edge。装上一个就能自动发布。");
  } else if (!auto.platform) {
    const first = (st.platforms || [])[0];
    auto.platform = first ? first.key : "";
  } else {
    showAutoWarn("");
  }
  renderAutoPlats();
  renderAutoMedia();
  syncAutoRun();
}

// syncAutoRun 让「开始发布」的可用性和平台真实状态一致。
// 没登录/没填凭证时让它可点，点下去只会得到一句报错——
// 那不如直接把按钮按住，并说清楚缺什么。
function syncAutoRun() {
  if (auto.running) return;
  const p = autoPlatOf(auto.platform);
  const blocked = !!p && !p.ready;
  // 公众号走接口，本机有没有浏览器都无所谓，别拿这条拦它。
  const needsBrowser = !!p && p.kind !== "api";
  ap.run.disabled = blocked || (needsBrowser && !auto.status?.browserReady);
  if (blocked) {
    showAutoWarn(
      p.kind === "api"
        ? "公众号还没填 AppID / AppSecret，点上面那张卡片里的「填 AppID」。"
        : `${p.name}还没登录，点一下平台卡片里的「扫码登录」。`
    );
  }
}

function autoPlatOf(key) {
  return (auto.status?.platforms || []).find((p) => p.key === key) || null;
}

function renderAutoPlats() {
  const list = auto.status?.platforms || [];
  if (!list.length) {
    ap.plats.innerHTML = `<p class="auto-empty">还没有可发布的平台</p>`;
    return;
  }
  // 没浏览器时给「扫码登录」是骗人的：点下去只会说找不到浏览器。
  // 这里和账号页用同一个判断，出口改成「去装」。
  const browserOK = auto.status?.browserReady !== false;
  ap.plats.innerHTML = list
    .map((p) => {
      const on = p.key === auto.platform ? " on" : "";
      let state;
      if (p.ready) {
        state = `<span class="auto-login ok">${p.kind === "api" ? "已配置" : "已登录"}</span>`;
      } else if (p.kind === "api") {
        state = `<span class="auto-login">没填凭证</span>
             <button class="link-btn" data-wxcfg="1">填 AppID</button>`;
      } else if (!browserOK) {
        state = `<span class="auto-login">缺浏览器</span>
             <button class="link-btn" data-gobind="1">去装</button>`;
      } else {
        state = `<span class="auto-login">未登录</span>
             <button class="link-btn" data-login="${esc(p.key)}">扫码登录</button>`;
      }
      return `<div class="auto-plat${on}" data-plat="${esc(p.key)}">
        <div class="auto-plat-top">
          <b>${esc(p.name)}</b>
          <span class="auto-kind">${p.kind === "api" ? "接口" : p.media === "video" ? "视频" : "图文"}</span>
        </div>
        <div class="auto-plat-sub">${esc(p.note || "")}</div>
        <div class="auto-plat-state">${state}</div>
      </div>`;
    })
    .join("");
}

function renderAutoMedia() {
  const p = autoPlatOf(auto.platform);
  if (!p || !p.media) {
    ap.media.hidden = true;
    ap.thumbs.hidden = true;
    return;
  }
  ap.media.hidden = false;
  // 公众号分两种形态，发之前必须让用户选清楚：
  // 图文消息是文章，图片消息（贴图）是小绿书那一种，排版完全不同。
  const hasModes = (p.modes || []).length > 0;
  ap.mode.hidden = !hasModes;
  if (hasModes && !(p.modes || []).includes(auto.mode)) auto.mode = p.modes[0];
  for (const b of ap.mode.querySelectorAll("[data-mode]")) {
    b.classList.toggle("on", b.dataset.mode === auto.mode);
  }
  const isVideo = p.media === "video";
  ap.mediaLabel.textContent = isVideo ? "视频" : "图片";
  ap.pick.textContent = isVideo ? "选视频" : "从电脑选";

  // 封面只对图文有意义：视频的封面要在剪辑里出，程序画一张图也替换不了。
  ap.cover.hidden = isVideo;
  ap.cover.textContent = auto.genCover ? "重画封面" : "生成封面";
  // 没标题就画不出封面，按钮先按住并说清楚，别让人点了才吃一个报错。
  ap.cover.disabled = auto.running || !auto.title.trim();
  ap.cover.title = auto.title.trim() ? "按这一稿的标题画一张封面，放第一张" : "先在稿子上写个标题，才能生成封面";

  const n = isVideo ? (auto.video ? 1 : 0) : auto.images.length;
  const max = p.maxImages || 0;

  // 每种形态的规矩不一样，得分开说：
  // 公众号图文是「第一张当封面、后面的插正文」，贴图是「全进图片列表、首图当封面」，
  // 小红书只说张数。说混了用户会以为自己选错图了。
  let ask;
  if (isVideo) ask = `${p.name}要发视频`;
  else if (auto.platform === "wechat" && auto.mode === "newspic")
    ask = `最多 ${max} 张，第一张当封面`;
  else if (auto.platform === "wechat") ask = `第一张当封面，其余插进正文，最多 ${max} 张`;
  else ask = `最多 ${max} 张`;

  ap.mediaNote.textContent = n
    ? isVideo
      ? auto.video.split(/[\\/]/).pop()
      : `已选 ${n} 张` + (max && n >= max ? "（已达上限）" : "")
    : "还没选，" + ask;

  renderAutoThumbs(p);
}

/* 把选好的图显示出来。
   「已选 2 张」这句话等于没说：用户既看不出选的是哪两张，
   也看不出顺序，而第一张就是封面——发出去之前必须能核对。 */
async function renderAutoThumbs(p) {
  if (!ap.thumbs) return;
  const isVideo = p?.media === "video";
  const files = isVideo ? (auto.video ? [auto.video] : []) : auto.images;
  if (!files.length) {
    ap.thumbs.hidden = true;
    ap.thumbs.innerHTML = "";
    return;
  }
  ap.thumbs.hidden = false;

  if (!isVideo) {
    const key = files.join("|");
    if (auto.thumbKey !== key) {
      let srcs = [];
      try {
        srcs = (await TopicService.PreviewImages(files)) || [];
      } catch {
        // 读不出预览不该拦住发布：这里只是"看清楚"，不是必需品。
        srcs = [];
      }
      // 期间用户可能又改了选图，那就作废这次结果，别把旧图贴上去。
      if (auto.images.join("|") === key) {
        auto.thumbs = srcs;
        auto.thumbKey = key;
      }
    }
  }

  ap.thumbs.innerHTML = files
    .map((f, i) => {
      const gen = f === auto.genCover;
      const src = isVideo ? "" : auto.thumbs[i] || "";
      const name = String(f).split(/[\\/]/).pop();
      const label = gen ? "封面" : name;
      const pic = src
        ? `<img src="${src}" alt="">`
        : `<span class="auto-thumb-x">${isVideo ? "▶" : "—"}</span>`;
      return `<figure class="auto-thumb${gen ? " gen" : ""}" title="${esc(f)}">
        ${pic}
        <figcaption>${esc(label)}</figcaption>
      </figure>`;
    })
    .join("");
}

ap.mode?.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-mode]");
  if (!b) return;
  auto.mode = b.dataset.mode;
  for (const x of ap.mode.querySelectorAll("[data-mode]")) {
    x.classList.toggle("on", x.dataset.mode === auto.mode);
  }
  // 形态变了，素材提示也要跟着变（贴图要图，图文要封面）。
  renderAutoMedia();
});

ap.wxSave?.addEventListener("click", async () => {
  ap.wxSave.disabled = true;
  try {
    await TopicService.SaveWeChatConfig(ap.wxAppID.value.trim(), ap.wxSecret.value.trim());
    ap.wxSecret.value = "";
    ap.wxNote.textContent = "已保存。点「测试连接」确认凭证和白名单都对。";
    await refreshAutoStatus();
    ap.wx.hidden = false;
  } catch (e) {
    ap.wxNote.textContent = humanError(e);
  } finally {
    ap.wxSave.disabled = false;
  }
});

ap.wxTest?.addEventListener("click", async () => {
  ap.wxTest.disabled = true;
  ap.wxNote.textContent = "正在调接口…";
  try {
    const msg = await TopicService.TestWeChat();
    ap.wxNote.textContent = msg || "连上了";
    await refreshAutoStatus();
  } catch (e) {
    // 白名单错的报错里带着要加的 IP，原样显示，别包一层"操作失败"。
    ap.wxNote.textContent = humanError(e);
  } finally {
    ap.wxTest.disabled = false;
  }
});

ap.plats?.addEventListener("click", async (ev) => {
  const cfg = ev.target.closest("[data-wxcfg]");
  if (cfg) {
    try {
      const v = await TopicService.WeChatConfig();
      ap.wxAppID.value = v?.appId || "";
      ap.wxSecret.value = "";
      ap.wxNote.textContent = v?.hasSecret
        ? "AppSecret 已存过，留空表示不改。"
        : "从公众号后台复制 AppID 和 AppSecret 填入。";
      ap.wx.hidden = false;
      ap.wxAppID.focus();
    } catch (e) {
      showAutoWarn(humanError(e));
    }
    return;
  }

  const login = ev.target.closest("[data-login]");
  if (login) {
    // 扫码登录：页面从右边滑出来，扫完自动认出来。
    const key = login.dataset.login;
    try {
      const url = await TopicService.LiveLoginURL(key).catch(() => "");
      await openLive(key, { url });
      startLoginWatch(key);
      showAutoWarn(`在右边那一屏扫码登录。登录成功后会自动跳到发布页，不用回来点「重新检测」。`);
    } catch (e) {
      showAutoWarn(humanError(e));
    }
    return;
  }
  if (ev.target.closest("[data-gobind]")) {
    // 缺浏览器：这个弹层里没有装浏览器的入口，直接送到发布台的绑定区。
    ap.root.hidden = true;
    goToPanel("publish", "bindPanel");
    return;
  }
  const card = ev.target.closest("[data-plat]");
  if (!card) return;
  auto.platform = card.dataset.plat;
  auto.images = [];
  auto.video = "";
  ap.wx.hidden = true;
  // 点卡片＝刷新一下这个平台的登录态：用户刚扫完码，这一下就是「我登好了」。
  await refreshAutoStatus();
});

ap.pick?.addEventListener("click", async () => {
  try {
    const files = await TopicService.PickPublishFiles(auto.platform);
    if (!files || !files.length) return;
    if (autoPlatOf(auto.platform)?.media === "video") {
      auto.video = files[0];
    } else {
      auto.images = files.slice(0, 9);
    }
    renderAutoMedia();
  } catch (e) {
    toast(humanError(e));
  }
});

/* 生成封面：用这一稿自己的标题画。
   封面永远放第一张——小红书和公众号图文都是拿第一张当封面，
   排在后面等于没生成。 */
ap.cover?.addEventListener("click", async () => {
  const p = autoPlatOf(auto.platform);
  if (!p || auto.running) return;
  const old = ap.cover.textContent;
  ap.cover.disabled = true;
  ap.cover.textContent = "正在画…";
  try {
    const v = await TopicService.GenerateCover(auto.title, auto.platform, `${p.name} · 图文`);
    auto.genCover = v.path;
    const max = p.maxImages || 9;
    auto.images = [v.path, ...auto.images.filter((x) => x !== v.path)].slice(0, max);
    renderAutoMedia();
    toast(`封面已生成（${v.w}×${v.h}），放在第一张`);
  } catch (e) {
    toast(humanError(e));
  } finally {
    ap.cover.textContent = auto.genCover ? "重画封面" : old;
    ap.cover.disabled = auto.running || !auto.title.trim();
  }
});

/* manual 之后的那一笔。
   浏览器停在发布页等用户按最后一下，程序不知道他到底按没按，
   所以不能自动记账——但可以让他按完顺手就在这里记掉。 */
ap.mark?.addEventListener("click", () => markRecord(ap.mark));

// markRecord 补一笔台账。
//
// 抽出来是因为它能从两个地方点：发布弹窗里、以及右边那一屏的进度条上。
// 用户在网页里按了发布，视线就在那儿，让他为了记一笔再回到弹窗是多余的。
async function markRecord(btn) {
  if (!auto.topicId) return toast("这张稿没关联选题，记不了账");
  const old = btn.textContent;
  btn.disabled = true;
  btn.textContent = "记录中…";
  try {
    const job = auto.status?.job || {};
    await TopicService.MarkPublished({
      topicId: auto.topicId,
      draftId: auto.draftId,
      platform: auto.platform,
      url: job.url || "",
      account: "",
      date: "",
      note: "",
    });
    btn.hidden = true;
    $("liveMark").hidden = true;
    toast("已记进台账");
    if (state.view === "publish") await loadPublish();
  } catch (e) {
    btn.disabled = false;
    btn.textContent = old;
    toast(humanError(e));
  }
}

ap.close?.addEventListener("click", () => {
  if (auto.running) {
    toast("发布还在跑，等它结束再关");
    return;
  }
  ap.root.hidden = true;
});
ap.root?.addEventListener("click", (ev) => {
  if (ev.target === ap.root && !auto.running) ap.root.hidden = true;
});

ap.run?.addEventListener("click", async () => {
  if (auto.running) return;
  const p = autoPlatOf(auto.platform);
  if (!p) return showAutoWarn("先选一个平台");
  if (p.media === "images" && !auto.images.length) {
    return showAutoWarn("小红书要发图文，先选 1-9 张图");
  }
  if (p.media === "video" && !auto.video) {
    return showAutoWarn("抖音要发视频，先选一个视频文件");
  }

  auto.running = true;
  ap.run.disabled = true;
  ap.run.textContent = "发布中…";
  // 跑起来之后素材就锁死了：这时候再换图或重画封面，
  // 页面里已经传上去的东西不会跟着变，改了只会让人以为改成功了。
  ap.pick.disabled = true;
  ap.cover.disabled = true;
  ap.logs.hidden = false;
  ap.logs.innerHTML = `<div class="auto-log">正在启动…</div>`;
  showAutoWarn("");

  // 公众号走接口、没有网页可看，所以只有浏览器平台才把页面摆出来。
  // 摆出来的这一刻就是「原来它是在我自己的软件里发」——比任何文案都管用。
  if (p.kind !== "api") {
    const url = await TopicService.LivePublishURL(auto.platform).catch(() => "");
    try {
      await openLive(auto.platform, { url });
      paintLiveStatus("正在准备发布…", "");
    } catch (e) {
      auto.running = false;
      ap.run.disabled = false;
      ap.run.textContent = "开始发布";
      ap.pick.disabled = false;
      renderAutoMedia();
      return showAutoWarn("平台页面没打开：" + humanError(e));
    }
  }

  try {
    await TopicService.StartPublish({
      topicId: auto.topicId,
      draftId: auto.draftId,
      platform: auto.platform,
      images: auto.images,
      video: auto.video,
      mode: auto.mode,
      autoSubmit: !!ap.submit.checked,
    });
  } catch (e) {
    auto.running = false;
    ap.run.disabled = false;
    ap.run.textContent = "开始发布";
    ap.pick.disabled = false;
    renderAutoMedia();
    paintLiveStatus("", "");
    showAutoWarn(humanError(e));
    return;
  }

  // 页面已经在右边动起来了，这个弹窗就该收起来——两样同时占屏，
  // 用户既看不清网页，也读不清日志。结果和「记一笔」都搬到那条进度上。
  if (p.kind !== "api") {
    ap.root.hidden = true;
  }
  startAutoPoll();
});

/* 轮询而不是等事件：发布要几十秒，中间要不停把「现在到哪一步」写出来。
   轮询的另一个好处是关掉面板也不会漏掉结果——状态在 Go 那边。 */
function startAutoPoll() {
  stopAutoPoll();
  auto.timer = setInterval(async () => {
    let st = null;
    try {
      st = await TopicService.PublishStatus();
    } catch {
      return;
    }
    auto.status = st;
    const job = st?.job;
    if (job?.logs?.length) {
      ap.logs.hidden = false;
      ap.logs.innerHTML = job.logs
        .map((l) => `<div class="auto-log">${esc(l)}</div>`)
        .join("");
      ap.logs.scrollTop = ap.logs.scrollHeight;
      // 右边那一屏开着时，滚动的最后一行就是它的进度。
      // 不用另外造一套进度文案：日志里写的本来就是「正在传第 3 张图」这种实话。
      if (job.running && live.platform === job.platform) {
        paintLiveStatus(job.logs[job.logs.length - 1].replace(/^\d{2}:\d{2}:\d{2}\s+/, ""), "");
      }
    }
    if (!job || job.running) return;

    stopAutoPoll();
    auto.running = false;
    ap.run.disabled = false;
    ap.pick.disabled = false;
    // 把素材区解锁回来（cover 按钮的可用态也在里面算）。
    renderAutoMedia();
    // 交接给用户（manual）也属于「这一轮跑完了」，再点一次是重新发，
    // 不是重试同一个动作——写成「重试」会让人以为还能接着上次。
    ap.run.textContent = job.done && (job.ok || job.manual) ? "再发一次" : "重试";

    if (job.ok) {
      paintLiveStatus("发布完成，已记进台账", "ok");
      toast("发布完成，已记进台账");
      showAutoWarn("");
      if (state.view === "publish") await loadPublish();
    } else if (job.manual) {
      // 公众号没有窗口可留，话术必须分开。同一句话套两个平台，
      // 用户会真的去找一个不存在的浏览器窗口。
      const tail =
        job.platform === "wechat"
          ? "到公众号后台 → 内容管理 → 草稿箱看一眼，发了就在这儿点「已发了，记一笔」。"
          : "右边那一屏就停在发布页，补完点发布，再点上面的「已发了，记一笔」。";
      paintLiveStatus(`${job.message || "需要你接手"}。${tail}`, "warn");
      showAutoWarn(`${job.message || "需要你接手"}。${tail}`);
      // 该补的账就在结果旁边补，别让用户为了记一笔再走一遍发布台。
      ap.mark.hidden = false;
      ap.mark.disabled = false;
      ap.mark.textContent = "已发了，记一笔";
      $("liveMark").hidden = false;
    } else {
      paintLiveStatus(job.message || "这次没发成", "warn");
      showAutoWarn(job.message || "这次没发成");
    }
  }, 700);
}

function stopAutoPoll() {
  if (auto.timer) {
    clearInterval(auto.timer);
    auto.timer = 0;
  }
}

/* ---------- 素材库 ----------
   这一页的定位是「存别人写得好的，写的时候翻」。所以它不做状态机、不算分，
   只做三件事：存下来、把值得学的地方指出来、能挂到某个选题上当参考。
   和选题池刻意分开：混在一起收，选题池会被学素材灌满，筛选就失效了。 */

const MAT_PLATS = [
  { id: "", label: "全部" },
  { id: "xiaohongshu", label: "小红书" },
  { id: "wechat", label: "公众号" },
  { id: "bilibili", label: "B站" },
  { id: "douyin", label: "抖音" },
  { id: "x", label: "X" },
  { id: "producthunt", label: "Product Hunt" },
  { id: "other", label: "其他" },
];

async function loadMaterial(plat) {
  state.matPlat = plat || "";
  try {
    const [list, stat] = await Promise.all([
      TopicService.Materials(state.matPlat),
      TopicService.MaterialStats(),
    ]);
    state.mat = list || [];
    state.matStat = stat || null;
    paintMatStats();
    paintMatSeg();
    paintMatList();
  } catch (e) {
    toast(humanError(e));
  }
}

function paintMatStats() {
  const st = state.matStat || { total: 0, torn: 0, raw: 0, linked: 0 };
  // 复用首页那套 stat-card：同一套视觉语言，用户不需要认两套数字卡。
  const card = (label, num, note) =>
    `<div class="stat-card"><div class="stat-label">${esc(label)}</div>
      <div class="stat-num">${num} <i>${esc(note)}</i></div></div>`;
  el.matStats.innerHTML = [
    card("收藏素材", st.total ?? 0, st.total ? "本地存着" : "还没有"),
    card("已提炼", st.torn ?? 0, st.torn ? "有摘要和学习点" : "粘正文就能提"),
    card("待提炼", st.raw ?? 0, st.raw ? "补上正文再试" : "都提过了"),
    card("关联选题", st.linked ?? 0, st.linked ? "写稿时能翻到" : "还没挂"),
  ].join("");
}

function paintMatSeg() {
  const count = (state.matStat && state.matStat.platform) || {};
  el.matSeg.innerHTML = MAT_PLATS.map((p) => {
    const n = p.id ? count[p.id] || 0 : state.matStat?.total || 0;
    // 空分组不显示：七个平台里只用了两个，剩下全挂 0 只会让筛选条变成噪音。
    if (p.id && !n) return "";
    return `<button class="${state.matPlat === p.id ? "on" : ""}" data-plat="${p.id}">${p.label}${
      n ? `<b>${n}</b>` : ""
    }</button>`;
  }).join("");
  el.matSeg.querySelectorAll("button").forEach((b) =>
    b.addEventListener("click", () => loadMaterial(b.dataset.plat))
  );
}

function matCard(m) {
  const pts = (m.points || [])
    .map((p) => `<li>${esc(p)}</li>`)
    .join("");
  const head = `<div class="mat-head">
      <span class="tag plat">${esc(m.platformName || "未归类")}</span>
      ${m.author ? `<span class="mat-author">@${esc(m.author)}</span>` : ""}
      <span class="mat-age">${esc(m.ageLabel || "")}</span>
      <span class="mat-metrics">
        ${m.likes ? `<i>赞 ${esc(m.likes)}</i>` : ""}
        ${m.collects ? `<i>藏 ${esc(m.collects)}</i>` : ""}
      </span>
    </div>`;

  // 标题能点回原文：素材的价值一半在「能回看」，链接不给出入口，
  // 用户就只能凭记忆去搜，那和存在浏览器书签里没区别。
  const title = m.url
    ? `<a class="mat-title" href="${esc(m.url)}" data-open="${esc(m.url)}">${esc(m.title)}</a>`
    : `<div class="mat-title">${esc(m.title)}</div>`;

  const digest = m.summary
    ? `<p class="mat-summary">${esc(m.summary)}</p>
       ${pts ? `<ul class="mat-points">${pts}</ul>` : ""}`
    : `<p class="mat-empty">${
        m.hasBody ? "还没提炼，点左下角「提炼」" : "只有链接，把正文粘进来就能提炼摘要和学习点"
      }</p>`;

  const topic = m.topicTitle
    ? `<span class="mat-link-topic" title="${esc(m.topicTitle)}">关联：${esc(m.topicTitle)}</span>`
    : "";

  return `<article class="mat-card" data-id="${esc(m.id)}">
    ${head}
    ${title}
    <div class="mat-body">${digest}</div>
    <div class="mat-foot">
      <span class="mat-status ${m.status === "torn" ? "ok" : ""}">${esc(m.statusLabel)}</span>
      ${topic}
      <span class="mat-acts">
        ${m.url ? `<button class="btn btn-ghost btn-mini" data-mat="open">看原文</button>` : ""}
        <button class="btn btn-ghost btn-mini" data-mat="link">${m.topicTitle ? "改关联" : "关联选题"}</button>
        ${
          m.status !== "torn"
            ? `<button class="btn btn-ghost btn-mini" data-mat="tear">提炼</button>`
            : ""
        }
        <button class="btn btn-ghost btn-mini" data-mat="edit">编辑</button>
        <button class="btn btn-ghost btn-mini mat-del" data-mat="del">删</button>
      </span>
    </div>
  </article>`;
}

function paintMatList() {
  if (!state.mat.length) {
    el.matList.innerHTML = `<div class="empty">
      <p class="empty-title">这里还空着</p>
      <p class="empty-sub">看到写得好的笔记，把链接和正文粘进来，AI 帮你拆出值得学的地方</p>
    </div>`;
    return;
  }
  el.matList.innerHTML = state.mat.map(matCard).join("");
  el.matList.querySelectorAll(".btn").forEach((b) =>
    b.addEventListener("click", () => matAction(b))
  );
  // 站内打开链接：用系统浏览器，不在应用里开新窗口——
  // 这一页是「回看原文」，翻译、收藏都在浏览器里做更顺。
  el.matList.querySelectorAll("[data-open]").forEach((a) =>
    a.addEventListener("click", (ev) => {
      ev.preventDefault();
      TopicService.OpenURL(a.dataset.open).catch(() => toast("打不开这个链接"));
    })
  );
}

async function matAction(btn) {
  const card = btn.closest(".mat-card");
  const id = card?.dataset.id;
  const m = state.mat.find((x) => x.id === id);
  if (!m) return;
  const act = btn.dataset.mat;

  if (act === "del") {
    if (!confirm(`删掉「${m.title.slice(0, 20)}」？素材删了不能恢复。`)) return;
    try {
      await TopicService.DeleteMaterial(id);
      toast("已删除");
      await loadMaterial(state.matPlat);
    } catch (e) {
      toast(humanError(e));
    }
    return;
  }

  if (act === "tear") {
    btn.disabled = true;
    btn.textContent = "提炼中…";
    try {
      await TopicService.TearMaterial(id);
      toast("提炼好了");
      await loadMaterial(state.matPlat);
    } catch (e) {
      toast(humanError(e));
      btn.disabled = false;
      btn.textContent = "提炼";
    }
    return;
  }

  if (act === "edit") {
    openMatPop(m);
    return;
  }

  if (act === "link") {
    openLinkPop(m);
    return;
  }

  if (act === "open" && m.url) {
    TopicService.OpenURL(m.url).catch(() => toast("打不开这个链接"));
  }
}

function openMatPop(m) {
  state.matEditing = m ? m.id : null;
  el.matPopTitle.textContent = m ? "编辑素材" : "添加素材";
  el.matOk.textContent = m ? "保存" : "存下来并提炼";
  el.matUrl.value = m?.url || "";
  el.matAuthor.value = m?.author || "";
  el.matTitle.value = m?.title || "";
  el.matBody.value = "";
  el.matLikes.value = "";
  el.matCollects.value = "";
  // 编辑时不回填正文：正文可能很长，回填了用户一改就是整篇重发。
  // 这边只做「留空表示不改」，需要重提炼就单独走「提炼」按钮。
  el.matBody.placeholder = m ? "留空表示不修改正文" : "把笔记正文粘到这里";
  el.matPlatform.innerHTML = MAT_PLATS.filter((p) => p.id !== "")
    .map((p) => `<option value="${p.id}"${m?.platform === p.id ? " selected" : ""}>${p.label}</option>`)
    .join("");
  el.matPop.hidden = false;
  el.matUrl.focus();
}

function closeMatPop() {
  el.matPop.hidden = true;
  state.matEditing = null;
}

async function submitMat() {
  const editing = state.matEditing;
  const payload = {
    url: el.matUrl.value.trim(),
    platform: el.matPlatform.value,
    author: el.matAuthor.value.trim(),
    title: el.matTitle.value.trim(),
    body: el.matBody.value.trim(),
    likes: el.matLikes.value === "" ? -1 : Number(el.matLikes.value),
    collects: el.matCollects.value === "" ? -1 : Number(el.matCollects.value),
  };
  if (!payload.title && !payload.url) {
    toast("至少填个标题或链接");
    return;
  }
  el.matOk.disabled = true;
  el.matOk.textContent = payload.body ? "提炼中…" : "保存中…";
  try {
    await TopicService.AddMaterial(payload);
    closeMatPop();
    toast(payload.body ? "存好了，正在提炼" : "存好了");
    await loadMaterial(state.matPlat);
  } catch (e) {
    toast(humanError(e));
  } finally {
    el.matOk.disabled = false;
    el.matOk.textContent = editing ? "保存" : "存下来并提炼";
  }
}

async function openLinkPop(m) {
  state.linkMat = m.id;
  el.linkMatTitle.textContent = m.title;
  try {
    // 只列「收下过」的选题：还没决定写什么的选题，拿它当参考没有意义。
    const list = await TopicService.List("picked", 100);
    const more = await TopicService.List("writing", 100);
    const all = [...(list || []), ...(more || [])];
    if (!all.length) {
      toast("选题库里还没有收下的选题，先去收一条");
      return;
    }
    el.linkTopic.innerHTML = all
      .map((t) => `<option value="${esc(t.id)}"${m.topicId === t.id ? " selected" : ""}>${esc(t.title)}</option>`)
      .join("");
    el.linkClear.textContent = m.topicId ? "取消关联" : "关闭";
    el.linkPop.hidden = false;
  } catch (e) {
    toast(humanError(e));
  }
}

async function submitLink() {
  const id = state.linkMat;
  const topicID = el.linkTopic.value;
  if (!id || !topicID) return;
  try {
    await TopicService.LinkMaterial(id, topicID);
    el.linkPop.hidden = true;
    toast("已关联");
    await loadMaterial(state.matPlat);
  } catch (e) {
    toast(humanError(e));
  }
}

async function clearLink() {
  const id = state.linkMat;
  if (!id) return;
  try {
    await TopicService.LinkMaterial(id, "");
    el.linkPop.hidden = true;
    toast("已取消关联");
    await loadMaterial(state.matPlat);
  } catch (e) {
    toast(humanError(e));
  }
}

el.matAdd.addEventListener("click", () => openMatPop(null));
el.matCancel.addEventListener("click", closeMatPop);
el.matOk.addEventListener("click", submitMat);
el.matPop.addEventListener("click", (e) => { if (e.target === el.matPop) closeMatPop(); });
el.linkOk.addEventListener("click", submitLink);
el.linkClear.addEventListener("click", clearLink);
el.linkPop.addEventListener("click", (e) => { if (e.target === el.linkPop) el.linkPop.hidden = true; });

boot();

/* ============================================================
   创作工具：爆款拆解 / 标题生成
   ============================================================ */

function escHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

/* 空状态里的示例卡。用同一份 tdCardHtml 渲染，不另写一套样式：
   写两套的话，改了真卡片忘了示例卡，空状态就会展示一个不存在的界面。 */
const TD_SAMPLE = {
  source: "试用期最后一周，这4件事千万别松",
  category: "职场 / 成长",
  title: "把时间点写进标题，比写「注意事项」更容易被点开",
  cover: "大字压满上半屏，只留一句话",
  structure: "先说错法 → 再给正确做法 → 结尾一句提醒",
  engage: "结尾问「你踩过哪个」，评论区自己会答",
  tone: "过来人劝告，不说教",
  topics: ["职场", "试用期", "汇报"],
  learn: "标题写「最后一周」，读者会立刻对号入座；写「注意事项」就只是资料。",
};

function paintTdSample() {
  const box = $("tdSample");
  if (!box || box.dataset.done) return;
  box.innerHTML = `<span class="td-sample-tag">示例</span>${tdCardHtml(TD_SAMPLE)}`;
  box.dataset.done = "1";
}

/* 拆解结果渲染成一张卡：六行短字段 + 一句可借鉴点。
   上一版是六段分析，用户的原话是「太繁琐」——不是块多，是每块都要读。
   这里每行只放一句，扫一眼就能拿走；真要照做，点底下的「照这个结构写」。

   卡片顶上是「拆的是哪条」的原文标题，不是「标题亮点」——
   一屏里可能并排看两条，先认出是哪条，比先看亮点有用。 */
const TD_KEYS = [
  ["标题亮点", "title"],
  ["封面设计", "cover"],
  ["内容结构", "structure"],
  ["互动引导", "engage"],
  ["语气风格", "tone"],
];

function tdCardHtml(t) {
  const rows = TD_KEYS.filter(([, f]) => String(t[f] || "").trim())
    .map(
      ([k, f], i) =>
        `<div class="td-row"><dt class="td-k${i + 1}">${k}</dt><dd>${escHtml(t[f])}</dd></div>`
    )
    .join("");

  const topics = (t.topics || []).length
    ? `<div class="td-row"><dt class="td-k6">适合话题</dt><dd>${t.topics
        .map((s) => `<span class="td-tag">${escHtml(s)}</span>`)
        .join("")}</dd></div>`
    : "";

  const learn = String(t.learn || "").trim()
    ? `<div class="td-learn"><span class="td-learn-k">可借鉴点</span><p>${escHtml(t.learn)}</p></div>`
    : "";

  return `
    <article class="td-card">
      <header class="td-card-head">
        <h2 class="td-src">${escHtml(t.source || "（没存原文标题）")}</h2>
        <div class="td-meta">
          ${t.category ? `<span class="td-cat">${escHtml(t.category)}</span>` : ""}
          ${t.atLabel ? `<span class="td-at">${escHtml(t.atLabel)}拆的</span>` : ""}
        </div>
      </header>
      <dl class="td-rows">${rows}${topics}</dl>
      ${learn}
      <div class="td-card-foot">
        <button class="link-btn" data-dl-td="1">下载报告</button>
        <button class="link-btn" data-use-structure="1">照这个结构写一篇</button>
        <button class="link-btn" data-copy-td="1">复制成文字</button>
      </div>
    </article>`;
}

/* 复制成纯文字。用户真正要拿去用的是这几行，
   让他对着卡片一行行手抄，等于把拆解的价值又还回去了。 */
function tdPlainText(t) {
  const out = [];
  const put = (k, v) => {
    if (String(v || "").trim()) out.push(`${k}：${v}`);
  };
  if (t.source) out.push(`【拆解】${t.source}`);
  put("标题亮点", t.title);
  put("封面设计", t.cover);
  put("内容结构", t.structure);
  put("互动引导", t.engage);
  put("语气风格", t.tone);
  if ((t.topics || []).length) out.push(`适合话题：${t.topics.join(" / ")}`);
  put("可借鉴点", t.learn);
  put("分类", t.category);
  return out.join("\n");
}

/* 拆过的记录。放在输入框下面而不是单独一页：
   它和拆解是同一个动作的两种状态——「刚拆的」和「上次拆的」，
   分成两个页面，用户就要在「我拆过什么」和「我要拆什么」之间来回切。 */
function paintTdHist() {
  const list = state.tdHist || [];
  if (!el.tdHist || !el.tdHistList) return;
  el.tdHist.hidden = list.length === 0;
  if (!list.length) return;
  el.tdHistList.innerHTML = list
    .map(
      (t) => `
      <div class="td-hist-item${t.id === state.tdID ? " on" : ""}" data-td="${escHtml(t.id)}" role="button" tabindex="0">
        <span class="td-hist-main">
          <span class="td-hist-title">${escHtml(t.source || t.title || "未命名")}</span>
          <span class="td-hist-meta">${escHtml(t.category || "")}${
            t.category && t.atLabel ? " · " : ""
          }${escHtml(t.atLabel || "")}</span>
        </span>
        <button class="td-hist-del" data-td-del="${escHtml(t.id)}" title="删掉这条记录">删</button>
      </div>`
    )
    .join("");
}

async function loadTdHist() {
  try {
    state.tdHist = (await TopicService.Teardowns()) || [];
  } catch {
    // 读历史失败不该挡住「拆一条」这件事，静默降级成空列表。
    state.tdHist = [];
  }
  paintTdHist();
}

function titleListHtml(list) {
  if (!list.length) return `<p class="rev-empty">这次没生成出来，再点一次。</p>`;
  return `<div class="tg-list">${list
    .map(
      (t) => `
    <button class="tg-item" data-copy="${escHtml(t.text)}">
      <span class="tg-hook">${escHtml(t.hook || "标题")}</span>
      <span class="tg-main">
        <span class="tg-text">${escHtml(t.text)}</span>
        <span class="tg-note">${escHtml(t.note || "")}</span>
      </span>
      <span class="tg-copy">复制</span>
    </button>`
    )
    .join("")}</div>
  <p class="tool-hint tg-foot">点任意一条复制。八条挑一条改，别直接用——平台会判重。</p>`;
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast("已复制");
  } catch {
    // 无剪贴板权限时退回 execCommand，比默默失败好。
    const ta = document.createElement("textarea");
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand("copy");
      toast("已复制");
    } catch {
      toast("复制失败，请手动选中");
    }
    ta.remove();
  }
}

async function runTeardown() {
  const raw = el.tdRaw.value.trim();
  if (!raw) {
    toast("先把要拆的笔记粘进来");
    el.tdRaw.focus();
    return;
  }
  el.tdRun.disabled = true;
  el.tdRun.textContent = "拆解中…";
  el.tdEmpty.hidden = true;
  el.tdResult.hidden = false;
  el.tdResult.innerHTML = `<div class="tool-loading"><i></i>正在读这条内容，通常十几秒</div>`;
  try {
    const td = (await TopicService.TeardownRaw(raw)) || {};
    state.tdCurrent = td;
    state.tdID = td.id || "";
    el.tdResult.innerHTML = tdCardHtml(td);
    // 新拆的排在最前面，不重新拉一遍列表——接口刚返回的就是最新那条。
    state.tdHist = [td, ...(state.tdHist || []).filter((x) => x.id !== td.id)];
    paintTdHist();
  } catch (e) {
    el.tdResult.innerHTML = `<div class="tool-fail">${escHtml(humanError(e))}</div>`;
  } finally {
    el.tdRun.disabled = false;
    el.tdRun.textContent = "开始拆解";
  }
}

/* 看一条历史记录：把存下来的结果放回结果区。
   不复制到输入框里——那会让用户以为「这条还没拆过」。 */
function openTeardown(id) {
  const t = (state.tdHist || []).find((x) => x.id === id);
  if (!t) return;
  state.tdCurrent = t;
  state.tdID = t.id;
  el.tdEmpty.hidden = true;
  el.tdResult.hidden = false;
  el.tdResult.innerHTML = tdCardHtml(t);
  paintTdHist();
}

async function deleteTeardown(id) {
  try {
    await TopicService.DeleteTeardown(id);
  } catch (e) {
    return toast(humanError(e));
  }
  state.tdHist = (state.tdHist || []).filter((x) => x.id !== id);
  if (state.tdID === id) {
    state.tdID = "";
    state.tdCurrent = null;
    el.tdResult.hidden = true;
    el.tdResult.innerHTML = "";
    el.tdEmpty.hidden = false;
  }
  paintTdHist();
  toast("删掉了");
}

async function runTitles() {
  const seed = el.tgSeed.value.trim();
  if (seed.length < 4) {
    toast("多写几个字，说清你想讲什么");
    el.tgSeed.focus();
    return;
  }
  const hooks = [...el.tgHooks.querySelectorAll(".hook-pick.on")].map((b) => b.dataset.hook);
  el.tgRun.disabled = true;
  el.tgRun.textContent = "生成中…";
  el.tgEmpty.hidden = true;
  el.tgResult.hidden = false;
  el.tgResult.innerHTML = `<div class="tool-loading"><i></i>正在想八个不一样的说法</div>`;
  try {
    const list = await TopicService.SuggestTitles(seed, hooks);
    el.tgResult.innerHTML = titleListHtml(list || []);
  } catch (e) {
    el.tgResult.innerHTML = `<div class="tool-fail">${escHtml(humanError(e))}</div>`;
  } finally {
    el.tgRun.disabled = false;
    el.tgRun.textContent = "生成标题";
  }
}

el.tdResult?.addEventListener("click", async (ev) => {
  if (ev.target.closest("[data-dl-td]")) {
    if (!state.tdID) return toast("这条还没存住，先重拆一次");
    try {
      const path = await TopicService.SaveTeardownReport(state.tdID);
      toast("存好了：" + String(path).split(/[\\/]/).pop());
    } catch (e) {
      const msg = humanError(e);
      // 取消保存不是错误，别弹红字吓人。
      if (!/取消/.test(msg)) toast(msg);
    }
    return;
  }
  if (ev.target.closest("[data-copy-td]")) {
    const t = state.tdCurrent;
    if (!t) return;
    copyText(tdPlainText(t));
    return;
  }
  if (!ev.target.closest("[data-use-structure]")) return;
  // 这一版的内容结构是一句话的公式（「疑问开场→3个方法→提问收尾」），
  // 直接当一行骨架传给写稿；不拆成多步是因为拆了就回到上一版的繁琐。
  const skel = String(state.tdCurrent?.structure || "").trim();
  if (!skel) return toast("这条没拆出结构");
  write.skeleton = [skel];
  write.skeletonFrom = "拆解";
  switchView("write");
});

el.tdHistList?.addEventListener("click", (ev) => {
  const del = ev.target.closest("[data-td-del]");
  if (del) {
    ev.stopPropagation();
    deleteTeardown(del.dataset.tdDel);
    return;
  }
  const item = ev.target.closest("[data-td]");
  if (item) openTeardown(item.dataset.td);
});

el.tdHistList?.addEventListener("keydown", (ev) => {
  if (ev.key !== "Enter" && ev.key !== " ") return;
  const item = ev.target.closest("[data-td]");
  if (!item) return;
  ev.preventDefault();
  openTeardown(item.dataset.td);
});

el.tdRun?.addEventListener("click", runTeardown);
el.tgRun?.addEventListener("click", runTitles);

el.tgHooks?.addEventListener("click", (ev) => {
  const b = ev.target.closest(".hook-pick");
  if (!b) return;
  b.classList.toggle("on");
});

el.tgResult?.addEventListener("click", (ev) => {
  const item = ev.target.closest(".tg-item");
  if (!item) return;
  copyText(item.dataset.copy || "");
});

/* 没配模型时，把按钮直接禁掉并说明原因。
   让用户点完再报错是浪费他一次注意力。 */
async function paintCraftStatus() {
  try {
    const st = await TopicService.CraftAvailable();
    if (st?.ready) return;
    for (const [btn, hint] of [
      [el.tdRun, el.tdHint],
      [el.tgRun, el.tgHint],
    ]) {
      if (btn) {
        btn.disabled = true;
        btn.title = st.message || "";
      }
      if (hint) {
        hint.textContent = st.message || "需要先配置模型";
        hint.classList.add("is-warn");
      }
    }
  } catch {
    /* 状态读不到不阻塞界面，点下去仍会给出明确错误 */
  }
}
