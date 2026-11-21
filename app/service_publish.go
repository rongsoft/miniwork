package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/draft"
	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 发布台是「写完了之后」那一段。
//
// 它替代的是原来那一下「标记已发」：以前点一下就没了，什么也没留下。
// 现在每发一次落一条账——哪个平台、哪个账号、哪天、链接、后来的数据。
// 这些数字是复盘唯一的输入，缺了它们，数据分析只能数选题数。

// QueueItem 是待发队列里的一条。
type QueueItem struct {
	TopicID      string `json:"topicId"`
	DraftID      string `json:"draftId"` // 最新一稿，空表示还没写
	DraftCount   int    `json:"draftCount"`
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	PlanDate     string `json:"planDate"`
	PlanLabel    string `json:"planLabel"`
	Due          string `json:"due"`      // overdue / today / soon / later
	DueLabel     string `json:"dueLabel"` // 「逾期 2 天」这种，给人看的
}

// PublishView 是台账里的一条。
type PublishView struct {
	ID           string `json:"id"`
	TopicID      string `json:"topicId"`
	DraftID      string `json:"draftId"`
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	Account      string `json:"account"`
	URL          string `json:"url"`
	Date         string `json:"date"`
	Note         string `json:"note"`
	// -1 表示还没回填。0 是真实数据，两者必须分开。
	Views    int  `json:"views"`
	Likes    int  `json:"likes"`
	Collects int  `json:"collects"`
	Comments int  `json:"comments"`
	Filled   bool `json:"filled"` // 有没有填过任一指标
}

// PublishBoardView 是发布台一屏要的全部数据。
//
// 一次返回而不是分四次调：这四块内容必须来自同一时刻的快照。
// 分开取的话，用户点完「标记已发」，队列少了但台账没加，
// 看上去像丢了一条。
type PublishBoardView struct {
	Queue []QueueItem `json:"queue"`
	// Tasks 是三段式发布生成的任务清单，和 Queue 是两回事：
	// Queue 来自「排了日期的选题」，Tasks 来自「明确决定要发的稿子」。
	// 前者是意向，后者是清单，界面上分两块显示，不合并。
	Tasks    []TaskView        `json:"tasks"`
	Records  []PublishView     `json:"records"`
	Accounts map[string]string `json:"accounts"`
	Today    string            `json:"today"`
	Stats    PublishStats      `json:"stats"`
}

// PublishStats 是台账顶部那几个数。
type PublishStats struct {
	Total    int `json:"total"`
	Views    int `json:"views"`
	Likes    int `json:"likes"`
	Collects int `json:"collects"`
	// Filled 是填了数据的有几条，用来在界面上说清「这几个数覆盖了几条」。
	// 只有 3 条填了数据就吹「总播放 12 万」，是拿部分当全部。
	Filled int `json:"filled"`
}

// PublishBoard 组装发布台。
func (s *TopicService) PublishBoard() (PublishBoardView, error) {
	today := time.Now().Format(store.DateLayout)
	queue, err := s.queue(today)
	if err != nil {
		return PublishBoardView{}, err
	}
	records, err := s.records(60)
	if err != nil {
		return PublishBoardView{}, err
	}
	totals, err := s.st.PublishStats()
	if err != nil {
		return PublishBoardView{}, err
	}
	tasks, err := s.PublishTasks()
	if err != nil {
		return PublishBoardView{}, err
	}
	return PublishBoardView{
		Queue:    queue,
		Tasks:    tasks,
		Records:  records,
		Accounts: s.Accounts(),
		Today:    today,
		Stats: PublishStats{
			Total: totals.Total, Views: totals.Views, Likes: totals.Likes,
			Collects: totals.Collects, Filled: totals.Filled,
		},
	}, nil
}

// queue 取已排期但还没发的选题，按「逾期 → 今天 → 未来」排。
//
// 只放 scheduled：待定的、收下没排期的都还不到「该发」这一步，
// 混进来会让这页变成第二个选题库，而它存在的意义是「今天必须动手的有哪些」。
func (s *TopicService) queue(today string) ([]QueueItem, error) {
	list, err := s.st.ListByStatus(topic.Scheduled, 200)
	if err != nil {
		return nil, err
	}
	drafted, err := s.st.DraftedTopicIDs()
	if err != nil {
		return nil, err
	}

	items := make([]QueueItem, 0, len(list))
	for _, t := range list {
		it := QueueItem{
			TopicID:      t.ID,
			Title:        t.Title,
			Platform:     t.Platform,
			PlatformName: format.Platform(t.Platform),
			PlanDate:     t.PlanDate,
			PlanLabel:    format.PlanDate(t.PlanDate),
			Due:          "later",
			DueLabel:     "",
		}
		it.Due, it.DueLabel = dueLabel(t.PlanDate, today)
		if drafted[t.ID] {
			// 队列里只关心「有没有稿」，具体哪一稿点进去再取：
			// 这里查全部稿件是几十次往返，而用户点开一条才有意义。
			it.DraftCount = 1
			if ds, err := s.st.DraftsByTopic(t.ID); err == nil && len(ds) > 0 {
				it.DraftCount = len(ds)
				it.DraftID = ds[0].ID
			}
		}
		items = append(items, it)
	}

	// 逾期在前：已经承诺过日子的没交付，比还没到期的重要。
	order := map[string]int{"overdue": 0, "today": 1, "soon": 2, "later": 3}
	sort.SliceStable(items, func(i, j int) bool {
		if order[items[i].Due] != order[items[j].Due] {
			return order[items[i].Due] < order[items[j].Due]
		}
		return items[i].PlanDate < items[j].PlanDate
	})
	return items, nil
}

// dueLabel 把日期变成一句带紧迫感的话。
//
// 不用「过期」「超时」这类词：那是系统在指责人。创作者漏发一天很正常，
// 说「逾期 2 天」是陈述，说「已超时」是审判，而他要的是「哦，补上」。
func dueLabel(planDate, today string) (string, string) {
	if planDate == "" {
		return "later", "没排期"
	}
	day, err := time.Parse(store.DateLayout, planDate)
	if err != nil {
		return "later", ""
	}
	now, err := time.Parse(store.DateLayout, today)
	if err != nil {
		return "later", ""
	}
	// 正数＝还有几天，负数＝已经过了几天。
	// 方向反了会得到「-1 天后」这种只有程序员看得懂的文案。
	diff := int(day.Sub(now).Hours() / 24)
	switch {
	case diff < 0:
		return "overdue", fmt.Sprintf("逾期 %d 天", -diff)
	case diff == 0:
		return "today", "今天发"
	case diff == 1:
		return "soon", "明天"
	default:
		return "soon", fmt.Sprintf("%d 天后", diff)
	}
}

// records 取台账并补上选题标题。
func (s *TopicService) records(limit int) ([]PublishView, error) {
	list, err := s.st.ListPublish(limit)
	if err != nil {
		return nil, err
	}
	cache := map[string]string{}
	out := make([]PublishView, 0, len(list))
	for _, p := range list {
		title, ok := cache[p.TopicID]
		if !ok {
			if t, err := s.st.TopicByID(p.TopicID); err == nil {
				title = t.Title
			}
			cache[p.TopicID] = title
		}
		out = append(out, PublishView{
			ID: p.ID, TopicID: p.TopicID, DraftID: p.DraftID, Title: title,
			Platform: p.Platform, PlatformName: format.Platform(p.Platform),
			Account: p.Account, URL: p.URL, Date: p.PublishedAt, Note: p.Note,
			Views: p.Views, Likes: p.Likes, Collects: p.Collects, Comments: p.Comments,
			Filled: p.Views >= 0 || p.Likes >= 0 || p.Collects >= 0 || p.Comments >= 0,
		})
	}
	return out, nil
}

// PublishInput 是「我发了」这一次的输入。
type PublishInput struct {
	TopicID  string `json:"topicId"`
	DraftID  string `json:"draftId"`
	Platform string `json:"platform"`
	Account  string `json:"account"`
	URL      string `json:"url"`
	Date     string `json:"date"` // 空则今天
	Note     string `json:"note"`
}

// MarkPublished 记一次发布，并把选题推进到「已发布」。
//
// 顺序是先落账、再改状态：台账是事实，状态是标签。
// 反过来做的话，改完状态若写台账失败，就会出现「选题显示已发、
// 台账里查不到」的账实不符，而复盘全靠台账。
func (s *TopicService) MarkPublished(in PublishInput) (string, error) {
	t, err := s.st.TopicByID(strings.TrimSpace(in.TopicID))
	if err != nil {
		return "", err
	}
	if t.Status == topic.Dropped {
		return "", fmt.Errorf("这条已经丢掉了，先捡回来再记发布")
	}

	platform := strings.TrimSpace(in.Platform)
	if platform == "" && in.DraftID != "" {
		if d, err := s.st.DraftByID(in.DraftID); err == nil {
			platform = d.Platform
		}
	}
	if platform == "" {
		platform = t.Platform
	}
	date, err := store.PlanDate(strings.TrimSpace(in.Date))
	if err != nil {
		return "", err
	}
	if date == "" {
		date = time.Now().Format(store.DateLayout)
	}

	id, err := s.st.AddPublish(store.PublishRecord{
		TopicID: t.ID, DraftID: in.DraftID, Platform: platform,
		Account: strings.TrimSpace(in.Account), URL: strings.TrimSpace(in.URL),
		PublishedAt: date, Note: strings.TrimSpace(in.Note),
		Views: -1, Likes: -1, Collects: -1, Comments: -1,
	})
	if err != nil {
		return "", err
	}

	// 已经发过的选题再发别的平台，只加台账不动状态：
	// SetStatus 对「已发布 -> 已发布」会报状态未变化，那不是错误。
	if t.Status != topic.Published {
		if err := s.st.SetStatus(t.ID, topic.Published); err != nil {
			// 台账已经写进去了，这里不能回滚掉——记录真发过比状态好看重要。
			return id, nil
		}
	}
	return id, nil
}

// UpdateMetrics 回填一条发布记录的数据。
// 四个参数传 -1 表示「这一项不改」，传 0 表示「真的是 0」。
func (s *TopicService) UpdateMetrics(id string, views, likes, collects, comments int) error {
	return s.st.UpdateMetrics(id, views, likes, collects, comments)
}

// Accounts 返回各平台记下的账号名。
func (s *TopicService) Accounts() map[string]string {
	out := map[string]string{}
	for _, sp := range draft.All() {
		if v := s.st.GetSetting(accountKey(sp.Key)); v != "" {
			out[sp.Key] = v
		}
	}
	return out
}

// SaveAccount 记住某个平台的账号名。
//
// 存账号名不存密码：这个程序不登录任何平台，账号名只是为了让台账
// 上写明「发在哪个号」。存密码会立刻把一个小工具的信任成本拉到最高。
func (s *TopicService) SaveAccount(platform, account string) error {
	if _, ok := draft.Get(platform); !ok {
		return fmt.Errorf("未知平台: %q", platform)
	}
	return s.st.SetSetting(accountKey(platform), strings.TrimSpace(account))
}

func accountKey(platform string) string { return "publish_account_" + platform }
