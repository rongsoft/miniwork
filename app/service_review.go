package main

import (
	"time"

	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/review"
)

// Review 生成一次复盘。
//
// 全部数据来自本地库的两条时间线（产出时间、决定时间），不需要联网、
// 不需要用户填任何东西。这一点很重要：复盘一旦需要用户补数据，
// 它就会变成「又一个要维护的表格」，没人会用。
func (s *TopicService) Review(days int) (review.Review, error) {
	if days <= 0 {
		days = 7
	}
	now := time.Now()
	since := now.AddDate(0, 0, -days+1)

	in := review.Input{Now: now, Days: days}
	var err error
	if in.Counts, err = s.st.StatusCountsSince(since); err != nil {
		return review.Review{}, err
	}
	if in.Created, err = s.st.CountCreatedSince(since); err != nil {
		return review.Review{}, err
	}
	if in.NewByDay, err = s.st.NewByDay(since); err != nil {
		return review.Review{}, err
	}
	if in.AIPairs, err = s.st.AIPairsSince(since); err != nil {
		return review.Review{}, err
	}
	if in.Events, err = s.st.EventsSince(since); err != nil {
		return review.Review{}, err
	}
	if in.Regrets, err = s.st.TopDropped(since, review.RegretMinScore, review.RegretLimit); err != nil {
		return review.Review{}, err
	}
	if in.Stuck, err = s.stuckTopics(now); err != nil {
		return review.Review{}, err
	}

	r := review.Build(in)
	// 平台中文名在 service 层补，review 包不引 format，免得把展示口径散到各处。
	for i := range r.Regrets {
		r.Regrets[i].PlatformName = format.Platform(r.Regrets[i].Platform)
	}
	return r, nil
}

// ReviewWindows 给界面上的 7 / 30 / 90 三个按钮。
func (s *TopicService) ReviewWindows() []int { return review.Windows }
