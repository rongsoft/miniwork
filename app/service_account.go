package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/account"
	"rongsoft/miniwork/app/internal/accountsync"
	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/config"
	"rongsoft/miniwork/app/internal/publish"
	"rongsoft/miniwork/app/internal/store"
)

// 账号：机器码即身份，第一次打开自动建档。
//
// 产品上的取舍写在两处：
//   - 不拦人。姓名/手机号可以为空，账号页提示去补，但不填也能把全流程走完。
//   - 不上传。资料只在本机 sqlite 里，连注册这一步都没有网络请求。
//     接服务端时只换 syncAccount 的实现，界面不用动。

// AccountView 是「账号」页要的全部内容。
type AccountView struct {
	Code         string        `json:"code"` // 4A7F-19C2-8E30-5B61
	Name         string        `json:"name"`
	Phone        string        `json:"phone"`
	CreatedLabel string        `json:"createdLabel"` // 「2026-11-16 建档」
	Complete     bool          `json:"complete"`     // 姓名和手机号都填了
	Synced       bool          `json:"synced"`       // 预留：服务端同步过为 true
	Bindings     []BindingView `json:"bindings"`
	Unbound      int           `json:"unbound"` // 还没绑的数量，导航上打点用
}

// BindingView 是一条「这个功能要用到的外部账号」。
//
// 全部由用户自己绑：模型 Key、公众号 AppID/Secret、平台登录态。
// 产品方不提供、也拿不到这些凭证——这是卖断工具和 SaaS 的根本区别。
type BindingView struct {
	Key    string `json:"key"`    // ai / wechat / xiaohongshu / douyin
	Name   string `json:"name"`   // 显示名
	Desc   string `json:"desc"`   // 绑它能干什么
	Ready  bool   `json:"ready"`  // 已绑定
	Status string `json:"status"` // 已连接 / 未绑定
	Detail string `json:"detail"` // 缺什么、绑到哪一步了
	Where  string `json:"where"`  // 去哪绑：settings / publish
	// Action 是这一行该放哪个按钮。放在后端算，是因为「该扫码还是该装浏览器」
	// 取决于浏览器探测结果——前端再判一遍就会算出和状态不一致的按钮。
	// setup-ai / wechat-config / login / relogin / install-browser
	Action string `json:"action"`
}

// BrowserInfo 是「发不发得出去」的前提，绑定区顶部一行小字。
type BrowserInfo struct {
	Ready   bool   `json:"ready"`   // 有浏览器可驱动
	Bundled bool   `json:"bundled"` // 用的是包里自带那份
	Version string `json:"version"` // 自带那份的版本，用系统 Chrome 时是空的
}

// Account 读本机账号，没有就现场建档。
//
// 「注册」就这么发生了：没有网络请求、没有表单、没有验证码。
// 用户第一次打开就是一个有身份的账号，他只需要后面补个名字。
func (s *TopicService) Account() (AccountView, error) {
	a, err := s.ensureAccount()
	if err != nil {
		return AccountView{}, err
	}
	v := AccountView{
		Code:     a.ID,
		Name:     tidyName(a.Name),
		Phone:    tidyQuotes(a.Phone),
		Synced:   a.SyncedAt != "",
		Bindings: s.bindings(),
	}
	// 读出来发现是脏的（老版本没挡住引号），顺手修一次库：
	// 只改显示不动库，用户下次打开还会看见那对引号。
	if v.Name != a.Name || v.Phone != a.Phone {
		if err := s.st.SaveAccountProfile(a.ID, v.Name, v.Phone, a.UpdatedAt); err == nil {
			a.Name, a.Phone = v.Name, v.Phone
		}
	}
	if !a.CreatedAt.IsZero() {
		v.CreatedLabel = a.CreatedAt.Format("2006-01-02") + " 建档"
	}
	v.Complete = a.Name != "" && a.Phone != ""
	for _, b := range v.Bindings {
		if !b.Ready {
			v.Unbound++
		}
	}
	return v, nil
}

// UpdateAccount 保存姓名和手机号。
//
// 叫 Update 而不是 Save：这个名字已经被两处占了——SaveAccount 记发布账号名、
// SaveProfile 存人设。三个都叫 Save 的东西，三个月后必然有人改错地方。
//
// 手机号不做「必须是大陆 11 位」的死校验：客户里有留座机的、有留境外号的，
// 卡死只会让他填不进去又不知道错在哪。只挡明显不是号码的输入。
func (s *TopicService) UpdateAccount(name, phone string) (AccountView, error) {
	a, err := s.ensureAccount()
	if err != nil {
		return AccountView{}, err
	}
	name = tidyName(name)
	if len([]rune(name)) > 24 {
		return AccountView{}, errors.New("名字太长了，24 个字以内")
	}
	phone, err = cleanPhone(phone)
	if err != nil {
		return AccountView{}, err
	}
	if err := s.st.SaveAccountProfile(a.ID, name, phone, time.Now()); err != nil {
		return AccountView{}, err
	}
	// 刚改完就要立刻推一次：不清这一下，用户会在五分钟内看到
	// 「名字改了但同步时间还是刚才那个」，然后以为没保存上。
	s.syncMu.Lock()
	s.syncAt = time.Time{}
	s.syncMu.Unlock()
	return s.Account()
}

var phoneRe = regexp.MustCompile(`^[0-9+\-]{6,20}$`)

// cleanPhone 去空格和横杠后校验。空串合法（允许不填）。
func cleanPhone(s string) (string, error) {
	s = strings.NewReplacer(" ", "", "-", "", "－", "").Replace(strings.TrimSpace(s))
	s = tidyQuotes(s)
	if s == "" {
		return "", nil
	}
	if !phoneRe.MatchString(s) {
		return "", errors.New("手机号看着不对：填号码就行，不用写别的")
	}
	return s, nil
}

// tidyQuotes 去掉用户粘贴时带进来的引号。
//
// 真实案例：库里的手机号是 '13900000001'——用户从表格里复制，把 Excel
// 用来防止科学计数法的那对单引号一起带进来了。校验规则当时放过了它，
// 结果账号页显示成「'13900000001'」，看着像我们存坏了。
func tidyQuotes(s string) string {
	return strings.Trim(strings.TrimSpace(s), "'\"“”‘’")
}

// tidyName 同上，姓名也常被引号包着粘进来。
func tidyName(s string) string {
	return strings.TrimSpace(tidyQuotes(s))
}

// ensureAccount 拿账号，拿不到就按机器码建一个。
func (s *TopicService) ensureAccount() (store.Account, error) {
	a, err := s.st.Account()
	if err == nil {
		return s.syncAccount(a), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.Account{}, err
	}
	a, err = s.st.CreateAccount(account.Code(), time.Now())
	if err != nil {
		return store.Account{}, err
	}
	return s.syncAccount(a), nil
}

// syncAccount 把本机账号和服务端对齐。
//
// 三条不变量，每条都有测试守着：
//  1. 没配服务端就一步都不走——默认装机就是这个状态，不能因此变慢或报错；
//  2. 网络不通/服务端报错时本地资料原样保留，绝不让「同步」变成数据丢失；
//  3. 服务端没实现这个接口（404/501）算「没启用」，不算错误。
//
// 节流五分钟：Account() 被导航栏、账号页、发布台反复调用，
// 每次都发一个请求等于给启动路径挂了个网络依赖。
func (s *TopicService) syncAccount(a store.Account) store.Account {
	cfg, err := config.Load()
	if err != nil || cfg.Remote.BaseURL == "" {
		return a
	}

	s.syncMu.Lock()
	recent := time.Since(s.syncAt) < 5*time.Minute
	if !recent {
		s.syncAt = time.Now()
	}
	s.syncMu.Unlock()
	if recent {
		return a
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	out := accountsync.Client{
		BaseURL: cfg.Remote.BaseURL,
		Token:   cfg.Remote.Token,
	}.Sync(ctx, accountsync.Account{
		Code:      a.ID,
		Name:      a.Name,
		Phone:     a.Phone,
		UpdatedAt: a.UpdatedAt,
	})
	if out.Err != nil {
		// 同步失败不该让用户看见红字：他填的资料好好在本地，
		// 只是这次没送到线上。下次打开会再试一次。
		log.Printf("账号同步未成功（本地资料不受影响）：%v", out.Err)
		return a
	}
	if out.Action == accountsync.ActionSkipped {
		return a
	}

	now := time.Now().Format(time.RFC3339)
	name, phone, updated := a.Name, a.Phone, a.UpdatedAt
	if out.Action == accountsync.ActionPulled {
		name, phone, updated = out.Account.Name, out.Account.Phone, out.Account.UpdatedAt
	}
	if err := s.st.ApplyAccountSync(a.ID, name, phone, updated, now); err != nil {
		log.Printf("同步结果没能写回本机：%v", err)
		return a
	}
	a.Name, a.Phone, a.UpdatedAt, a.SyncedAt = name, phone, updated, now
	return a
}

// bindings 汇总所有「要用外部账号」的功能。
//
// 状态不各算一套：模型看客户端是否可用，公众号看凭证是否齐全，
// 小红书/抖音复用发布面板那套登录态检测——同一个事实只在一处判断，
// 否则「账号页说已绑定、发布时说没登录」这种矛盾迟早出现。
func (s *TopicService) bindings() []BindingView {
	out := []BindingView{}

	// 模型
	aiView := BindingView{
		Key:    "ai",
		Name:   "AI 模型",
		Desc:   "拆解、写稿、素材提炼都靠它",
		Where:  "settings",
		Detail: "去设置里填接口地址和 API Key",
		Action: "setup-ai",
	}
	if c := s.client(); c != nil {
		aiView.Ready = true
		aiView.Status = "已连接"
		aiView.Detail = ai.ModelNameOf(c)
	} else {
		aiView.Status = "未绑定"
	}
	out = append(out, aiView)

	// 外部平台：公众号走接口凭证，另外两个走浏览器登录态。
	st := s.PublishStatus()
	names := map[string]string{
		"wechat":      "公众号 / 服务号",
		"xiaohongshu": "小红书",
		"douyin":      "抖音",
	}
	desc := map[string]string{
		"wechat":      "用你自己的 AppID + AppSecret，填完就能发图文和贴图",
		"xiaohongshu": "扫码登录你自己的账号，自动填好标题正文和图片",
		"douyin":      "扫码登录你自己的账号，自动上传视频和文案",
	}
	for _, p := range st.Platforms {
		b := BindingView{
			Key:    p.Key,
			Name:   names[p.Key],
			Desc:   desc[p.Key],
			Where:  "publish",
			Action: "login",
		}
		if b.Name == "" {
			b.Name = p.Name
		}
		if p.Kind == "api" {
			// 公众号走接口：填完凭证就能用，和浏览器装没装无关。
			// 没配过就让他扫码——上一版直接甩一张 AppID/AppSecret 表单，
			// 用户原话是「填写的方式太复杂」。
			b.Action = "wechat-config"
			if !p.Ready {
				b.Action = "wechat-scan"
			}
			if p.Ready {
				b.Ready = true
				b.Status = "已连接"
				b.Detail = "凭证已填，可直接发布"
			} else {
				b.Status = "未绑定"
				b.Detail = "填 AppID + AppSecret（在公众号后台拿）"
			}
			out = append(out, b)
			continue
		}
		if !st.BrowserReady {
			// 走不到这里了：浏览器随包发，除非包没解压全。
			// 留着这个分支是为了「内置目录被删」这种情况仍有一句人话，
			// 而不是让界面显示一个点了就报错的「扫码登录」。
			b.Action = "install-browser"
			b.Status = "缺浏览器"
			b.Detail = "内置浏览器不见了，重装应用能修好；也可以「复制全部」手动发"
			out = append(out, b)
			continue
		}
		if p.Ready {
			b.Action = "relogin"
			b.Ready = true
			b.Status = "已连接"
			b.Detail = "登录态还在，可直接发布"
		} else {
			b.Status = "未登录"
			b.Detail = "点「扫码登录」，在弹出窗口里扫一下就行"
		}
		out = append(out, b)
	}
	return out
}

// Bindings 给发布台的「平台账号」区块用。
//
// 和账号页同一份数据：两处各算一遍，迟早出现「账号页说已绑定、
// 发布台说没登录」的矛盾，而用户会以为是我们坏了。
func (s *TopicService) Bindings() []BindingView {
	return s.bindings()
}

// BrowserInfo 只说浏览器这一件事，给绑定区顶部那行小字用。
//
// 单独一个方法而不是塞进 Bindings：Bindings 是「每个平台一行」，
// 浏览器是四行共用的前提，混进去就得每行都重复一遍同样的信息。
//
// 不启动浏览器，只查路径——用户在发布台停留多久，这个方法就被调多少次。
func (s *TopicService) BrowserInfo() BrowserInfo {
	out := BrowserInfo{}
	ps, err := s.publisher()
	if err != nil {
		return out
	}
	p, err := ps.eng.BrowserPath()
	if err != nil {
		return out
	}
	out.Ready = true
	out.Bundled = publish.IsBundled(p)
	if out.Bundled {
		_, out.Version = publish.BundledBrowser()
	}
	return out
}
