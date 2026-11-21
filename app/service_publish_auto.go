package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rongsoft/miniwork/app/internal/config"
	"rongsoft/miniwork/app/internal/publish"
	"rongsoft/miniwork/app/internal/store"
)

// 自动发布：把稿子直接发到平台上。
//
// 分工是这样的——写稿、检查、复制这些都留在这里，
// 「最后按发布按钮」交给用户（默认）。原因不是技术做不到，
// 是这一步不可撤回：平台判定机器操作会限流甚至封号，
// 而客户第一次用，出事就是事故。
//
// 想让它点，面板上有一个开关，用户自己决定。

// PlatformView 是一个平台的当前状态。
type PlatformView struct {
	Key   string   `json:"key"`
	Name  string   `json:"name"`
	Media string   `json:"media"` // images / video
	Kind  string   `json:"kind"`  // browser / api
	Modes []string `json:"modes,omitempty"`
	// MaxImages 让界面把「最多几张」先说清楚，而不是选完再砍。
	MaxImages int    `json:"maxImages"`
	Note      string `json:"note"`
	// Ready 是「现在能不能发」。
	// 浏览器平台看登录态，接口平台看凭证填没填全——
	// 对用户是同一件事，所以合并成一个字段，别让他分辨两种「没准备好」。
	Ready bool `json:"ready"`
}

// PublishStatus 是发布面板启动时需要的全部信息。
type PublishStatus struct {
	BrowserReady bool   `json:"browserReady"`
	Browser      string `json:"browser"`
	// BrowserVer 只在用包里自带那份时才有值，界面拿来告诉用户
	// 「不用你自己装」——这句话本身就是卖点，别藏起来。
	BrowserVer string `json:"browserVer"`
	// Bundled 说明用的是自带浏览器还是用户系统里的。
	Bundled   bool           `json:"bundled"`
	Reason    string         `json:"reason"`
	Platforms []PlatformView `json:"platforms"`
	Job       *PublishJob    `json:"job"`
}

// PublishJob 是一次发布任务的进度。
//
// 用轮询而不是事件推送：发布是「用户盯着看几十秒」的操作，
// 前端每 600 毫秒问一次状态，比跨进程事件少一整类时序问题，
// 也让这套逻辑在没有窗口的测试里能直接跑。
type PublishJob struct {
	Running      bool     `json:"running"`
	Platform     string   `json:"platform"`
	PlatformName string   `json:"platformName"`
	Logs         []string `json:"logs"`
	Done         bool     `json:"done"`
	OK           bool     `json:"ok"`
	Manual       bool     `json:"manual"`
	Message      string   `json:"message"`
	URL          string   `json:"url"`
	TopicID      string   `json:"topicId"`
	DraftID      string   `json:"draftId"`
}

type publishState struct {
	mu  sync.Mutex
	eng *publish.Engine
	job *PublishJob
	// gate 限制每秒推给界面的画面帧数，见 service_live.go
	gate liveGate
}

// busy 报告此刻有没有一条发布正在跑。
//
// 给「别的功能要不要跟发布抢浏览器」用。发布是全流程独占一个浏览器的，
// 中途被切走就是失败，而且失败得莫名其妙（页面跳了）。
func (ps *publishState) busy() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.job != nil && ps.job.Running
}

// publisher 懒建发布引擎。
//
// 不放在 main 里创建：没用到发布的人不该为它付启动成本，
// 而浏览器的路径探测在 Windows 上要摸几个目录。
func (s *TopicService) publisher() (*publishState, error) {
	s.pubOnce.Do(func() {
		dir := filepath.Dir(s.dbPath)
		if dir == "" || dir == "." {
			dir = "."
		}
		ps := &publishState{}
		ps.eng = publish.New(publish.Options{
			DataDir:  filepath.Join(dir, "publish"),
			ExecPath: browserOverride(),
			Windowed: windowModeOverride(),
			Logger:   func(msg string) { ps.appendLog(msg) },
		})
		// 画面要往界面推，这里接上；接不上（还没有窗口）就只当没有画面。
		s.attachLiveFrames(ps.eng, &ps.gate)
		s.pub = ps
	})
	if s.pub == nil {
		return nil, errors.New("发布引擎没起来")
	}
	return s.pub, nil
}

// appendLog 把一行进度写进当前任务。
func (ps *publishState) appendLog(msg string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.job == nil {
		return
	}
	// 只留最近 40 行：日志是给"现在卡在哪"看的，不是存档。
	if len(ps.job.Logs) >= 40 {
		ps.job.Logs = ps.job.Logs[len(ps.job.Logs)-39:]
	}
	ps.job.Logs = append(ps.job.Logs, time.Now().Format("15:04:05")+"  "+msg)
}

// PublishStatus 返回面板要的东西。
//
// 每次都重新探浏览器：用户可能刚把 Chrome 卸了，或者刚装好 Edge。
// 缓存这个结果只会让界面显示一个不存在的浏览器。
func (s *TopicService) PublishStatus() PublishStatus {
	st := PublishStatus{}
	ps, err := s.publisher()
	if err != nil {
		st.Reason = err.Error()
		return st
	}

	if p, err := ps.eng.BrowserPath(); err == nil {
		st.BrowserReady = true
		st.Browser = p
		st.Bundled = publish.IsBundled(p)
		if st.Bundled {
			_, st.BrowserVer = publish.BundledBrowser()
		}
	} else {
		st.Reason = "找不到可用的浏览器"
	}

	wx := s.wechat()
	for _, p := range publish.All() {
		v := PlatformView{
			Key:       p.Key,
			Name:      p.Name,
			Media:     mediaName(p.Media),
			Kind:      p.Kind,
			Modes:     p.Modes,
			MaxImages: p.MaxImages,
			Note:      p.Note,
		}
		if p.Kind == publish.KindAPI {
			v.Ready = wx.Configured()
		} else if st.BrowserReady {
			v.Ready = ps.eng.Authed(p.Key)
		}
		st.Platforms = append(st.Platforms, v)
	}
	// 两个浏览器平台都没有时，界面要说明为什么一行都发不了。
	// 现在浏览器是随包发的，走到这里说明包不完整（或被人删了），
	// 所以不该再教用户去装 Chrome——那是上一版的说辞。
	if !st.BrowserReady {
		st.Reason = "内置浏览器不见了，安装包可能没解压完整。重装一次应用能修好；急着发也可以走成稿页「复制全部」自己粘到平台。"
	}

	ps.mu.Lock()
	if ps.job != nil {
		cp := *ps.job
		cp.Logs = append([]string{}, ps.job.Logs...)
		st.Job = &cp
	}
	ps.mu.Unlock()
	return st
}

func mediaName(m publish.Media) string {
	switch m {
	case publish.MediaImages:
		return "images"
	case publish.MediaVideo:
		return "video"
	default:
		return ""
	}
}

// OpenPlatformLogin 打开平台的登录页让用户扫码。
//
// 走界面那一屏（LiveOpen），不再单独弹浏览器窗口：这个函数是给
// 「外部调用」留的兜底入口，行为要和界面上的按钮一致——否则哪天又有人
// 从别处调它，用户面前就会再冒出一个 Chrome。
func (s *TopicService) OpenPlatformLogin(platform string) error {
	p, ok := publish.Get(platform)
	if !ok {
		return fmt.Errorf("未知平台: %q", platform)
	}
	_, err := s.LiveOpen(LiveOpenInput{Platform: p.Key, URL: p.LoginURL})
	return err
}

// AutoPublishInput 是「发到平台」这一步的输入。
type AutoPublishInput struct {
	TopicID  string   `json:"topicId"`
	DraftID  string   `json:"draftId"`
	Platform string   `json:"platform"`
	Images   []string `json:"images"`
	Video    string   `json:"video"`
	// Mode 是公众号的内容形态：news=图文消息，newspic=图片消息（贴图）。
	Mode string `json:"mode"`
	// AutoSubmit 默认关。开着它才会替你点最后那一下。
	AutoSubmit bool `json:"autoSubmit"`
}

// StartPublish 起一个后台发布任务，立刻返回。
//
// 同步做会让界面卡住几十秒：Wails 的绑定调用是把主线程交出去的，
// 用户看到的是「点了没反应」。
func (s *TopicService) StartPublish(in AutoPublishInput) error {
	ps, err := s.publisher()
	if err != nil {
		return err
	}
	ps.mu.Lock()
	if ps.job != nil && ps.job.Running {
		ps.mu.Unlock()
		return errors.New("上一次发布还在跑，等它结束")
	}

	plat, ok := publish.Get(in.Platform)
	if !ok {
		ps.mu.Unlock()
		return fmt.Errorf("未知平台: %q", in.Platform)
	}

	// 稿子从库里取，不用界面传的文本。
	// 界面上的编辑框可能有没保存的改动，而「发出去的东西」必须和
	// 台账里记的那一稿是同一份，否则复盘时对不上。
	title, body, tags, err := s.draftForPublish(in.TopicID, in.DraftID)
	if err != nil {
		ps.mu.Unlock()
		return err
	}

	ps.job = &PublishJob{
		Running:      true,
		Platform:     plat.Key,
		PlatformName: plat.Name,
		TopicID:      in.TopicID,
		DraftID:      in.DraftID,
	}
	job := ps.job
	ps.mu.Unlock()

	// 公众号走接口，和浏览器那条链路没有共用的部分，直接分流。
	if plat.Kind == publish.KindAPI {
		go s.runWeChatPublish(ps, job, in, publish.Article{
			Kind:   wechatKind(in.Mode),
			Title:  title,
			Body:   body,
			Digest: firstLine(body),
			Images: in.Images,
		})
		return nil
	}

	req := publish.Request{
		Platform:   plat.Key,
		Title:      title,
		Body:       body,
		Tags:       tags,
		Images:     in.Images,
		Video:      in.Video,
		AutoSubmit: in.AutoSubmit,
	}

	go func() {
		res, err := ps.eng.Publish(req)

		// 先落台账再报完成。
		//
		// 顺序反过来会有一个很尴尬的窗口：界面已经提示「发布完成，已记进台账」，
		// 而台账那一刻还是空的——用户刷新一下会以为没记上，再手动补一条。
		// 落账失败也不再吞掉：账本比提示重要，失败要说出来。
		if err == nil && res.OK && res.URL != "" {
			if _, e := s.MarkPublished(PublishInput{
				TopicID:  in.TopicID,
				DraftID:  in.DraftID,
				Platform: plat.Key,
				URL:      res.URL,
			}); e == nil {
				ps.appendLog("已记进发布台账")
			} else {
				ps.appendLog("已发出去，但没记进台账：" + e.Error())
			}
		}

		ps.mu.Lock()
		job.Running = false
		job.Done = true
		if err != nil {
			job.Message = err.Error()
			ps.mu.Unlock()
			return
		}
		job.OK = res.OK
		job.Manual = res.Manual
		job.URL = res.URL
		job.Message = res.Reason
		ps.mu.Unlock()
	}()
	return nil
}

// draftForPublish 取这一稿的标题、正文、标签。
func (s *TopicService) draftForPublish(topicID, draftID string) (title, body string, tags []string, err error) {
	var d store.Draft
	if strings.TrimSpace(draftID) != "" {
		d, err = s.st.DraftByID(draftID)
	} else {
		var list []store.Draft
		list, err = s.st.DraftsByTopic(topicID)
		if err == nil && len(list) > 0 {
			d = list[0]
		} else if err == nil {
			err = errors.New("这条选题还没有稿子，先写成稿")
		}
	}
	if err != nil {
		return "", "", nil, err
	}
	if strings.TrimSpace(d.Title) == "" {
		return "", "", nil, errors.New("这一稿还没有标题")
	}
	return d.Title, d.Body, d.Tags, nil
}

// PickPublishFiles 打开系统文件选择框。
//
// 让用户自己挑图，而不是从素材库里猜：平台对图片尺寸、比例有要求，
// 谁都可能拍一张就要发，硬塞素材库里的图反而挡路。
func (s *TopicService) PickPublishFiles(platform string) ([]string, error) {
	if s.app == nil {
		return nil, errors.New("没有窗口，选不了文件")
	}
	plat, ok := publish.Get(platform)
	if !ok {
		return nil, fmt.Errorf("未知平台: %q", platform)
	}
	dlg := s.app.Dialog.OpenFile().SetTitle("选择要发布的素材")
	switch plat.Media {
	case publish.MediaImages:
		dlg = dlg.AddFilter("图片", "*.jpg;*.jpeg;*.png;*.webp")
	case publish.MediaVideo:
		dlg = dlg.AddFilter("视频", "*.mp4;*.mov;*.m4v")
	}
	files, err := dlg.PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}
	// 上限按平台各自的规定来：小红书 9 张，公众号能到 20 张。
	if max := plat.MaxImages; max > 0 && len(files) > max && plat.Media == publish.MediaImages {
		files = files[:max]
	}
	return files, nil
}

// ClosePublisher 在退出时关掉浏览器。
func (s *TopicService) ClosePublisher() {
	if s.pub != nil && s.pub.eng != nil {
		s.pub.eng.Close()
	}
}

// browserOverride 让测试能指定一个浏览器。
//
// 生产不读这个变量：客户机器上的路径是固定几种，报错信息也更清楚。
func browserOverride() string {
	return strings.TrimSpace(os.Getenv("MW_BROWSER"))
}

// windowModeOverride 把浏览器切回「有头 + 挪出屏幕」。
//
// 只在排查问题时用：怀疑某个平台在无头下行为和平时不一样，就开起来看一眼。
// 正式运行不要开——那个窗口会抢走前台焦点，用户会看到菜单栏突然变成
// 「Google Chrome for Testing」。
func windowModeOverride() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MW_PUBLISH_WINDOW"))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

/* ---------- 公众号（接口链路） ---------- */

// wechatKind 把界面上选的内容形态翻成接口里的取值。
func wechatKind(mode string) string {
	if strings.TrimSpace(mode) == "newspic" {
		return "newspic"
	}
	return "news"
}

// firstLine 取正文第一句当摘要。
//
// 不调模型再写一句：摘要是「这个号里列表页显示的那行字」，
// 用正文开头最准确，也让用户能通过改第一句来改摘要。
func firstLine(body string) string {
	body = strings.TrimSpace(body)
	for _, sep := range []string{"\n", "。", "！", "？", "."} {
		if i := strings.Index(body, sep); i > 0 && i < 120 {
			return body[:i]
		}
	}
	if len([]rune(body)) > 120 {
		return string([]rune(body)[:120])
	}
	return body
}

// runWeChatPublish 是公众号的发布任务：取凭证 → 建草稿 → （可选）发布 → 取链接。
func (s *TopicService) runWeChatPublish(ps *publishState, job *PublishJob, in AutoPublishInput, a publish.Article) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	ps.appendLog("开始发「" + a.Title + "」")
	wx := s.wechat()

	fail := func(err error) {
		ps.mu.Lock()
		job.Running, job.Done = false, true
		job.Message = err.Error()
		ps.mu.Unlock()
		ps.appendLog("失败：" + err.Error())
	}

	ps.appendLog("正在取接口凭证…")
	if _, err := wx.Token(ctx); err != nil {
		fail(err)
		return
	}

	if a.Kind == "newspic" {
		ps.appendLog(fmt.Sprintf("正在上传 %d 张图（图片消息）…", len(a.Images)))
		// 贴图的图进的是「永久素材」，不吃临时素材那套过期清理。
		// 每周发几条看起来没事，发一年素材库就该满了，到时候报错很难查。
		ps.appendLog("提示：贴图的图会存进公众号素材库，发多了到后台删一删（有数量上限）")
	} else {
		ps.appendLog(fmt.Sprintf("正在上传封面和 %d 张配图…", maxInt(len(a.Images)-1, 0)))
	}
	mediaID, err := wx.CreateDraft(ctx, a)
	if err != nil {
		fail(err)
		return
	}
	ps.appendLog("草稿已建好")

	if !in.AutoSubmit {
		ps.mu.Lock()
		job.Running, job.Done, job.Manual = false, true, true
		job.Message = "草稿已存在公众号后台，去后台看一眼再点发布"
		ps.mu.Unlock()
		ps.appendLog("等你到公众号后台确认")
		return
	}

	ps.appendLog("正在提交发布…")
	publishID, err := wx.Submit(ctx, mediaID)
	if err != nil {
		fail(err)
		return
	}
	ps.appendLog("已提交，等微信生成链接…")

	st := wx.WaitPublish(ctx, publishID, 40*time.Second)

	// 发成功了就先落台账，再报完成——顺序和浏览器那条链路一致。
	//
	// 这里原本漏了落账：公众号发出去之后台账是空的，用户回发布台账
	// 一看没有这条，只会以为没发出去，然后再发一遍（公众号每天有次数限制）。
	if st.URL != "" && st.Status >= 0 && st.Reason == "" {
		if _, e := s.MarkPublished(PublishInput{
			TopicID:  in.TopicID,
			DraftID:  in.DraftID,
			Platform: "wechat",
			URL:      st.URL,
		}); e == nil {
			ps.appendLog("已记进发布台账")
		} else {
			ps.appendLog("已发出去，但没记进台账：" + e.Error())
		}
	}

	ps.mu.Lock()
	job.Running, job.Done = false, true
	job.URL = st.URL
	switch {
	case st.Status == -1:
		// 查询本身失败：不算发成功，也不装作发成功。
		job.Manual = true
		job.Message = "已提交，但查不到发布结果：" + st.Reason + "。到公众号后台确认一下"
	case st.Reason != "":
		// 2/3/4：微信明确说这次发布失败了。
		job.Message = st.Reason
	case st.URL != "":
		job.OK = true
		job.Message = "已发布"
	default:
		// 还在发布中（status=1）或超时没等到：提交成功了，但没拿到链接。
		// 这种情况说"已发布"是在赌，说"失败"又是冤枉它——让用户去看一眼。
		job.Manual = true
		job.Message = "已提交，微信还在处理。过一两分钟到公众号后台看一眼，发了就点「已发」记一笔"
	}
	ps.mu.Unlock()

	if st.Reason != "" {
		ps.appendLog("发布失败：" + st.Reason)
	} else if st.URL != "" {
		ps.appendLog("发布完成：" + st.URL)
	} else {
		ps.appendLog("已提交，等微信处理")
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wechat 按当前配置建一个客户端。
//
// MW_WECHAT_API 只在测试里用：把接口地址指到本地假服务端，
// 才能在不碰真实公众号的前提下把「凭证→传图→建草稿→发布→取链接」
// 整条链路跑一遍。真实发布用不到它。
func (s *TopicService) wechat() *publish.WeChat {
	cfg, _ := config.Load()
	return &publish.WeChat{
		AppID:     cfg.WeChat.AppID,
		AppSecret: cfg.WeChat.AppSecret,
		BaseURL:   strings.TrimSpace(os.Getenv("MW_WECHAT_API")),
	}
}

// WeChatView 是公众号凭证在界面上的样子。
type WeChatView struct {
	AppID     string `json:"appId"`
	HasSecret bool   `json:"hasSecret"`
	Ready     bool   `json:"ready"`
}

// WeChatConfig 回填公众号配置。
func (s *TopicService) WeChatConfig() WeChatView {
	cfg, _ := config.Load()
	return WeChatView{
		AppID:     cfg.WeChat.AppID,
		HasSecret: strings.TrimSpace(cfg.WeChat.AppSecret) != "",
		Ready:     s.wechat().Configured(),
	}
}

// SaveWeChatConfig 存公众号凭证。
//
// 空 secret 表示「不改」，和模型那块同一套约定：
// 界面永远不回显密钥，用户改 AppID 时不该被迫重打一遍 secret。
func (s *TopicService) SaveWeChatConfig(appID, secret string) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	cfg.WeChat.AppID = strings.TrimSpace(appID)
	if v := strings.TrimSpace(secret); v != "" {
		cfg.WeChat.AppSecret = v
	}
	if cfg.WeChat.AppID == "" {
		return errors.New("AppID 不能空着")
	}
	if cfg.WeChat.AppSecret == "" {
		return errors.New("AppSecret 还没填过，先填一次")
	}
	if err := cfg.SaveTo(config.Path()); err != nil {
		return fmt.Errorf("保存失败：%w", err)
	}
	return nil
}

// TestWeChat 真调一次接口，确认 AppID/Secret/白名单三样都对。
//
// 只验凭证不够：白名单错的时候凭证是对的，错在「这个 IP 不许调」，
// 所以这里直接取一次 token——失败信息里就带着要加白名单的 IP。
func (s *TopicService) TestWeChat() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	wx := s.wechat()
	if !wx.Configured() {
		return "", errors.New("还没填 AppID 和 AppSecret")
	}
	if _, err := wx.Token(ctx); err != nil {
		return "", err
	}
	return "连上了，凭证和白名单都没问题", nil
}
