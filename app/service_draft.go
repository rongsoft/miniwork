package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/draft"
	"rongsoft/miniwork/app/internal/store"
)

// 成稿与发布这一条线要解决的是一个具体的断点：
// 选题收下、排了日子，然后就没了下文——日历上写着「11-18 发」，
// 到那天打开却没有稿子，于是那天就过去了。
//
// 所以这里只做两件事，不多做：
//   - 把选题变成一篇能直接贴出去的稿子（含平台长度校验）
//   - 让「发了没发」这件事有地方落账（含效果回填）
//
// 「自动发布」这个动作本身不在客户端做：小红书、抖音没有面向个人的
// 发布接口，唯一能真点下去的路是模拟浏览器，会被风控且随页面改版失效。
// 半自动（一键复制 + 跳转 + 回来标记）覆盖了 90% 的省力，且零风险。

// DraftView 是给界面的稿子形态。
type DraftView struct {
	ID           string       `json:"id"`
	TopicID      string       `json:"topicId"`
	TopicTitle   string       `json:"topicTitle"`
	Platform     string       `json:"platform"`
	PlatformName string       `json:"platformName"`
	Format       string       `json:"format"`
	FormatLabel  string       `json:"formatLabel"`
	Title        string       `json:"title"`
	Body         string       `json:"body"`
	Tags         []string     `json:"tags"`
	Segments     []ai.Segment `json:"segments"`
	Note         string       `json:"note"` // 模型给的一句话：这一稿的角度
	Edited       bool         `json:"edited"`
	CreatedAt    string       `json:"createdAt"`

	// 平台规格与越界提醒。字数统计在这里做，不在前端做：
	// 前端按 JS 的 length 数会把 emoji 数成两个，和平台口径对不上。
	TitleMax      int           `json:"titleMax"`
	BodyMax       int           `json:"bodyMax"`
	TagMax        int           `json:"tagMax"`
	TitleLen      int           `json:"titleLen"`
	BodyLen       int           `json:"bodyLen"`
	SpeechSeconds int           `json:"speechSeconds"` // 口播估算秒数，图文为 0
	Issues        []draft.Issue `json:"issues"`
	Where         string        `json:"where"`
	PlatformNote  string        `json:"platformNote"`
}

// DraftInput 是界面回传的稿子。ID 为空表示新稿。
type DraftInput struct {
	ID       string       `json:"id"`
	TopicID  string       `json:"topicId"`
	Platform string       `json:"platform"`
	Format   string       `json:"format"`
	Title    string       `json:"title"`
	Body     string       `json:"body"`
	Tags     []string     `json:"tags"`
	Segments []ai.Segment `json:"segments"`
	Note     string       `json:"note"`
}

// Platforms 给界面渲染平台选择。
func (s *TopicService) Platforms() []draft.Spec { return draft.All() }

// Write 生成一篇成稿并落库。
//
// format 传空时按平台默认形态走；platform 未知直接报错，不猜——
// 猜错平台的后果是标题上限按错了，稿子一复制就被截断，而用户不知道为什么。
func (s *TopicService) Write(topicID, platform, format, material string, skeleton []string) (DraftView, error) {
	if err := s.craftReady(); err != nil {
		return DraftView{}, err
	}
	t, err := s.st.TopicByID(strings.TrimSpace(topicID))
	if err != nil {
		return DraftView{}, err
	}
	sp, ok := draft.Get(platform)
	if !ok {
		return DraftView{}, fmt.Errorf("未知平台: %q", platform)
	}
	if format != draft.Short {
		format = draft.Long
	}
	// 短视频稿只认口播：给抖音写一篇图文，模型会照着公众号的路子写。
	if sp.Format == draft.Short {
		format = draft.Short
	}

	system, user := ai.BuildDraftPrompt(ai.DraftInput{
		Topic:    t.Title,
		Platform: sp.Name,
		Format:   format,
		Material: material,
		Skeleton: skeleton,
		TitleMax: sp.TitleMax,
		BodyMax:  sp.BodyMax,
		TagMax:   sp.TagMax,
	}, s.craftBrief())

	// 写稿比打分慢一个量级：上千字的输出，60 秒常常不够。
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	content, err := ai.Write(ctx, s.client(), system, user)
	if err != nil {
		return DraftView{}, craftErr(err)
	}
	d, err := ai.ParseDraft(content)
	if err != nil {
		return DraftView{}, fmt.Errorf("模型这次没写出可用的稿子，再点一次试试：%w", err)
	}

	segs, _ := json.Marshal(d.Segments)
	id, err := s.st.SaveDraft(store.Draft{
		TopicID:  t.ID,
		Platform: sp.Key,
		Format:   format,
		Title:    d.Title,
		Body:     d.Body,
		Tags:     d.Tags,
		Segments: string(segs),
		Note:     d.Note,
	})
	if err != nil {
		return DraftView{}, err
	}
	return s.DraftByID(id, t.Title)
}

// DraftByID 取单篇稿件。topicTitle 为空时回查一次库。
func (s *TopicService) DraftByID(id, topicTitle string) (DraftView, error) {
	d, err := s.st.DraftByID(strings.TrimSpace(id))
	if err != nil {
		return DraftView{}, err
	}
	if topicTitle == "" {
		if t, err := s.st.TopicByID(d.TopicID); err == nil {
			topicTitle = t.Title
		}
	}
	return toDraftView(d, topicTitle), nil
}

// Drafts 取某条选题的所有稿，新的在前。
func (s *TopicService) Drafts(topicID string) ([]DraftView, error) {
	list, err := s.st.DraftsByTopic(strings.TrimSpace(topicID))
	if err != nil {
		return nil, err
	}
	title := ""
	if t, err := s.st.TopicByID(topicID); err == nil {
		title = t.Title
	}
	out := make([]DraftView, 0, len(list))
	for _, d := range list {
		out = append(out, toDraftView(d, title))
	}
	return out, nil
}

// RecentDrafts 取最近写过的稿子，写作台首页用它列出「刚写的」。
func (s *TopicService) RecentDrafts(limit int) ([]DraftView, error) {
	list, err := s.st.RecentDrafts(limit)
	if err != nil {
		return nil, err
	}
	// 标题按需回查：稿子可能分布在几十条选题上，逐条查库是几十次往返。
	// 先一次性把用到的选题读出来，这里用简单缓存就够，量级是几十。
	cache := map[string]string{}
	out := make([]DraftView, 0, len(list))
	for _, d := range list {
		title, ok := cache[d.TopicID]
		if !ok {
			if t, err := s.st.TopicByID(d.TopicID); err == nil {
				title = t.Title
			}
			cache[d.TopicID] = title
		}
		out = append(out, toDraftView(d, title))
	}
	return out, nil
}

// SaveDraft 保存用户编辑过的稿子。
//
// 这里不校验长度、也不阻止保存：写稿时中间状态一定是超长的，
// 边写边拦等于让人一边写一边看红字。越界提醒在视图里给，发之前他自己会看到。
func (s *TopicService) SaveDraft(in DraftInput) (DraftView, error) {
	if strings.TrimSpace(in.ID) == "" {
		return DraftView{}, fmt.Errorf("稿件 id 为空")
	}
	segs, err := json.Marshal(in.Segments)
	if err != nil {
		return DraftView{}, fmt.Errorf("分镜格式有误: %w", err)
	}
	// 用户按下保存就当改过，哪怕只删了一个空格。
	// 想精确判断「改没改」，得存一份原文比对，为这点信息不值得多一列。
	id, err := s.st.SaveDraft(store.Draft{
		ID:       in.ID,
		TopicID:  in.TopicID,
		Platform: in.Platform,
		Format:   in.Format,
		Title:    strings.TrimSpace(in.Title),
		Body:     in.Body,
		Tags:     in.Tags,
		Segments: string(segs),
		Note:     in.Note,
		Edited:   true,
	})
	if err != nil {
		return DraftView{}, err
	}
	return s.DraftByID(id, "")
}

// DeleteDraft 删掉一稿。台账里的记录不删，见 store 层的说明。
func (s *TopicService) DeleteDraft(id string) error {
	return s.st.DeleteDraft(id)
}

// DraftPack 把稿子拼成能直接粘进后台的文本。
//
// 拼装在 Go 侧做，不在前端做：标签去重、按平台决定加不加 #、
// 公众号换成顿号，这些规则只该有一份实现。
func (s *TopicService) DraftPack(id string) (draft.Pack, error) {
	d, err := s.st.DraftByID(strings.TrimSpace(id))
	if err != nil {
		return draft.Pack{}, err
	}
	sp, ok := draft.Get(d.Platform)
	if !ok {
		sp, _ = draft.Get("xiaohongshu")
	}
	return draft.Build(sp, d.Title, d.Body, d.Tags), nil
}

// CopyPack 把一稿拼好并写进系统剪贴板。
//
// 走 Go 侧而不是前端 navigator.clipboard：wails 的自定义协议不是 https，
// 浏览器剪贴板接口在这种上下文里会被判为不可用，失败时还是静默的——
// 用户点了「复制全部」，什么也没发生，他还以为稿子丢了。
func (s *TopicService) CopyPack(id string) (draft.Pack, error) {
	pack, err := s.DraftPack(id)
	if err != nil {
		return draft.Pack{}, err
	}
	if strings.TrimSpace(pack.Text) == "" {
		return pack, fmt.Errorf("这篇还没有内容")
	}
	if s.app == nil {
		return pack, fmt.Errorf("剪贴板不可用")
	}
	if !s.app.Clipboard.SetText(pack.Text) {
		return pack, fmt.Errorf("复制失败，请手动选中正文复制")
	}
	return pack, nil
}

func toDraftView(d store.Draft, topicTitle string) DraftView {
	sp, ok := draft.Get(d.Platform)
	if !ok {
		// 平台被改名或数据来自旧版本时，退回小红书规格：
		// 给一个保守的标题上限，比让界面上的字数计数全空着好。
		sp, _ = draft.Get("xiaohongshu")
	}
	var segs []ai.Segment
	if strings.TrimSpace(d.Segments) != "" {
		// 解析失败就当没有分镜：一段脏 JSON 不该让整篇稿子打不开。
		_ = json.Unmarshal([]byte(d.Segments), &segs)
	}
	v := DraftView{
		ID:            d.ID,
		TopicID:       d.TopicID,
		TopicTitle:    topicTitle,
		Platform:      d.Platform,
		PlatformName:  sp.Name,
		Format:        d.Format,
		FormatLabel:   draft.FormatLabel(d.Format),
		Title:         d.Title,
		Body:          d.Body,
		Tags:          d.Tags,
		Segments:      segs,
		Note:          d.Note,
		Edited:        d.Edited,
		TitleMax:      sp.TitleMax,
		BodyMax:       sp.BodyMax,
		TagMax:        sp.TagMax,
		TitleLen:      len([]rune(strings.TrimSpace(d.Title))),
		BodyLen:       len([]rune(strings.TrimSpace(d.Body))),
		SpeechSeconds: draft.SpeechSeconds(d.Body),
		Where:         sp.Where,
		PlatformNote:  sp.Note,
	}
	if !d.CreatedAt.IsZero() {
		v.CreatedAt = d.CreatedAt.Format("01-02 15:04")
	}
	v.Issues = draft.Check(sp, d.Format, d.Title, d.Body, d.Tags)
	return v
}
