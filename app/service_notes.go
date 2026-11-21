package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/store"
)

// 笔记复盘是「发出去之后」那一段。
//
// 它和「数据分析」上半部分的复盘分工明确，不是同一件事：
//
//	上半部分（review 包）复盘的是「判断」——收下、丢掉、发布全是本地点击，
//	一个数都不用填，所以它永远有数据、永远不需要维护。
//	这一块复盘的是「效果」——播放、点赞只有平台知道，这个程序拿不到，
//	必须用户自己回填，也必须由他自己打星、写结论。
//
// 分开不是为了代码整齐：两类数据一个免费、一个要花时间。
// 混成一个页面的后果很具体——需要填的那半永远空着，
// 然后把不用填的那半也一起拖成 0。

// NoteView 是一条待复盘 / 已复盘的笔记。
type NoteView struct {
	ID           string `json:"id"`
	TopicID      string `json:"topicId"`
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	URL          string `json:"url"`
	Date         string `json:"date"`
	AgeLabel     string `json:"ageLabel"` // 「发了 3 天」

	// 四个指标，-1 表示没回填。0 是真实数据，界面上必须能分开。
	Views    int  `json:"views"`
	Likes    int  `json:"likes"`
	Collects int  `json:"collects"`
	Comments int  `json:"comments"`
	Filled   bool `json:"filled"`

	// 这四个数是平台给的还是自己抄的。手抄的会抄错、会抄成前一天的数，
	// 界面上不标出来，一列数字看着一样，可信度差得远。
	MetricsSrc string `json:"metricsSrc"`
	MetricsAt  string `json:"metricsAt"`

	// Rate 是互动率百分比，-1 表示算不出来（没有播放数就没有分母）。
	Rate float64 `json:"rate"`

	Rating   int    `json:"rating"`
	Analysis string `json:"analysis"`
	Lesson   string `json:"lesson"`
	Reviewed bool   `json:"reviewed"`
}

// NoteStatView 是这一块顶上的四个数。
type NoteStatView struct {
	Total   int     `json:"total"`   // 台账一共记过几条
	Pending int     `json:"pending"` // 有数据但没打星
	Rated   int     `json:"rated"`
	AvgStar float64 `json:"avgStar"`
	Lessons int     `json:"lessons"`
	Unrated int     `json:"unrated"` // 连数据都还没填的（在发布台那边）
}

// NoteLesson 是一条沉淀下来的可复用经验。
type NoteLesson struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	PlatformName string `json:"platformName"`
	Date         string `json:"date"`
	Rating       int    `json:"rating"`
	Lesson       string `json:"lesson"`
}

// NoteInsight 是这块唯一的一句结论：你打的星和真实数据对不对得上。
//
// 这是整个程序里除了「模型准不准」之外，第二处能校准直觉的地方，
// 也是唯一一处拿真实结果校准的。所以宁可只给一句话，也不堆图表。
type NoteInsight struct {
	Available bool    `json:"available"`
	GoodN     int     `json:"goodN"` // 4–5 星
	BadN      int     `json:"badN"`  // 1–2 星
	GoodAvg   float64 `json:"goodAvg"`
	BadAvg    float64 `json:"badAvg"`
	Verdict   string  `json:"verdict"`
}

// NoteBoardView 是「笔记复盘」一屏要的全部数据。
type NoteBoardView struct {
	Items   []NoteView   `json:"items"`
	Stats   NoteStatView `json:"stats"`
	Lessons []NoteLesson `json:"lessons"`
	Insight NoteInsight  `json:"insight"`
}

// NoteBoard 组装笔记复盘。
func (s *TopicService) NoteBoard() (NoteBoardView, error) {
	totals, err := s.st.PublishStats()
	if err != nil {
		return NoteBoardView{}, err
	}
	recs, err := s.st.ListPublish(120)
	if err != nil {
		return NoteBoardView{}, err
	}

	now := time.Now()
	titles := map[string]string{}
	items := make([]NoteView, 0, len(recs))
	for _, p := range recs {
		filled := p.Views >= 0 || p.Likes >= 0 || p.Collects >= 0 || p.Comments >= 0
		// 只收「有数据的」和「已经打过星的」。两样都没有的属于发布台那边
		// 还没记完账的，摆到这里只会让这一页变成第二个台账。
		if !filled && p.Rating == 0 {
			continue
		}
		title, ok := titles[p.TopicID]
		if !ok {
			if t, err := s.st.TopicByID(p.TopicID); err == nil {
				title = t.Title
			}
			titles[p.TopicID] = title
		}
		items = append(items, NoteView{
			ID: p.ID, TopicID: p.TopicID, Title: title,
			Platform: p.Platform, PlatformName: format.Platform(p.Platform),
			URL: p.URL, Date: p.PublishedAt, AgeLabel: noteAge(p.PublishedAt, now),
			Views: p.Views, Likes: p.Likes, Collects: p.Collects, Comments: p.Comments,
			Filled: filled,
			Rate:   noteRate(p.Likes, p.Collects, p.Comments, p.Views),
			// 只透出时间，不透出完整时间戳：用户要判断的是
			// 「这个数新不新」，精确到秒没有意义还占位置。
			MetricsSrc: p.MetricsSrc,
			MetricsAt:  hhmm(p.MetricsAt),
			Rating: p.Rating, Analysis: p.Analysis, Lesson: p.Lesson,
			Reviewed: p.Rating > 0,
		})
	}

	// 排序：没复盘的排前面，且越早发的越靠前——数据已经稳定了，
	// 现在不写，过两周就想不起来当时为什么那么写。
	// 已复盘的按星级从高到低，让「值得回看的那几条」浮在上面。
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Reviewed != b.Reviewed {
			return !a.Reviewed
		}
		if a.Reviewed {
			if a.Rating != b.Rating {
				return a.Rating > b.Rating
			}
			return a.Date > b.Date
		}
		return a.Date < b.Date
	})

	lessons := make([]NoteLesson, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.Lesson) == "" {
			continue
		}
		lessons = append(lessons, NoteLesson{
			ID: it.ID, Title: it.Title, PlatformName: it.PlatformName,
			Date: it.Date, Rating: it.Rating, Lesson: it.Lesson,
		})
	}

	avg := 0.0
	if totals.Rated > 0 {
		avg = float64(totals.StarSum) / float64(totals.Rated)
	}

	out := NoteBoardView{
		Items:   items,
		Lessons: lessons,
		Stats: NoteStatView{
			Total:   totals.Total,
			Pending: totals.Pending,
			Rated:   totals.Rated,
			AvgStar: round1(avg),
			Lessons: totals.Lessons,
			Unrated: totals.Total - totals.Filled,
		},
		Insight: noteInsight(items),
	}
	return out, nil
}

// NoteReviewInput 是一次复盘提交。
type NoteReviewInput struct {
	ID       string `json:"id"`
	Rating   int    `json:"rating"`
	Analysis string `json:"analysis"`
	Lesson   string `json:"lesson"`
}

// 两个长度上限只是防手滑粘一整篇文章进来，不是内容规则。
// 上限按字数算，中文按字而不是按字节——按字节算会把 1000 字的分析砍掉三分之二。
const (
	noteAnalysisMax = 2000
	noteLessonMax   = 200
)

// SaveNoteReview 存一次复盘。
func (s *TopicService) SaveNoteReview(in NoteReviewInput) error {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return errors.New("缺发布记录 id")
	}
	if in.Rating < 0 || in.Rating > 5 {
		return errors.New("星级只能是 1 到 5，或者 0 表示撤销")
	}
	return s.st.SaveNoteReview(id, in.Rating,
		clipRunes(in.Analysis, noteAnalysisMax),
		clipRunes(in.Lesson, noteLessonMax))
}

// noteRate 算互动率百分比。
//
// 分母是播放/阅读数；没有播放数就返回 -1——用赞数除以 0 得到的不是「无限高」，
// 是「这条没有分母」。硬算成 100% 会让一条 3 赞的笔记排到 12 万播放的上面，
// 而后者才是真答案。
func noteRate(likes, collects, comments, views int) float64 {
	if views <= 0 {
		return -1
	}
	// 未回填的项按 0 算：一条只填了播放 1000、其他都没填的笔记，
	// 互动率显示 0% 是诚实的——用户自己能看出「另外三项还没填」。
	l, c, m := max0(likes), max0(collects), max0(comments)
	return round1(float64(l+c+m) * 100 / float64(views))
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// hhmm 从存库的完整时间戳里取出「时:分」。
// 解析不了就返回空串，界面上不显示这一段——好过显示一串乱码时间。
func hhmm(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return t.Format("01-02 15:04")
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

// noteAge 把发布日变成「发了几天」。
// 解析失败就返回空串，让界面不显示这一段——好过显示「0 天」这种假话。
func noteAge(date string, now time.Time) string {
	// 必须 ParseInLocation：time.Parse 把「2026-11-14」当成 UTC 零点，
	// 而 today 是当地零点。在东八区相差 8 小时，Sub 出来会是 16 小时，
	// 除以 24 取整就成了 0 天——每一篇隔夜发的笔记都会显示「今天发的」。
	d, err := time.ParseInLocation(store.DateLayout, date, time.Local)
	if err != nil {
		return ""
	}
	// 用当地零点对齐再算天数：直接相减的话，昨晚 23:59 发的
	// 到今天早上会被算成「0 天」，而用户心里那是「昨天发的」。
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	gap := int(today.Sub(d).Hours() / 24)
	switch {
	case gap <= 0:
		return "今天发的"
	case gap == 1:
		return "发了 1 天"
	case gap > 365:
		return ""
	default:
		return fmt.Sprintf("发了 %d 天", gap)
	}
}

// noteInsight 检查「你觉得好的」和「真的有人看的」是不是同一件事。
//
// 门槛写死在代码里而不是让用户调：一条笔记的星是 1–5，高星只能是 4 和 5，
// 这个划分没有第二种合理解释，做成设置项只会多一个没人改的开关。
func noteInsight(items []NoteView) NoteInsight {
	var good, bad []float64
	for _, it := range items {
		if !it.Reviewed || it.Rate < 0 {
			continue
		}
		switch {
		case it.Rating >= 4:
			good = append(good, it.Rate)
		case it.Rating <= 2:
			bad = append(bad, it.Rate)
		}
	}
	out := NoteInsight{GoodN: len(good), BadN: len(bad)}
	out.GoodAvg = round1(mean(good))
	out.BadAvg = round1(mean(bad))

	// 两边各要 2 条才比。1 条对 1 条的差额多半是运气，
	// 拿它下结论会让用户按噪音调整写法，比不给结论更糟。
	if len(good) < 2 || len(bad) < 2 {
		out.Verdict = "两边各攒到 2 条打过星的，就能看出你的眼光和数据对不对得上"
		return out
	}
	out.Available = true

	switch {
	case out.GoodAvg >= out.BadAvg*1.3:
		out.Verdict = fmt.Sprintf("你打高星的互动率 %.1f%%，低星的 %.1f%%——你的眼光是准的，按它挑就行",
			out.GoodAvg, out.BadAvg)
	case out.BadAvg >= out.GoodAvg*1.3:
		out.Verdict = fmt.Sprintf("你打高星的互动率反而更低（%.1f%% vs %.1f%%）——你喜欢和读者想看不是一回事，选题时多看看数据好的那几条有什么共同点",
			out.GoodAvg, out.BadAvg)
	default:
		out.Verdict = fmt.Sprintf("高星低星的数据差不多（%.1f%% vs %.1f%%）——决定这条数据的是封面、标题或发布时间，不是内容本身的好坏",
			out.GoodAvg, out.BadAvg)
	}
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// clipRunes 按字数截断，不按字节——按字节截会切出半个汉字。
func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
