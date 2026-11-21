package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/store"
)

// 创作工具的两个动作：拆别人的、起自己的。
//
// 它们和打分不是一回事：打分是「批量判断，要稳定」，
// 这两个是「单个请求，要给出能用的具体句子」。
// 所以不共用 ai 的打分提示词，也不做缓存——同一句话拆两次，
// 用户期待的是换个角度，而不是命中缓存返回一模一样的结果。

// craftBrief 组装作者信息。人设 + 最近选择，和打分用的是同一套记忆。
//
// 在这里复用记忆有个附带好处：用户改完人设，拆解和起标题会立刻变，
// 不用再去别的地方调一次。
func (s *TopicService) craftBrief() ai.Brief {
	p, _ := s.st.Profile()
	d, _ := s.st.RecentDecisions(40)
	return ai.Brief{
		Niche:       p.Niche,
		Positioning: p.Positioning,
		Audience:    p.Audience,
		Tone:        p.Tone,
		Avoid:       p.Avoid,
		Picked:      d.Picked,
		Dropped:     d.Dropped,
	}
}

// craftReady 在调模型之前检查「能不能调」。
//
// 不做静默降级：这两个功能没有规则版本可退，没模型就是做不了。
// 界面上必须明说原因，否则用户点完只看到空白，会以为按钮坏了。
func (s *TopicService) craftReady() error {
	if s.client() == nil {
		return errors.New("还没配置模型。到「设置」里填一个 API Key，这两个工具才能用")
	}
	return nil
}

// TeardownView 是给界面的拆解结果。
//
// 字段和 ai.Teardown 一一对应，只多了 id/source/atLabel 三样：
// id 用来删、source 是被拆的那条原文标题（卡片顶上要显示「拆的是哪条」）、
// atLabel 是「3 天前」。不直接返回 ai.Teardown 是因为界面不能碰
// 「这条是刚拆的还是从库里翻出来的」这件事——统一成一种形状，前端只写一份渲染。
type TeardownView struct {
	ID        string   `json:"id"`
	Source    string   `json:"source"`    // 被拆的标题原文
	Title     string   `json:"title"`     // 标题亮点
	Cover     string   `json:"cover"`     // 封面设计
	Structure string   `json:"structure"` // 内容结构（可直接当写稿骨架）
	Engage    string   `json:"engage"`    // 互动引导
	Tone      string   `json:"tone"`      // 语气风格
	Topics    []string `json:"topics"`    // 适合话题
	Learn     string   `json:"learn"`     // 可借鉴点
	Category  string   `json:"category"`  // 分类
	AtLabel   string   `json:"atLabel"`   // 「3 天前」
}

// Teardown 拆一条别人的爆款。
//
// 拆完立刻落库：用户拆十条，第二天一定会有「那个标题公式怎么写来着」的时刻。
// 不落库就只能再拆一遍，既花一次模型调用，结果还可能不一样。
func (s *TopicService) Teardown(title, body string) (TeardownView, error) {
	if err := s.craftReady(); err != nil {
		return TeardownView{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return TeardownView{}, errors.New("先把标题粘进来")
	}
	if len([]rune(title)) > 200 {
		return TeardownView{}, errors.New("标题太长了，只要标题本身，正文放下面那栏")
	}
	body = strings.TrimSpace(body)

	td, err := s.teardown(tdInput{Title: title, Body: body})
	if err != nil {
		return TeardownView{}, err
	}

	now := time.Now()
	rec := store.Teardown{
		ID:        store.NewTeardownID(),
		Title:     title,
		Body:      body,
		Result:    mustJSON(td),
		Category:  td.Category,
		CreatedAt: now,
	}
	if err := s.st.SaveTeardown(rec); err != nil {
		// 存不下就明说，但结果照常给用户看——他已经等到结果了，
		// 因为写库失败把它吞掉，等于白等一次。
		return toTeardownView(rec, now), fmt.Errorf("结果出来了，但没存住（%v）。截图或复制一下再关窗口", err)
	}
	return toTeardownView(rec, now), nil
}

// tdInput 把「要拆什么」收成一个参数，省得每个内部函数都带两个 string。
type tdInput struct{ Title, Body string }

// TeardownRaw 拆用户粘进来的一段东西。
//
// 界面上只给一个框：用户从手机里「分享 → 复制」拿到的是一整段话
// （标题 + 正文 + 链接 + App 广告），不是三个干净的字段。让他在三个框之间
// 自己切，他十次有八次会把整段贴进「标题」，然后觉得这功能拆得不准。
func (s *TopicService) TeardownRaw(raw string) (TeardownView, error) {
	if err := s.craftReady(); err != nil {
		return TeardownView{}, err
	}
	title, body, url := format.SplitShared(raw)
	if title == "" {
		if url != "" {
			// 诚实说清楚，别让他以为只要再点一次就行。
			return TeardownView{}, errors.New(
				"只认出了链接。小红书和抖音的正文要登录才给，程序抓不到——" +
					"把标题和正文也一起粘进来（在笔记里长按复制就行）")
		}
		return TeardownView{}, errors.New("先把要拆的东西粘进来")
	}
	// 认出了标题、却没认出正文：网页版「分享」复制出来的就是这种，
	// 它压根不带正文。只凭一个标题拆，模型只能编结构，
	// 用户拿到的是一份看着像那么回事、实际用不上的报告。
	// 与其让他误以为功能不行，不如当场说清缺什么。
	if body == "" && url != "" {
		return TeardownView{}, fmt.Errorf(
			"只认出了标题《%s》。网页版分享不带正文，这里拆不出结构——"+
				"在笔记里全选复制正文，连标题一起粘进来", clipRunes(title, 24))
	}
	return s.Teardown(title, body)
}

// teardown 是真调模型的那一步。拆出来是因为以后要支持「重新拆」，
// 那时候手上只有库里的记录，不该再走一遍参数校验和落库。
func (s *TopicService) teardown(in tdInput) (ai.Teardown, error) {
	system, user := ai.BuildTeardownPrompt(in.Title, in.Body, s.craftBrief())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 走创作场景的模型：拆解要读得懂结构，便宜的模型经常只复述标题。
	content, err := ai.CompleteFor(ctx, s.client(), ai.SceneCraft, system, user)
	if err != nil {
		return ai.Teardown{}, craftErr(err)
	}
	td, err := ai.ParseTeardown(content)
	if err != nil {
		return ai.Teardown{}, fmt.Errorf("模型这次没给出可用结果，再点一次试试：%w", err)
	}
	return td, nil
}

// Teardowns 取拆过的记录，最新的在前。
//
// 某条记录的 JSON 读不出来就跳过那一条，不整体报错：
// 老版本存下的格式会在升级后解析失败，为了一条坏记录让整个历史列表打不开，
// 用户丢的是全部记录，不是一个。
func (s *TopicService) Teardowns() ([]TeardownView, error) {
	list, err := s.st.Teardowns(30)
	if err != nil {
		return nil, err
	}
	out := make([]TeardownView, 0, len(list))
	for _, r := range list {
		var td ai.Teardown
		if json.Unmarshal([]byte(r.Result), &td) != nil {
			continue
		}
		out = append(out, toTeardownView(r, r.CreatedAt))
	}
	return out, nil
}

// DeleteTeardown 删掉一条拆解记录。
func (s *TopicService) DeleteTeardown(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("没指定要删哪条")
	}
	return s.st.DeleteTeardown(id)
}

// SaveTeardownReport 把一条拆解导成 markdown，让用户自己选存哪。
//
// 为什么是文件而不是「复制到剪贴板」：拆解报告是拿来反复看的
// （写稿时对照结构、选题时对照话题），剪贴板只活到下一次复制。
// 也不做成 PDF——为了一个报告引一整套排版依赖不值得。
func (s *TopicService) SaveTeardownReport(id string) (string, error) {
	r, err := s.st.TeardownByID(id)
	if err != nil {
		return "", err
	}
	var td ai.Teardown
	if err := json.Unmarshal([]byte(r.Result), &td); err != nil {
		return "", errors.New("这条记录的格式读不出来了，重新拆一次吧")
	}
	if s.app == nil {
		return "", errors.New("没有窗口，选不了保存位置")
	}

	body := teardownMarkdown(r, td)
	home, _ := os.UserHomeDir()
	name := reportName(r.Title, r.CreatedAt)
	defaultPath := filepath.Join(home, "Downloads", name)

	path, derr := s.app.Dialog.SaveFile().
		SetMessage("保存拆解报告").
		AddFilter("Markdown", "*.md").
		SetFilename(name).
		SetDirectory(filepath.Join(home, "Downloads")).
		PromptForSingleSelection()
	if derr != nil {
		// 对话框在某些环境下会起不来（没有窗口焦点、沙箱限制）。
		// 这时候别把已经生成好的报告丢掉——直接落到下载目录，
		// 返回值里带完整路径，用户照样找得到。
		path = defaultPath
	}
	if strings.TrimSpace(path) == "" {
		return "", errCanceled
	}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		path += ".md"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("写文件失败：%w", err)
	}
	return path, nil
}

var errCanceled = errors.New("取消了")

// reportName 生成默认文件名：标题前 24 字 + 日期。
// 不用随机 id：用户下三份报告后，靠 id 分不出哪份是哪条。
func reportName(title string, at time.Time) string {
	t := strings.TrimSpace(title)
	if t == "" {
		t = "拆解"
	}
	r := []rune(t)
	if len(r) > 24 {
		r = r[:24]
	}
	// 文件名里不能有路径分隔符，Windows 上还禁 :*?"<>|
	clean := strings.Map(func(c rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, c) {
			return '-'
		}
		return c
	}, string(r))
	return fmt.Sprintf("%s-拆解-%s.md", clean, at.Format("20060102"))
}

func teardownMarkdown(r store.Teardown, td ai.Teardown) string {
	var b strings.Builder
	b.WriteString("# " + strings.TrimSpace(r.Title) + "\n\n")
	if td.Category != "" || !r.CreatedAt.IsZero() {
		meta := []string{}
		if td.Category != "" {
			meta = append(meta, td.Category)
		}
		if !r.CreatedAt.IsZero() {
			meta = append(meta, r.CreatedAt.Format("2006-01-02")+" 拆解")
		}
		b.WriteString("> " + strings.Join(meta, " · ") + "\n\n")
	}
	rows := [][2]string{
		{"标题亮点", td.Title},
		{"封面设计", td.Cover},
		{"内容结构", td.Structure},
		{"互动引导", td.Engage},
		{"语气风格", td.Tone},
	}
	for _, kv := range rows {
		if strings.TrimSpace(kv[1]) == "" {
			continue
		}
		b.WriteString("## " + kv[0] + "\n\n" + strings.TrimSpace(kv[1]) + "\n\n")
	}
	if len(td.Topics) > 0 {
		b.WriteString("## 适合话题\n\n" + strings.Join(td.Topics, " / ") + "\n\n")
	}
	if strings.TrimSpace(td.Learn) != "" {
		b.WriteString("## 可借鉴点\n\n" + strings.TrimSpace(td.Learn) + "\n\n")
	}
	// 原文附在最后：三个月后看到这份报告，只有结论没有原文，
	// 会想不起当时为什么这么判断。
	if body := strings.TrimSpace(r.Body); body != "" {
		b.WriteString("---\n\n### 原文正文\n\n" + body + "\n")
	}
	return b.String()
}

// mustJSON 把拆解结果序列化。字段全是 string/[]string，不会失败；
// 真失败说明结构被改坏了，那时候给空串比 panic 好——外层会当成读不出来跳过。
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func toTeardownView(r store.Teardown, at time.Time) TeardownView {
	var td ai.Teardown
	_ = json.Unmarshal([]byte(r.Result), &td)
	v := TeardownView{
		ID:        r.ID,
		Source:    r.Title,
		Title:     td.Title,
		Cover:     td.Cover,
		Structure: td.Structure,
		Engage:    td.Engage,
		Tone:      td.Tone,
		Topics:    td.Topics,
		Learn:     td.Learn,
		Category:  td.Category,
	}
	if v.Category == "" {
		v.Category = r.Category
	}
	if !at.IsZero() {
		v.AtLabel = format.Age(at)
	}
	return v
}

// SuggestTitles 给一句内容描述，生成候选标题。
func (s *TopicService) SuggestTitles(seed string, hooks []string) ([]ai.Title, error) {
	if err := s.craftReady(); err != nil {
		return nil, err
	}
	seed = strings.TrimSpace(seed)
	if len([]rune(seed)) < 4 {
		return nil, errors.New("多写几个字，让我知道你想讲什么")
	}
	if len([]rune(seed)) > 800 {
		// 不报错，截断即可：用户粘了一大段正文进来是常事，
		// 让他自己删一半再点一次是多余的摩擦。
		seed = string([]rune(seed)[:800])
	}

	system, user := ai.BuildTitlePrompt(seed, hooks, s.craftBrief())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	content, err := ai.CompleteFor(ctx, s.client(), ai.SceneCraft, system, user)
	if err != nil {
		return nil, craftErr(err)
	}
	list, err := ai.ParseTitles(content)
	if err != nil {
		return nil, fmt.Errorf("模型这次没给出可用结果，再点一次试试：%w", err)
	}
	return list, nil
}

// craftErr 把模型层的错误翻成用户能行动的话。
func craftErr(err error) error {
	switch {
	case errors.Is(err, ai.ErrQuotaExceeded):
		return errors.New("模型额度用完了。充值后再试，或到设置里换成自己的 Key")
	case errors.Is(err, ai.ErrUnauthorized):
		return errors.New("API Key 无效。到设置里检查一下")
	case errors.Is(err, ai.ErrNotConfigured):
		return errors.New("模型还没配置好。到设置里填上 API Key")
	default:
		return fmt.Errorf("模型没有返回结果：%w", err)
	}
}

// CraftStatus 告诉界面这两个工具现在能不能用。
//
// 单独一个方法而不是让界面去猜 aiClient 是否为 nil：
// 界面拿不到那个字段，猜出来的结果迟早和真实状态不一致。
type CraftStatus struct {
	Ready   bool   `json:"ready"`
	AIName  string `json:"aiName"`
	Message string `json:"message"`
}

// CraftAvailable 返回创作工具的可用状态。
func (s *TopicService) CraftAvailable() CraftStatus {
	if s.client() == nil {
		return CraftStatus{
			Ready:   false,
			AIName:  "关闭",
			Message: "这两个工具需要模型。到「设置」里填一个 API Key 就能用。",
		}
	}
	return CraftStatus{Ready: true, AIName: ai.ModelNameOf(s.client())}
}
