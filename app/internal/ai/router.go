package ai

import (
	"context"
	"fmt"
	"strings"
)

// 场景名。配置里的 routes 用这些字符串做键，改了就是改配置契约。
//
// 只有两个：打分与创作。拆解和起标题都归到创作——它们同样要求「给得出
// 具体句子」，和写稿是同一类活；再细分下去，用户要多配几行，
// 而换来的差异他自己也感受不到。
const (
	SceneCraftKey = "craft"
)

// ModelSpec 是一个模型的连接信息。
//
// 与 config.Model 结构相同但刻意不复用：ai 包不该依赖 config，
// 否则所有测试都得先造一份配置文件。转换在 main 里做一次。
type ModelSpec struct {
	ID      string
	Label   string
	BaseURL string
	APIKey  string
	Model   string
	Note    string
}

// Router 按场景把请求分给不同的模型。
//
// 「多个大模型配合」在产品上只有一个真正有用的形态：让便宜的干粗活、
// 强的干细活。剩下的几种玩法都不划算——
// 让两个模型对同一条选题投票，成本翻倍，而它俩给的分本来就会趋同；
// 让一个模型审另一个的稿，得多花一次长输出，审出来的意见还都是「可以更具体」。
//
// 分工的依据是这两件事的物理差异：
//   - 打分：一次 50 条、要稳定、输出极短 → 快模型，温度 0.3
//   - 写稿：一次一篇、要发散、输出上千字 → 强模型，温度 0.85
type Router struct {
	// scenes 是「场景 -> 客户端」。没配的场景走 def。
	scenes map[string]Client
	// ids 是「场景 -> 模型标识」，只用于在界面上回答「这一段是谁写的」。
	ids   map[string]string
	def   Client
	defID string
}

// NewRouter 用一个兜底客户端建路由器。defID 是它在配置里的标识。
func NewRouter(def Client, defID string) *Router {
	return &Router{scenes: map[string]Client{}, ids: map[string]string{}, def: def, defID: defID}
}

// Route 把一个场景指到某个客户端。传 nil 等于取消这个场景的分工。
func (r *Router) Route(scene, id string, c Client) *Router {
	if c == nil {
		delete(r.scenes, scene)
		delete(r.ids, scene)
		return r
	}
	r.scenes[scene] = c
	r.ids[scene] = id
	return r
}

// SceneModel 返回某场景实际使用的上游模型名，给界面用。
func (r *Router) SceneModel(scene string) string { return modelName(r.Scene(scene)) }

// SceneID 返回某场景实际用到的模型标识。没配就是兜底那个。
func (r *Router) SceneID(scene string) string {
	if id, ok := r.ids[scene]; ok && id != "" {
		return id
	}
	return r.defID
}

// Scene 取某个场景实际会用到的客户端。
// 没有专门配置就返回兜底——界面上要能回答「这一段到底是谁写的」。
func (r *Router) Scene(scene string) Client {
	if c, ok := r.scenes[scene]; ok && c != nil {
		return c
	}
	return r.def
}

// Split 报告是否真的存在分工。
// 只配了一个模型时不该在界面上吹「多模型协作」，那是在给用户看配置。
func (r *Router) Split() bool {
	if len(r.scenes) == 0 || r.def == nil {
		return len(r.scenes) > 1
	}
	for _, c := range r.scenes {
		if c != r.def {
			return true
		}
	}
	return false
}

// Name 是给界面看的一句话。分工时列出两边分别是谁，
// 不分工时就是单个模型名。
func (r *Router) Name() string {
	if !r.Split() {
		if r.def != nil {
			return modelName(r.def)
		}
		return "关闭"
	}
	return fmt.Sprintf("%s 打分 / %s 写稿", modelName(r.Scene(SceneScore)), modelName(r.Scene(SceneCraft)))
}

// Complete 是打分通道。
func (r *Router) Complete(ctx context.Context, system, user string) (string, error) {
	c := r.Scene(SceneScore)
	if c == nil {
		return "", ErrNotConfigured
	}
	return c.Complete(ctx, system, user)
}

// CompleteCreative 是写稿通道。
//
// 这里要处理一种真实存在的组合：写稿用的是个只实现了 Complete 的客户端
// （比如测试替身，或者某个只支持基础协议的服务）。那就退回普通通道，
// 稿子会保守一点，但功能不是坏的。
func (r *Router) CompleteCreative(ctx context.Context, system, user string) (string, error) {
	c := r.Scene(SceneCraft)
	if c == nil {
		return "", ErrNotConfigured
	}
	return Write(ctx, c, system, user)
}

// Roles 报告每个场景用的是哪个模型，给设置页用。
func (r *Router) Roles() map[string]string {
	out := map[string]string{}
	for _, s := range []string{SceneScore, SceneCraft} {
		if c := r.Scene(s); c != nil {
			out[s] = modelName(c)
		}
	}
	return out
}

// BuildStack 按配置组装模型调用链。
//
// 三种情况：
//   - mode=off 或配置不全 → nil（调用方按「没模型」处理，退回规则）
//   - 只有一个模型（没填 models）→ 直接返回它，不包 Router：
//     包一层没有任何好处，只会让日志里多一个无意义的名字
//   - 多个模型 → Router，按 routes 分工，未指定的场景用第一个
func BuildStack(mode, relayURL, relayToken string, models []ModelSpec, routes map[string]string, single ModelSpec) Client {
	if len(models) > 0 {
		clients := make(map[string]Client, len(models))
		order := make([]string, 0, len(models))
		for _, m := range models {
			// 配一半的条目直接跳过。NewByok 本身不校验，空 base 会造出一个
			// 每次调用都返 ErrNotConfigured 的客户端——它算作「有模型」，
			// 会让分工判定和「能不能用」的判断同时失真。
			if m.BaseURL == "" || m.APIKey == "" {
				continue
			}
			// 多模型只在 byok 下有意义：relay 的 Key 在服务端，
			// 客户端没有「换一个 Key 试试」这回事。
			c := NewByok(m.BaseURL, m.APIKey, m.Model)
			if c == nil {
				continue
			}
			clients[m.ID] = c
			order = append(order, m.ID)
		}
		if len(clients) == 0 {
			return nil
		}
		def := clients[order[0]]
		r := NewRouter(def, order[0])

		// applied 记「哪条 route 真的落上了」。不能只看 routes 里有没有这个键：
		// 指向一个不存在的 id 时键在、效果没在，那种情况必须落回默认分工，
		// 否则第二个模型配了却从不被用，界面上只会显示「未分工」。
		applied := map[string]bool{}
		for scene, id := range routes {
			if c, ok := clients[id]; ok {
				r.Route(scene, id, c)
				applied[scene] = true
			}
		}

		// 没写 routes 又配了多个模型时，打分用第一个、写稿用最后一个。
		//
		// 这是猜，但猜得有理：人列模型清单的顺序几乎总是从便宜到贵，
		// 而多模型的全部价值就在于「粗活省钱、细活花钱」。全用第一个
		// 等于配了等于没配，用户还会以为是功能没生效。设置页会明写
		// 每个场景实际用的是谁，猜错了当场就能看见。
		if len(order) > 1 && !applied[SceneCraft] {
			last := order[len(order)-1]
			if last != order[0] {
				r.Route(SceneCraft, last, clients[last])
			}
		}

		if !r.Split() {
			// 分工没成（比如两个 id 其实指向同一个模型），就当单模型用。
			// 包一层 Router 只会让「用了哪个模型」这个问题多绕一道。
			return def
		}
		return r
	}

	return New(mode, relayURL, relayToken, single.BaseURL, single.APIKey, single.Model)
}

// ParseScene 把界面传来的场景名收敛到已知集合，避免拼错产生一条静默失效的配置。
func ParseScene(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case SceneScore, SceneCraft:
		return s, true
	}
	return "", false
}

// CompleteFor 用指定场景的模型走普通（低温度）通道。
//
// 拆解和起标题要的是「用好模型、但别瞎发挥」：它们属于创作类，
// 该和写稿用同一个强模型；但它们输出的是分析结论，温度必须压住，
// 所以不能走 CompleteCreative。
func CompleteFor(ctx context.Context, c Client, scene, system, user string) (string, error) {
	if r, ok := c.(*Router); ok {
		target := r.Scene(scene)
		if target == nil {
			return "", ErrNotConfigured
		}
		return target.Complete(ctx, system, user)
	}
	return c.Complete(ctx, system, user)
}

// Named 是可选接口：能报出自己实际使用的上游模型名。
//
// 光靠 Name() 不够：Byok.Name() 返回的是「byok」这个模式名，
// 界面上显示「byok 打分 / byok 写稿」等于什么都没说，
// 而用户配多模型时最想确认的恰恰是「写稿用的到底是哪个」。
type Named interface {
	ModelName() string
}

// ModelNameOf 返回客户端的上游模型名，取不到时退回 Name()。
func ModelNameOf(c Client) string { return modelName(c) }

func modelName(c Client) string {
	if c == nil {
		return "关闭"
	}
	if n, ok := c.(Named); ok {
		if v := strings.TrimSpace(n.ModelName()); v != "" {
			return v
		}
	}
	return c.Name()
}
