package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/config"
	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/memory"
	"rongsoft/miniwork/app/internal/pipeline"
	"rongsoft/miniwork/app/internal/schedule"
	"rongsoft/miniwork/app/internal/score"
	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// TopicService 是前端唯一能碰到的 Go 入口。
// 所有方法都会暴露成 JS 调用，方法签名要经得起 JSON 序列化。
type TopicService struct {
	st       *store.Store
	src      source.Source
	srcLabel string
	dbPath   string
	app      *application.App
	sched    *schedule.Scheduler

	// aiClient / aiCfg 会在运行期被设置页替换（填完 Key 不重启就生效），
	// 所以读要走 s.client() / s.aiCfgOf()，别直接碰字段。
	aiMu     sync.RWMutex
	aiClient ai.Client
	aiCfg    config.AI

	// sh 是桌面外壳（托盘、关窗行为）。main 里在窗口建好后注入，
	// 设置页的「后台运行」要读它、改它。
	sh *shell

	// pub 是自动发布的浏览器会话，第一次用到才建。
	pubOnce sync.Once
	pub     *publishState

	// liveWin 是「拆出去的那个平台窗口」。
	//
	// 放这里而不是塞进 publishState：publishState 的锁护着发布任务，
	// 一个窗口的开关不该排在一次发布后面。
	liveWin liveWinState

	// syncMu / syncAt 是账号同步的节流。Account() 会被界面反复调用，
	// 每调一次就发一个网络请求等于给启动路径挂了个网络依赖。
	syncMu sync.Mutex
	syncAt time.Time

	mu      sync.Mutex
	running bool
}

func NewTopicService(st *store.Store, src source.Source, srcLabel string, aiClient ai.Client, dbPath string) *TopicService {
	return &TopicService{st: st, src: src, srcLabel: srcLabel, aiClient: aiClient, dbPath: dbPath}
}

// DBPath 给设置页展示，用户想手工备份或换机时得能找到这个文件。
func (s *TopicService) DBPath() string { return s.dbPath }

// Platform 返回当前操作系统（darwin / windows / linux）。
//
// 界面靠它决定要不要留出窗口顶部那条 46px 的可拖拽区：那条是为 macOS 的
// 隐藏式标题栏留的（红绿灯压在内容上）。Windows 有真标题栏，再留 46px
// 就是标题栏下面多一条空白，看着像没对齐。
func (s *TopicService) Platform() string { return runtime.GOOS }

// setApp 在 application.New 之后注入，用于向界面推进度事件。
func (s *TopicService) setApp(app *application.App) { s.app = app }

// setShell 在窗口建好之后注入。托盘和关窗行为都归它管。
func (s *TopicService) setShell(sh *shell) { s.sh = sh }

// setAIConfig 注入模型配置原文。设置页要显示「配了哪几个、每个用在哪」，
// 这些东西只有配置里有，从已建好的客户端反推不出来。
func (s *TopicService) setAIConfig(cfg config.AI) { s.aiCfg = cfg }

// TopicView 是给界面的选题形态：只带展示需要的字段，不暴露内部结构。
type TopicView struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	Niche        string `json:"niche"`
	Score        int    `json:"score"`
	AIScore      int    `json:"aiScore"` // -1 表示没走过模型
	Reason       string `json:"reason"`
	HeatLabel    string `json:"heatLabel"`
	AgeLabel     string `json:"ageLabel"`
	Status       string `json:"status"`
	StatusLabel  string `json:"statusLabel"`
	PlanDate     string `json:"planDate"`  // yyyy-mm-dd，空表示没排期
	PlanLabel    string `json:"planLabel"` // 11-15，日历和卡片角标用

	// Dims 是卡片上那四条进度条（热度/新颖/匹配/可写），值域 0-100。
	// 老数据没有 factors 时返回空数组，前端不画这四条，而不是画四条 0。
	Dims []Dim `json:"dims"`

	// Reasons 是「为什么推荐你写」，3-4 条短句，全部来自规则打分的分项说明。
	// 和 Reason 的区别：Reason 是一段连着的解释，放在卡片标题下面一行；
	// Reasons 是可以逐条对勾的清单，用户扫一眼就知道这条合不合胃口。
	Reasons []string `json:"reasons"`

	// Steps 是这条选题要走的阶段名，Done 是已经走到第几步（1 表示刚开始）。
	// 「第 2/4 步」这种话用户一看就懂，比 status 的英文枚举有用。
	Steps []string `json:"steps"`
	Done  int      `json:"done"`

	// Today 表示这条是本次刷新抓回来的（在列表里排在最前面那一批）。
	// 界面靠它在「今天这批」和「之前没处理的」之间插一条分隔；
	// 没有这个字段，用户没法判断一张卡是三小时前抓的还是三天前的。
	Today bool `json:"today"`

	// DayLabel 是「昨天」「前天」「11月15日」，今天的条目为空。
	// 和 AgeLabel 不是一回事：那个说「这条内容发出来多久了」，
	// 这个说「这条是什么时候被我抓到的」。
	DayLabel string `json:"dayLabel"`
}

// Dim 是一个评分维度的展示形态。
//
// 单独定义而不直接返回 score.Factor：Factor 带 Max（满分 40/30 这种），
// 前端要的是 0-100 的百分比，让它自己除一次不如这里算好。
// Name 用界面上的叫法（「赛道相关」→「匹配」），说明文字跟着一起回。
type Dim struct {
	Name  string `json:"name"`
	Value int    `json:"value"` // 0-100
	Note  string `json:"note"`
}

// RunSummary 是一次管道执行的汇总。
type RunSummary struct {
	Fetched    int         `json:"fetched"`
	NewHotspot int         `json:"newHotspot"`
	Saved      int         `json:"saved"`   // 本次真正新入池的条数
	Updated    int         `json:"updated"` // 之前抓过、这次只刷新了分数
	Skipped    int         `json:"skipped"`
	Stale      bool        `json:"stale"`
	Source     string      `json:"source"`
	AIScored   int         `json:"aiScored"`
	AINote     string      `json:"aiNote"`
	Topics     []TopicView `json:"topics"`
}

// StatsView 是顶部与底部的计数。
type StatsView struct {
	Draft     int `json:"draft"`
	Picked    int `json:"picked"`
	Writing   int `json:"writing"`
	Scheduled int `json:"scheduled"`
	Published int `json:"published"`
	Dropped   int `json:"dropped"`

	// Today 是本次刷新新增的待定条数。首页那句「今天为你找到了 N 条」
	// 必须用它：拿 Draft（全部历史待定）去说「今天新增」，
	// 数字会一天比一天大，而列表并没有变长。
	Today int `json:"today"`
}

// List 按状态取选题。status 传空表示待定。
//
// 待定列表不是全局按分数排，而是「今天这批优先，再按分数」：
// 库里攒着历史所有没处理的选题，全局按分数排的话，昨天那条 80 分
// 会永远压在今天新抓的 75 分上面——用户点完刷新，看到的还是同一屏，
// 很自然会认定「数据没更新」。今天这批必须立刻能被看见，
// 之前没处理的留在下面，并标明是哪天抓的。
func (s *TopicService) List(status string, limit int) ([]TopicView, error) {
	if limit <= 0 {
		limit = 50
	}
	st := topic.Status(status)
	if st == "" {
		st = topic.Draft
	}
	batch := s.batchStart()

	// 多取几倍：同一条内容两个平台各发一次就是两行，去重后条数会掉，
	// 只取 limit 条的话，去掉重复列表就短了一截。
	list, err := s.st.ListByStatus(st, limit*3)
	if err != nil {
		return nil, err
	}
	sortByBatchScore(list, batch)
	list = dedupeTitles(list)
	if len(list) > limit {
		list = list[:limit]
	}

	out := make([]TopicView, 0, len(list))
	for _, t := range list {
		out = append(out, toViewAt(t, batch))
	}
	return out, nil
}

// batchStart 是「今天这批」的起点：今天 0 点。
//
// 用自然日而不是「上一次点击刷新的时刻」：用户一天里会刷好几次，
// 按点击时刻算的话，第二次刷新会把十分钟前刚刷出来的条目挤到「更早」里去，
// 而它们明明是今天新抓的。界面上的字是「今日新增」，口径就得按日算。
//
// 今天确实没有新东西时，列表里全是「更早」的条目，首页也会说「今天还没有新的」——
// 这比拿昨天的数据冒充今天要诚实，也才修得掉「刷新没反应」这个观感。
func (s *TopicService) batchStart() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// sortByBatchScore 把本批次的排前面，同一批内按分数降序。
func sortByBatchScore(list []store.Topic, batch time.Time) {
	sort.SliceStable(list, func(i, j int) bool {
		bi := !list[i].CreatedAt.Before(batch)
		bj := !list[j].CreatedAt.Before(batch)
		if bi != bj {
			return bi
		}
		return list[i].Score > list[j].Score
	})
}

// dedupeTitles 去掉同标题的重复卡片（同一内容被两个平台各发一次）。
//
// 必须在排序之后做：留下来的那条就是用户第一眼看到的那条，
// 「今天 + 高分」的优先被保留，翻页翻到一半又冒出一条一模一样的概率就没了。
func dedupeTitles(list []store.Topic) []store.Topic {
	seen := make(map[string]bool, len(list))
	out := list[:0]
	for _, t := range list {
		key := strings.TrimSpace(t.Title)
		if key == "" {
			key = t.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

// Run 执行一次取数打分。运行中重复调用会直接返回，避免用户连点把库写乱。
func (s *TopicService) Run(niche, keyword string) (RunSummary, error) {
	if !s.begin() {
		return RunSummary{}, fmt.Errorf("正在生成中，请稍候")
	}
	defer s.end()
	return s.runLocked(niche, keyword)
}

// runLocked 假定调用方已持有运行锁。定时器和手动刷新共用它，
// 保证「跑完记下今天」这件事只有一处实现——两处各写一遍迟早会漏一处。
func (s *TopicService) runLocked(niche, keyword string) (RunSummary, error) {
	res, err := pipeline.Run(context.Background(), s.st, s.src, s.client(), pipeline.Input{
		Niche: niche, Keyword: keyword, Limit: 50, MinScore: 50,
	}, func(pct int, msg string) {
		if s.app != nil {
			s.app.Event.Emit("pipeline:progress", map[string]any{"pct": pct, "msg": msg})
		}
	})
	if err != nil {
		return RunSummary{}, err
	}
	// 手动跑也算「今天有过新数据」。否则用户早上自己点了一次刷新，
	// 到点定时器还会再抓一次，白花一次配额，列表还会整个洗一遍。
	//
	// 但关键词挖掘不算：那是「我临时想找点素材」，不是「今天已经刷过了」。
	// 混在一起的话，用户早上搜个词，当天的定时刷新就再也不会跑。
	if keyword == "" {
		_ = s.st.SetSetting(store.SettingScheduleLastRun, time.Now().Format(store.DateLayout))
	}

	return RunSummary{
		Fetched:    res.Fetched,
		NewHotspot: res.NewHotspot,
		Saved:      res.Saved,
		Updated:    res.Updated,
		Skipped:    res.Skipped,
		Stale:      res.Stale,
		Source:     res.Source,
		AIScored:   res.AIScored,
		AINote:     res.AINote,
		Topics:     toViews(res.Topics),
	}, nil
}

// Decide 是「收下 / 丢掉 / 标记已发 / 收回」唯一入口，状态机在 store 层校验。
func (s *TopicService) Decide(id string, action string) error {
	var to topic.Status
	switch action {
	case "pick":
		to = topic.Picked
	case "drop":
		to = topic.Dropped
	case "restore":
		to = topic.Draft
	case "publish":
		// 「标记已发」是待办里最常点的一下：用户真正发完内容回来勾掉。
		// 它只能从「已排期」过来，状态机保证了这一点。
		to = topic.Published
	case "unschedule":
		// 撤回排期，日期由 store.Schedule 一并清掉。
		return s.st.Schedule(id, "")
	default:
		return fmt.Errorf("未知操作: %s", action)
	}
	if err := s.st.SetStatus(id, to); err != nil {
		return err
	}

	// 收下/丢掉时，把同标题的其他待定条目一并处理掉。
	//
	// 同一条内容常被两个平台各发一次，库里是两行，列表上却是同一张卡
	// （List 去重了）。只处理用户点中的那一行，另一行会在下次刷新时
	// 原样冒出来——用户会觉得「我明明丢掉了，它怎么又来了」。
	//
	// 只在待定之间传染：已经排期或已发的兄弟不能被动，那是另一件事了。
	if action == "pick" || action == "drop" {
		if t, err := s.st.TopicByID(id); err == nil {
			siblings, _ := s.st.DraftSiblingIDs(t.Title, id)
			for _, sid := range siblings {
				_ = s.st.SetStatus(sid, to)
			}
		}
	}
	return nil
}

// Stats 返回各状态计数。
func (s *TopicService) Stats() (StatsView, error) {
	m, err := s.st.Stats()
	if err != nil {
		return StatsView{}, err
	}
	// 今日新增读不到就当 0：这是首页的一句文案，不该让整页统计跟着失败。
	today, _ := s.st.CountTitlesSince(topic.Draft, s.batchStart())
	return StatsView{
		Draft:     m[string(topic.Draft)],
		Picked:    m[string(topic.Picked)],
		Writing:   m[string(topic.Writing)],
		Scheduled: m[string(topic.Scheduled)],
		Published: m[string(topic.Published)],
		Dropped:   m[string(topic.Dropped)],
		Today:     today,
	}, nil
}

// Niches 是首启动时给用户选的赛道。
func (s *TopicService) Niches() []string {
	return []string{"职场", "副业", "理财", "育儿", "健身", "美食", "穿搭", "AI工具"}
}

// ProfileView 是界面上「我是谁」那张表。
// 字段刻意只有五项：多一项，用户就少填一项。
type ProfileView struct {
	Niche       string `json:"niche"`
	Positioning string `json:"positioning"`
	Audience    string `json:"audience"`
	Tone        string `json:"tone"`
	Avoid       string `json:"avoid"`
}

// MemoryView 回答用户一个必须能看见的问题：这东西到底记住了我什么。
//
// 「长期记忆」如果不可见，用户不会信它，也不会去纠正它。所以这三个数
// 要摆在界面上，而不是藏在日志里。
type MemoryView struct {
	Profile ProfileView     `json:"profile"`
	Picked  int             `json:"picked"`  // 记住的「喜欢」
	Dropped int             `json:"dropped"` // 记住的「不喜欢」
	Filled  bool            `json:"filled"`  // 人设填了没有
	AIName  string          `json:"aiName"`  // 关闭 / relay / byok
	Learnt  memory.Learning `json:"learnt"`  // 第三层：从选择里学到的
	Blocked []string        `json:"blocked"` // 用户手动屏蔽掉的词
}

// Memory 返回当前记忆状态。
func (s *TopicService) Memory() (MemoryView, error) {
	p, err := s.st.Profile()
	if err != nil {
		return MemoryView{}, err
	}
	d, err := s.st.RecentDecisions(300)
	if err != nil {
		return MemoryView{}, err
	}
	name := "关闭（规则打分）"
	if c := s.client(); c != nil {
		// 角标要显示具体模型（deepseek-chat），不是供应商代号（byok）：
		// 用户想知道「现在是不是在用模型、用的哪个」，看到 byok 是答不上来的。
		name = ai.ModelNameOf(c)
	}

	// 学到的偏好要和「当前正在生效的口径」完全一致：
	// 排除赛道词（已在规则分里算过）和用户屏蔽词（他删掉的不许再学回来）。
	// 否则界面上列出的词和实际参与打分的词会对不上，用户看到的是一份假账。
	//
	// 赛道优先取人设里的；人设没填时（用户跳过引导很常见）退到库里最近的赛道。
	// 不这么做，跳过引导的用户会在记忆页看到「汇报、领导」被当成他的偏好——
	// 而那些恰恰是打分时排除掉的赛道词。
	niche := p.Niche
	if niche == "" {
		niche = s.st.LastNiche()
	}

	// 拼新切片而不是 append 到 NicheWords 的返回值上：那个切片是词库 map
	// 里的原件，容量够时 append 会写穿原数组，污染全局赛道词表。
	blocked := s.st.BlockedWords()
	nicheWords := score.NicheWords(niche)
	exclude := make([]string, 0, len(nicheWords)+len(blocked))
	exclude = append(exclude, nicheWords...)
	exclude = append(exclude, blocked...)
	learnt := memory.Learn(d.Picked, d.Dropped, exclude)

	return MemoryView{
		Profile: ProfileView{
			Niche: p.Niche, Positioning: p.Positioning,
			Audience: p.Audience, Tone: p.Tone, Avoid: p.Avoid,
		},
		Picked: len(d.Picked), Dropped: len(d.Dropped),
		Filled: !p.Empty(), AIName: name,
		Learnt: learnt, Blocked: blocked,
	}, nil
}

// BlockLearntWord 删掉一条学到的偏好，以后不再按它加分。
//
// 这个动作必须有：偏好是机器从标题里猜的，会猜错。用户能删，
// 猜错才不致命；不能删，一次猜错就会一直错下去，而且他没法证明是你错。
func (s *TopicService) BlockLearntWord(text string) error {
	return s.st.BlockLearntWord(text)
}

// UnblockLearntWord 撤销屏蔽，让这个词重新参与学习。
func (s *TopicService) UnblockLearntWord(text string) error {
	return s.st.UnblockLearntWord(text)
}

// SaveProfile 保存人设。它下一轮抓取就会生效，不需要重启。
func (s *TopicService) SaveProfile(v ProfileView) error {
	return s.st.SaveProfile(store.Profile{
		Niche: v.Niche, Positioning: v.Positioning,
		Audience: v.Audience, Tone: v.Tone, Avoid: v.Avoid,
	})
}

// SourceName 是界面角标上的那句话。它必须让用户一眼看出自己在看什么数据：
// 「线上数据」和「示例数据」混为一谈，试用反馈就全废了。
func (s *TopicService) SourceName() string { return s.srcLabel }

// OnboardingView 决定首启动要不要问那三个问题。
//
// Need 只在「引导没走过」且「人设是空的」同时成立时为真。分开判断是必要的：
// 用户点了跳过（走了引导但没填）不该再弹，老用户升级上来（人设已有）也不该再弹。
type OnboardingView struct {
	Need  bool   `json:"need"`
	Niche string `json:"niche"`
}

// Onboarding 返回首启动状态。
func (s *TopicService) Onboarding() (OnboardingView, error) {
	p, err := s.st.Profile()
	if err != nil {
		return OnboardingView{}, err
	}
	done := s.st.GetSetting(store.SettingOnboarding) == "1"
	return OnboardingView{Need: !done && p.Empty(), Niche: p.Niche}, nil
}

// FinishOnboarding 收尾。无论填没填都记一笔，否则跳过的人下次启动还会被拦一次。
func (s *TopicService) FinishOnboarding() error {
	return s.st.SetSetting(store.SettingOnboarding, "1")
}

// Rescore 用当前人设把已有选题重判一遍，不重新取数。
// 引导填完立刻调用它——让用户马上看见「它因为你这几句话改了判断」，
// 这比任何文案都能说明记忆是有效的。
func (s *TopicService) Rescore(niche string) (RunSummary, error) {
	if !s.begin() {
		return RunSummary{}, fmt.Errorf("正在生成中，请稍候")
	}
	defer s.end()

	res, err := pipeline.Rescore(context.Background(), s.st, s.client(), niche, 50,
		func(pct int, msg string) {
			if s.app != nil {
				s.app.Event.Emit("pipeline:progress", map[string]any{"pct": pct, "msg": msg})
			}
		})
	if err != nil {
		return RunSummary{}, err
	}
	return RunSummary{
		Source:   res.Source,
		AIScored: res.AIScored,
		AINote:   res.AINote,
		Topics:   toViews(res.Topics),
	}, nil
}

// begin/end 是运行互斥。Run 与 Rescore 不能同时跑：两者都会写 topic 分数，
// 并发时会互相覆盖，而用户看到的是分数莫名其妙跳变。
func (s *TopicService) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *TopicService) end() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func toViews(list []store.Topic) []TopicView {
	out := make([]TopicView, 0, len(list))
	for _, t := range list {
		out = append(out, toView(t))
	}
	return out
}

// toViewAt 在 toView 基础上标出「是不是今天这批」「哪天抓的」。
func toViewAt(t store.Topic, batch time.Time) TopicView {
	v := toView(t)
	v.Today = !t.CreatedAt.Before(batch)
	v.DayLabel = dayLabel(t.CreatedAt, batch)
	return v
}

// dayLabel 把抓取时间翻成「昨天 / 前天 / 11月15日」。
//
// 按日历日算而不是按 24 小时：用户说的「昨天」就是昨天那个日期，
// 不是「24 到 48 小时之前」。跨天计算用日历日才能和界面上的日期对得上。
func dayLabel(at, batch time.Time) string {
	if at.IsZero() || !at.Before(batch) {
		return ""
	}
	loc := at.Location()
	bd := time.Date(batch.Year(), batch.Month(), batch.Day(), 0, 0, 0, 0, loc)
	ad := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, loc)
	// +12h 再除：夏令时切换那天只有 23 小时，直接除会差一天（国内没有，
	// 但这段逻辑以后可能被搬到别的时区去）。
	days := int((bd.Sub(ad) + 12*time.Hour).Hours() / 24)
	switch {
	case days <= 0:
		return ""
	case days == 1:
		return "昨天"
	case days == 2:
		return "前天"
	default:
		return at.Format("1月2日")
	}
}

func toView(t store.Topic) TopicView {
	v := TopicView{
		ID:           t.ID,
		Title:        t.Title,
		Platform:     t.Platform,
		PlatformName: format.Platform(t.Platform),
		Niche:        t.Niche,
		Score:        t.Score,
		AIScore:      t.AIScore,
		Reason:       t.ScoreReason,
		HeatLabel:    format.Heat(t.Heat),
		AgeLabel:     format.Age(t.PublishedAt),
		Status:       string(t.Status),
		StatusLabel:  t.Status.Label(),
		PlanDate:     t.PlanDate,
		PlanLabel:    format.PlanDate(t.PlanDate),
	}
	v.Dims, v.Reasons = parseFactors(t.Factors)
	v.Steps, v.Done = stageOf(t.Status)
	return v
}

// dimNames 把打分维度的名字换成界面上更短的说法。
//
// 「赛道相关」是算分时的叫法，用户看卡片时想的是「这条跟我的方向合不合」，
// 所以叫「匹配」；「时效」他未必知道指发布时间，所以叫「新颖」。
// 没列进来的维度用原名——是给「以后加了新维度但忘了改这里」留的兜底，
// 显示成原名总比显示成空白强。
var dimNames = map[string]string{
	"赛道相关": "匹配",
	"热度":   "热度",
	"时效":   "新颖",
	"标题结构": "可写",
}

// dimOrder 固定四条进度条的显示顺序。
//
// 不按 JSON 里的顺序：那是打分代码的书写顺序，改动一次界面顺序就跟着变。
// 用户扫卡片的路径是「合不合我 → 火不火 → 新不新 → 我好不好写」，按这个排。
var dimOrder = []string{"赛道相关", "热度", "时效", "标题结构"}

// parseFactors 从库里那条 JSON 解出四条进度条和逐条推荐理由。
//
// 解析失败一律当「没有维度信息」处理，绝不返回错误：这是展示用的附加信息，
// 因为一条老数据格式不认就让整个列表打不开，是本末倒置。
func parseFactors(raw string) ([]Dim, []string) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var fs []score.Factor
	if err := json.Unmarshal([]byte(raw), &fs); err != nil {
		return nil, nil
	}

	byName := make(map[string]score.Factor, len(fs))
	for _, f := range fs {
		byName[f.Name] = f
	}

	dims := make([]Dim, 0, len(fs))
	// 理由带分值收集，最后按贡献排序截断——「为什么推荐你写」按分值从高到低，
	// 用户先看到的是最硬的那条理由。
	type scored struct {
		pts  float64
		text string
	}
	var picked []scored
	for _, name := range dimOrder {
		f, ok := byName[name]
		if !ok {
			continue
		}
		pct := 0
		if f.Max > 0 {
			pct = int(math.Round(f.Points / f.Max * 100))
		}
		dims = append(dims, Dim{
			Name:  dimNames[name],
			Value: pct,
			Note:  f.Detail,
		})
		// 理由只收「有实质内容」的：满分维度说「命中三个赛道词」有用，
		// 零分维度说「未命中赛道词」是废话，卡片上不该出现。
		if f.Points > 0 && strings.TrimSpace(f.Detail) != "" {
			picked = append(picked, scored{f.Points, reasonText(name, f.Detail)})
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].pts > picked[j].pts })
	const maxReasons = 4
	reasons := make([]string, 0, maxReasons)
	for _, p := range picked {
		if len(reasons) == maxReasons {
			break
		}
		reasons = append(reasons, p.text)
	}
	return dims, reasons
}

// stageSteps 是两条创作路线的阶段名。
//
// 为什么按形态分两套而不是一套通用阶段：图文的「定稿」之后就是排版成图文卡片，
// 视频的「定稿」之后还有拍摄和剪辑两件完全不同的事。用一套阶段名必然有一边
// 在说谎，用户按着它做会漏步骤。
var stageSteps = map[string][]string{
	"article": {"灵感", "初稿", "定稿", "发布"},
	"video":   {"灵感", "大纲", "脚本", "拍摄剪辑", "发布"},
}

// stageOf 把状态机的位置翻译成「第几步 / 共几步」。
//
// 不新增 stage 字段：状态机已经在跑，而且每次转移都写 topic_event，
// 再加一套并行的阶段状态，两套迟早对不上——那种不一致不会报错，
// 只会让进度轴显示的和实际做的不一样。
func stageOf(st topic.Status) ([]string, int) {
	steps := stageSteps["article"]
	var idx int
	switch st {
	case topic.Draft, topic.Picked:
		idx = 1
	case topic.Writing:
		idx = 2
	case topic.Scheduled:
		idx = 3
	case topic.Published:
		idx = len(steps)
	default:
		return nil, 0 // 丢掉的不显示进度轴
	}
	return steps, idx
}

// reasonText 把打分维度的一句「事实」写成一句「理由」。
//
// 差别在哪：Detail 里的「互动 12.3万」是数据，「3小时前」是时间，
// 单独摆出来用户要自己换算成判断。理由得把判断说出来——
// 「讨论度够」「还在窗口期」才是他决定要不要写的依据。
//
// 不做成模板表：每种维度的说法就一句，写成 map 反而要在两个地方对齐字段名。
func reasonText(dim, detail string) string {
	switch dim {
	case "赛道相关":
		return detail + "，跟你的方向对得上"
	case "热度":
		return detail + "，讨论度够"
	case "时效":
		// Detail 里可能已经是「3小时前」或「2天前」，接「发布」都通顺。
		if strings.HasSuffix(detail, "前") {
			return detail + "发布，还在窗口期"
		}
		return detail + "，还在窗口期"
	case "标题结构":
		return "标题" + detail
	}
	return detail
}
