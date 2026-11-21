// Package topic 定义选题的主线状态机。
//
// 设计约束：status 只表达「人做过的决定」，不表达「机器在干什么」。
// 生成中、失败、重试属于 job，不属于 topic。
package topic

import "fmt"

// Status 是选题在流水线上的位置。
type Status string

const (
	Draft     Status = "draft"     // 系统产出，等人决定
	Picked    Status = "picked"    // 人决定要写
	Writing   Status = "writing"   // 人在写
	Scheduled Status = "scheduled" // 已排期
	Published Status = "published" // 已发布
	Dropped   Status = "dropped"   // 人决定不要
)

// transitions 是唯一的状态转移白名单，UI 与 job 都必须走这里。
//
// Picked 可以直达 Scheduled：把选题排到日历上，本身就是「这条我要写、
// 哪天写」的承诺，不强迫他先点一遍「写作中」——MVP 没有编辑器，
// 那个状态目前没有别的入口，逼出来的点击只是假数据。
//
// Picked / Writing 都可以直达 Published：稿子写好了当天就发，是创作者
// 最常走的路，根本没经过日历。以前这条边不存在，用户发完回来标记时
// 会被要求「先排个期」，而那个期只是为了让状态机开心——那是形式主义。
// 排期是计划，发布是事实；计划不该拦住已经发生的事实。
//
// Draft 仍然到不了 Published：连「这条我要写」都没决定就发出去，
// 不可能是真的，多半是点错了。
var transitions = map[Status][]Status{
	Draft:     {Picked, Dropped},
	Picked:    {Writing, Scheduled, Published, Dropped, Draft}, // 退回待定＝撤销「收下」这个决定
	Writing:   {Scheduled, Published, Dropped, Draft},
	Scheduled: {Published, Writing, Picked}, // 退回改稿，或撤回排期
	Published: {},
	Dropped:   {Draft}, // 允许捡回来
}

// Can 判断转移是否合法。
func Can(from, to Status) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Check 在非法转移时返回带上下文的错误。
func Check(from, to Status) error {
	if from == to {
		return fmt.Errorf("状态未变化: %s", from)
	}
	if !Can(from, to) {
		return fmt.Errorf("非法状态转移: %s -> %s", from, to)
	}
	return nil
}

// Label 是给人看的中文名，UI 只认这个，不再自己拼。
func (s Status) Label() string {
	switch s {
	case Draft:
		return "待定"
	case Picked:
		return "已收下"
	case Writing:
		return "写作中"
	case Scheduled:
		return "已排期"
	case Published:
		return "已发布"
	case Dropped:
		return "已丢弃"
	}
	return string(s)
}

// Terminal 标记不会再有后续动作的状态，用于统计。
func (s Status) Terminal() bool {
	return s == Published || s == Dropped
}
