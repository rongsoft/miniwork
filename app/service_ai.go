package main

import (
	"strings"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/config"
)

// 多模型这一块在界面上只回答三个问题：
// 现在有哪几个模型、每个场景实际用哪个、想改去哪儿改。
//
// 不做「在界面里填 Key」这件事：Key 是明文存进 config.json 的，
// 让用户在界面里输入只会让他以为它是加密存的。配置就放在那个文件里，
// 他能看见、能备份、能整个删掉。

// AIModelCard 是设置页上的一行模型。
type AIModelCard struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Model   string   `json:"model"`
	BaseURL string   `json:"baseUrl"`
	Note    string   `json:"note"`
	KeyHint string   `json:"keyHint"` // sk-…a1b2，只给认得出的程度
	Scenes  []string `json:"scenes"`  // 被哪些场景用着
}

// AISceneView 是「这个场景用哪个模型」的一行。
type AISceneView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Why   string `json:"why"` // 为什么这个场景要单独挑模型
	Model string `json:"model"`
}

// AIStackView 描述当前模型是怎么分工的。
type AIStackView struct {
	Enabled    bool          `json:"enabled"`
	Mode       string        `json:"mode"`
	Summary    string        `json:"summary"`
	Scenes     []AISceneView `json:"scenes"`
	Models     []AIModelCard `json:"models"`
	ConfigPath string        `json:"configPath"`
	Hint       string        `json:"hint"`
}

// AIStack 返回模型分工的当前状态。
func (s *TopicService) AIStack() AIStackView {
	path := config.Path()
	client := s.client()
	if client == nil {
		return AIStackView{
			Enabled:    false,
			Mode:       s.aiCfg.Mode,
			Summary:    "关闭（规则打分）",
			ConfigPath: path,
			Hint:       "填一个模型就能用。打分与写稿可以分开配，见 config.json 里的 models 和 routes。",
			Scenes: []AISceneView{
				{Key: ai.SceneScore, Label: "批量打分", Why: "一次 50 条、要稳定不要创意", Model: "关闭"},
				{Key: ai.SceneCraft, Label: "写稿与拆解", Why: "要发散、要长输出", Model: "关闭"},
			},
		}
	}

	scenes := []AISceneView{
		{Key: ai.SceneScore, Label: "批量打分", Why: "一次 50 条，只要稳定，便宜的模型足够", Model: s.sceneModel(ai.SceneScore)},
		{Key: ai.SceneCraft, Label: "写稿与拆解", Why: "要长出稿、要给出具体句子，值得用好的", Model: s.sceneModel(ai.SceneCraft)},
	}

	cards := make([]AIModelCard, 0, len(s.aiCfg.Models))
	if len(s.aiCfg.Models) == 0 {
		// 单模型：也列成一张卡，否则界面上「有哪几个模型」是空的。
		cards = append(cards, AIModelCard{
			ID: "single", Label: s.sceneModel(ai.SceneScore),
			Model: s.aiCfg.Model, BaseURL: s.aiCfg.BaseURL,
			Note: "未分工：所有场景都用它", KeyHint: maskKey(s.aiCfg.APIKey),
			Scenes: []string{ai.SceneScore, ai.SceneCraft},
		})
	} else {
		for _, m := range s.aiCfg.Models {
			cards = append(cards, AIModelCard{
				ID: m.ID, Label: m.Label, Model: m.Model, BaseURL: m.BaseURL,
				Note: m.Note, KeyHint: maskKey(m.APIKey),
				Scenes: s.scenesOf(m.ID),
			})
		}
	}

	return AIStackView{
		Enabled:    true,
		Mode:       s.aiCfg.Mode,
		Summary:    ai.ModelNameOf(client),
		Scenes:     scenes,
		Models:     cards,
		ConfigPath: path,
		Hint:       hint(len(s.aiCfg.Models)),
	}
}

// sceneModel 取某场景实际使用的上游模型名。
func (s *TopicService) sceneModel(scene string) string {
	c := s.client()
	if c == nil {
		return "关闭"
	}
	if r, ok := c.(*ai.Router); ok {
		return r.SceneModel(scene)
	}
	return ai.ModelNameOf(c)
}

// scenesOf 返回某个模型被哪些场景用着。
func (s *TopicService) scenesOf(id string) []string {
	out := []string{}
	r, ok := s.client().(*ai.Router)
	if !ok {
		return out
	}
	for _, sc := range []string{ai.SceneScore, ai.SceneCraft} {
		if r.SceneID(sc) == id {
			out = append(out, sc)
		}
	}
	return out
}

func hint(n int) string {
	if n <= 1 {
		return "想让它分工？在 config.json 的 ai.models 里加第二条，再用 ai.routes 指定哪个场景用哪个。"
	}
	return "打分用便宜的、写稿用好的，等于把钱花在看得见的地方。改 models 与 routes 后重启生效。"
}

// maskKey 只留头尾各 4 位。
// 完整 Key 不该出现在任何界面上：截图、录屏、远程协助时它都会被带走。
func maskKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return "未设置"
	}
	r := []rune(k)
	if len(r) <= 10 {
		return "已设置"
	}
	return string(r[:4]) + "…" + string(r[len(r)-4:])
}
