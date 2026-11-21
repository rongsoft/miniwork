package main

import (
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/store"
)

// 三段式发布最关键的一条不变式：勾了几篇 × 几个平台，就必须生成几条任务。
// 少一条意味着有一篇稿子被静默漏掉——用户以为安排好了，实际没发出去。
func TestCreatePublishTasksBuildsCartesian(t *testing.T) {
	svc := newSvc(t)
	addTopic(t, svc, "t1", "选题一", "draft", "")
	now := time.Now()
	id1, err := svc.st.SaveDraft(store.Draft{TopicID: "t1", Platform: "xiaohongshu", Format: "long", Title: "稿一", Body: "正文一", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("存稿失败: %v", err)
	}
	id2, err := svc.st.SaveDraft(store.Draft{TopicID: "t1", Platform: "xiaohongshu", Format: "long", Title: "稿二", Body: "正文二", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("存稿失败: %v", err)
	}

	n, err := svc.CreatePublishTasks(TaskInput{
		DraftIDs:  []string{id1, id2},
		Platforms: []string{"xiaohongshu", "wechat"},
		Accounts:  map[string]string{"xiaohongshu": "@我"},
	})
	if err != nil {
		t.Fatalf("生成任务失败: %v", err)
	}
	if n != 4 {
		t.Errorf("2 篇 × 2 平台应得 4 条，实际 %d 条", n)
	}

	list, err := svc.PublishTasks()
	if err != nil {
		t.Fatalf("读任务失败: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("应读到 4 条，实际 %d 条", len(list))
	}
	// 账号按平台带过来：公众号没记过账号，就应该是空的，
	// 不能把小红书的账号串到公众号那条上。
	for _, v := range list {
		switch v.Platform {
		case "xiaohongshu":
			if v.Account != "@我" {
				t.Errorf("小红书任务账号应为 @我，实际 %q", v.Account)
			}
		case "wechat":
			if v.Account != "" {
				t.Errorf("公众号任务不该带账号，实际 %q", v.Account)
			}
		}
		if v.Words == 0 {
			t.Errorf("任务 %s 没带字数，清单上就分不出哪篇是长文", v.ID)
		}
	}
}

// 平台留空 = 按每篇稿自己的平台发。这是最常用的一条路，
// 报错或者发到默认平台都会让用户白跑一趟。
func TestCreatePublishTasksFallsBackToDraftPlatform(t *testing.T) {
	svc := newSvc(t)
	addTopic(t, svc, "t2", "选题二", "draft", "")
	now := time.Now()
	id3, err := svc.st.SaveDraft(store.Draft{
		TopicID: "t2", Platform: "douyin", Format: "short", Title: "口播", Body: "第一句", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := svc.CreatePublishTasks(TaskInput{DraftIDs: []string{id3}})
	if err != nil {
		t.Fatalf("生成任务失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应生成 1 条，实际 %d 条", n)
	}
	list, _ := svc.PublishTasks()
	if len(list) != 1 || list[0].Platform != "douyin" {
		t.Fatalf("应发到抖音，实际 %+v", list)
	}
}

// 稿子被删之后，任务要留在清单上并标成「稿子已删」。
// 直接让它消失，用户会以为自己没生成过。
func TestPublishTasksKeepsDeletedDraft(t *testing.T) {
	svc := newSvc(t)
	addTopic(t, svc, "t3", "选题三", "draft", "")
	now := time.Now()
	id4, err := svc.st.SaveDraft(store.Draft{
		TopicID: "t3", Platform: "xiaohongshu", Format: "long", Title: "会删的稿", Body: "正文", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePublishTasks(TaskInput{DraftIDs: []string{id4}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteDraft(id4); err != nil {
		t.Fatal(err)
	}

	list, err := svc.PublishTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("任务不该跟着稿子消失，实际剩 %d 条", len(list))
	}
	if !list[0].DraftGone {
		t.Error("稿子删了但任务没标出来，用户会以为是数据丢了")
	}
	if list[0].Title != "会删的稿" {
		t.Errorf("标题应保留生成任务时的快照，实际 %q", list[0].Title)
	}
}
