package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/store"
)

func mustTeardown(t *testing.T, r store.Teardown) ai.Teardown {
	t.Helper()
	var td ai.Teardown
	if err := json.Unmarshal([]byte(r.Result), &td); err != nil {
		t.Fatalf("库里那条拆解解析失败: %v", err)
	}
	return td
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// 用户从手机里复制到的是「标题 + 正文 + 链接」一整段。
// 上一版界面有两个框，他把整段贴进「标题」，模型把广告语当标题拆——
// 所以他看到的六行全是「未提供封面，看不出」这类废话。
func TestTeardownRawSplit(t *testing.T) {
	svc := mkService(t)
	raw := "试用期最后一周，这4件事千万别松\n" +
		"刚入职三个月的人最容易翻车，尤其是第二周就开始表现的那个。\n" +
		"http://xhslink.com/a/abc123，复制本条信息，打开【小红书】App查看精彩内容！"

	v, err := svc.TeardownRaw(raw)
	if err != nil {
		t.Fatalf("TeardownRaw: %v", err)
	}
	if v.Source != "试用期最后一周，这4件事千万别松" {
		t.Fatalf("原文标题没拆对: %q", v.Source)
	}
	if v.Structure == "" || v.Learn == "" {
		t.Fatalf("六行没生成全: %+v", v)
	}

	// 落库，且列表能读到——拆十条之后要靠这个找回来。
	list, err := svc.Teardowns()
	if err != nil || len(list) != 1 {
		t.Fatalf("拆完没落库: n=%d err=%v", len(list), err)
	}
	if list[0].Source != v.Source {
		t.Fatalf("库里的原文标题不对: %q", list[0].Source)
	}
}

// 只给一条链接时必须明说「抓不到」，而不是拿链接当标题硬拆。
// 这是这一版最容易给出假结果的地方：链接照样能送进模型，
// 模型也会一本正经地编出六行——用户拿到的是一份看起来很像的废报告。
func TestTeardownRawLinkOnlyFails(t *testing.T) {
	svc := mkService(t)
	_, err := svc.TeardownRaw("https://www.xiaohongshu.com/explore/66f0a1b2000000001e02c3d4")
	if err == nil {
		t.Fatal("只给链接应该报错")
	}
	if !strings.Contains(err.Error(), "链接") {
		t.Fatalf("报错要说清是链接的问题，实际: %v", err)
	}
	if list, _ := svc.Teardowns(); len(list) != 0 {
		t.Fatalf("失败的拆解不该落库，实际 %d 条", len(list))
	}
}

// 空输入不能走到模型：那会白烧一次调用，还会存一条空记录。
func TestTeardownRawEmpty(t *testing.T) {
	svc := mkService(t)
	if _, err := svc.TeardownRaw("   \n  "); err == nil {
		t.Fatal("空输入应该报错")
	}
}

// 报告要能把六行 + 原文都写全，不然用户下载到手里的是一份没头没尾的 md。
func TestTeardownMarkdown(t *testing.T) {
	svc := mkService(t)
	v, err := svc.TeardownRaw("汇报时领导只回「知道了」，其实是这个意思\n这是我踩了三年坑才明白的。")
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.st.TeardownByID(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	md := teardownMarkdown(r, mustTeardown(t, r))
	for _, want := range []string{"# 汇报时领导只回", "## 标题亮点", "## 封面设计", "## 可借鉴点", "### 原文正文"} {
		if !strings.Contains(md, want) {
			t.Fatalf("报告缺 %q:\n%s", want, md)
		}
	}
}

// 文件名要能一眼认出是哪条，且不能带路径分隔符（Windows 上会直接失败）。
func TestReportName(t *testing.T) {
	at := mustTime(t, "2026-11-16T10:00:00Z")
	got := reportName(`怎样写周报/月报：3 个模板`, at)
	if strings.ContainsAny(got, `/\:*?"<>|`) {
		t.Fatalf("文件名里有非法字符: %q", got)
	}
	if !strings.HasSuffix(got, "20261116.md") {
		t.Fatalf("文件名没带日期: %q", got)
	}
	if !strings.HasPrefix(got, "怎样写周报-月报") {
		t.Fatalf("文件名没带标题: %q", got)
	}
}
