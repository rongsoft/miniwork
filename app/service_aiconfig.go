package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/config"
)

// 设置页「模型」这一块：填地址、填 Key、点保存就能用。
//
// 这里推翻过一次设计。原来的注释写的是「不做在界面里填 Key，Key 是明文存进
// config.json 的，让用户在界面里输入只会让他以为它是加密存的」——理由本身没错，
// 但结果是：拆解、写稿、素材提炼三处的报错都在说「到设置里填一个 API Key」，
// 而设置页根本没有那个输入框。文案承诺了一个不存在的功能，用户照着找只会卡住。
//
// 所以现在两条一起做：给输入框（客户不会去改 json），同时把「明文存在本机」
// 写在表单下面，不假装它是加密的。Key 存哪儿本来就是用户有权知道的事。

// AIKeyView 是设置页模型表单的当前状态。
type AIKeyView struct {
	Mode      string `json:"mode"`      // off | byok | relay
	BaseURL   string `json:"baseUrl"`   // 回填用
	Model     string `json:"model"`     // 回填用
	KeyMasked string `json:"keyMasked"` // sk-8dd1…1992 / 未设置
	HasKey    bool   `json:"hasKey"`
	Ready     bool   `json:"ready"`
	Multi     int    `json:"multi"` // 多模型条数；>0 时单模型表单不生效
	Path      string `json:"path"`
}

// AIKeyInput 是表单提交的内容。APIKey 留空表示「不改」——
// 回填时不可能把明文塞回输入框，只能让用户重填。
type AIKeyInput struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}

const (
	// 这两条不是随便挑的默认值：DeepSeek 是当前配置里在用的，
	// 而 OpenAI 兼容的服务址格式基本都长这样，用户改个域名就能接别家。
	defAIBase  = "https://api.deepseek.com/v1"
	defAIModel = "deepseek-chat"
)

// AIConfig 返回模型表单要回填的内容。
func (s *TopicService) AIConfig() AIKeyView {
	cfg, _ := config.Load()
	// ready 看配置本身，不看运行时客户端：有没有填齐是文件里的事实，
	// 而 s.client() 在测试替身下也是非 nil，用它判断会让空配置显示成「已配置」。
	ready := strings.TrimSpace(cfg.AI.APIKey) != "" && strings.TrimSpace(cfg.AI.BaseURL) != ""
	if len(cfg.AI.Models) > 0 {
		ready = true
	}
	v := AIKeyView{
		Mode:      cfg.AI.Mode,
		BaseURL:   cfg.AI.BaseURL,
		Model:     cfg.AI.Model,
		KeyMasked: maskKey(cfg.AI.APIKey),
		HasKey:    strings.TrimSpace(cfg.AI.APIKey) != "",
		Ready:     ready,
		Multi:     len(cfg.AI.Models),
		Path:      config.Path(),
	}
	if v.BaseURL == "" {
		v.BaseURL = defAIBase
	}
	if v.Model == "" {
		v.Model = defAIModel
	}
	return v
}

// SaveAIKey 写盘并当场换上新客户端，不用重启。
//
// 不重启是刻意的：用户填完 Key 的第一件事就是回去点「开始拆解」，
// 如果这时还要「请重启后生效」，前面那步的挫败感会直接盖过成功的部分。
func (s *TopicService) SaveAIKey(in AIKeyInput) (AIKeyView, error) {
	cfg, err := config.Load()
	if err != nil {
		// 配置读不出来不该阻止用户填一份新的——他多半就是来修这个的。
		cfg = config.Default()
	}

	base := strings.TrimSpace(in.BaseURL)
	if base == "" {
		base = defAIBase
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return AIKeyView{}, errors.New("接口地址要以 http:// 或 https:// 开头")
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = defAIModel
	}

	key := strings.TrimSpace(in.APIKey)
	if key == "" {
		// 留空 = 沿用已存的 Key。否则用户只想改模型名，也会被要求重新
		// 去别处翻一遍 Key，而界面上只显示得出 sk-8dd1…1992。
		key = cfg.AI.APIKey
	}
	if key == "" {
		return AIKeyView{}, errors.New("请填写 API Key")
	}

	cfg.AI.Mode = "byok"
	cfg.AI.BaseURL = base
	cfg.AI.APIKey = key
	cfg.AI.Model = model
	if err := cfg.SaveTo(config.Path()); err != nil {
		return AIKeyView{}, errors.New("写配置文件失败：" + err.Error())
	}

	s.applyAIConfig(cfg.AI)
	return s.AIConfig(), nil
}

// applyAIConfig 按配置重建客户端并替换。启动时和保存时走同一条路，
// 免得「重启后能用、保存后不能」这种只在一边出现的差异。
func (s *TopicService) applyAIConfig(cfg config.AI) {
	s.setAIConfig(cfg)
	s.setClient(ai.BuildStack(cfg.Mode, cfg.RelayURL, cfg.RelayToken,
		toModelSpecs(cfg.Models), cfg.Routes,
		ai.ModelSpec{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}))
}

// TestAI 真发一次请求。只回显模型说了什么，不做判断——
// 能返回内容就说明地址、Key、模型名三样都对上了。
func (s *TopicService) TestAI() (string, error) {
	c := s.client()
	if c == nil {
		return "", errors.New("还没配置模型。先在上面填好地址和 Key，点保存")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := ai.CompleteFor(ctx, c, ai.SceneCraft,
		"你是一个连通性测试助手，只回复用户要求的那几个字。", "只回复三个字：连上了")
	if err != nil {
		return "", craftErr(err)
	}
	return strings.TrimSpace(out), nil
}

// client 是读取当前客户端的唯一入口。
//
// 加锁不是为了性能，是因为设置页会在运行期替换它：管道正在跑的时候换掉
// interface 变量，Go 里可能读到「类型是 A、数据是 B」的半成品。低频写入，
// 别的地方照旧只读，一把读写锁就够了。
func (s *TopicService) client() ai.Client {
	s.aiMu.RLock()
	defer s.aiMu.RUnlock()
	return s.aiClient
}

func (s *TopicService) setClient(c ai.Client) {
	s.aiMu.Lock()
	s.aiClient = c
	s.aiMu.Unlock()
}
