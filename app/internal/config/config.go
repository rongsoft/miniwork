// Package config 只管一件事：从用户配置目录读一份 json，缺失就写一份带注释味的默认值。
//
// 刻意不做的东西：配置分环境、配置热加载、配置中心。桌面工具的一份配置
// 就该是用户能拿文本编辑器改的两个字段。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Remote 是数据服务的连接信息。
type Remote struct {
	BaseURL string `json:"baseUrl"` // 例：https://api.rongsoft.com/miniwork
	Token   string `json:"token"`   // 服务端签发，可为空（内网/调试）
}

// AI 是模型调用的配置。
//
// 三种模式：
//   - off   只用规则打分（默认）。规则零成本、毫秒级，是永远在的基线。
//   - relay 走我们自己的 /ai/relay：Key 在服务端，按 license 计额度。
//   - byok  用户自己填 OpenAI 兼容的 Key，直连，不经我们。
//
// 默认 off：没有服务端时，AI 打分只会让首启动卡住。
type AI struct {
	Mode string `json:"mode"` // off | relay | byok

	RelayURL   string `json:"relayUrl"`   // relay 模式：https://api.rongsoft.com/miniwork
	RelayToken string `json:"relayToken"` // relay 模式：license token

	// 单模型配置（byok 模式）。Models 为空时用它，所有场景都走它。
	BaseURL string `json:"baseUrl"` // https://api.deepseek.com/v1
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"` // deepseek-chat

	// Models 是多模型清单。填了就按场景分工，不填就走上面那一个。
	//
	// 为什么这是「多个大模型配合」唯一有意义的形式：打分是批量的（一次 50 条）、
	// 要稳定不要创意，成稿是单篇的、要发散要长输出。让它们共用一个模型，
	// 要么打分贵得离谱，要么稿子写得像会议纪要。分开放，各用各的长处。
	Models []Model `json:"models,omitempty"`

	// Routes 把场景映射到 Models 里的 id，如 {"score":"cheap","craft":"strong"}。
	// 没指定的场景退回 Models 的第一个——不猜「哪个最强」，猜错用户看不出来，
	// 只会觉得稿子突然变笨了。
	Routes map[string]string `json:"routes,omitempty"`
}

// Model 是一个可用的模型端点。
type Model struct {
	ID      string `json:"id"`    // 内部标识，routes 里用它，如 cheap
	Label   string `json:"label"` // 显示名，如 DeepSeek Chat
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"` // 上游模型名，如 deepseek-chat
	Note    string `json:"note"`  // 干什么用的，写给人看
}

// WeChat 是公众号的发布凭证。
//
// 只存 AppID / AppSecret，不存账号密码：接口用的是凭证，不是登录态。
// IP 白名单在公众号后台配，本地记不住也管不了，所以报错时要把它显示出来。
type WeChat struct {
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
}

// Config 是完整的用户配置。
type Config struct {
	Source string `json:"source"` // mock | remote
	Remote Remote `json:"remote"`
	AI     AI     `json:"ai"`
	WeChat WeChat `json:"wechat"`
}

// Default 是首次启动写进磁盘的内容。
//
// 默认用 mock 而不是 remote：新装用户没有服务端地址，默认 remote 只会得到
// 一个打不开的空界面。「示例数据」至少能让用户看见产品长什么样。
func Default() Config {
	return Config{Source: "mock", AI: AI{Mode: "off"}}
}

// Dir 返回配置与数据库共用的目录。
func Dir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "miniwork")
	}
	return "."
}

// Path 返回配置文件路径。
func Path() string { return filepath.Join(Dir(), "config.json") }

// Load 读配置。文件不存在时写默认值并返回它，不报错——首次启动不该因为
// 少一个文件就退出。
func Load() (Config, error) { return LoadFrom(Path()) }

// LoadFrom 读指定路径，便于测试。
func LoadFrom(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// 首次启动：写一份默认值，方便用户直接改。
		c := Default()
		if e := c.SaveTo(path); e != nil {
			// 写不进去不是致命错误，配置照样能用：只是用户下次还得改。
			return finish(c), fmt.Errorf("配置目录不可写: %w", e)
		}
		return finish(c), nil
	}
	if err != nil {
		return finish(Default()), fmt.Errorf("读取配置失败: %w", err)
	}

	c := Default()
	if err := json.Unmarshal(raw, &c); err != nil {
		// 坏配置也给一份能用的默认值，让调用方决定是打日志还是继续。
		return finish(Default()), fmt.Errorf("配置文件不是合法 json（%s）: %w", path, err)
	}
	return finish(c), nil
}

// finish 统一收尾：规范化 + 环境变量覆盖。
// 它必须走遍每一条返回路径——之前漏了「文件不存在」那条，导致容器里
// 只给环境变量、不给文件时，配置被静默忽略，白白调了半天。
func finish(c Config) Config {
	c.normalize()
	applyEnv(&c)
	return c
}

// SaveTo 写磁盘，用于界面上填地址后保存。
func (c Config) SaveTo(path string) error {
	c.normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func (c *Config) normalize() {
	c.Source = strings.ToLower(strings.TrimSpace(c.Source))
	if c.Source != "remote" {
		c.Source = "mock"
	}
	c.Remote.BaseURL = strings.TrimRight(strings.TrimSpace(c.Remote.BaseURL), "/")
	c.Remote.Token = strings.TrimSpace(c.Remote.Token)

	c.AI.Mode = strings.ToLower(strings.TrimSpace(c.AI.Mode))
	switch c.AI.Mode {
	case "relay", "byok":
	default:
		c.AI.Mode = "off"
	}
	c.AI.RelayURL = strings.TrimRight(strings.TrimSpace(c.AI.RelayURL), "/")
	c.AI.RelayToken = strings.TrimSpace(c.AI.RelayToken)
	c.AI.BaseURL = strings.TrimRight(strings.TrimSpace(c.AI.BaseURL), "/")
	c.AI.APIKey = strings.TrimSpace(c.AI.APIKey)
	c.AI.Model = strings.TrimSpace(c.AI.Model)
	c.normalizeModels()

	c.WeChat.AppID = strings.TrimSpace(c.WeChat.AppID)
	c.WeChat.AppSecret = strings.TrimSpace(c.WeChat.AppSecret)
}

// normalizeModels 清理多模型清单。
//
// 两条硬要求：id 不能空（routes 靠它索引），id 不能重复（后一个会静默
// 顶掉前一个，而用户以为自己配了两个）。指向不存在 id 的 route 直接删掉——
// 留着它会让「配了但没生效」变成一种查不出来的状态。
func (c *Config) normalizeModels() {
	if len(c.AI.Models) == 0 {
		c.AI.Models = nil
		c.AI.Routes = nil
		return
	}
	seen := make(map[string]bool, len(c.AI.Models))
	out := make([]Model, 0, len(c.AI.Models))
	for _, m := range c.AI.Models {
		m.ID = strings.TrimSpace(m.ID)
		m.Label = strings.TrimSpace(m.Label)
		m.BaseURL = strings.TrimRight(strings.TrimSpace(m.BaseURL), "/")
		m.APIKey = strings.TrimSpace(m.APIKey)
		m.Model = strings.TrimSpace(m.Model)
		m.Note = strings.TrimSpace(m.Note)

		// id 缺省时用上游模型名兜底：让人必须给 id 才能用，
		// 会让「我只想加个模型」变成一次阅读文档的成本。
		if m.ID == "" {
			m.ID = m.Model
		}
		if m.ID == "" || m.BaseURL == "" || m.APIKey == "" {
			continue // 配一半的条目不算数，宁可不用也不要半渲染
		}
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		if m.Label == "" {
			m.Label = m.Model
		}
		out = append(out, m)
	}
	c.AI.Models = out

	if len(c.AI.Routes) > 0 {
		routes := make(map[string]string, len(c.AI.Routes))
		for k, v := range c.AI.Routes {
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.TrimSpace(v)
			if k == "" || v == "" || !seen[v] {
				continue
			}
			routes[k] = v
		}
		c.AI.Routes = routes
	}
}

// applyEnv 让自动化测试和临时排查不必改文件。
// 环境变量优先于文件，这是唯一一处「两套来源」的地方，且只读。
func applyEnv(c *Config) {
	if v := strings.TrimSpace(os.Getenv("MINIWORK_SOURCE")); v != "" {
		c.Source = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_API_BASE")); v != "" {
		c.Remote.BaseURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("MINIWORK_API_TOKEN"); v != "" {
		c.Remote.Token = strings.TrimSpace(v)
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_AI_MODE")); v != "" {
		c.AI.Mode = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_AI_RELAY_URL")); v != "" {
		c.AI.RelayURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_AI_RELAY_TOKEN")); v != "" {
		c.AI.RelayToken = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_AI_BASE")); v != "" {
		c.AI.BaseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_AI_KEY")); v != "" {
		c.AI.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_AI_MODEL")); v != "" {
		c.AI.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_WECHAT_APPID")); v != "" {
		c.WeChat.AppID = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIWORK_WECHAT_SECRET")); v != "" {
		c.WeChat.AppSecret = v
	}
	c.normalize()
}
