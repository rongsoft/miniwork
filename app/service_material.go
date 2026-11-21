package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/store"
)

// 素材库：存「别人写得好、我留着学」的内容。
//
// 和选题池的区别要在界面上说清楚，否则用户会把两类东西混着收：
// 选题是「我要写什么」（有分数、有状态机、要推进），
// 素材是「这篇怎么写得好」（有作者、有链接、要拆解）。
// 混在一起收的直接后果是选题池被灌满，筛选失效。

// MaterialView 是给界面的素材形态。
type MaterialView struct {
	ID           string   `json:"id"`
	URL          string   `json:"url"`
	Platform     string   `json:"platform"`
	PlatformName string   `json:"platformName"`
	Author       string   `json:"author"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Points       []string `json:"points"`
	Likes        string   `json:"likes"`
	Collects     string   `json:"collects"`
	Status       string   `json:"status"`
	StatusLabel  string   `json:"statusLabel"`
	TopicID      string   `json:"topicId"`
	TopicTitle   string   `json:"topicTitle"`
	HasBody      bool     `json:"hasBody"`
	AgeLabel     string   `json:"ageLabel"`
}

// MaterialInput 是「添加素材」表单提交上来的东西。
//
// 不带 summary/points：那两块只能由提炼流程写。让表单也能写，
// 就等于允许「用户手改摘要」，而界面上摘要的措辞是「AI 摘要」，
// 两边对不上时没人分得清哪句是谁写的。
type MaterialInput struct {
	URL      string `json:"url"`
	Platform string `json:"platform"`
	Author   string `json:"author"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Likes    int    `json:"likes"`
	Collects int    `json:"collects"`
}

// MaterialStatView 是素材页顶部四个数。
type MaterialStatView struct {
	Total    int            `json:"total"`
	Torn     int            `json:"torn"`
	Raw      int            `json:"raw"`
	Linked   int            `json:"linked"`
	Platform map[string]int `json:"platform"`
}

// Materials 取素材列表，platform 为空表示全部。
func (s *TopicService) Materials(platform string) ([]MaterialView, error) {
	list, err := s.st.Materials(platform, 200)
	if err != nil {
		return nil, err
	}
	out := make([]MaterialView, 0, len(list))
	for _, m := range list {
		out = append(out, s.toMaterialView(m))
	}
	return out, nil
}

// MaterialStats 给顶部四个统计。
func (s *TopicService) MaterialStats() (MaterialStatView, error) {
	st, err := s.st.MaterialStats()
	if err != nil {
		return MaterialStatView{}, err
	}
	return MaterialStatView{
		Total:    st.Total,
		Torn:     st.Torn,
		Raw:      st.Raw,
		Linked:   st.Linked,
		Platform: st.Platform,
	}, nil
}

// AddMaterial 保存一条素材。
//
// 存完立刻尝试提炼：用户刚粘完正文，最有价值的动作是「马上看到它被拆开」。
// 提炼失败不回滚——素材本身已经存好了，界面提示可以稍后重试。
func (s *TopicService) AddMaterial(in MaterialInput) (MaterialView, error) {
	title := strings.TrimSpace(in.Title)
	url := strings.TrimSpace(in.URL)
	body := strings.TrimSpace(in.Body)
	if title == "" && url == "" {
		return MaterialView{}, fmt.Errorf("至少填个标题或链接，否则以后认不出这条是什么")
	}
	if title == "" {
		title = url
	}
	plat := s.materialPlatform(in.Platform, url)

	now := time.Now()
	m := store.Material{
		ID:        store.NewMaterialID(url, plat, title, now),
		URL:       url,
		Platform:  plat,
		Author:    strings.TrimSpace(in.Author),
		Title:     title,
		Body:      body,
		Likes:     in.Likes,
		Collects:  in.Collects,
		Status:    store.MaterialRaw,
		CreatedAt: now,
	}
	if err := s.st.SaveMaterial(m); err != nil {
		return MaterialView{}, err
	}

	// 有正文才提炼。只存了链接的，界面会显示「待提炼」并留一个按钮，
	// 等用户补正文后再点——这里不主动报错，那会让「存个书签」也变成失败。
	if body != "" && s.client() != nil {
		if v, err := s.tear(m.ID); err == nil {
			return v, nil
		}
		// 提炼失败不影响保存结果，下面照常回读。
	}

	saved, err := s.st.MaterialByID(m.ID)
	if err != nil {
		return MaterialView{}, err
	}
	return s.toMaterialView(saved), nil
}

// TearMaterial 重试提炼。用户在补上正文后点它，或者上次模型超时后重来。
func (s *TopicService) TearMaterial(id string) (MaterialView, error) {
	return s.tear(id)
}

func (s *TopicService) tear(id string) (MaterialView, error) {
	m, err := s.st.MaterialByID(id)
	if err != nil {
		return MaterialView{}, err
	}
	if strings.TrimSpace(m.Body) == "" {
		return MaterialView{}, fmt.Errorf("这条只有链接，没有正文。把正文粘进来再提炼")
	}
	if s.client() == nil {
		return MaterialView{}, fmt.Errorf("没有配置模型，暂时提炼不了。素材已经存好了")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	raw, err := s.client().Complete(ctx, ai.DigestSystem, ai.DigestUser(m.Title, m.Body))
	if err != nil {
		return MaterialView{}, fmt.Errorf("提炼没成功: %w", err)
	}
	d, err := ai.ParseDigest(raw)
	if err != nil {
		return MaterialView{}, fmt.Errorf("模型给的结果读不懂: %w", err)
	}
	if err := s.st.SetMaterialDigest(id, d.Summary, d.Points); err != nil {
		return MaterialView{}, err
	}

	fresh, err := s.st.MaterialByID(id)
	if err != nil {
		return MaterialView{}, err
	}
	return s.toMaterialView(fresh), nil
}

// LinkMaterial 把素材挂到某个选题上。topicID 为空表示取消关联。
func (s *TopicService) LinkMaterial(id, topicID string) (MaterialView, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID != "" {
		// 先确认选题存在：关联到一个不存在的 id，界面上会显示空标题，
		// 用户以为关联成功，实际这条素材再也找不到归属。
		if _, err := s.st.TopicByID(topicID); err != nil {
			return MaterialView{}, fmt.Errorf("找不到这条选题: %s", topicID)
		}
	}
	if err := s.st.SetMaterialTopic(id, topicID); err != nil {
		return MaterialView{}, err
	}
	m, err := s.st.MaterialByID(id)
	if err != nil {
		return MaterialView{}, err
	}
	return s.toMaterialView(m), nil
}

// DeleteMaterial 删掉一条素材。
func (s *TopicService) DeleteMaterial(id string) error {
	return s.st.DeleteMaterial(id)
}

func (s *TopicService) toMaterialView(m store.Material) MaterialView {
	v := MaterialView{
		ID:           m.ID,
		URL:          m.URL,
		Platform:     m.Platform,
		PlatformName: format.Platform(m.Platform),
		Author:       m.Author,
		Title:        m.Title,
		Summary:      m.Summary,
		Likes:        metric(m.Likes),
		Collects:     metric(m.Collects),
		Status:       m.Status,
		TopicID:      m.TopicID,
		HasBody:      strings.TrimSpace(m.Body) != "",
		AgeLabel:     format.Age(m.CreatedAt),
	}
	if m.Status == store.MaterialTorn {
		v.StatusLabel = "已提炼"
	} else {
		v.StatusLabel = "待提炼"
	}
	if strings.TrimSpace(m.Points) != "" {
		// 解析失败就当没有要点：摘要还在，界面照常显示，只是少一个清单。
		var pts []string
		if json.Unmarshal([]byte(m.Points), &pts) == nil {
			v.Points = pts
		}
	}
	// 关联选题的标题要回查：素材卡片上写「关联选题」但不说哪一条，
	// 用户得点进去才知道，那这个关联就只完成了一半。
	if m.TopicID != "" {
		if t, err := s.st.TopicByID(m.TopicID); err == nil {
			v.TopicTitle = t.Title
		}
	}
	return v
}

// metric 把点赞/收藏数写成「2.9w」这种。
//
// 传 -1 表示没填，显示成空串而不是「-1」：没填和 0 赞是两件事。
func metric(n int) string {
	if n < 0 {
		return ""
	}
	if n == 0 {
		// Heat(0) 返回的是「—」，那是「没有数据」的意思。
		// 素材里 0 赞是真实读数（刚发出去的笔记就是 0），不能显示成没填。
		return "0"
	}
	return format.Heat(int64(n))
}

// materialPlatform 决定这条素材属于哪个平台。
//
// 先看用户选的，没选就从 URL 猜。猜错的代价很小（筛选时归错组），
// 而每次都让用户手选平台的代价是「加一条素材要点两下拉框」。
func (s *TopicService) materialPlatform(picked, url string) string {
	if p := strings.TrimSpace(strings.ToLower(picked)); p != "" {
		return p
	}
	u := strings.ToLower(url)
	switch {
	case strings.Contains(u, "xiaohongshu") || strings.Contains(u, "xhslink"):
		return "xiaohongshu"
	case strings.Contains(u, "douyin"):
		return "douyin"
	case strings.Contains(u, "mp.weixin") || strings.Contains(u, "weixin.qq"):
		return "wechat"
	case strings.Contains(u, "bilibili") || strings.Contains(u, "b23.tv"):
		return "bilibili"
	case strings.Contains(u, "x.com") || strings.Contains(u, "twitter"):
		return "x"
	case strings.Contains(u, "producthunt"):
		return "producthunt"
	}
	return "other"
}

// OpenURL 用系统默认浏览器打开素材链接。
//
// 只放行 http/https：这个 URL 来自用户粘贴，如果放开 file:// 或
// javascript:，一次手滑粘贴就等于让外部内容有机会碰本地文件或页面脚本。
// 校验放在这里而不是界面：界面只是入口之一，将来任何调用方都该受同一条约束。
func (s *TopicService) OpenURL(raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" {
		return fmt.Errorf("这条素材没有链接")
	}
	lower := strings.ToLower(u)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("只支持 http/https 链接")
	}
	if s.app == nil {
		return fmt.Errorf("界面还没准备好")
	}
	return s.app.Browser.OpenURL(u)
}
