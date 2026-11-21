package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 微信公众号走官方接口，不用浏览器。
//
// 为什么不和另外两个平台一样驱动网页：公众号后台的编辑器在 iframe 里，
// 封面、原创、摘要一堆必填项，页面结构还常改；而它偏偏是所有平台里
// 唯一有公开接口的。有接口就先用接口——这是稳定性差别，不是偏好。
//
// 代价是要认证号 + AppID/Secret + IP 白名单。这三样缺一样都会失败，
// 所以每一种失败都翻译成了「你要去后台做什么」，而不是抛一个 errcode。

// WeChat 是公众号接口客户端。
type WeChat struct {
	AppID     string
	AppSecret string
	HTTP      *http.Client
	// BaseURL 只在测试里替换成假服务端；留空就用微信的正式地址。
	BaseURL string

	mu    sync.Mutex
	token string
	exp   time.Time
}

const wxAPI = "https://api.weixin.qq.com"

// WeChatError 是接口返回的业务错误。
type WeChatError struct {
	Code int
	Msg  string
	IP   string // 仅 40164 有：微信看到的调用方 IP
}

func (e *WeChatError) Error() string {
	switch e.Code {
	case 40164:
		return fmt.Sprintf("这个 IP 不在公众号的接口白名单里（微信看到的 IP 是 %s）。"+
			"到公众号后台 → 设置与开发 → 基本配置 → IP 白名单里加上它，等一分钟再试", e.IP)
	case 40001, 40014, 42001:
		return "AppSecret 不对或已失效。到公众号后台重置一下 AppSecret，重新填进来"
	case 48001:
		return "这个号没有接口权限。群发与发布接口需要已认证的服务号或订阅号"
	case 45009:
		return "今天的发布次数用完了（公众号对发布频率有硬限制），明天再发"
	case 45002, 45003:
		return "内容超限了：" + e.Msg
	case 40007, 40013:
		return "AppID 不对，检查一下（不是微信号、也不是原始 ID）"
	case 40125:
		return "AppSecret 无效，到公众号后台重新生成一个"
	default:
		return fmt.Sprintf("公众号接口返回错误 %d：%s", e.Code, e.Msg)
	}
}

// Token 取接口调用凭证。
//
// 用 stable_token 而不是 cgi-bin/token：后者每调一次就把上一个 token 作废，
// 用户如果在别处（比如自己的服务器）也在用同一个号，两边会互相踢掉。
func (w *WeChat) Token(ctx context.Context) (string, error) {
	w.mu.Lock()
	if w.token != "" && time.Now().Before(w.exp) {
		t := w.token
		w.mu.Unlock()
		return t, nil
	}
	w.mu.Unlock()

	if strings.TrimSpace(w.AppID) == "" || strings.TrimSpace(w.AppSecret) == "" {
		return "", fmt.Errorf("还没填公众号的 AppID 和 AppSecret")
	}

	body, _ := json.Marshal(map[string]any{
		"grant_type": "client_credential",
		"appid":      w.AppID,
		"secret":     w.AppSecret,
	})
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Errcode     int    `json:"errcode"`
		Errmsg      string `json:"errmsg"`
	}
	if err := w.postJSON(ctx, w.base()+"/cgi-bin/stable_token", body, &out); err != nil {
		return "", err
	}
	if out.Errcode != 0 {
		return "", &WeChatError{Code: out.Errcode, Msg: out.Errmsg, IP: ipFromMsg(out.Errmsg)}
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("公众号没有返回 access_token，检查 AppID 和 AppSecret")
	}

	w.mu.Lock()
	w.token = out.AccessToken
	// 提前 5 分钟过期，避免卡在边界上拿到一个刚失效的 token。
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 7200 * time.Second
	}
	w.exp = time.Now().Add(ttl - 5*time.Minute)
	w.mu.Unlock()
	return out.AccessToken, nil
}

// UploadContentImage 上传正文里的配图，返回可直接写进 HTML 的 URL。
//
// 用 uploadimg 而不是永久素材：正文配图不需要进素材库，
// 用户发一次就完了，往素材库里塞图会让后台越用越乱。
func (w *WeChat) UploadContentImage(ctx context.Context, path string) (string, error) {
	file, cleanup, err := fitImage(path, maxContentImage)
	if err != nil {
		return "", err
	}
	if cleanup != nil {
		defer cleanup()
	}

	var out struct {
		URL     string `json:"url"`
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := w.postFile(ctx, "/cgi-bin/media/uploadimg", "media", file, &out); err != nil {
		return "", err
	}
	if out.Errcode != 0 {
		return "", &WeChatError{Code: out.Errcode, Msg: out.Errmsg, IP: ipFromMsg(out.Errmsg)}
	}
	if out.URL == "" {
		return "", fmt.Errorf("图片上传成功但没拿到地址")
	}
	return out.URL, nil
}

// UploadCoverImage 上传封面图，返回永久素材的 media_id。
//
// 图文消息必须有封面，而封面只能引用永久素材（或已有素材库里的图），
// 所以这里走 add_material。
func (w *WeChat) UploadCoverImage(ctx context.Context, path string) (string, error) {
	file, cleanup, err := fitImage(path, maxMaterialImage)
	if err != nil {
		return "", err
	}
	if cleanup != nil {
		defer cleanup()
	}

	var out struct {
		MediaID string `json:"media_id"`
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := w.postFile(ctx, "/cgi-bin/material/add_material?type=image", "media", file, &out); err != nil {
		return "", err
	}
	if out.Errcode != 0 {
		return "", &WeChatError{Code: out.Errcode, Msg: out.Errmsg, IP: ipFromMsg(out.Errmsg)}
	}
	if out.MediaID == "" {
		return "", fmt.Errorf("封面上传成功但没拿到素材 ID")
	}
	return out.MediaID, nil
}

// Article 是一篇要放进公众号草稿的东西。
type Article struct {
	Kind   string // news=图文消息，newspic=图片消息（贴图）
	Title  string
	Body   string
	Digest string
	Images []string // 本地图片路径，第一张当封面
}

// CreateDraft 建一条草稿，返回草稿的 media_id。
//
// 先建草稿再发布，而不是直接群发：草稿是可见、可改、可删的中间态，
// 出了问题用户能自己去后台改，不用重新跑一遍程序。
func (w *WeChat) CreateDraft(ctx context.Context, a Article) (string, error) {
	token, err := w.Token(ctx)
	if err != nil {
		return "", err
	}
	if len(a.Images) == 0 {
		return "", fmt.Errorf("公众号必须有图：图文消息要封面，图片消息要至少一张")
	}

	var art map[string]any
	switch a.Kind {
	case "newspic":
		// 图片消息（贴图）：图片走永久素材，正文是一句话说明。
		//
		// 每张图都会进「永久素材」库，而永久图片素材有数量上限（服务号 5000 张），
		// 不会自动清理。所以这里不省事地把图塞进素材库，而是如实告诉调用方
		// 这回事——发得多了要自己去后台删，否则迟早撞天花板。
		ids := make([]map[string]string, 0, len(a.Images))
		for _, p := range a.Images {
			id, err := w.UploadCoverImage(ctx, p)
			if err != nil {
				return "", err
			}
			ids = append(ids, map[string]string{"image_media_id": id})
		}
		art = map[string]any{
			"article_type":          "newspic",
			"title":                 clipTitle(a.Title),
			"content":               paragraphHTML(a.Body),
			"image_info":            map[string]any{"image_list": ids},
			"need_open_comment":     1,
			"only_fans_can_comment": 0,
		}
	default:
		cover, err := w.UploadCoverImage(ctx, a.Images[0])
		if err != nil {
			return "", err
		}
		html, err := w.buildNewsHTML(ctx, a)
		if err != nil {
			return "", err
		}
		art = map[string]any{
			"article_type":          "news",
			"title":                 clipTitle(a.Title),
			"author":                "",
			"digest":                clipRunes(a.Digest, 120),
			"content":               html,
			"thumb_media_id":        cover,
			"need_open_comment":     1,
			"only_fans_can_comment": 0,
		}
	}

	payload, _ := json.Marshal(map[string]any{"articles": []any{art}})
	var out struct {
		MediaID string `json:"media_id"`
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := w.postJSON(ctx, w.base()+"/cgi-bin/draft/add?access_token="+token, payload, &out); err != nil {
		return "", err
	}
	if out.Errcode != 0 {
		return "", &WeChatError{Code: out.Errcode, Msg: out.Errmsg, IP: ipFromMsg(out.Errmsg)}
	}
	if out.MediaID == "" {
		return "", fmt.Errorf("草稿建好了但没拿到 ID")
	}
	return out.MediaID, nil
}

// Submit 把草稿发布出去。
//
// 用 freepublish（发布）而不是群发（mass/send）：发布不占每日群发次数，
// 也不会给粉丝推送消息。多数创作者写的是"内容"，不是"通知"。
func (w *WeChat) Submit(ctx context.Context, mediaID string) (string, error) {
	token, err := w.Token(ctx)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]string{"media_id": mediaID})
	var out struct {
		PublishID string `json:"publish_id"`
		Errcode   int    `json:"errcode"`
		Errmsg    string `json:"errmsg"`
	}
	if err := w.postJSON(ctx, w.base()+"/cgi-bin/freepublish/submit?access_token="+token, body, &out); err != nil {
		return "", err
	}
	if out.Errcode != 0 {
		return "", &WeChatError{Code: out.Errcode, Msg: out.Errmsg, IP: ipFromMsg(out.Errmsg)}
	}
	return out.PublishID, nil
}

// PublishState 是一次发布的结果。
//
// 单独建一个结构而不是只返回链接，是因为「拿到链接」和「发成功」不是一回事：
// 微信的 publish_status 有五种取值，0 之外都不该当成功。只返回 string 的话，
// 发布被原创校验或平台审核拦下来时，界面上只能显示"链接还没生成"——
// 用户会以为发出去了，过两天才发现粉丝根本看不到。
type PublishState struct {
	URL    string // 文章链接，没拿到就是空
	Status int    // 微信的 publish_status：0 成功 / 1 发布中 / 2-4 失败
	Reason string // 人话版的失败原因
}

// 发布状态码到人话的映射。
// 微信文档里只写了数字含义，具体该让用户怎么办是我们的事：
// 2 和 4 都是"内容层面"的问题（原创声明、审核），改稿就能过；
// 3 是"提交层面"的问题，多半要重发。
var publishStatusText = map[int]string{
	2: "原创校验没过——正文可能和已发过的文章重复度太高，改一改再发",
	3: "发布失败，微信没给出具体原因，去后台草稿箱重新发一次",
	4: "平台审核没通过，检查正文里有没有违规内容",
}

// WaitPublish 等发布完成并取回文章链接。
//
// 发布是异步的：submit 只返回一个 publish_id，微信要生成页面才给出链接。
// 所以这里必须轮询，而且必须把"还在发"和"发失败"分开——混在一起就变成
// 拿不到链接一律报成功。
func (w *WeChat) WaitPublish(ctx context.Context, publishID string, timeout time.Duration) PublishState {
	token, err := w.Token(ctx)
	if err != nil {
		return PublishState{Status: -1, Reason: err.Error()}
	}
	deadline := time.Now().Add(timeout)
	st := PublishState{Status: 1}
	for {
		body, _ := json.Marshal(map[string]string{"publish_id": publishID})
		var out struct {
			PublishStatus int `json:"publish_status"`
			ArticleDetail struct {
				Article []struct {
					URL string `json:"url"`
				} `json:"article"`
			} `json:"article_detail"`
			Errcode int    `json:"errcode"`
			Errmsg  string `json:"errmsg"`
		}
		if err := w.postJSON(ctx, w.base()+"/cgi-bin/freepublish/get?access_token="+token, body, &out); err == nil {
			if out.Errcode != 0 {
				// 查状态本身失败了（token 过期、参数错）——继续等也不会有结果。
				return PublishState{Status: -1, Reason: (&WeChatError{Code: out.Errcode, Msg: out.Errmsg}).Error()}
			}
			st.Status = out.PublishStatus
			if len(out.ArticleDetail.Article) > 0 {
				st.URL = out.ArticleDetail.Article[0].URL
			}
			// 成功了就立刻返回；5/6 是"发布过又被删了"，链接还在，也当成功。
			if out.PublishStatus == 0 || ((out.PublishStatus == 5 || out.PublishStatus == 6) && st.URL != "") {
				return st
			}
			if reason, bad := publishStatusText[out.PublishStatus]; bad {
				st.Reason = reason
				return st
			}
		}
		if time.Now().After(deadline) {
			return st
		}
		select {
		case <-ctx.Done():
			return st
		case <-time.After(2 * time.Second):
		}
	}
}

// buildNewsHTML 把纯文本正文排成公众号能吃的 HTML，并把配图插进去。
//
// 插在段落之间而不是堆在末尾：图片是用来断开长文的，
// 全放最后等于没放。
func (w *WeChat) buildNewsHTML(ctx context.Context, a Article) (string, error) {
	paras := strings.Split(strings.TrimSpace(a.Body), "\n")
	var b strings.Builder
	imgIdx := 1 // 第 0 张是封面
	every := 3
	if len(paras) < 6 {
		every = 2
	}
	for i, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b.WriteString("<p>" + escapeHTML(p) + "</p>")
		if i > 0 && i%every == 0 && imgIdx < len(a.Images) {
			u, err := w.UploadContentImage(ctx, a.Images[imgIdx])
			if err != nil {
				return "", err
			}
			b.WriteString(`<p><img src="` + u + `" style="max-width:100%"/></p>`)
			imgIdx++
		}
	}
	// 正文太短就一张都没插进去，那就补在后面，别让用户选的图白选。
	for ; imgIdx < len(a.Images); imgIdx++ {
		u, err := w.UploadContentImage(ctx, a.Images[imgIdx])
		if err != nil {
			return "", err
		}
		b.WriteString(`<p><img src="` + u + `" style="max-width:100%"/></p>`)
	}
	return b.String(), nil
}

/* ---------- 请求工具 ---------- */

// base 返回接口根地址。
func (w *WeChat) base() string {
	if strings.TrimSpace(w.BaseURL) != "" {
		return strings.TrimRight(w.BaseURL, "/")
	}
	return wxAPI
}

func (w *WeChat) client() *http.Client {
	if w.HTTP != nil {
		return w.HTTP
	}
	return &http.Client{Timeout: 40 * time.Second}
}

func (w *WeChat) postJSON(ctx context.Context, url string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return w.do(req, out)
}

func (w *WeChat) postFile(ctx context.Context, path, field, file string, out any) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("打不开文件 %s: %w", filepath.Base(file), err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filepath.Base(file))
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return err
	}
	mw.Close()

	token, err := w.Token(ctx)
	if err != nil {
		return err
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	url := w.base() + path + sep + "access_token=" + token

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return w.do(req, out)
}

func (w *WeChat) do(req *http.Request, out any) error {
	res, err := w.client().Do(req)
	if err != nil {
		return fmt.Errorf("连不上公众号接口（网络问题）: %w", err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("公众号返回的不是 JSON（%d）: %s", res.StatusCode, clipRunes(string(data), 120))
	}
	return nil
}

// ipFromMsg 从「invalid ip 1.2.3.4 ipv6 ::ffff:1.2.3.4」里抠出 IP。
//
// 顺手抠出来是为了让用户能直接复制去后台加白名单——
// 让他自己去百度「我的 IP」既慢又可能查到另一个（运营商出口不同）。
func ipFromMsg(msg string) string {
	fields := strings.Fields(msg)
	for i, f := range fields {
		if f == "ip" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], ",.;")
		}
	}
	return ""
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// paragraphHTML 把纯文本变成最简单的段落 HTML（图片消息的正文用）。
func paragraphHTML(body string) string {
	var b strings.Builder
	for _, p := range strings.Split(strings.TrimSpace(body), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>" + escapeHTML(p) + "</p>")
		}
	}
	return b.String()
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// clipTitle 把标题压到微信允许的 32 字以内。
//
// 这个上限是接口硬限制，超了就是 45003 一条看不懂的报错。截断比报错好：
// 小红书那套标题习惯写到 20 字上下，正常不会触发，触发了说明用户抄了
// 公众号原文的长标题——砍掉尾巴他还能改，报错他就只能自己猜。
func clipTitle(s string) string {
	const max = 32
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	// 用省略号占最后一个字，保证结果不超 32。
	return string(r[:max-1]) + "…"
}

// 把「配置是否完整」单独提出来，界面要据此决定显示"去配置"还是"可以发"。
func (w *WeChat) Configured() bool {
	return strings.TrimSpace(w.AppID) != "" && strings.TrimSpace(w.AppSecret) != ""
}
