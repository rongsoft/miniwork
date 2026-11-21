package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"rongsoft/miniwork/app/internal/config"
)

// 公众号绑定：扫码替代手抄 AppID。
//
// 之前用户要做三件事：去后台翻 AppID、点重置生成 AppSecret、把本机出口 IP
// 加进白名单。三件事里只有「点重置」必须他本人做（重置会让旧密钥立刻失效，
// 用户可能还有别的系统在用同一个号），另外两件机器都能替他做。
//
// 所以这里把两件省了：AppID 从登录后的后台页面读，出口 IP 直接查出来给他
// 复制。AppSecret 仍要他自己生成一次——但只在需要时点一下，不用再找入口。

// WeChatScanView 是「扫码绑定」面板要显示的状态。
type WeChatScanView struct {
	Authed  bool   `json:"authed"`  // 后台已登录
	AppID   string `json:"appId"`   // 读到的 AppID，没读到是空串
	Saved   string `json:"saved"`   // 库里已存的 AppID
	HasSec  bool   `json:"hasSec"`  // 已经存过 AppSecret
	Egress  string `json:"egress"`  // 本机出口 IP，白名单要填它
	Console string `json:"console"` // 后台基本配置页说明用的地址
	Note    string `json:"note"`    // 给用户的一句话，随状态变
}

// WeChatScanStart 打开公众号后台，并顺手把出口 IP 查出来。
//
// 不阻塞等登录：扫码要人动手，方法立刻返回，界面照着轮询 WeChatScanStatus。
func (s *TopicService) WeChatScanStart() (WeChatScanView, error) {
	ps, err := s.publisher()
	if err != nil {
		return WeChatScanView{}, err
	}
	// 引擎只有一个浏览器会话。正在发别的平台时把它抢过来，发布
	// 会跑到一半页面跳走——那种失败用户根本看不出是我们干的。
	if ps.busy() {
		return WeChatScanView{}, errors.New("有稿子正在发，等它发完再绑定")
	}
	if err := ps.eng.WeChatOpenConsole(); err != nil {
		return WeChatScanView{}, err
	}
	v := s.wechatScanBase()
	v.Egress = s.egressIP()
	v.Note = "用微信扫页面上的二维码。登录后这里会自动把 AppID 填好。"
	return v, nil
}

// WeChatScanStatus 是轮询用的：登录了没、AppID 读到没。
//
// 每次轮询都会真的去问一次页面，不做缓存——用户扫完码的那一秒，
// 他要看见的就是状态立刻变过来。
func (s *TopicService) WeChatScanStatus() WeChatScanView {
	v := s.wechatScanBase()
	if s.pub == nil || s.pub.eng == nil {
		v.Note = "还没打开平台页面"
		return v
	}

	ps := s.pub
	if ps.busy() {
		v.Note = "有稿子正在发，等它发完这里会自动继续"
		return v
	}
	v.Authed = ps.eng.Authed("wechat")
	if !v.Authed {
		v.Note = "等扫码中…"
		return v
	}

	if id, err := ps.eng.WeChatAppID(); err == nil && id != "" {
		v.AppID = id
		if v.Saved == "" {
			v.Note = "AppID 已读到，填好 AppSecret 就能用了"
		} else if v.Saved == id {
			v.Note = "这个号已经在用了"
		} else {
			v.Note = "后台这个号和已存的不一样，确认后再保存"
		}
		return v
	}

	// 登录了但没读到：多半是页面还在加载，或者账号没有「基本配置」权限
	// （未认证的号进不去那一页）。两句话分开说，别让用户以为程序坏了。
	if v.Saved == "" {
		v.Note = "已登录，但没读到 AppID。可以自己去后台「设置与开发 → 基本配置」复制过来。"
	} else {
		v.Note = "已登录。没读到 AppID 也没关系，库里已经存着 " + v.Saved
	}
	return v
}

// wechatScanBase 是两种返回共用的那部分事实。
func (s *TopicService) wechatScanBase() WeChatScanView {
	cfg, _ := config.Load()
	return WeChatScanView{
		Saved:  strings.TrimSpace(cfg.WeChat.AppID),
		HasSec: strings.TrimSpace(cfg.WeChat.AppSecret) != "",
		Console: "https://mp.weixin.qq.com/ → 设置与开发 → 基本配置",
	}
}

// egressIP 查本机出口 IP，也就是微信服务器看到的那个地址。
//
// 问的是第三方（ipify），不是微信：要微信自己说出这个 IP，得先调用一次
// 接口并让它报 40164——那意味着用户得先失败一次。查一次更快，而且
// 用户去后台加白名单时看到的就是这个值。
//
// 查不到返回空串：白名单这一步是可选的（有些号没开白名单限制），
// 不能因为一个公共接口不通就让整个绑定流程卡住。
var egressOnce sync.Once
var egressVal string

func (s *TopicService) egressIP() string {
	egressOnce.Do(func() {
		cli := &http.Client{Timeout: 3 * time.Second}
		resp, err := cli.Get("https://api.ipify.org")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		if err != nil {
			return
		}
		v := strings.TrimSpace(string(raw))
		if len(v) >= 7 && len(v) <= 45 { // IPv4 最短 7 位，IPv6 最长 45
			egressVal = v
		}
	})
	return egressVal
}
