// Package publish 把稿子真正发到平台上。
//
// 做法和蚁小二这类工具是同一个原理：不碰平台的私有接口，而是用本机
// 已安装的 Chrome / Edge 打开平台网页版，复用一次扫码登录留下的登录态。
// 好处是平台改版时只改选择器、不改协议；坏处是必须有一台真实浏览器，
// 而且平台改版会让某几步失败——所以每条链路都留了「填不完就交给用户」
// 的出口，绝不假装成功。
//
// 不使用无头模式，也不用自动化指纹伪装：用户要能看见它在做什么，
// 遇到验证码能自己点。这也让风控看到的是一个正常的人工会话。
package publish

import "strings"

// Media 是发布时需要用户准备的素材。
//
// 这三个平台没有一个是纯文字就能发的小红书要图、抖音要视频、
// 公众号要封面，所以界面上必须先把这件事说清楚，而不是发到一半才报错。
type Media int

const (
	MediaNone   Media = iota // 不需要素材
	MediaImages              // 图文：1-9 张图
	MediaVideo               // 视频：1 个文件
)

// 平台分两类，走的链路完全不同。
const (
	// KindBrowser 驱动本机浏览器发：小红书、抖音没有公开接口。
	KindBrowser = "browser"
	// KindAPI 走官方接口：公众号是唯一有发布接口的平台。
	KindAPI = "api"
)

// Platform 是一个发布目标。
//
// 选择器放这里而不是散在各平台文件里，是因为它们是最容易过期的部分：
// 平台一改版，改的就是这张表，一看就知道要改哪几行。
type Platform struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Media Media  `json:"-"`
	Kind  string `json:"-"`

	// Modes 是可选的内容形态（公众号分图文消息和图片消息）。
	Modes []string `json:"-"`

	// MaxImages 是这个平台一次最多收几张图。
	//
	// 以前所有平台统一按 9 张截，公众号的图片消息其实能收 20 张——
	// 用户选了 15 张，程序默默只传前 9 张，他不会知道少了什么。
	MaxImages int `json:"-"`

	LoginURL   string `json:"-"`
	PublishURL string `json:"-"`

	// MetricsURL 是「我的笔记 / 作品管理」页。抓数据先导航到这里，
	// 让页面自己在正确的域和上下文里待着，再借它的会话调接口——
	// 直接在空白页发 fetch 会因为跨域和 Referer 被拒。
	MetricsURL string `json:"-"`

	// MetricsAPIs 是取数据的候补接口（相对路径）。
	// 平台改版时路径和字段都会变，写死一个必然过期；
	// 按顺序试，第一个解析出笔记列表的就用。
	MetricsAPIs []string `json:"-"`

	// Cookie 用来判断「这个平台登没登录」。
	//
	// 不看页面元素：登录成功后页面结构会变，而会话 cookie 是稳定事实。
	// 只要这个 cookie 在，凭证就是有效的，不必让用户再扫一次码。
	Cookie string `json:"-"`

	// PreClick 是进发布页之后要先点的东西（比如「上传图文」这个页签）。
	PreClick []string `json:"-"`

	// Upload 是文件输入框。
	Upload []string `json:"-"`

	Title []string `json:"-"`
	Body  []string `json:"-"`

	// Submit 是发布按钮，按文字找：这些按钮的内联样式和 class 天天变，
	// 唯一稳定的是它上面写着「发布」。
	Submit []string `json:"-"`

	// EditorPath 是发布页的路径特征。点完发布之后 URL 里应该不再有它；
	// 还停在这个路径上，说明被弹窗、验证码或必填项拦住了，不能算发成功。
	//
	// 用它而不是「发成功页的 URL」：失败页千变万化，而"还在原地"
	// 这个信号在所有平台上都成立。
	EditorPath string `json:"-"`

	Note string `json:"note"` // 界面上提醒这个平台的坑
}

// platforms 是平台事实表。加一个平台＝在这里加一条 + 写一个 flow 分支（多数情况不用写）。
var platforms = []Platform{
	{
		Key: "xiaohongshu", Name: "小红书", Media: MediaImages,
		LoginURL:   "https://creator.xiaohongshu.com/login",
		PublishURL: "https://creator.xiaohongshu.com/publish/publish",
		Cookie:     "web_session",
		// 笔记管理页：登录态下这里能列出所有发过的笔记和数据。
		MetricsURL: "https://creator.xiaohongshu.com/new/note-manager",
		// 三个候选来自不同时期的接口，按命中率排。
		MetricsAPIs: []string{
			"/api/galaxy/creator/note/user/posted?tab=0&page=0&page_size=30",
			"/api/galaxy/creator/note/user/posted?page=0&page_size=30",
			"/api/galaxy/v2/creator/note/user/posted?tab=0&page=0&page_size=30",
		},
		PreClick:   []string{"上传图文", "图文"},
		Upload:     []string{`input[type=file][accept*="image"]`, `input[type=file]`},
		Title:      []string{`input[placeholder*="标题"]`, `input.d-text`, `input[type=text]`},
		Body:       []string{`div.ql-editor`, `div[contenteditable="true"]`, `textarea[placeholder*="正文"]`},
		Submit:     []string{"发布", "发布笔记"},
		MaxImages:  9,
		EditorPath: "/publish/publish",
		Note:       "标题超过 20 字在信息流里会被截断；图片建议 3:4",
	},
	{
		// 公众号没有网页自动化：编辑器在 iframe 里、封面和原创一堆必填，
		// 而它恰好是唯一有公开接口的平台，所以走接口。
		//
		// 但绑定这一步还是扫码：接口认 AppID + AppSecret，扫码认的是后台会话，
		// 两者换不来。扫码只是为了让 AppID 自动填上（见 wechat_scan.go），
		// 发不发布仍以凭证为准。
		Key: "wechat", Name: "公众号", Media: MediaImages, Kind: KindAPI,
		Modes: []string{"news", "newspic"},
		// 发不走网页（走接口），但这个地址不是给发布用的，是给
		// 「登录了没」用的：Authed 的活会话分支要拿它去取 cookie。
		// 少了这一行，公众号的登录态永远判成「没登录」——见 browser.go
		// 里 authedLocked 的第二条路。
		LoginURL:   "https://mp.weixin.qq.com/",
		PublishURL: "https://mp.weixin.qq.com/",
		Cookie:     "slave_sid", // 扫码登录后微信写的会话 cookie，不落盘，只能问活会话
		MaxImages:  20,
		Note:       "服务号、订阅号都行，要已认证；扫一次码能自动填 AppID，AppSecret 要你在后台生成一次",
	},
	{
		Key: "douyin", Name: "抖音", Media: MediaVideo,
		// 登录入口指向发布页，不是首页。
		//
		// 实测（Chrome 157，无头）：
		//   首页 creator.douyin.com/ → 落地在营销介绍页，页面上没有登录框，
		//   用户点「登录」之后看到一屏宣传文案，只会认为程序没反应；
		//   发布页 content/upload → 未登录时直接弹「扫码登录 / 验证码登录 /
		//   密码登录」三选一，正是我们要的那一步。
		LoginURL:   "https://creator.douyin.com/creator-micro/content/upload",
		PublishURL: "https://creator.douyin.com/creator-micro/content/upload",
		Cookie:     "sessionid",
		MetricsURL: "https://creator.douyin.com/creator-micro/content/manage",
		MetricsAPIs: []string{
			"/janus/douyin/creator/pc/work_list?status=0&count=30&cursor=0",
		},
		Upload:     []string{`input[type=file][accept*="video"]`, `input[type=file]`},
		Title:      []string{`input[placeholder*="标题"]`, `input[placeholder*="作品"]`, `input.semi-input`},
		Body:       []string{`div[data-placeholder]`, `div[contenteditable="true"]`, `textarea`},
		Submit:     []string{"发布", "立即发布"},
		EditorPath: "/content/upload",
		Note:       "视频要先上传转码完成，网慢时这一步会等十几秒",
	},
}

// All 返回可发布的平台。
func All() []Platform {
	out := make([]Platform, len(platforms))
	copy(out, platforms)
	return out
}

// Get 按 key 取平台。
func Get(key string) (Platform, bool) {
	key = strings.TrimSpace(key)
	for _, p := range platforms {
		if p.Key == key {
			return p, true
		}
	}
	return Platform{}, false
}
