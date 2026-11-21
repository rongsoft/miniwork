package format

import (
	"regexp"
	"strings"
	"unicode"
)

// SplitShared 把用户粘进来的一段东西拆成「标题 / 正文 / 链接」。
//
// 为什么需要它：用户从手机里点「分享 → 复制链接」，拿到的从来不是一条干净的
// URL，而是「标题 + 正文 + 链接 + 一句 App 广告」这样一段话。上一版界面上有两个
// 输入框（标题、正文），用户把这段整个贴进「标题」，模型就把广告语当标题拆，
// 拆出来的六行全是废话——他只会说「这功能不行」，不会想到是自己贴错了框。
//
// 所以这里认三种粘贴方式，全都能用：
//   - 整段分享文案（最常见）
//   - 只有标题
//   - 标题 + 正文（老用户的手工用法）
//
// 只给一条链接时返回空标题：小红书笔记页要登录态才给正文，硬抓只会拿到
// 验证码页，然后让模型对着一堆 HTML 编故事。这种情况必须让用户补标题。
func SplitShared(raw string) (title, body, url string) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", ""
	}

	// 网页版（电脑上点「分享 → 复制链接」）是另一种格式，和 App 的完全不同：
	//   88 【《逃出布迪秀》电影解说 模拟恐怖 - 孤灯说影 | 小红书 - 你的生活兴趣社区】
	//   😆 jgtYo2akqr3aqBF 😆 https://www.xiaohongshu.com/discovery/item/…
	// 整段只有一行，去掉链接之后剩下的全是包装：互动数、书名号、作者、
	// 平台名、分享口令。上一版把这一整行当标题交给模型，标题里带着
	// 「| 小红书 - 你的生活兴趣社区」，拆解出来的结论自然全是平台的影子。
	if t, b, u, ok := splitWebShare(s); ok {
		return t, b, u
	}

	// 先摘链接，再把带链接的那一行整行删掉——分享文案里链接常常单独一行，
	// 留着它会变成「正文第一行」，模型会把它当内容的一部分。
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if u := firstURL(line); u != "" {
			if url == "" {
				url = u
			}
			// 同一行还有别的文字（"标题 http://…"）时保留文字部分。
			rest := strings.TrimSpace(strings.Replace(line, u, "", 1))
			rest = trimShareJunk(rest)
			if rest != "" && !adLine(rest) {
				kept = append(kept, rest)
			}
			continue
		}
		line = trimShareJunk(line)
		if line != "" {
			kept = append(kept, line)
		}
	}

	if len(kept) == 0 {
		// 只剩一条链接。
		return "", "", url
	}

	// 第一行当标题。分享文案里第一行就是笔记标题，这是小红书/抖音的固定格式。
	title = cleanTitle(kept[0])
	if len(kept) > 1 {
		body = strings.TrimSpace(strings.Join(kept[1:], "\n"))
	}

	// 一行长文（用户直接把整篇粘进来）：前 40 字当标题只是索引，
	// 真正交给模型的是整段正文，不能截断——截了就拆不全结构。
	if len([]rune(title)) > 60 && body == "" {
		r := []rune(title)
		body = title
		title = string(r[:40])
	}
	if len([]rune(title)) > 200 {
		title = string([]rune(title)[:200])
	}
	return title, body, url
}

var urlRe = regexp.MustCompile(`https?://[^\s，。！？、）)】」"']+`)

// webShareHeadRe 匹配网页版分享文案的开头：
//
//	88 【标题 - 作者 | 小红书 - 你的生活兴趣社区】
//
// 前面的数字是点赞/收藏数，位置不固定，有就吃掉。
var webShareHeadRe = regexp.MustCompile(`^\s*(?:\d+(?:\.\d+)?\s*万?\s+)?【([^【】\n]{3,160})】`)

// splitWebShare 拆网页版（PC「分享 → 复制链接」）的分享文案。
//
// 和 App 版是两个格式，不能共用一条解析路：App 版是「标题 / 正文 / 链接 / 广告」
// 分好几行，网页版是**一整行**，标题被包在【】里，后面还挂着作者、
// 平台名和分享口令。不认这个格式，整行就会被当成标题，拆出来的结构
// 全是平台的影子，用户只会说「效果不好」，不会想到是标题里带着广告。
func splitWebShare(s string) (title, body, url string, ok bool) {
	m := webShareHeadRe.FindStringSubmatchIndex(s)
	if m == nil {
		return "", "", "", false
	}
	head := s[m[2]:m[3]]
	rest := strings.TrimSpace(s[m[1]:])

	if u := firstURL(rest); u != "" {
		url = u
		rest = strings.Replace(rest, u, "", 1)
	}
	rest = strings.TrimSpace(trimShareCode(rest))
	rest = trimShareJunk(rest)
	if len([]rune(rest)) > 8 {
		body = rest
	}

	// 【标题 - 作者 | 小红书 - 你的生活兴趣社区】
	// 只有明确带「| 平台」时才切作者，否则「复盘 - 我的第一年」
	// 这种标题会被自己的破折号截断。
	if i := strings.Index(head, "|"); i > 0 {
		head = strings.TrimSpace(head[:i])
		if j := strings.LastIndex(head, " - "); j > 0 {
			head = strings.TrimSpace(head[:j])
		}
	}
	head = cleanTitle(head)
	if head == "" {
		return "", "", url, false
	}
	return head, body, url, true
}

// trimShareCode 去掉「😆 jgtYo2akqr3aqBF 😆」这种分享口令。
//
// 形状很固定：同一个符号夹着一串 6–24 位的字母数字。
// 用 rune 扫而不是正则，是因为要判断「是不是符号」——emoji 落在
// Unicode 的 So 类里，写死几个 emoji 早晚漏掉新的。
func trimShareCode(s string) string {
	for {
		next := cutOneShareCode(s)
		if next == s {
			return s
		}
		s = next
	}
}

func cutOneShareCode(s string) string {
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if !isSymbolRune(r[i]) {
			continue
		}
		j := i + 1
		for j < len(r) && r[j] == ' ' {
			j++
		}
		k := j
		for k < len(r) && isASCIIAlnum(r[k]) {
			k++
		}
		if n := k - j; n < 6 || n > 24 {
			continue
		}
		m := k
		for m < len(r) && r[m] == ' ' {
			m++
		}
		if m < len(r) && isSymbolRune(r[m]) {
			return strings.TrimSpace(string(r[:i]) + string(r[m+1:]))
		}
	}
	return s
}

func isSymbolRune(r rune) bool { return unicode.IsSymbol(r) }

func isASCIIAlnum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func firstURL(s string) string {
	return urlRe.FindString(s)
}

// shareJunk 是各平台分享尾巴上的固定广告语。它们既不是标题也不是正文，
// 留给模型只会被当成"内容风格"的样本，把语气学歪。
var shareJunk = []string{
	"复制本条信息，打开【小红书】App查看精彩内容",
	"复制这条信息，打开【小红书】",
	"打开小红书App查看",
	"打开抖音，看看",
	"复制链接，打开抖音",
	"点击链接直接打开",
	"长按复制此条消息",
}

func trimShareJunk(s string) string {
	s = strings.TrimSpace(s)
	for _, j := range shareJunk {
		s = strings.ReplaceAll(s, j, "")
	}
	s = strings.TrimSpace(s)
	// 只清理「删干净之后剩下的孤立标点」，不动正文里的句号。
	// 上一版无条件 Trim("。!，") 把用户正文的句号都吃了，
	// 粘进模型的是一段没有句读的文字，拆出来的语气就跟着变硬。
	if strings.Trim(s, " \t！!。，,、~～") == "" {
		return ""
	}
	return s
}

// adLine 判断摘掉链接后剩下的这半行是不是平台广告。
// 抖音的分享尾巴是「复制链接，打开抖音，看看作者的作品」，
// 摘掉 URL 会剩半句，留着它就成了一段假正文。
func adLine(s string) bool {
	if len([]rune(s)) > 40 {
		return false
	}
	for _, k := range []string{"复制链接", "打开抖音", "打开小红书", "查看精彩", "看看作者", "长按复制"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// cleanTitle 去掉「标题：」「【】」这类从别处复制过来的标签，
// 以及行尾的话题串。标题里带 5 个 #话题，模型就只会照着话题拆。
func cleanTitle(s string) string {
	s = trimShareJunk(s)
	for _, p := range []string{"标题：", "标题:", "标题 - ", "标题-"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.TrimSpace(s)
	// 【标题】形式，只在整行被括号包住时拆，避免误删正文里的括号。
	if strings.HasPrefix(s, "【") && strings.HasSuffix(s, "】") && strings.Count(s, "【") == 1 {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "【"), "】")
	}
	// 行尾连续话题：「怎样写周报 #职场 #汇报 #模板」→ 只留前半句。
	if i := strings.Index(s, " #"); i > 6 {
		if tail := s[i:]; strings.Count(tail, "#") >= 2 && !strings.ContainsAny(tail, "。！？") {
			s = strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}
