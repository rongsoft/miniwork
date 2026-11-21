// 假 Wails 运行时：把绑定的 $Call.ByID(hash, ...args) 转成对真后端的 HTTP 调用。
//
// 这里不编造任何数据——数据全部来自 Go 侧真实的服务与数据库，
// 只是把「进程内 IPC」换成「本地 HTTP」。这样页面看到的字段就是后端真有的字段，
// 写操作也真的落库，跑完可以拿 sqlite3 查。

const ids = await (await fetch("/ids.json")).json();

window.__calls = [];

async function invoke(method, args) {
  window.__calls.push({ name: method, args });
  const r = await fetch("/call", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  const j = await r.json();
  if (!j.ok) throw new Error(j.error || "调用失败");
  return j.data;
}

export const Call = {
  ByID(id, ...args) {
    const name = ids[String(id)];
    if (!name) return Promise.reject(new Error("未知的调用号 " + id));
    return invoke(name, args);
  },
  ByName(name, ...args) {
    return invoke(name, args);
  },
};

// 绑定文件里有 Object.freeze($Create.Events)，所以 Events 得是个对象。
// Array/Map 是「工厂」：$Create.Array(createFn) 返回一个 (src) => src.map(createFn)，
// 不是「把数组转一下」。这点弄反了会让所有返回列表的接口变成空数组且不报错。
export const Create = {
  Any: (x) => x,
  Array: (x) =>
    typeof x === "function"
      ? (src) => (Array.isArray(src) ? src.map(x) : [])
      : Array.isArray(x) ? x : [],
  Map: (x) => (typeof x === "function" ? (src) => src || {} : x || {}),
  // Nullable 也是工厂：$Create.Nullable(inner) 返回一个转换器，
  // 原值为 null/undefined 时原样返回 null，否则交给 inner。
  // 少了它，凡是带可为空对象字段的返回类型一加载就抛 TypeError。
  Nullable: (x) => (src) =>
    src === null || src === undefined
      ? null
      : typeof x === "function"
        ? x(src)
        : src,
  Struct: (x) => x,
  Events: {},
};

export class CancellablePromise extends Promise {
  cancel() {}
  static get [Symbol.species]() {
    return Promise;
  }
}

// 事件：真后端在 app 为 nil 时不发事件，所以这里只登记、不推送。
// 页面靠主动刷新拿结果，不依赖推送。
export const Events = {
  On(name, cb) {
    (window.__subs = window.__subs || []).push(name);
    return () => {};
  },
  Once(name, cb) {
    return () => {};
  },
  Emit() {},
};

export const Window = { SetTitle() {}, SetSize() {}, Center() {} };
export const Browser = { OpenURL: (u) => invoke("OpenURL", [u]) };
export const Clipboard = { SetText: async () => true, Text: async () => "" };
export const Application = { Show() {}, Hide() {} };
