package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 这一组测试跑的是「服务层 + 公众号接口」的整条链路，只把微信那一端换成
// 本地假服务端。单元测试（internal/publish）验的是报文对不对，这里验的是
// 中间那层：任务状态、日志、台账落不落库——也就是用户真正看到的东西。
//
// 为什么必须验：公众号是唯一走接口的平台，链路一断就是全套失败，
// 而它的失败方式很隐蔽——submit 返回 errcode=0 只代表"提交成功"，
// 真正的结果要去 freepublish/get 轮询。旧实现把"查不到链接"当成了成功。

type fakeWeChat struct {
	*httptest.Server
	mu          sync.Mutex
	paths       []string
	draft       map[string]any
	materials   int
	pubStatus   int // freepublish/get 返回的状态
	tokenCalls  int
	submitCalls int
}

func newFakeWeChat(t *testing.T) *fakeWeChat {
	t.Helper()
	f := &fakeWeChat{pubStatus: 0}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/stable_token"):
			f.mu.Lock()
			f.tokenCalls++
			f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"access_token": "TOKEN", "expires_in": 7200})

		case strings.HasSuffix(r.URL.Path, "/material/add_material"):
			f.mu.Lock()
			f.materials++
			f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"media_id": "MAT-1"})

		case strings.HasSuffix(r.URL.Path, "/media/uploadimg"):
			json.NewEncoder(w).Encode(map[string]any{"url": "https://mmbiz.qpic.cn/x.png"})

		case strings.HasSuffix(r.URL.Path, "/draft/add"):
			var payload map[string]any
			json.Unmarshal(body, &payload)
			f.mu.Lock()
			f.draft = payload
			f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"media_id": "DRAFT-1"})

		case strings.HasSuffix(r.URL.Path, "/freepublish/submit"):
			f.mu.Lock()
			f.submitCalls++
			f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"publish_id": "PUB-1"})

		case strings.HasSuffix(r.URL.Path, "/freepublish/get"):
			f.mu.Lock()
			st := f.pubStatus
			f.mu.Unlock()
			out := map[string]any{"publish_status": st}
			if st == 0 {
				out["article_detail"] = map[string]any{
					"article": []map[string]string{{"url": "https://mp.weixin.qq.com/s/fake-note"}},
				}
			}
			json.NewEncoder(w).Encode(out)

		default:
			http.Error(w, "unknown path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// seedDraft 造一条选题和一稿，返回两个 ID。
func seedDraft(t *testing.T, svc *TopicService) (topicID, draftID string) {
	t.Helper()
	if _, err := svc.Run("职场", ""); err != nil {
		t.Fatalf("取选题失败: %v", err)
	}
	topics, err := svc.List("", 5)
	if err != nil || len(topics) == 0 {
		t.Fatalf("没有选题可用: %v", err)
	}
	topicID = topics[0].ID
	// 走真实的写稿路径（而不是直接塞一条 DraftInput）：稿子的 id、
	// 平台、格式都是 Write 生成的，绕过它测出来的东西跟实际不一样。
	d, err := svc.Write(topicID, "wechat", "long", "", nil)
	if err != nil {
		t.Fatalf("写稿失败: %v", err)
	}
	edited, err := svc.SaveDraft(DraftInput{
		ID:       d.ID,
		TopicID:  topicID,
		Platform: "wechat",
		Format:   "long",
		Title:    "三张图看懂排期",
		Body:     "今天把发布日历排满了\n明天开始按计划走",
	})
	if err != nil {
		t.Fatalf("存稿失败: %v", err)
	}
	return topicID, edited.ID
}

// waitJob 等后台发布任务收尾。
func waitJob(t *testing.T, svc *TopicService) *PublishJob {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if j := svc.PublishStatus().Job; j != nil && j.Done {
			return j
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("发布任务 20 秒没结束，卡在: %+v", svc.PublishStatus().Job)
	return nil
}

// pngFile 造一张小 PNG。体积没超限，压缩逻辑不会介入，路径原样上传。
func pngFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestWeChatNewspicPublish 贴图（图片消息）整条链路，发成功要落台账。
func TestWeChatNewspicPublish(t *testing.T) {
	f := newFakeWeChat(t)
	t.Setenv("MINIWORK_WECHAT_APPID", "wx-test")
	t.Setenv("MINIWORK_WECHAT_SECRET", "secret-test")
	t.Setenv("MW_WECHAT_API", f.URL)

	svc := mkService(t)
	topicID, draftID := seedDraft(t, svc)
	imgs := []string{pngFile(t, "1.png"), pngFile(t, "2.png"), pngFile(t, "3.png")}

	if err := svc.StartPublish(AutoPublishInput{
		TopicID:    topicID,
		DraftID:    draftID,
		Platform:   "wechat",
		Mode:       "newspic",
		Images:     imgs,
		AutoSubmit: true,
	}); err != nil {
		t.Fatalf("起任务失败: %v", err)
	}

	job := waitJob(t, svc)
	if !job.OK {
		t.Fatalf("应当发成功，实际 %+v", job)
	}
	if job.URL != "https://mp.weixin.qq.com/s/fake-note" {
		t.Fatalf("没取回文章链接: %q", job.URL)
	}

	// 报文核对：三张图都进 image_list，首图即封面，评论要开着。
	f.mu.Lock()
	draft := f.draft
	materials := f.materials
	f.mu.Unlock()

	arts, _ := draft["articles"].([]any)
	if len(arts) != 1 {
		t.Fatalf("只该发一篇，实际 %d", len(arts))
	}
	a := arts[0].(map[string]any)
	if a["article_type"] != "newspic" {
		t.Fatalf("形态错了: %v", a["article_type"])
	}
	info, _ := a["image_info"].(map[string]any)
	list, _ := info["image_list"].([]any)
	if len(list) != 3 {
		t.Fatalf("三张图应当全进 image_list，实际 %d", len(list))
	}
	if a["need_open_comment"] != float64(1) {
		t.Errorf("贴图应当打开评论，实际 %v", a["need_open_comment"])
	}
	if materials != 3 {
		t.Errorf("每张图都要传一次永久素材，实际 %d 次", materials)
	}

	// 台账要有一条：这是「发成功了」唯一的可核对凭据。
	board, err := svc.PublishBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Records) != 1 {
		t.Fatalf("台账应当记 1 条，实际 %d", len(board.Records))
	}
	if board.Records[0].URL != job.URL {
		t.Errorf("台账里的链接不对: %q", board.Records[0].URL)
	}
}

// TestWeChatPublishRejectedNoRecord 微信判失败时不能记成功、不能落台账。
//
// 这条是上一版的真实缺陷：publish_status=4（审核不通过）时旧实现
// 只拿到空链接，界面上写的是「已发布，链接还在生成」，台账还会多一条
// 不存在的文章。用户要等粉丝说"没看到"才会发现。
func TestWeChatPublishRejectedNoRecord(t *testing.T) {
	f := newFakeWeChat(t)
	f.pubStatus = 4
	t.Setenv("MINIWORK_WECHAT_APPID", "wx-test")
	t.Setenv("MINIWORK_WECHAT_SECRET", "secret-test")
	t.Setenv("MW_WECHAT_API", f.URL)

	svc := mkService(t)
	topicID, draftID := seedDraft(t, svc)

	if err := svc.StartPublish(AutoPublishInput{
		TopicID:    topicID,
		DraftID:    draftID,
		Platform:   "wechat",
		Mode:       "newspic",
		Images:     []string{pngFile(t, "1.png")},
		AutoSubmit: true,
	}); err != nil {
		t.Fatalf("起任务失败: %v", err)
	}

	job := waitJob(t, svc)
	if job.OK {
		t.Fatalf("审核没过却报成功: %+v", job)
	}
	if !strings.Contains(job.Message, "审核") {
		t.Fatalf("失败原因要能指导用户，实际 %q", job.Message)
	}

	board, err := svc.PublishBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Records) != 0 {
		t.Fatalf("没发成功却记了台账: %+v", board.Records)
	}
}

// TestWeChatDraftOnly 不勾「自动发布」时，只建草稿不提交。
//
// 这是默认行为：草稿是可见可改的中间态，用户到后台确认成本极低，
// 而误发一条不可撤回的内容成本极高。
func TestWeChatDraftOnly(t *testing.T) {
	f := newFakeWeChat(t)
	t.Setenv("MINIWORK_WECHAT_APPID", "wx-test")
	t.Setenv("MINIWORK_WECHAT_SECRET", "secret-test")
	t.Setenv("MW_WECHAT_API", f.URL)

	svc := mkService(t)
	topicID, draftID := seedDraft(t, svc)

	if err := svc.StartPublish(AutoPublishInput{
		TopicID:  topicID,
		DraftID:  draftID,
		Platform: "wechat",
		Mode:     "news",
		Images:   []string{pngFile(t, "cover.png")},
	}); err != nil {
		t.Fatalf("起任务失败: %v", err)
	}

	job := waitJob(t, svc)
	if job.OK || !job.Manual {
		t.Fatalf("应当停在草稿，实际 %+v", job)
	}
	f.mu.Lock()
	submits := f.submitCalls
	f.mu.Unlock()
	if submits != 0 {
		t.Fatalf("没勾自动发布却提交了 %d 次", submits)
	}
}
