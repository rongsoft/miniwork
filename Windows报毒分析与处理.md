# Windows 报毒：现象、根因与处理方案

> 写于 2026-11-17。用于交接：下一个接手的人（或 AI）读完这一份就能直接动手，
> 不用重新查一遍。
>
> 触发这个问题的原话：「为什么 minizip 生成的 exe 在 windows 里面没有被识别为风险，
> 为什么你做的这个 exe 被识别风险了」

---

## 一、先分清是哪一种「风险提示」

这两种提示的对策完全不同，**先看原文，再动手**。

| 提示原文 | 机制 | 签名能不能解决 | 现象 |
|---|---|---|---|
| 「Windows 已保护你的电脑」/「已阻止无法识别的应用」 | SmartScreen（声誉制） | **能** | 蓝底，有「更多信息 → 仍要运行」 |
| 「已阻止威胁：Trojan:Win32/Wacatac.B!ml」之类 | Defender 主动查杀（ML 误报） | **不能** | 直接隔离/删除，没有「仍要运行」 |
| 解压时被杀软直接删掉 browser 目录 | 同上，扫到 Chromium 载荷 | 不能 | 解压完程序起不来 |

`Wacatac.B!ml` 里的 `!ml` 就是「机器学习判定」，不是特征库命中——这是理解整件事的关键。

---

## 二、实测对照：两个 exe 到底差在哪

用 `strings` 直接数特征（在 macOS 上对 PE 文件也有效）：

```bash
for k in taskkill CreateProcess remote-debugging-port no-sandbox devtools websocket cookies; do
  printf "%-22s %s\n" "$k" "$(strings -a Miniwork.exe | grep -ci "$k")"
done
```

| 特征 | minizip.exe | Miniwork.exe (0.1.4) |
|---|---|---|
| 体积 | 1.4 MB | 19 MB（发行 zip 207 MB） |
| 文件类型 | PE32+ GUI x86-64 | 同 |
| Go 二进制 | 否 | 是 |
| 数字签名 | 无 | 无 |
| 版本信息资源 | 无 | 有（0.1.4） |
| `--remote-debugging-port` | 0 | 有 |
| `--no-sandbox` | 0 | 有 |
| `devtools` / `websocket` | 0 / 0 | 99 / 32 |
| `cookies` 字样 | 0 | 58（含 `FROM cookies WHERE`） |
| 内嵌 SQLite 引擎 | 无 | 有（`cookies` 相关 4765 处） |
| 捆绑 Chromium | 无 | 有，317 个文件 / 约 190 MB |
| `Google Chrome for Testing` 目录 | 无 | 有（在 `browser/<平台>/` 下） |

结论：**不是「minizip 写得好」，是它的行为面接近零，而我们的行为面就是一款
商品化 info-stealer 的标准形态。**

---

## 三、根因拆解：五条特征叠在一起

反病毒引擎不读代码，只看行为组合。我们现在同时满足：

### 1. 读别人浏览器里的 Cookie 库

`internal/publish/browser.go` → `cookieInProfile()`

```go
dsn := "file:" + db + "?immutable=1&mode=ro"
conn.QueryRow(`SELECT count(*) FROM cookies WHERE name = ?`, name)
```

打开 Chrome 的 `Default/Network/Cookies` 并跑 SQL。这是 info-stealer 第一大特征。
我们只看 `count(*)` 不读 value，但**静态特征完全一样**，引擎不会替我们区分。

用途：不启动浏览器就判断「这个平台登录过没有」，用于发布台那三行状态。

### 2. 用调试端口远程控制浏览器

`--remote-debugging-port=0` + 全量 CDP（WebSocket）。这是 bot / 远控工具特征。
用途：自动发布必须有它，无法去掉。

### 3. 带 `--no-sandbox` 启动子进程

`browser.go` 里 `needsNoSandbox()` 目前在 **macOS 返回 true**，Windows 返回 false。
但字符串仍在二进制里，仍是加分项。

### 4. 捆绑一整份 Chromium，且改了名、放在自建目录

`browser/win64/` 下 317 个文件，可执行文件叫 `Google Chrome for Testing.exe`，
被一个 19 MB 的陌生 exe 启动并开放调试端口 —— 这是「携带完整浏览器内核的
未知程序」，评分极高。

### 5. 207 MB 的发行包

陌生、体积大、解压出大量可执行文件。用户下载时 Defender 会整包扫描。

---

## 四、处理方案（按性价比排序）

### A. 代码签名 —— SmartScreen 的唯一正解

- OV 证书：约 ¥1000–3000/年，签完仍需积累声誉，但不会再显示「未知发布者」
- EV 证书：立即有声誉，需企业材料、走人工审核
- **局限**：不解决 Defender 的 ML 误报，只解决 SmartScreen

### B. 提交 Defender 误报 —— 最快见效，但要反复做

向微软 WDSI 提交误报（`https://www.microsoft.com/en-us/wdsi/filesubmission`），
选择「软件开发者」，附上文件哈希与产品说明。一般 24–72 小时解除。

**注意：针对的是文件哈希，每发一版都要重新提交。** 这是长期成本，不是一次性动作。

### C. 去掉磁盘读 Cookie 库（建议做）

改成一件事：**登录态只认活会话**。

- `Authed(platform)` 里删掉 `cookieInProfile` 那条分支，只走 CDP `Network.getCookies`
- 代价：引擎没开会话时答 `false`，发布台三行会显示「未检测」，而不是「已连接」
- 现状：`authedLocked` 已经写好活会话分支，删掉磁盘那半即可
- 附带好处：少一个 SQLite 依赖面（`modernc.org/sqlite` 仍被 store 用着，不能整个去掉）

### D. 不捆 Chromium，优先驱动系统 Edge（建议做）

Windows 10/11 一定装了 Edge，且 Edge 同样说 CDP。

- `BrowserPath()` 的顺序改成：系统 Edge → 系统 Chrome → 内置 Chromium
- 内置那份单独出「离线版/绿色版」，给没装浏览器的机器用
- 预期：exe 19 MB → 5 MB 左右，发行 zip 207 MB → 十几 MB
- 风险：客户机器上 Edge 被组策略锁死（少见），此时给离线版兜底

### E. 改成签名安装器

让用户下载 NSIS 安装器而不是裸 exe。SmartScreen 看的是安装器证书声誉，
且安装器可以在安装时一次性把 browser 目录放进去（避免解压时被逐个查杀）。

`build/windows/nsis/project.nsi` 与 `-DWAILS_BUNDLED_BROWSER` 已经就绪，
只是这台机器没有 `makensis`，需要在 Windows 上跑 `wails3 task windows:package`。

---

## 五、建议执行顺序

1. 先确认弹的是哪一句（见第一节表格）——决定要不要立刻做 B
2. **C + D 一起做**：这是产品形态问题，不是绕过检测。签名过了之后，
   客户公司的安全软件（火绒、360、卡巴）仍会陆续拦「读 cookie + 开调试端口」的包
3. A 走商务流程（办证书要时间，越早启动越好）
4. B 在每次发版后提交
5. E 在有 Windows 构建机之后做

**不建议做的**：改字符串、加壳、混淆、把 `--no-sandbox` 拆成拼接。
那是规避检测，会让后续每一次误报都变成「有前科」，而且客户装上去仍然被
运行时行为拦截（Defender 有运行时监控，不只看静态特征）。

---

## 六、尚未确认的事实

- [ ] 用户看到的提示原文（SmartScreen 还是 Defender 隔离）
- [ ] 是否解压时就被删文件，还是双击 exe 才拦
- [ ] 客户机器上装的是 Defender 还是第三方（火绒/360 的判定更激进）
- [ ] 内置 Chromium 在 Windows 上启动时是否被单独拦（`Google Chrome for Testing.exe`
      被杀软删掉的情况要单独验证）

---

## 七、交接时必须知道的当前状态

- 版本 0.1.4 已出包：`bin/Miniwork.dmg`（183 MB）、
  `bin/miniwork-0.1.4-windows-amd64.zip`（207 MB，含 `browser/win64`）
- **0.1.3 的 Windows 包漏了 browser 目录**，那个包装到客户机上「扫码登录」是死的，
  已修；以后打 Windows 包必须 `unzip -l | grep browser` 数一遍
- 公众号「扫码绑定」已实现（自动读 AppID + 显示出口 IP），**二维码页已实测可扫**：
  登录页 `<img class="login__type__container__scan__qrcode">` 的 `naturalWidth=472`，
  连续 5 次轮询地址不变（不会刷新二维码）。
  但**扫码之后能不能读到 AppID 仍未验证**，要人拿手机扫一次才知道
- 公众号这条路上踩过并已修的坑（交接时别退回去）：
  1. `WeChatOpenConsole` 必须进 `https://mp.weixin.qq.com/` 根地址。
     进 `/cgi-bin/home?t=home/index` 时，未登录会被甩到「登录超时，请重新登录」
     的空白错误页——**页面上根本没有二维码**
  2. 轮询时不能导航：地址已在 `mp.weixin.qq.com` 且无 token，就什么都不做
  3. 基本配置页（`action=dev`）只为同一个 token 翻一次，翻过只读
- 平台上没有 miniwork 的下载页，发版只到出包，上传另做
