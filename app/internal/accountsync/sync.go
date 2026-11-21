// Package accountsync 把本机账号资料推给服务端、把服务端的改动拉回来。
//
// 为什么单独一个包、而不是写在 service_account.go 里：这段逻辑有三条
// 容易写错的边界——服务端还没实现、服务端比本地新、网络不通。它们都能
// 用 httptest 验，不需要开真浏览器、不需要真服务器。放在根包里就只能
// 靠人点界面来验，那种「验过了」不算数。
//
// 协议只有一个接口：
//
//	POST {base}/account/sync
//	请求 {"code","name","phone","updatedAt"}   code 是机器码，唯一身份
//	响应 {"account":{"code","name","phone","updatedAt"}}
//
// 合并权在服务端：它拿到请求就能比较 updatedAt，返回的 account 就是
// 最终事实。客户端不做「谁更新」的判断——两边各判一套，遇到时钟偏差
// 必然出现「每台机器都认为自己对」的循环覆盖。
package accountsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Account 是同步的单位。字段与 store.Account 对齐，但不复用：
// 那一份带着数据库列名的味道（ID/SyncedAt），这一份只描述线上协议。
type Account struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// 同步结果。Action 是给界面和日志看的一句话，不是错误。
const (
	ActionSkipped = "skipped" // 没配服务端，或服务端还没这个接口
	ActionSame    = "same"    // 两边一致
	ActionPulled  = "pulled"  // 服务端更新，已拉回
	ActionPushed  = "pushed"  // 本地更新，已推上
)

// Outcome 是一次同步的结果。
//
// Err 非空不代表失败到不能用：本地资料照样好好在库里，只是这次没同步上。
// 调用方该做的是「记一下、照常用」，而不是弹错误框拦人。
type Outcome struct {
	Account Account
	Action  string
	Err     error
}

// Client 是同步客户端。零值不可用，至少要有 BaseURL。
type Client struct {
	BaseURL string
	Token   string
	// Timeout 默认 4 秒：这是启动路径上的请求，卡住等于让用户看着窗口发呆。
	Timeout time.Duration
	HTTP    *http.Client
}

// Enabled 判断是否配了服务端。
//
// 没配就是纯本地模式——这是默认状态，也是绝大多数装机的情况。
// 判断放在这里而不是调用方，是因为「要不要同步」只有一个答案来源。
func (c Client) Enabled() bool { return strings.TrimSpace(c.BaseURL) != "" }

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	t := c.Timeout
	if t <= 0 {
		t = 4 * time.Second
	}
	return &http.Client{Timeout: t}
}

type syncReq struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	UpdatedAt string `json:"updatedAt"`
	Client    string `json:"client,omitempty"`
}

type syncResp struct {
	Account *struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Phone     string `json:"phone"`
		UpdatedAt string `json:"updatedAt"`
	} `json:"account"`
}

// Sync 跑一次同步。永不返回「致命错误」——所有失败都装在 Outcome.Err 里。
func (c Client) Sync(ctx context.Context, local Account) Outcome {
	out := Outcome{Account: local, Action: ActionSkipped}
	if !c.Enabled() {
		return out
	}

	body := syncReq{
		Code:   local.Code,
		Name:   local.Name,
		Phone:  local.Phone,
		Client: "desktop",
	}
	if !local.UpdatedAt.IsZero() {
		body.UpdatedAt = local.UpdatedAt.UTC().Format(time.RFC3339)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		out.Err = err
		return out
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/account/sync"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		out.Err = err
		return out
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		out.Err = fmt.Errorf("连不上同步服务：%w", err)
		return out
	}
	defer resp.Body.Close()

	// 404 / 501 是「服务端还没做这个接口」，不是故障。
	// 把它当错误报出来，用户会以为是自己配错了地址，然后去改一个本来对的配置。
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		return out
	}
	if resp.StatusCode != http.StatusOK {
		out.Err = fmt.Errorf("同步服务返回 %d", resp.StatusCode)
		return out
	}

	var parsed syncResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		out.Err = fmt.Errorf("同步服务返回的不是合法 json：%w", err)
		return out
	}
	if parsed.Account == nil {
		// 服务端认了这个请求但没给账号：视为「已登记、无变更」。
		out.Action = ActionSame
		return out
	}

	remote := Account{
		Code:  strings.TrimSpace(parsed.Account.Code),
		Name:  strings.TrimSpace(parsed.Account.Name),
		Phone: strings.TrimSpace(parsed.Account.Phone),
	}
	remote.UpdatedAt, _ = time.Parse(time.RFC3339, strings.TrimSpace(parsed.Account.UpdatedAt))
	if remote.Code == "" {
		remote.Code = local.Code
	}

	switch {
	case remote.UpdatedAt.After(local.UpdatedAt):
		out.Account = remote
		out.Action = ActionPulled
	case local.UpdatedAt.After(remote.UpdatedAt):
		// 本地更新：这次推送已经把值给服务端了，服务端返回的是旧的。
		// 不回写本地，否则刚改完的名字会被服务端的旧值盖回去。
		out.Action = ActionPushed
	default:
		// 时间一样但内容不同（服务端没实现比较）：以本地为准，别覆盖用户刚填的。
		out.Action = ActionSame
	}
	return out
}

// ErrDisabled 只在需要明确区分「没配」时用。
var ErrDisabled = errors.New("没有配置同步服务地址")
