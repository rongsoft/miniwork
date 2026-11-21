package format

import (
	"strings"
	"testing"
)

const longOneLine = "我做了七年 HR，见过太多人在述职会上把一年的功劳说成三句流水账，最后输给了一个 PPT 做得好看但活干得少的人，这件事的根子在于没人教过他怎么汇报，而汇报本身就是一项可以练的技能。"

func TestSplitShared(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantTitle  string
		wantBody   string
		wantURLHas string
	}{
		{
			name: "小红书标准分享文案",
			raw: "试用期最后一周，这4件事千万别松\n" +
				"刚入职三个月的人最容易翻车\n" +
				"51 人赞过\n" +
				"http://xhslink.com/a/abc123，复制本条信息，打开【小红书】App查看精彩内容！",
			wantTitle:  "试用期最后一周，这4件事千万别松",
			wantBody:   "刚入职三个月的人最容易翻车\n51 人赞过",
			wantURLHas: "xhslink.com",
		},
		{
			name: "小红书网页版分享（整行、带口令、带互动数）",
			raw: "88 【《逃出布迪秀》电影解说 模拟恐怖 - 孤灯说影 | 小红书 - 你的生活兴趣社区】 " +
				"😆 jgtYo2akqr3aqBF 😆 " +
				"https://www.xiaohongshu.com/discovery/item/6aec0601000000000303b05c?source=webshare&xsec_token=ABWETz6yKDEpXAwGcy1HufX260HnhayvoJB8hcx29qZHY=&xsec_source=pc_share",
			wantTitle:  "《逃出布迪秀》电影解说 模拟恐怖",
			wantURLHas: "xiaohongshu.com/discovery/item",
		},
		{
			name:      "只贴标题",
			raw:       "  汇报时领导只回「知道了」，其实是这个意思  ",
			wantTitle: "汇报时领导只回「知道了」，其实是这个意思",
		},
		{
			name:      "标题加正文两行",
			raw:       "为什么越加班的人越难被看见\n这是我做了三年才想明白的事。",
			wantTitle: "为什么越加班的人越难被看见",
			wantBody:  "这是我做了三年才想明白的事。",
		},
		{
			name:       "只有一条链接",
			raw:        "https://www.xiaohongshu.com/explore/66f0a1b2000000001e02c3d4",
			wantTitle:  "",
			wantURLHas: "xiaohongshu.com",
		},
		{
			name:      "标题带话题串",
			raw:       "怎样把周报写成领导爱看的样子 #职场 #汇报 #模板 #升职",
			wantTitle: "怎样把周报写成领导爱看的样子",
		},
		{
			name:      "【标题】包装",
			raw:       "【被裁员后我申请劳动仲裁】\n钱到账那天我才知道流程可以这么走",
			wantTitle: "被裁员后我申请劳动仲裁",
			wantBody:  "钱到账那天我才知道流程可以这么走",
		},
		{
			name: "抖音分享带链接",
			raw: "同事都在摸鱼，我该不该跟着躺\n" +
				"3.2w 播放\n" +
				"https://v.douyin.com/iRxY8abc/ 复制链接，打开抖音，看看作者的作品",
			wantTitle:  "同事都在摸鱼，我该不该跟着躺",
			wantBody:   "3.2w 播放",
			wantURLHas: "douyin.com",
		},
		{
			name: "一整篇没换行的长文",
			// 40 字以上的单行：标题取前 40 字，整段进正文，
			// 让模型看全，而不是只拆那句被截断的标题。
			raw:      longOneLine,
			wantBody: longOneLine,
			// 标题就是前 40 个字，别在测试里手抄一遍——抄错一个标点就要查半天。
			wantTitle: string([]rune(longOneLine)[:40]),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			title, body, url := SplitShared(c.raw)
			if title != c.wantTitle {
				t.Errorf("标题 = %q，想要 %q", title, c.wantTitle)
			}
			if body != c.wantBody {
				t.Errorf("正文 = %q，想要 %q", body, c.wantBody)
			}
			if c.wantURLHas == "" {
				if url != "" {
					t.Errorf("不该有链接，却拿到 %q", url)
				}
			} else if !strings.Contains(url, c.wantURLHas) {
				t.Errorf("链接 = %q，应该包含 %q", url, c.wantURLHas)
			}
		})
	}
}

// 空串要能安全返回，否则「开始拆解」在没填时就 panic。
func TestSplitSharedEmpty(t *testing.T) {
	title, body, url := SplitShared("   \n\n  ")
	if title != "" || body != "" || url != "" {
		t.Fatalf("空输入应该全空，实际 %q %q %q", title, body, url)
	}
}
