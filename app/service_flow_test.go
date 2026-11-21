package main

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 这条测试走的是「一个人的完整使用路径」，不是单个函数。
//
// 为什么值得单独写一条：单元测试各自都绿，接起来照样会断——
// 状态机只允许 draft→picked→scheduled→published，任何一处漏改状态，
// 后面的接口就全都报错。只有顺序走一遍才能发现「第 5 步用不了」。
//
// 用空库 + 脚本化的假模型：不碰真实库、不烧额度，跑一次两秒。

// scriptedAI 按 system 提示词分派固定回复。
// 验的不是模型，而是「模型按格式返回时，解析→落库→再读出来」整条链通不通。
type scriptedAI struct{}

func (scriptedAI) Name() string { return "scripted" }

func (scriptedAI) Complete(_ context.Context, system, user string) (string, error) {
	return scriptedReply(system, user), nil
}

func (scriptedAI) CompleteCreative(_ context.Context, system, user string) (string, error) {
	return scriptedReply(system, user), nil
}

var idRe = regexp.MustCompile(`id=(\S+)`)

func scriptedReply(system, user string) string {
	switch {
	case strings.Contains(system, "爆款笔记拆解师"):
		return `{"title":"数字+身份，把结果前置","cover":"未提供封面，看不出",
			"structure":"身份立信→反常识结论→具体场景","engage":"结尾问「你遇到过吗」",
			"tone":"冷静复盘，带自嘲","topics":["职场","求职"],
			"learn":"标题先给数字再给身份，正文每段配一个具体场景。","category":"职场/成长"}`
	case strings.Contains(system, "标题写手"):
		return `[{"text":"跳槽涨薪快，是因为公司不想留","hook":"反常识","note":"让人想确认自己是不是被坑了"},
			{"text":"内部晋升慢，不是能力问题","hook":"身份刺痛","note":"对上号就想点开"}]`
	case strings.Contains(system, "自媒体内容拆解助手"):
		return `{"summary":"用三个问题筛掉无效社交，核心是先看对方能不能给出具体帮助。",
			"points":["标题用数字加结果","开头先给三个问题","每个观点配一段自己的经历"]}`
	case strings.Contains(system, "选题顾问"):
		ids := idRe.FindAllStringSubmatch(user, -1)
		out := make([]string, 0, len(ids))
		for i, m := range ids {
			out = append(out, fmt.Sprintf(`{"id":%q,"fit":%d,"reason":"和你的人设对得上"}`, m[1], 60+(i*7)%35))
		}
		return "[" + strings.Join(out, ",") + "]"
	default:
		return `{"title":"我在第 40 次投简历时才明白的事","body":"第一段。\n\n第二段。",
			"tags":["职场","求职"],"note":"从失败次数切入"}`
	}
}

// mkService 造一个「能用但没桌面外壳」的服务：托盘/自启那几个方法
// 在这种状态下本来就该报错，测试正好把这条降级路径也走一遍。
func mkService(t *testing.T) *TopicService {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	src, label := source.Build(source.Spec{Kind: "mock"})
	return NewTopicService(st, src, label, scriptedAI{}, "flow.db")
}

func TestFullUserJourney(t *testing.T) {
	svc := mkService(t)

	// ---- 0. 库版本：新库必须直接建到最新，否则用户第一个版本就缺列 ----
	// 13 = v11 的拆解表 + 账号表，v12 的指标来源两列，v13 的发布任务表。
	// 改迁移时必须同步这个数，这是有意的：版本号变了却没人复核这条测试，
	// 说明迁移没被认真对待。
	const wantSchema = 13
	if v, err := svc.st.SchemaVersion(); err != nil || v != wantSchema {
		t.Fatalf("新库版本应为 %d，实际 %d (err=%v)", wantSchema, v, err)
	}

	// ---- 1. 首启动引导 ----
	ob, err := svc.Onboarding()
	if err != nil {
		t.Fatalf("Onboarding: %v", err)
	}
	if !ob.Need {
		t.Fatal("空库首启动应该需要引导")
	}
	if err := svc.FinishOnboarding(); err != nil {
		t.Fatalf("FinishOnboarding: %v", err)
	}
	if ob, _ = svc.Onboarding(); ob.Need {
		t.Fatal("走完引导后不该再要求引导")
	}

	// ---- 2. 赛道与人设 ----
	niches := svc.Niches()
	if len(niches) == 0 {
		t.Fatal("赛道列表是空的，引导页会没有可选项")
	}
	if err := svc.SaveProfile(ProfileView{
		Niche: niches[0], Positioning: "做了 8 年 HR，专讲职场潜规则",
		Audience: "25-35 岁打工人", Tone: "直白不客气", Avoid: "鸡汤",
	}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	mem, err := svc.Memory()
	if err != nil {
		t.Fatalf("Memory: %v", err)
	}
	if !mem.Filled || mem.Profile.Niche != niches[0] {
		t.Fatalf("人设没回显: filled=%v niche=%q", mem.Filled, mem.Profile.Niche)
	}
	if mem.AIName == "" {
		t.Fatal("记忆页要显示当前模型名，空串会让角标显示不出来")
	}

	// ---- 3. 跑一次管道：抓取 → 规则打分 → 模型加权 → 落库 ----
	sum, err := svc.Run(niches[0], "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Fetched == 0 || sum.Saved == 0 {
		t.Fatalf("管道没产出: fetched=%d saved=%d", sum.Fetched, sum.Saved)
	}
	if sum.AIScored == 0 {
		t.Fatalf("配了模型却没走加权打分: ainote=%q", sum.AINote)
	}

	drafts, err := svc.List("draft", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drafts) == 0 {
		t.Fatal("待定池是空的")
	}
	// 卡片上那三条新东西：推荐理由、四维度条、阶段进度轴。
	// 任一为空，界面上就是少一块——不会报错，只是默默不显示。
	var withDims int
	for _, d := range drafts {
		if len(d.Reasons) == 0 {
			t.Fatalf("%q 没有推荐理由", d.Title)
		}
		if len(d.Steps) == 0 || d.Done < 1 {
			t.Fatalf("%q 没有阶段进度: steps=%v done=%d", d.Title, d.Steps, d.Done)
		}
		if len(d.Dims) == 4 {
			withDims++
		}
	}
	if withDims == 0 {
		t.Fatal("所有选题都没有四维度，说明 factors 没落库")
	}

	st, _ := svc.Stats()
	if st.Draft != len(drafts) {
		t.Fatalf("统计与列表不一致: stats.draft=%d list=%d", st.Draft, len(drafts))
	}

	// ---- 4. 收下 → 排期 → 进日历 ----
	first := drafts[0]
	if err := svc.Decide(first.ID, "pick"); err != nil {
		t.Fatalf("Decide pick: %v", err)
	}
	picked, err := svc.List("picked", 0)
	if err != nil || len(picked) != 1 || picked[0].ID != first.ID {
		t.Fatalf("收下后应出现在 picked: %+v err=%v", picked, err)
	}

	planDate := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	if err := svc.Plan(first.ID, planDate); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	planned, _ := svc.List("scheduled", 0)
	if len(planned) != 1 || planned[0].PlanDate != planDate {
		t.Fatalf("排期没写进去: %+v", planned)
	}

	cal, err := svc.Calendar(planDate[:7])
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if cal.Planned == 0 {
		t.Fatal("日历上本月的排期数是 0")
	}

	// ---- 5. 出稿 → 取回 → 改 → 复制包 ----
	dv, err := svc.Write(first.ID, "xiaohongshu", "long", "素材一句", nil)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if dv.Title == "" || dv.Body == "" {
		t.Fatalf("稿子缺内容: %+v", dv)
	}
	list, err := svc.Drafts(first.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("Drafts: %d 篇 err=%v", len(list), err)
	}
	pack, err := svc.DraftPack(dv.ID)
	if err != nil || pack.Title == "" {
		t.Fatalf("DraftPack: %+v err=%v", pack, err)
	}
	// CopyPack 会顺手写系统剪贴板。测试进程没有剪贴板（headless），
	// 这种失败不该算功能坏——内容仍然要完整返回，界面照常能显示。
	cp, err := svc.CopyPack(dv.ID)
	if err != nil && !strings.Contains(err.Error(), "剪贴板") {
		t.Fatalf("CopyPack: %v", err)
	}
	if cp.Text == "" || cp.Title != pack.Title || !strings.Contains(cp.Text, "#职场") {
		t.Fatalf("复制包内容不完整: %+v", cp)
	}
	edited, err := svc.SaveDraft(DraftInput{
		ID: dv.ID, TopicID: first.ID, Platform: "xiaohongshu", Format: "long",
		Title: "改过的标题", Body: "改过的正文", Tags: []string{"职场"},
	})
	if err != nil || edited.Title != "改过的标题" {
		t.Fatalf("SaveDraft 没生效: %+v err=%v", edited, err)
	}

	// ---- 6. 发布 → 回填 → 笔记复盘 ----
	pid, err := svc.MarkPublished(PublishInput{
		TopicID: first.ID, DraftID: dv.ID, Platform: "xiaohongshu",
		URL: "https://www.xiaohongshu.com/explore/x", Date: "", Note: "",
	})
	if err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}
	if got, _ := svc.List("published", 0); len(got) != 1 {
		t.Fatalf("标记发布后应进已发布: %+v", got)
	}
	board, err := svc.PublishBoard()
	if err != nil {
		t.Fatalf("PublishBoard: %v", err)
	}
	if len(board.Records) != 1 {
		t.Fatalf("发布台应有 1 条台账: %d", len(board.Records))
	}
	if board.Stats.Total != 1 || board.Stats.Filled != 0 {
		t.Fatalf("刚记的发布应算「未回填」: %+v", board.Stats)
	}

	// 没回填之前，笔记复盘不该把它算进来——那是待办，不是复盘。
	nb, err := svc.NoteBoard()
	if err != nil {
		t.Fatalf("NoteBoard: %v", err)
	}
	if len(nb.Items) != 0 {
		t.Fatalf("没填数据的不该进复盘列表: %d", len(nb.Items))
	}

	if err := svc.UpdateMetrics(pid, 12400, 318, 205, 42); err != nil {
		t.Fatalf("UpdateMetrics: %v", err)
	}
	nb, err = svc.NoteBoard()
	if err != nil {
		t.Fatalf("NoteBoard: %v", err)
	}
	if len(nb.Items) != 1 || !nb.Items[0].Filled {
		t.Fatalf("回填后应出现 1 条已填数据: %+v", nb.Items)
	}
	if r := nb.Items[0].Rate; r < 4.5 || r > 4.6 {
		t.Fatalf("互动率算错: (318+205+42)/12400 应约 4.56，实际 %v", r)
	}
	if nb.Stats.Pending != 1 {
		t.Fatalf("待复盘应为 1，实际 %d", nb.Stats.Pending)
	}

	if err := svc.SaveNoteReview(NoteReviewInput{
		ID: pid, Rating: 5, Analysis: "前三行留人", Lesson: "开篇先给结论",
	}); err != nil {
		t.Fatalf("SaveNoteReview: %v", err)
	}
	nb, _ = svc.NoteBoard()
	if nb.Stats.Pending != 0 || nb.Stats.Rated != 1 {
		t.Fatalf("存完复盘统计没跟上: %+v", nb.Stats)
	}
	if len(nb.Lessons) != 1 || nb.Lessons[0].Lesson != "开篇先给结论" {
		t.Fatalf("可复用经验没捞出来: %+v", nb.Lessons)
	}
	// 撤销评分：点错星要能退回去，否则均值里永远留着一个假分。
	if err := svc.SaveNoteReview(NoteReviewInput{ID: pid, Rating: 0}); err != nil {
		t.Fatalf("撤销评分: %v", err)
	}
	if nb, _ = svc.NoteBoard(); nb.Stats.Pending != 1 {
		t.Fatalf("撤销后应回到待复盘，实际 %+v", nb.Stats)
	}

	// ---- 7. 素材库：录入 → 提炼 → 关联 → 删除 ----
	mat, err := svc.AddMaterial(MaterialInput{
		URL: "https://www.xiaohongshu.com/explore/m1", Platform: "xiaohongshu",
		Title: "三个问题筛掉无效社交", Body: "正文正文正文", Likes: 0, Collects: 3,
	})
	if err != nil {
		t.Fatalf("AddMaterial: %v", err)
	}
	if mat.Summary == "" || len(mat.Points) != 3 {
		t.Fatalf("素材没提炼出来: summary=%q points=%v", mat.Summary, mat.Points)
	}
	if mat.Likes != "0" {
		t.Fatalf("0 赞是真实读数，不该显示成 —: %q", mat.Likes)
	}
	ms, err := svc.MaterialStats()
	if err != nil || ms.Total != 1 || ms.Torn != 1 {
		t.Fatalf("素材统计: %+v err=%v", ms, err)
	}
	linked, err := svc.LinkMaterial(mat.ID, first.ID)
	if err != nil || linked.TopicTitle == "" {
		t.Fatalf("关联选题后应能回查到标题: %+v err=%v", linked, err)
	}
	// 同一条链接再存一次：URL 即主键，不该变成两条。
	if again, err := svc.AddMaterial(MaterialInput{
		URL: mat.URL, Platform: "xiaohongshu", Title: "改个标题",
	}); err != nil || again.ID != mat.ID {
		t.Fatalf("同链接应去重: %+v err=%v", again, err)
	}
	if all, _ := svc.Materials("xiaohongshu"); len(all) != 1 {
		t.Fatalf("同链接应该只有 1 条，实际 %d", len(all))
	}
	if err := svc.DeleteMaterial(mat.ID); err != nil {
		t.Fatalf("DeleteMaterial: %v", err)
	}
	if all, _ := svc.Materials(""); len(all) != 0 {
		t.Fatalf("删完应为空，实际 %d", len(all))
	}

	// ---- 8. 数据分析（判断层）与选题复盘 ----
	rv, err := svc.Review(30)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if rv.Headline == "" || len(rv.Funnel) == 0 {
		t.Fatalf("复盘缺结论或漏斗: headline=%q funnel=%d", rv.Headline, len(rv.Funnel))
	}
	if len(svc.ReviewWindows()) == 0 {
		t.Fatal("复盘窗口列表是空的，页头上就没有 7/30/90")
	}

	// ---- 9. 创作工具 ----
	td, err := svc.Teardown("干了8年HR，说几句实话", "正文")
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if td.Structure == "" || td.Learn == "" || td.Category == "" {
		t.Fatalf("拆解结果不完整: %+v", td)
	}
	// 拆完必须入库：否则「拆过十条，第二天想回看标题公式」只能重拆一遍。
	if hist, err := svc.Teardowns(); err != nil || len(hist) != 1 {
		t.Fatalf("拆解没入库: %d 条 err=%v", len(hist), err)
	} else if hist[0].Source != "干了8年HR，说几句实话" || hist[0].Learn == "" {
		t.Fatalf("入库的内容不对: %+v", hist[0])
	}
	if err := svc.DeleteTeardown(td.ID); err != nil {
		t.Fatalf("DeleteTeardown: %v", err)
	}
	if hist, _ := svc.Teardowns(); len(hist) != 0 {
		t.Fatalf("删完应为空，实际 %d 条", len(hist))
	}
	titles, err := svc.SuggestTitles("跳槽涨薪比晋升快", []string{"反常识"})
	if err != nil || len(titles) != 2 {
		t.Fatalf("SuggestTitles: %d 条 err=%v", len(titles), err)
	}

	// ---- 10. 设置：定时、屏蔽词、外壳 ----
	if err := svc.SaveSchedule(ScheduleView{Enabled: true, Time: "07:30"}); err != nil {
		t.Fatalf("SaveSchedule: %v", err)
	}
	sch, err := svc.Schedule()
	if err != nil || !sch.Enabled || sch.Time != "07:30" {
		t.Fatalf("定时设置没存住: %+v err=%v", sch, err)
	}
	if sch.NextLabel == "" {
		t.Fatal("开着定时却没算下次运行时间")
	}
	if err := svc.SaveSchedule(ScheduleView{Enabled: false, Time: "25:99"}); err == nil {
		t.Fatal("非法时间应该报错")
	}

	if err := svc.BlockLearntWord("周报"); err != nil {
		t.Fatalf("BlockLearntWord: %v", err)
	}
	if mem, _ = svc.Memory(); !contains(mem.Blocked, "周报") {
		t.Fatalf("屏蔽词没生效: %v", mem.Blocked)
	}
	if err := svc.UnblockLearntWord("周报"); err != nil {
		t.Fatalf("UnblockLearntWord: %v", err)
	}
	if mem, _ = svc.Memory(); contains(mem.Blocked, "周报") {
		t.Fatalf("解除屏蔽没生效: %v", mem.Blocked)
	}

	// 没有桌面外壳时，开关必须给明确错误，而不是静默失败。
	if err := svc.SetResident(true); err == nil {
		t.Fatal("没有外壳时 SetResident 应该报错")
	}
	if sh := svc.AppShell(); sh.AutoStart.Supported && !sh.AutoStart.Enabled {
		// 正常情况：读系统真实状态，没开就是没开。
	}
	if svc.DBPath() == "" || svc.Platform() == "" || svc.SourceName() == "" {
		t.Fatal("设置页要显示的三个基础信息有为空的")
	}
	if len(svc.Platforms()) == 0 || svc.CraftAvailable().Ready != true {
		t.Fatal("平台列表或创作可用状态不对")
	}

	// ---- 11. 回收站与清理 ----
	if err := svc.DeleteDraft(dv.ID); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if list, _ = svc.Drafts(first.ID); len(list) != 0 {
		t.Fatalf("删稿后还剩 %d 篇", len(list))
	}
}

// 没有模型时必须给出「能看懂、能行动」的提示，而不是空指针或英文报错。
// 这条和上一条分开：上一条验功能，这一条验「功能不可用时用户看到什么」。
func TestAIUnavailableMessages(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "noai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := &TopicService{st: st, srcLabel: "示例数据"}

	if _, err := svc.Teardown("标题", "正文"); err == nil {
		t.Fatal("没有模型时拆解应该报错")
	} else if !strings.Contains(err.Error(), "模型") && !strings.Contains(err.Error(), "配置") {
		t.Fatalf("报错信息要能指导用户去配置模型，实际: %v", err)
	}
	if _, err := svc.SuggestTitles("内容", nil); err == nil {
		t.Fatal("没有模型时出标题应该报错")
	}
	if _, err := svc.Write("t-x", "xiaohongshu", "long", "", nil); err == nil {
		t.Fatal("没有模型时写稿应该报错")
	}
	if c := svc.CraftAvailable(); c.Ready {
		t.Fatal("没有模型时创作工具不该显示可用")
	}
}

// 状态机是这一堆接口的地基：越级操作必须被拒，否则排期/发布台账会错位。
func TestStatusMachineRejectsIllegalJumps(t *testing.T) {
	svc := mkService(t)
	if _, err := svc.st.SaveTopic(store.Topic{
		ID: "t-1", Title: "标题", Platform: "xiaohongshu", Niche: "职场",
		Score: 80, Status: topic.Draft, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// 没排期不能直接标记发布。
	if err := svc.Decide("t-1", "publish"); err == nil {
		t.Fatal("draft 直接 publish 应该被拒")
	}
	if err := svc.Decide("t-1", "乱写"); err == nil {
		t.Fatal("未知操作应该报错")
	}
	if err := svc.Decide("不存在的id", "pick"); err == nil {
		t.Fatal("操作不存在的选题应该报错")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
