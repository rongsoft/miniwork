// Package memory 是记忆的第三层：从「你做过什么选择」里学，不靠你填。
//
// 前两层分别是：
//   - 人设（你手填的四句话，喂给模型）
//   - 决定（你每收一条、丢一条，都留痕）
//
// 这一层把第二层的数据变成能反过来影响打分的结论。没有它，系统对你的了解
// 就停在「你填过什么」，永远学不会「你实际上偏爱什么」——
// 而后者才是用户不填也能感受到的那部分。
//
// 一条硬约束写在最前面：**只学词，不学结论**。
// 学出的结果必须能原样显示成「我注意到你常收带『汇报』的」，用户看一眼就能
// 判断对不对、能删掉。任何不能这样表达的「画像」「标签」都不该做——
// 用户没法纠错的记忆，等于往他脑子里塞了一个他不认识的人。
package memory

import (
	"fmt"
	"sort"
	"strings"
)

// 为什么这几个数是这些值，都写在推导里，改之前先读一遍。
const (
	// minSupport：一个词至少出现在这么多条「收下」的标题里才算数。
	//
	// 2 条太松——用户可能只是那天顺手，两条撞上同一个词纯属巧合；
	// 4 条太紧——新用户攒到 4 条同类要写小半个月，功能等于不启动。
	// 3 条是「反复出现」能成立的最小值。
	minSupport = 3

	// maxWords：最多同时生效这么多个偏好词。
	//
	// 不是性能考虑。是「我学到了 8 条」用户还能逐条看一遍，
	// 「我学到了 40 条」他就直接划过去了，那等于没告诉他。
	maxWords = 8

	// ratioBias：收下次数要超过丢掉次数的这个倍数才对它加分。
	//
	// 只写 picked >= dropped 不够。用户收 5 条丢 5 条的词，
	// 说明这个词对判断毫无区分度（两边都沾），按它加分纯属噪声。
	// 要求 2 倍，是要求「这个词明显偏向你收下的那一侧」。
	ratioBias = 2

	// minLen / maxLen：候选词的字数范围。
	//
	// 2 字是中文实词的下限（领导、汇报、面试），1 字噪声太大（「他」「上」）。
	// 4 字封顶：再长的组合通常是整句碎片，换一篇标题就不出现了，
	// 加上去只会让加分永远命中不了。
	minLen = 2
	maxLen = 4
)

// Word 是一条学到的偏好，带它的证据。
//
// Picked / Dropped 必须原样给用户看：「汇报 出现在你收下的 6 条里」
// 这句话本身就是可信度。只给一个「权重 0.82」用户没法判断该不该信。
type Word struct {
	Text    string `json:"text"`
	Picked  int    `json:"picked"`
	Dropped int    `json:"dropped"`
}

// Learning 是一次学习的全部结果。
type Learning struct {
	Words   []Word `json:"words"`
	Picked  int    `json:"picked"`  // 参与学习的「收下」条数
	Dropped int    `json:"dropped"` // 参与学习的「丢掉」条数
	Enough  bool   `json:"enough"`  // 样本够不够下结论
	Note    string `json:"note"`    // 一句人话，界面直接用
}

// 这几句都在回答同一个问题：凭什么。
//
// 界面上方已经有一句静态说明（「从你的取舍里学来的，不是你填的」），
// 这里不再重复那层意思，只补它补不了的：这次是基于多少数据得出的。
// 两处都强调「不是我填的」，用户读两遍会觉得这产品在自我辩解。
//
// 空结果也要有话说。这里刻意不返回「正在学习中」这类假进度：
// 用户会等一个永远不会到的东西。
const (
	noteNoChoice  = "你还没收下过任何选题。收几条，我就能看出你偏爱什么。"
	noteTooFew    = "已经收下 %d 条，还看不出稳定的偏好。再多几条我就有数了。"
	noteNoPattern = "收下 %d 条、丢掉 %d 条，但没找到能反复出现的共同点——说明你挑得比较分散，这本身也算一种风格。"
	noteLearned   = "依据是你收下的 %d 条和丢掉的 %d 条。词后面的数字，是它出现在多少条标题里。"
)

// Learn 从「收下的标题」和「丢掉的标题」里提取偏好词。
//
// exclude 是调用方要求排除的词（通常是当前赛道的关键词）：
// 赛道匹配已经在打分里占了 40 分，再按偏好加一次是重复计分，
// 会让「赛道词命中多」的标题被算了两次，排序失真。
func Learn(picked, dropped, exclude []string) Learning {
	l := Learning{Picked: len(picked), Dropped: len(dropped)}

	if len(picked) < minSupport {
		l.Note = noteNoChoice
		if len(picked) > 0 {
			l.Note = fmt.Sprintf(noteTooFew, len(picked))
		}
		return l
	}

	// 统计每个候选词在两边的「不同标题」数。
	//
	// 用「标题数」而不是「出现次数」：一条标题里「领导」出现两次
	// 不该算两票，那不是两条独立的证据。
	picks := countTitles(picked)
	drops := countTitles(dropped)

	// 排除用「包含」而不是「相等」。
	//
	// 赛道词「裁员」要连「裁员的」一起排除，否则赛道词换个后缀就漏回来了；
	// 用户手动屏蔽「汇报」时，心里想的是「别再按汇报这类词给我加分」，
	// 不是「除了『汇报』两个字以外都行」。停用词同理。
	blocked := func(s string) bool {
		for _, w := range exclude {
			if strings.Contains(s, w) {
				return true
			}
		}
		for _, w := range stopWords {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}

	type cand struct {
		text    string
		picked  int
		dropped int
	}
	var cands []cand
	for text, n := range picks {
		if n < minSupport || blocked(text) {
			continue
		}
		if d := drops[text]; n < d*ratioBias {
			// 两边都沾，或者丢掉那边更多：这个词对你没有区分度。
			continue
		}
		// 全是虚字拼出来的（「的了」「是在」）没有实义。
		if allStopChars(text) {
			continue
		}
		cands = append(cands, cand{text, n, drops[text]})
	}

	// 排序：先按出现次数（证据多的优先），同次数时短的优先。
	//
	// 短词优先是个有意的选择。「领导」和「领导的决策」若出现次数相同，
	// 说明后者从没单独出现过——留长的看似更精确，实际上换一条新标题
	// 就只有「领导」能命中，加分机会少一大半。宁可泛一点。
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].picked != cands[j].picked {
			return cands[i].picked > cands[j].picked
		}
		if len(cands[i].text) != len(cands[j].text) {
			return len(cands[i].text) < len(cands[j].text)
		}
		return cands[i].text < cands[j].text
	})

	// 子串抑制：已经被更短词覆盖的候选丢掉。
	// 「领导」入选之后，「领导的」再来一遍是重复加分。
	for _, c := range cands {
		covered := false
		for _, chosen := range l.Words {
			if strings.Contains(c.text, chosen.Text) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		l.Words = append(l.Words, Word{Text: c.text, Picked: c.picked, Dropped: c.dropped})
		if len(l.Words) >= maxWords {
			break
		}
	}

	if len(l.Words) == 0 {
		l.Note = fmt.Sprintf(noteNoPattern, len(picked), len(dropped))
		return l
	}
	l.Enough = true
	l.Note = fmt.Sprintf(noteLearned, len(picked), len(dropped))
	return l
}

// countTitles 返回「某词出现在多少条不同标题里」。
func countTitles(titles []string) map[string]int {
	out := map[string]int{}
	for _, t := range titles {
		seen := map[string]bool{}
		for _, g := range ngrams(t) {
			if seen[g] {
				continue
			}
			seen[g] = true
			out[g]++
		}
	}
	return out
}

// ngrams 在一条标题里切出 minLen..maxLen 字的纯汉字片段。
//
// 不用现成的分词库有两个原因：
//  1. 主流中文分词库要带几 MB 词典，这个 exe 一共 22MB，
//     为了一处功能塞一份词典，用户下载时就多等几秒；
//  2. 分词库的切分边界对这里没有意义——我们要的不是「正确的词」，
//     而是「反复出现且能区分收下与丢掉的片段」。词表法反而更稳。
//
// 先按非汉字切成片段再在片段内滑窗，避免「裁员后我把」切出「员后」这种
// 跨越标点的假词。假词本身会被后面的频次门槛滤掉，但从源头少产生一些
// 总是更好——免得哪天门槛调松，假词就漏出来了。
func ngrams(title string) []string {
	var out []string
	for _, seg := range strings.FieldsFunc(title, func(r rune) bool {
		return !isHan(r)
	}) {
		rs := []rune(seg)
		for n := minLen; n <= maxLen; n++ {
			for i := 0; i+n <= len(rs); i++ {
				out = append(out, string(rs[i:i+n]))
			}
		}
	}
	return out
}

func isHan(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// allStopChars 判断一个片段是否全由虚字组成。
func allStopChars(s string) bool {
	for _, r := range s {
		if !stopChars[r] {
			return false
		}
	}
	return true
}

// stopChars 是虚字集合，用来剔掉「的了」「是在」这类拼得出的无义片段。
var stopChars = func() map[rune]bool {
	m := map[rune]bool{}
	for _, r := range "的了着过是在有和与或但而就都也还很更最这那我你他她它们个不没要会能可被把给对从向到于以为所之其上下里外中" {
		m[r] = true
	}
	return m
}()

// stopWords 是两字以上的常见虚词/口头语。
//
// 它们能通过「反复出现」的检验（谁的标题里都有一堆），但学它们毫无意义——
// 按「一个」给你加分，等于什么都不分。
var stopWords = []string{
	"时候", "这个", "那个", "什么", "怎么", "为什", "因为", "所以", "但是",
	"如果", "可以", "没有", "不是", "就是", "这样", "那样", "我们", "你们",
	"他们", "自己", "已经", "还是", "只是", "真的", "其实", "到底", "一直",
	"一些", "有些", "一个", "两个", "几个", "多少", "如何", "哪里", "这里",
	"那里", "现在", "以后", "之前", "之后", "起来", "出来", "下来", "过来",
	"一起", "一样", "这么", "那么", "知道", "觉得", "开始", "最后", "第一",
	"第二", "第三", "今天", "昨天", "明天", "一年", "一天", "一次", "事情",
	"东西", "问题", "办法", "方式", "情况", "地方", "时间", "年前", "个月",
}

// Texts 返回纯词表，给打分用。
//
// 打分只需要「哪些词」，不需要证据条数。分开两个方法是为了让
// score 包拿到的类型尽可能窄：它要是能拿到 Word，慢慢就会去用 Picked
// 做加权，那样记忆的解释性就散到别的包里去了。
func (l Learning) Texts() []string {
	if len(l.Words) == 0 {
		return nil
	}
	out := make([]string, 0, len(l.Words))
	for _, w := range l.Words {
		out = append(out, w.Text)
	}
	return out
}
