package main

import (
	"errors"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/draft"
	"rongsoft/miniwork/app/internal/store"
)

// 发布任务是三段式发布（选类型 → 选素材和账号 → 生成任务）的产物。
//
// 为什么要有这一层，而不是直接从草稿点「去发」：
// 一次发三五篇、发到两三个平台时，真正难的不是某一次发布，而是
// 「哪篇发到哪、还有哪些没发」。这层把决定先落成一张清单，
// 之后每一次发布都只是把清单上的一条划掉——断了也能接着来。

// TaskView 是界面上的一行。
type TaskView struct {
	ID           string `json:"id"`
	DraftID      string `json:"draftId"`
	TopicID      string `json:"topicId"`
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	Format       string `json:"format"`
	FormatName   string `json:"formatName"`
	Account      string `json:"account"`
	Words        int    `json:"words"`
	// DraftGone 表示稿子已经不在了（改平台后删了、或清理过）。
	// 这一条任务还是要显示出来，否则用户会以为「我明明生成过」是自己记错了。
	DraftGone bool `json:"draftGone"`
}

// TaskInput 是一次生成任务的输入。
//
// Drafts × Platforms 是笛卡尔积：选中三篇稿、勾两个平台，就是六条任务。
// 这正是「一篇稿发多个号」的表达方式，不用再单独做一个分配模式开关。
// Platforms 为空时按每篇稿自己的平台走——大多数时候人就该这么发。
type TaskInput struct {
	DraftIDs  []string          `json:"draftIds"`
	Platforms []string          `json:"platforms"`
	Accounts  map[string]string `json:"accounts"`
}

// taskLimit 是一次生成的条数上限。
//
// 不是为了限制用户，是为了让「一次点下去生成 60 条」这种手滑有救：
// 真的批量发那么多时，人也只该一条条确认，不该由界面替他打包。
const taskLimit = 20

func (s *TopicService) PublishTasks() ([]TaskView, error) {
	list, err := s.st.Tasks("todo")
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(list))
	for _, t := range list {
		v := TaskView{
			ID: t.ID, DraftID: t.DraftID, TopicID: t.TopicID, Title: t.Title,
			Platform: t.Platform, PlatformName: platformName(t.Platform),
			Format: t.Format, FormatName: draft.FormatLabel(t.Format),
			Account: t.Account,
		}
		if d, err := s.st.DraftByID(t.DraftID); err == nil {
			v.Words = len([]rune(strings.TrimSpace(d.Body)))
			if v.Title == "" {
				v.Title = d.Title
			}
		} else {
			v.DraftGone = true
		}
		out = append(out, v)
	}
	return out, nil
}

// CreatePublishTasks 生成一批任务。
//
// 稿子不存在就跳过并在结果里说清楚跳过几条——直接报错会让用户
// 因为一篇已删的稿子而整批都发不出去。
func (s *TopicService) CreatePublishTasks(in TaskInput) (int, error) {
	ids := dedupeStrings(in.DraftIDs)
	if len(ids) == 0 {
		return 0, errors.New("先选要发的稿子")
	}
	plats := dedupeStrings(in.Platforms)
	for _, p := range plats {
		if _, ok := draft.Get(p); !ok {
			return 0, errors.New("不认识的平台：" + p)
		}
	}

	seq, err := s.st.NextSeq()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	var list []store.Task
	skipped := 0

	for _, id := range ids {
		d, err := s.st.DraftByID(id)
		if err != nil {
			skipped++
			continue
		}
		targets := plats
		if len(targets) == 0 {
			targets = []string{d.Platform}
		}
		for _, p := range targets {
			if len(list) >= taskLimit {
				break
			}
			list = append(list, store.Task{
				ID: store.TaskID(), DraftID: d.ID, TopicID: d.TopicID, Title: d.Title,
				Platform: p, Format: d.Format,
				Account: strings.TrimSpace(in.Accounts[p]),
				Status:  "todo", Seq: seq, CreatedAt: now,
			})
			seq++
		}
	}

	if len(list) == 0 {
		if skipped > 0 {
			return 0, errors.New("选中的稿子都已被删掉")
		}
		return 0, errors.New("没有生成任何任务")
	}
	if err := s.st.AddTasks(list); err != nil {
		return 0, err
	}
	return len(list), nil
}

func (s *TopicService) DropPublishTask(id string) error {
	return s.st.DeleteTask(id)
}

// CompletePublishTask 在记完账之后调用：任务划掉，不再出现在清单里。
//
// 顺序是「先记账、再划掉」。反过来的话，记账失败时任务已经消失，
// 用户既看不到这条待办、台账里也没有，等于凭空丢了一次发布。
func (s *TopicService) CompletePublishTask(id string) error {
	return s.st.DeleteTask(id)
}

func (s *TopicService) ClearPublishTasks() error {
	return s.st.ClearTasks("todo")
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// platformName 给平台 key 配中文名，认不出就原样返回：
// 显示成空串比显示一个不认识的 key 更难排查。
func platformName(key string) string {
	if sp, ok := draft.Get(key); ok {
		return sp.Name
	}
	return key
}
