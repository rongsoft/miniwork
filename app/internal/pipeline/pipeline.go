// Package pipeline 把取数、落库、打分、排队串成一条可执行流水线。
// CLI 与桌面界面共用这一份，避免两套逻辑走偏。
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/flow"
	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/memory"
	"rongsoft/miniwork/app/internal/score"
	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// Result 是一次执行的汇总，用于界面顶部与 CLI 尾部。
type Result struct {
	Fetched    int
	NewHotspot int
	Saved      int // 本次新入池的选题数
	Updated    int // 之前抓过、这次只重算了分数的条数
	Skipped    int
	Topics     []store.Topic

	// Stale 为真表示这次用的是本地缓存，不是刚取回来的。
	// 界面必须把它显示出来——数据陈旧可以让用户知情，不能让用户误判。
	Stale bool
	// Source 是实际出数的那一端：remote / cache / mock。
	Source string
	// AIScored 是走过模型的条数；AINote 说明模型为什么没用上。
	AIScored int
	AINote   string
}

// Input 是流水线的入参。
type Input struct {
	Niche    string
	Keyword  string // 用户主动输入的关键词；非空时跨赛道挖掘
	Limit    int
	MinScore int
}

// NewEngine 注册全部 step。新增工作流只需在这里多加一个 Step。
// aiClient 可以为 nil，表示只用规则打分。
func NewEngine(st *store.Store, src source.Source, aiClient ai.Client) *flow.Engine {
	e := flow.NewEngine()
	_ = e.Register(&fetchStep{src: src, st: st})
	_ = e.Register(&persistStep{st: st})
	_ = e.Register(&scoreStep{st: st})
	_ = e.Register(&aiStep{st: st, client: aiClient})
	_ = e.Register(&rankStep{st: st})
	return e
}

// Daily 是 MVP 唯一的流水线：取数 -> 落库 -> 打分 -> 模型复核 -> 排队。
func Daily() flow.Flow {
	return flow.Flow{
		Key:  "daily",
		Name: "每日选题",
		Steps: []flow.StepRef{
			{Step: "fetch"},
			{Step: "persist_hotspots"},
			{Step: "score_topics"},
			{Step: "ai_refine"},
			{Step: "rank"},
		},
	}
}

// Run 执行一次 Daily 并整理出界面需要的汇总。
func Run(ctx context.Context, st *store.Store, src source.Source, aiClient ai.Client, in Input, emit flow.Emit) (Result, error) {
	if in.Limit <= 0 {
		in.Limit = 10
	}
	if in.MinScore <= 0 {
		in.MinScore = 50
	}

	eng := NewEngine(st, src, aiClient)
	state, err := eng.Run(ctx, Daily(), map[string]any{
		"niche":     in.Niche,
		"keyword":   in.Keyword,
		"limit":     in.Limit,
		"min_score": in.MinScore,
	}, emit)
	if err != nil {
		return Result{}, err
	}

	res := Result{
		Fetched:    intOf(state, "hotspot_total"),
		NewHotspot: intOf(state, "hotspot_new"),
		Saved:      intOf(state, "topic_saved"),
		Updated:    intOf(state, "topic_updated"),
		Skipped:    intOf(state, "topic_skipped"),
		Source:     stringOf(state, "fetch_source"),
		AIScored:   intOf(state, "ai_scored"),
		AINote:     stringOf(state, "ai_note"),
	}
	res.Stale, _ = state["stale"].(bool)
	if list, ok := state["topic_list"].([]store.Topic); ok {
		res.Topics = list
	}
	return res, nil
}

// Rescore 只重跑「模型复核」这一步，不重新取数、不重新入库。
//
// 为什么需要它：首启动时人设还没填，列表就已经抓回来了。用户填完三问之后，
// 正确的做法是拿新人设把这批选题重新判一遍，而不是整条流水线再跑一次——
// 后者会多抓一次数据、多扣一次配额，换来的结果完全一样。
//
// 代价是规则分会被重算（时效分会随时钟微调），所以这里按当前时刻重算，
// 与 aiStep 内部的口径保持一致，不会出现两条路径分数规则不同的问题。
func Rescore(ctx context.Context, st *store.Store, aiClient ai.Client, niche string, limit int, emit flow.Emit) (Result, error) {
	if limit <= 0 {
		limit = 50
	}
	topics, err := st.TopDraft(limit)
	if err != nil {
		return Result{}, err
	}

	list := make([]source.Hotspot, 0, len(topics))
	ids := make([]string, 0, len(topics))
	for _, t := range topics {
		// 手工加入的选题没有 hotspot 来源，模型也不知道它的热度语境，
		// 跳过比硬编一个 0 热度进去更诚实。
		if t.HotspotID == "" {
			continue
		}
		list = append(list, source.Hotspot{
			ID:          t.HotspotID,
			Platform:    t.Platform,
			Title:       t.Title,
			Heat:        t.Heat,
			PublishedAt: t.PublishedAt,
		})
		ids = append(ids, t.ID)
	}
	if len(list) == 0 {
		return Result{Source: "cache", AINote: "没有可重算的选题"}, nil
	}

	// 没有模型时，规则分就是最终分：按当前人设重算一遍并落库。
	// 这一步不花钱，所以必须无条件做——用户填完三问，当场要看到分数变了，
	// 而不是被告知「下次生成才生效」。等下次，他今天就关掉不会再打开。
	if aiClient == nil {
		if err := applyPrefsOnly(st, topics, niche); err != nil {
			return Result{}, err
		}
		fresh, err := st.TopDraft(limit)
		if err != nil {
			return Result{}, err
		}
		return Result{Source: "cache", Topics: fresh}, nil
	}

	e := flow.NewEngine()
	_ = e.Register(&aiStep{st: st, client: aiClient})
	_ = e.Register(&rankStep{st: st})

	state, err := e.Run(ctx, flow.Flow{
		Key:   "rescore",
		Name:  "按新人设重算",
		Steps: []flow.StepRef{{Step: "ai_refine"}, {Step: "rank"}},
	}, map[string]any{
		"niche":     niche,
		"limit":     limit,
		"hotspots":  list,
		"topic_ids": ids,
	}, emit)
	if err != nil {
		return Result{}, err
	}

	res := Result{
		Source:   "cache",
		AIScored: intOf(state, "ai_scored"),
		AINote:   stringOf(state, "ai_note"),
	}
	if l, ok := state["topic_list"].([]store.Topic); ok {
		res.Topics = l
	}
	return res, nil
}

// memoryState 读出「此刻对这个用户的了解」：人设 + 从选择里学到的偏好词。
//
// 读不到就返回零值不报错。记忆是增强项，不是取数的前置条件：
// 用户的期待是「点了就有东西」，不该因为记忆这块出问题就整页空白。
func memoryState(st *store.Store, niche string) (score.Prefs, []string) {
	p, err := st.Profile()
	if err != nil {
		return score.Prefs{}, nil
	}
	prefs := score.Prefs{
		Positioning: p.Positioning,
		Audience:    p.Audience,
		Avoid:       p.Avoid,
	}

	// 用哪个赛道排除，要和打分用的赛道一致。
	// 人设里记着用户选的赛道，通常以它为准；人设没填时才用调用方传进来的。
	n := p.Niche
	if n == "" {
		n = niche
	}

	d, err := st.RecentDecisions(300)
	if err != nil {
		return prefs, nil
	}

	// 排除两类词：赛道词（规则分里已经算过 40 分）和用户手动屏蔽的。
	//
	// 必须拼成新切片，不能 append(score.NicheWords(n), ...)：
	// NicheWords 返回的是词库 map 里那个切片本身，容量够时 append 会直接
	// 写进原数组，把用户屏蔽的词塞进全局赛道词表——之后所有打分都带着它，
	// 而且没有任何地方会报错，只有「怎么越来越不相关」这种模糊症状。
	nicheWords := score.NicheWords(n)
	blocked := st.BlockedWords()
	exclude := make([]string, 0, len(nicheWords)+len(blocked))
	exclude = append(exclude, nicheWords...)
	exclude = append(exclude, blocked...)

	return prefs, memory.Learn(d.Picked, d.Dropped, exclude).Texts()
}

// applyPrefsOnly 只用规则按当前人设重算，不碰模型。
//
// 只在没有模型时调用：有模型的时候，规则分是加权基础，得等 aiStep 一起算，
// 提前写进去会被覆盖，白写一次库。
func applyPrefsOnly(st *store.Store, topics []store.Topic, niche string) error {
	prof, err := st.Profile()
	if err != nil {
		return nil
	}
	prefs, learnt := memoryState(st, niche)

	// 人设和偏好都空时无事可做。
	//
	// 这里不能只看人设：用户可能一句人设都没填，却已经收下过十几条，
	// 那样学到的偏好早就能改变分数了。按「人设空就返回」的老逻辑，
	// 这层记忆等于白学——算出来了，但从来没被用上。
	//
	// 第三个条件同样必要：即便人设和偏好都没变，也要把缺失的 factors 补上。
	// 否则老库升级后那四条维度条永远是空的——分数没变，谁都不会再算一次。
	if prof.Empty() && len(learnt) == 0 && !hasMissingFactors(topics) {
		return nil
	}

	now := time.Now()
	for _, t := range topics {
		n := t.Niche
		if n == "" {
			n = niche
		}
		h := source.Hotspot{Title: t.Title, Heat: t.Heat, PublishedAt: t.PublishedAt}
		r := score.Evaluate(h, n, now, prefs, learnt)
		// 三个条件都相同才跳过。第三个条件是必须的：老库里面没有 factors
		// （v6 之前只有一段 score_reason），只看「分数和理由没变」就跳过的话，
		// 这些行永远补不上维度，界面上那四条进度条永远是空的。
		if r.Score == t.Score && r.Reason == t.ScoreReason && t.Factors != "" {
			continue
		}
		if err := st.UpdateScoreIfDraft(t.ID, r.Score, r.Reason, factorsJSON(r.Factors)); err != nil {
			return err
		}
	}
	return nil
}

// 四个 step 实现。

type fetchStep struct {
	src source.Source
	st  *store.Store
}

func (s *fetchStep) Name() string { return "fetch" }

// cacheWindow 是降级的可接受范围：三天内的缓存还能当「今天的热点」用。
const cacheWindow = 72 * time.Hour

func (s *fetchStep) Run(ctx context.Context, in map[string]any, emit flow.Emit) (map[string]any, error) {
	niche, _ := in["niche"].(string)
	keyword, _ := in["keyword"].(string)
	keyword = strings.TrimSpace(keyword)

	// 有关键词时赛道可以不填：用户想找的是「所有讲裁员的内容」，
	// 不该被「你必须先选一个赛道」拦住。
	if niche == "" && keyword == "" {
		return nil, errors.New("缺少赛道或关键词参数")
	}

	if keyword != "" {
		emit(20, "正在挖掘："+keyword)
	} else {
		emit(20, "正在取数："+niche)
	}
	list, err := s.src.Fetch(ctx, source.Query{Niche: niche, Keyword: keyword})
	if err == nil {
		if len(list) == 0 && keyword != "" {
			return nil, fmt.Errorf("没找到和「%s」相关的内容。换个说法，或者先按赛道看看现在有什么", keyword)
		}
		emit(100, fmt.Sprintf("取到 %d 条候选", len(list)))
		return map[string]any{"hotspots": list, "fetch_source": s.src.Name()}, nil
	}

	// 关键词搜索不降级到赛道缓存：用户搜「裁员」，缓存里全是「职场」，
	// 退回去等于答非所问——他会以为系统根本没懂他要什么。
	// 宁可明说「搜失败了」，也不要给一份看起来像结果的东西。
	if keyword != "" {
		return nil, friendlyFetchErr(err)
	}

	// 第二级降级：用本地缓存。桌面工具的可用性比数据新鲜度更值钱——
	// 早上打不开，用户当天就不会再打开第二次。
	cached, cerr := s.st.CachedHotspots(niche, time.Now().Add(-cacheWindow), 80)
	if cerr == nil && len(cached) > 0 {
		emit(100, fmt.Sprintf("数据源不可用，改用本地缓存 %d 条", len(cached)))
		return map[string]any{
			"hotspots":     cached,
			"fetch_source": "cache",
			"stale":        true,
		}, nil
	}

	return nil, friendlyFetchErr(err)
}

// friendlyFetchErr 把底层错误翻成用户能读懂、且指向下一步动作的话。
// 直接把 "Get https://...: context deadline exceeded" 甩给创作者等于没说。
func friendlyFetchErr(err error) error {
	switch {
	case errors.Is(err, source.ErrQuotaExceeded):
		return errors.New("今天的数据配额用完了，先用本地缓存撑一撑，明天会自动恢复")
	case errors.Is(err, source.ErrNotConfigured):
		return fmt.Errorf("数据服务还没配好：%w", err)
	default:
		return fmt.Errorf("取数失败（本地也没有可用的缓存）：%w", err)
	}
}

type persistStep struct{ st *store.Store }

func (s *persistStep) Name() string { return "persist_hotspots" }

func (s *persistStep) Run(_ context.Context, in map[string]any, emit flow.Emit) (map[string]any, error) {
	list, ok := in["hotspots"].([]source.Hotspot)
	if !ok {
		return nil, errors.New("上游未提供 hotspots")
	}
	niche, _ := in["niche"].(string)
	n, err := s.st.SaveHotspots(list, niche)
	if err != nil {
		return nil, err
	}
	emit(100, fmt.Sprintf("新增 %d 条，重复 %d 条", n, len(list)-n))
	return map[string]any{"hotspot_new": n, "hotspot_total": len(list)}, nil
}

type scoreStep struct{ st *store.Store }

func (s *scoreStep) Name() string { return "score_topics" }

func (s *scoreStep) Run(_ context.Context, in map[string]any, emit flow.Emit) (map[string]any, error) {
	list, ok := in["hotspots"].([]source.Hotspot)
	if !ok {
		return nil, errors.New("上游未提供 hotspots")
	}
	niche, _ := in["niche"].(string)
	minScore := intOf(in, "min_score")
	if minScore == 0 {
		minScore = 50
	}
	now := time.Now()

	// 记忆读一次就够，放在循环外：同一个用户这轮生成里不会中途改。
	// 读不到也不影响出数——记忆是增强项，不是前置条件。
	prefs, learnt := memoryState(s.st, niche)

	saved, updated, skipped := 0, 0, 0
	ids := make([]string, 0, len(list))
	for i, h := range list {
		id := store.NewTopicID(h.ID)
		r := score.Evaluate(h, niche, now, prefs, learnt)
		if r.Score < minScore {
			// 已经躺在待定列表里的（用户刚补了「不碰法律」这类禁忌），
			// 要把新分数写回去。不写的话旧的高分会一直挂在列表上，
			// 用户看到的就是「我填的禁忌根本没生效」。
			_ = s.st.UpdateScoreIfDraft(id, r.Score, r.Reason, factorsJSON(r.Factors))
			skipped++
			continue
		}
		created, err := s.st.SaveTopic(store.Topic{
			ID:          id,
			Title:       h.Title,
			Platform:    h.Platform,
			Niche:       niche,
			Score:       r.Score,
			ScoreReason: r.Reason,
			Factors:     factorsJSON(r.Factors),
			Source:      "hotspot",
			Status:      topic.Draft,
			HotspotID:   h.ID,
			Heat:        h.Heat,
			PublishedAt: h.PublishedAt,
			CreatedAt:   now,
		})
		if err != nil {
			return nil, err
		}
		// 新增和「刷新旧条目」必须分开报数：每轮都会把整批候选重新写一遍，
		// 混成一笔的话每天都在报「入选 20 条」，而列表一条没多。
		if created {
			saved++
		} else {
			updated++
		}
		ids = append(ids, id)
		if len(list) > 10 && i%10 == 9 {
			emit(i*100/len(list), fmt.Sprintf("已打分 %d/%d", i+1, len(list)))
		}
	}
	emit(100, fmt.Sprintf("新增 %d 条，重算 %d 条，低于阈值丢弃 %d 条", saved, updated, skipped))
	return map[string]any{
		"topic_saved":   saved,
		"topic_updated": updated,
		"topic_skipped": skipped,
		"topic_ids":     ids,
	}, nil
}

// 模型分与规则分的权重。
//
// 规则分里有热度和时效，这是模型在 prompt 里只看到「18.2万互动」四个字
// 判断不了的东西；但「适不适合你」只有模型能给。所以两边都要，模型略重。
// 这条权重写死在代码里而不是配置项：用户不该为这种事做决定。
const (
	aiWeight   = 0.6
	ruleWeight = 0.4
)

// aiStep 是「带记忆的复核」。
//
// 它是整条流水线里唯一会花钱、也唯一可能失败的一步，所以单独拆出来：
// 失败就退回规则分，绝不让模型服务的问题把用户的选题流程卡死。
type aiStep struct {
	st     *store.Store
	client ai.Client
}

func (s *aiStep) Name() string { return "ai_refine" }

func (s *aiStep) Run(ctx context.Context, in map[string]any, emit flow.Emit) (map[string]any, error) {
	if s.client == nil {
		return map[string]any{"ai_scored": 0}, nil
	}
	list, ok := in["hotspots"].([]source.Hotspot)
	if !ok || len(list) == 0 {
		return map[string]any{"ai_scored": 0}, nil
	}
	niche, _ := in["niche"].(string)

	// 记忆：人设 + 历史选择。这就是「长期记忆」在产品里的全部含义——
	// 不需要向量库，一个人的选题偏好用几十条标题就说得清。
	p, err := s.st.Profile()
	if err != nil {
		return degradedAI(err)
	}
	d, err := s.st.RecentDecisions(40)
	if err != nil {
		return degradedAI(err)
	}

	// 规则分在这里也要过一遍人设否决与偏好加成，否则入库时的分和这里的基线
	// 口径不一致，会出现「规则说 40 分、最终分却按 70 算」的错位。
	prefs, learnt := memoryState(s.st, niche)

	brief := ai.Brief{
		Niche:       niche,
		Positioning: p.Positioning,
		Audience:    p.Audience,
		Tone:        p.Tone,
		Avoid:       p.Avoid,
		Picked:      d.Picked,
		Dropped:     d.Dropped,
	}

	items := make([]ai.Item, 0, len(list))
	for _, h := range list {
		items = append(items, ai.Item{
			ID:       h.ID,
			Platform: h.Platform,
			Title:    h.Title,
			Heat:     h.Heat,
			AgeLabel: format.Age(h.PublishedAt),
		})
	}

	system, user := ai.BuildPrompt(brief, items)
	key := promptKey(s.client.Name(), system, user)

	emit(30, "AI 正在按你的口味复核")

	content, hit, err := s.st.AICacheGet(key)
	if err != nil {
		return degradedAI(err)
	}
	if hit {
		emit(60, "命中模型结果缓存，不重复计费")
	} else {
		content, err = s.client.Complete(ctx, system, user)
		if err != nil {
			return degradedAI(err)
		}
		// 缓存写失败不影响本次结果，只是下次要多花一次钱。
		_ = s.st.AICachePut(key, s.client.Name(), content)
	}

	results, err := ai.Parse(content)
	if err != nil {
		return degradedAI(err)
	}

	// 只更新这一轮真正入库的选题，别去动历史记录。
	wanted := make(map[string]bool, len(list))
	if ids, ok := in["topic_ids"].([]string); ok {
		for _, id := range ids {
			wanted[id] = true
		}
	}

	now := time.Now()
	n := 0
	for _, h := range list {
		id := store.NewTopicID(h.ID)
		if len(wanted) > 0 && !wanted[id] {
			continue
		}
		r, ok := results[h.ID]
		if !ok {
			continue
		}
		rr := score.Evaluate(h, niche, now, prefs, learnt)
		rule := rr.Score
		final := int(math.Round(aiWeight*float64(r.Fit) + ruleWeight*float64(rule)))
		if err := s.st.UpdateAIScore(id, r.Fit, final, r.Reason, factorsJSON(rr.Factors)); err != nil {
			return nil, err
		}
		n++
	}

	if n == 0 {
		return map[string]any{"ai_scored": 0, "ai_note": "模型没有返回可用的判断，这次用规则分"}, nil
	}
	emit(100, fmt.Sprintf("AI 复核 %d 条", n))
	return map[string]any{"ai_scored": n}, nil
}

// degradedAI 把 AI 失败翻译成一句用户看得懂的话，并且不打断流水线。
// 这里是「降级」而不是「报错」：AI 是增强项，规则分永远在。
func degradedAI(err error) (map[string]any, error) {
	var note string
	switch {
	case errors.Is(err, ai.ErrQuotaExceeded):
		note = "模型额度用完，这次用规则分"
	case errors.Is(err, ai.ErrUnauthorized):
		note = "模型密钥失效，这次用规则分"
	case errors.Is(err, ai.ErrNotConfigured):
		note = "模型没配好，这次用规则分"
	default:
		note = "AI 复核失败，已退回规则分：" + err.Error()
	}
	if r := []rune(note); len(r) > 60 {
		note = string(r[:60]) + "…"
	}
	return map[string]any{"ai_scored": 0, "ai_note": note}, nil
}

// promptKey 让同一份提示词只花一次钱。
// 提示词里已经含标题、热度、时效和记忆，用户改任何一项都会自然换 key，
// 所以不需要额外设过期时间。
func promptKey(clientName, system, user string) string {
	sum := sha256.Sum256([]byte(clientName + "\x00" + ai.SceneScore + "\x00" + system + "\x00" + user))
	return hex.EncodeToString(sum[:])
}

type rankStep struct{ st *store.Store }

func (s *rankStep) Name() string { return "rank" }

func (s *rankStep) Run(_ context.Context, in map[string]any, emit flow.Emit) (map[string]any, error) {
	limit := intOf(in, "limit")
	if limit == 0 {
		limit = 10
	}
	list, err := s.st.TopDraft(limit)
	if err != nil {
		return nil, err
	}
	emit(100, fmt.Sprintf("待定 %d 条", len(list)))
	return map[string]any{"topic_list": list}, nil
}

func intOf(m map[string]any, key string) int {
	if v, ok := m[key].(int); ok {
		return v
	}
	return 0
}

func stringOf(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// factorsJSON 把评分维度序列化后存库。
//
// 编码失败就返回空串：界面上少四条进度条，比整批选题因此写不进去好得多。
// 这几个 float 值不可能编码失败，兜底是为了「以后加维度时有人塞进一个
// 不可编码的类型」这种改动——那时候宁可丢维度，不能丢选题。
func factorsJSON(fs []score.Factor) string {
	if len(fs) == 0 {
		return ""
	}
	raw, err := json.Marshal(fs)
	if err != nil {
		return ""
	}
	return string(raw)
}

// hasMissingFactors 判断这批选题里有没有「分数在、维度缺」的。
//
// 专门服务升级路径：v6 -> v7 加了 factors 列，老行全是空串。
// 没有这个判断，重算会被人设和偏好都为空的短路挡在门外，
// 结果就是界面上四条进度条永远空白，而代码看起来完全正常。
func hasMissingFactors(topics []store.Topic) bool {
	for _, t := range topics {
		if t.Factors == "" {
			return true
		}
	}
	return false
}
