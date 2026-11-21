package publish

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 公众号那条链路不碰浏览器，所以可以整条假掉：起一个 httptest 服务端，
// 按真实接口的形状回包，验证「凭证 → 传图 → 建草稿 → 发布 → 取链接」。
//
// 这些测试真正的价值不在"跑通"，而在失败分支：白名单、无权限、频率限制
// 这三种一定会遇到，而它们的报错文案决定了用户能不能自己解决。

// fakeWX 按路径分发假接口。
type fakeWX struct {
	t *testing.T
	// 收到的请求体，按 path 记下来，供断言
	got map[string]string
	// 上传文件的实际字节数，key 是 path + "#" + 文件名
	size map[string]int64
	// 用来模拟错误
	tokenErr     int
	tokenErrMsg  string
	draftErr     int
	draftErrMsg  string
	submitErr    int
	submitErrMsg string
	// pubStatus 是 freepublish/get 依次返回的状态码，最后一个会一直重复。
	// 真实微信是异步的：第一次查往往还是 1（发布中），过几秒才落到 0。
	pubStatus []int
	getCalls  int
}

func (f *fakeWX) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if f.got == nil {
			f.got = map[string]string{}
			f.size = map[string]int64{}
		}
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/") {
			// 文件上传要把字节数量清楚：压缩逻辑到底有没有生效，
			// 只能从这里看——假服务端是唯一知道他收到多大的地方。
			f.got[path] = "multipart:" + ct
			if mr, err := r.MultipartReader(); err == nil {
				for {
					part, err := mr.NextPart()
					if err != nil {
						break
					}
					n, _ := io.Copy(io.Discard, part)
					if part.FormName() == "media" {
						f.size[path+"#"+part.FileName()] = n
					}
				}
			}
		} else {
			body, _ := io.ReadAll(r.Body)
			f.got[path] = string(body)
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(path, "/stable_token"):
			if f.tokenErr != 0 {
				json.NewEncoder(w).Encode(map[string]any{"errcode": f.tokenErr, "errmsg": f.tokenErrMsg})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "TOKEN123", "expires_in": 7200})

		case strings.HasSuffix(path, "/media/uploadimg"):
			json.NewEncoder(w).Encode(map[string]any{"url": "https://mmbiz.qpic.cn/fake-body.png"})

		case strings.HasSuffix(path, "/material/add_material"):
			json.NewEncoder(w).Encode(map[string]any{"media_id": "MATERIAL-1"})

		case strings.HasSuffix(path, "/draft/add"):
			if f.draftErr != 0 {
				json.NewEncoder(w).Encode(map[string]any{"errcode": f.draftErr, "errmsg": f.draftErrMsg})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"media_id": "DRAFT-1"})

		case strings.HasSuffix(path, "/freepublish/submit"):
			if f.submitErr != 0 {
				json.NewEncoder(w).Encode(map[string]any{"errcode": f.submitErr, "errmsg": f.submitErrMsg})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"publish_id": "PUB-1"})

		case strings.HasSuffix(path, "/freepublish/get"):
			st := 0
			if len(f.pubStatus) > 0 {
				i := f.getCalls
				if i >= len(f.pubStatus) {
					i = len(f.pubStatus) - 1
				}
				st = f.pubStatus[i]
				f.getCalls++
			}
			out := map[string]any{"publish_status": st}
			if st == 0 {
				out["article_detail"] = map[string]any{
					"article": []map[string]string{{"url": "https://mp.weixin.qq.com/s/fake"}},
				}
			}
			json.NewEncoder(w).Encode(out)

		default:
			f.t.Errorf("假服务端不认识这个路径: %s", path)
		}
	})
}

func newFakeWX(t *testing.T) (*WeChat, *fakeWX) {
	t.Helper()
	f := &fakeWX{t: t}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return &WeChat{AppID: "wx123", AppSecret: "secret", BaseURL: srv.URL, HTTP: srv.Client()}, f
}

func tempImage(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestWeChatNewsDraft 图文消息：要对上封面素材、摘要和段落 HTML。
func TestWeChatNewsDraft(t *testing.T) {
	wx, f := newFakeWX(t)
	cover := tempImage(t, "cover.png")
	body := tempImage(t, "body.png")

	id, err := wx.CreateDraft(context.Background(), Article{
		Kind:   "news",
		Title:  "领导突然对你客气，多半是这3个信号",
		Body:   "第一，他开始把决定权收回去了。\n第二，他在给你留退路。\n第三，他在等你自己开口。\n第四，别急着高兴。",
		Digest: "领导突然客气不是好事",
		Images: []string{cover, body},
	})
	if err != nil {
		t.Fatalf("建草稿失败: %v", err)
	}
	if id != "DRAFT-1" {
		t.Fatalf("草稿 ID 不对: %q", id)
	}

	var payload struct {
		Articles []map[string]any `json:"articles"`
	}
	if err := json.Unmarshal([]byte(f.got["/cgi-bin/draft/add"]), &payload); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v", err)
	}
	if len(payload.Articles) != 1 {
		t.Fatalf("应当只发一篇，实际 %d", len(payload.Articles))
	}
	a := payload.Articles[0]
	if a["article_type"] != "news" {
		t.Errorf("article_type 应为 news，实际 %v", a["article_type"])
	}
	if a["thumb_media_id"] != "MATERIAL-1" {
		t.Errorf("封面素材 ID 不对: %v", a["thumb_media_id"])
	}
	if a["digest"] != "领导突然客气不是好事" {
		t.Errorf("摘要不对: %v", a["digest"])
	}
	content, _ := a["content"].(string)
	if !strings.Contains(content, "<p>第一，他开始把决定权收回去了。</p>") {
		t.Errorf("正文没排成段落: %s", content)
	}
	// 除封面外的图要插进正文，而不是丢掉。
	if !strings.Contains(content, "https://mmbiz.qpic.cn/fake-body.png") {
		t.Errorf("配图没插进正文: %s", content)
	}
	// 标题里不该出现 HTML 转义问题，正文里的 < > 要被转义。
	if !strings.Contains(a["title"].(string), "领导突然对你客气") {
		t.Errorf("标题被改坏了: %v", a["title"])
	}
}

// TestWeChatNewspicDraft 图片消息（贴图）：全部图片走 image_list。
func TestWeChatNewspicDraft(t *testing.T) {
	wx, f := newFakeWX(t)
	a1 := tempImage(t, "1.png")
	a2 := tempImage(t, "2.png")

	if _, err := wx.CreateDraft(context.Background(), Article{
		Kind:   "newspic",
		Title:  "三张图看懂排期",
		Body:   "今天把发布日历排满了",
		Images: []string{a1, a2},
	}); err != nil {
		t.Fatalf("建贴图草稿失败: %v", err)
	}

	var payload struct {
		Articles []map[string]any `json:"articles"`
	}
	json.Unmarshal([]byte(f.got["/cgi-bin/draft/add"]), &payload)
	a := payload.Articles[0]
	if a["article_type"] != "newspic" {
		t.Errorf("article_type 应为 newspic，实际 %v", a["article_type"])
	}
	info, _ := a["image_info"].(map[string]any)
	list, _ := info["image_list"].([]any)
	if len(list) != 2 {
		t.Errorf("应当带上 2 张图，实际 %d", len(list))
	}
	// 贴图不需要封面字段；出现了说明走错了分支。
	if _, ok := a["thumb_media_id"]; ok {
		t.Error("图片消息不该有 thumb_media_id")
	}
}

// TestWeChatWhitelistError 白名单错误要把微信看到的 IP 原样带出来。
func TestWeChatWhitelistError(t *testing.T) {
	wx, f := newFakeWX(t)
	f.tokenErr = 40164
	f.tokenErrMsg = "invalid ip 58.56.12.34 ipv6 ::ffff:58.56.12.34, not in whitelist"

	_, err := wx.Token(context.Background())
	if err == nil {
		t.Fatal("应当报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "58.56.12.34") {
		t.Errorf("报错里要带出要加白名单的 IP，实际: %s", msg)
	}
	if !strings.Contains(msg, "白名单") {
		t.Errorf("报错要告诉用户去哪儿改，实际: %s", msg)
	}
}

// TestWeChatPermissionError 未认证的号会拿到 48001，要说清原因。
func TestWeChatPermissionError(t *testing.T) {
	wx, f := newFakeWX(t)
	f.tokenErr = 48001
	f.tokenErrMsg = "api unauthorized"

	_, err := wx.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "认证") {
		t.Fatalf("应当提示需要认证号，实际 %v", err)
	}
}

// TestWeChatSecretError 凭证错时不能只说"失败"。
func TestWeChatSecretError(t *testing.T) {
	wx, f := newFakeWX(t)
	f.tokenErr = 40125
	f.tokenErrMsg = "invalid appsecret"

	_, err := wx.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "AppSecret") {
		t.Fatalf("应当指向 AppSecret，实际 %v", err)
	}
}

// TestWeChatSubmitAndURL 发布后要能把文章链接取回来（台账靠它）。
func TestWeChatSubmitAndURL(t *testing.T) {
	wx, _ := newFakeWX(t)
	publishID, err := wx.Submit(context.Background(), "DRAFT-1")
	if err != nil {
		t.Fatalf("提交发布失败: %v", err)
	}
	if publishID != "PUB-1" {
		t.Fatalf("publish_id 不对: %q", publishID)
	}
	st := wx.WaitPublish(context.Background(), publishID, 3e9)
	if st.URL != "https://mp.weixin.qq.com/s/fake" {
		t.Fatalf("没取到文章链接: %q", st.URL)
	}
	if st.Status != 0 || st.Reason != "" {
		t.Fatalf("成功时不该带失败原因: %+v", st)
	}
}

// TestWeChatPublishRejected 微信明确判失败时，不能报成"链接还没生成"。
//
// publish_status 4 = 平台审核不通过。这是真实会发生的（尤其带外链、
// 医疗金融类内容），而旧实现只返回空链接，界面上显示的是
// "已发布，链接还在生成"——等于告诉用户发出去了。
func TestWeChatPublishRejected(t *testing.T) {
	wx, f := newFakeWX(t)
	f.pubStatus = []int{4}

	st := wx.WaitPublish(context.Background(), "PUB-1", 3e9)
	if st.URL != "" {
		t.Fatalf("没发成功却有链接: %q", st.URL)
	}
	if st.Status != 4 {
		t.Fatalf("状态码没带回来: %+v", st)
	}
	if !strings.Contains(st.Reason, "审核") {
		t.Fatalf("失败原因要能指导用户：%q", st.Reason)
	}
}

// TestWeChatPublishPending 一直"发布中"时，返回的是「还没结果」而不是失败。
//
// 微信生成页面有时要几十秒，这时候说失败是冤枉它，说成功是骗人。
func TestWeChatPublishPending(t *testing.T) {
	wx, f := newFakeWX(t)
	f.pubStatus = []int{1}

	st := wx.WaitPublish(context.Background(), "PUB-1", 1500*time.Millisecond)
	if st.Status != 1 {
		t.Fatalf("应当是发布中，实际 %+v", st)
	}
	if st.URL != "" || st.Reason != "" {
		t.Fatalf("发布中不该有链接也不该有失败原因: %+v", st)
	}
	if f.getCalls < 2 {
		t.Errorf("应当轮询多次，实际只查了 %d 次", f.getCalls)
	}
}

// TestWeChatPublishSettles 第一次查是发布中、第二次成功——要接着查下去。
func TestWeChatPublishSettles(t *testing.T) {
	wx, f := newFakeWX(t)
	f.pubStatus = []int{1, 0}

	st := wx.WaitPublish(context.Background(), "PUB-1", 10e9)
	if st.URL != "https://mp.weixin.qq.com/s/fake" {
		t.Fatalf("第二次就成功了，没接着查: %+v", st)
	}
}

// TestWeChatNotConfigured 没填凭证时不要发网络请求。
func TestWeChatNotConfigured(t *testing.T) {
	wx := &WeChat{}
	if _, err := wx.Token(context.Background()); err == nil {
		t.Fatal("没凭证应当直接报错")
	}
	if wx.Configured() {
		t.Error("Configured 应为 false")
	}
}
