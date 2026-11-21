package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"rongsoft/miniwork/app/internal/draft"
)

// Request 是一次发布要做的事。
type Request struct {
	Platform string   `json:"platform"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Tags     []string `json:"tags"`
	Images   []string `json:"images"` // 图文：本地图片路径
	Video    string   `json:"video"`  // 视频：本地文件路径

	// AutoSubmit 决定最后那一下点不点。
	//
	// 默认关：前 90% 的机械活（打开页面、上传、填标题正文标签）替用户干，
	// 最后一下「发布」留给他自己按。这一步是唯一不可撤回的操作，
	// 而且平台风控对「无人值守连续发布」最敏感。
	AutoSubmit bool `json:"autoSubmit"`
}

// Result 是发完之后的回执。
type Result struct {
	OK     bool     `json:"ok"`
	URL    string   `json:"url"`
	Manual bool     `json:"manual"` // 浏览器留在手上，需要人接手补完
	Reason string   `json:"reason"`
	Steps  []string `json:"steps"`
}

// Publish 走完整条发布链路。
//
// 失败时的约定：只要浏览器已经打开，就不返回 error，而是返回
// Manual=true + 具体卡在哪儿。因为这时候用户的稿子已经填了一半，
// 关掉窗口等于把他的工作扔掉——比失败更糟。
func (e *Engine) Publish(req Request) (Result, error) {
	p, ok := Get(req.Platform)
	if !ok {
		return Result{}, fmt.Errorf("未知平台: %q", req.Platform)
	}
	return e.publishOn(p, req)
}

// publishOn 是真正的发布流程，平台由参数给定。
//
// 和 Publish 分开是为了能在测试里拿一个本地页面当平台：
// 选择器、填表、上传、点按钮这些逻辑必须能验，而真实平台不登录进不去。
func (e *Engine) publishOn(p Platform, req Request) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if strings.TrimSpace(req.Title) == "" {
		return Result{}, fmt.Errorf("标题是空的，发不了")
	}
	if err := checkMedia(p, req); err != nil {
		return Result{}, err
	}

	ctx, err := e.ensure(p.Key)
	if err != nil {
		return Result{}, err
	}
	res := Result{}

	// 1. 打开发布页
	e.logf("正在打开%s发布页…", p.Name)
	if err := chromedp.Run(ctx, chromedp.Navigate(p.PublishURL)); err != nil {
		return e.manual(&res, "打不开发布页："+err.Error())
	}
	res.Steps = append(res.Steps, "打开 "+p.Name+" 发布页")

	// 2. 先切页签，再上传。
	//
	// 这一步的顺序踩过坑：「上传图文 / 上传视频」是两个页签，各自是独立的
	// 上传区。之前是先上传、后切页签，结果切页签把刚传进去的图片连同一整块
	// 编辑区一起重置了——页面回到空白上传页，后面当然找不到标题输入框，
	// 报出来是「找不到标题输入框」，看着像选择器失效，其实是素材被清空了。
	//
	// 小红书默认停在视频页签，所以这一步几乎每次都要走。
	if len(p.PreClick) > 0 && p.Media == MediaImages && len(req.Images) > 0 {
		// 页签是异步渲染的，刚 Navigate 完还不一定在。点一次没点中是常事，
		// 所以重试到出现为止；而且失败要说出来——静默跳过的话，
		// 后面每一步都在错的页面上下文里跑，最后报的是"找不到标题框"，指错地方。
		if txt, err := clickByTextWait(ctx, p.PreClick, 15*time.Second); err == nil && txt != "" {
			e.logf("已切到「%s」", txt)
			res.Steps = append(res.Steps, "切到「"+txt+"」")
			// 切完页签上传区会重建，等它挂上去再找 input，否则拿到的是旧节点。
			time.Sleep(1500 * time.Millisecond)
		} else {
			e.logf("没点到「%s」页签，先按当前页签继续", strings.Join(p.PreClick, "/"))
		}
	}

	// 3. 上传素材（要放在填文字之前：上传完编辑器才会出现）
	if len(req.Images) > 0 || req.Video != "" {
		files := req.Images
		if req.Video != "" {
			files = []string{req.Video}
		}
		e.logf("正在上传 %d 个文件…", len(files))
		sel, err := waitAny(ctx, p.Upload, 25*time.Second)
		if err != nil {
			return e.manual(&res, "找不到上传入口："+err.Error())
		}
		if err := chromedp.Run(ctx, chromedp.SetUploadFiles(sel, files, chromedp.ByQuery)); err != nil {
			return e.manual(&res, "上传失败："+err.Error())
		}
		res.Steps = append(res.Steps, fmt.Sprintf("上传 %d 个文件", len(files)))
	}

	// 4. 标题
	//
	// 直接等标题框出现，不再先睡固定几秒：素材传完、编辑器渲染出来的时间
	// 取决于网速和图片大小。睡 8 秒遇到慢网就误报"找不到标题框"，
	// 遇到快网又白等 8 秒——用「等到出现为止」既快又稳。
	e.logf("等平台处理素材…")
	tsel, err := waitAny(ctx, p.Title, 30*time.Second)
	if err != nil {
		// 这时候最可能的原因不是选择器失效，而是还停在视频页签上：
		// 切页签那一步没点中时，后面的路全是错的，报错却指向标题框。
		return e.manual(&res, "找不到标题输入框："+err.Error()+
			"。多半还停在「视频」页签，去窗口里点一下「上传图文」再看看")
	}
	if err := typeInto(ctx, tsel, req.Title); err != nil {
		return e.manual(&res, "标题填不进去："+err.Error())
	}
	res.Steps = append(res.Steps, "填标题")

	// 5. 正文（标签按平台规则拼在末尾，和「复制全部」用同一份规则）
	body := draft.Build(mustSpec(p), req.Title, req.Body, req.Tags).Text
	if bsel, err := waitAny(ctx, p.Body, 15*time.Second); err == nil {
		if err := typeInto(ctx, bsel, body); err != nil {
			e.logf("正文没填进去（%v），留给你自己粘", err)
		} else {
			res.Steps = append(res.Steps, "填正文和标签")
		}
	} else {
		e.logf("没找到正文框，留给你自己粘")
	}

	// 5.5 收尾：把平台自己弹的遮挡层关掉
	if n := tidyUp(ctx, tsel); n > 0 {
		e.logf("关掉了 %d 个平台弹窗", n)
		res.Steps = append(res.Steps, "关掉遮挡弹窗")
	}

	// 6. 最后一下
	if !req.AutoSubmit {
		res.Manual = true
		res.Reason = "已经填好了，检查一遍后点「发布」"
		e.logf("已填完，等你点发布")
		return res, nil
	}

	e.logf("正在点发布…")
	label, err := clickByText(ctx, p.Submit)
	if err != nil || label == "" {
		return e.manual(&res, "找不到发布按钮，请手动点一下")
	}
	res.Steps = append(res.Steps, "点「"+label+"」")
	time.Sleep(4 * time.Second)

	u, _ := currentURL(ctx)
	res.URL = u

	// 点过按钮不等于发出去了。平台在提交前会再校验一遍：图片没传完、
	// 必填项没填、触发了验证码、内容命中敏感词——这些都在原页面弹一个
	// 提示，URL 一动不动。之前这里无条件 OK，等于把"卡在弹窗上"记成了
	// 发布成功，用户过两天才发现粉丝看不到。
	if p.EditorPath != "" && strings.Contains(u, p.EditorPath) {
		return e.manual(&res, "点过「"+label+"」了，但还停在编辑页——可能弹了验证码或提示必填项没填，去窗口里看一眼")
	}
	res.OK = true
	if u != "" {
		e.logf("发布完成：%s", u)
	}
	return res, nil
}

// manual 把「卡住了」包装成可交付的结果。
func (e *Engine) manual(res *Result, reason string) (Result, error) {
	res.Manual = true
	res.Reason = reason
	e.logf("停在：%s", reason)
	return *res, nil
}

// checkMedia 在开浏览器之前就把「缺素材」拦掉。
//
// 打开了窗口才告诉用户「小红书要图片」，他会觉得这软件不专业。
func checkMedia(p Platform, req Request) error {
	switch p.Media {
	case MediaImages:
		if len(req.Images) == 0 {
			return fmt.Errorf("%s 发图文必须带图，先在发布面板里选 1-9 张", p.Name)
		}
		if len(req.Images) > 9 {
			return fmt.Errorf("%s 最多 9 张图，现在选了 %d 张", p.Name, len(req.Images))
		}
	case MediaVideo:
		if strings.TrimSpace(req.Video) == "" {
			return fmt.Errorf("%s 要发视频，先选一个视频文件", p.Name)
		}
	}
	for _, f := range append(append([]string{}, req.Images...), req.Video) {
		if f == "" {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("文件找不到了：%s", f)
		}
	}
	return nil
}

func mustSpec(p Platform) draft.Spec {
	if sp, ok := draft.Get(p.Key); ok {
		return sp
	}
	return draft.Spec{Key: p.Key, Name: p.Name}
}

/* ---------- 下面是对页面做动作的小工具 ---------- */

// pick 返回候选里第一个真的存在的选择器。
//
// 用 querySelector 而不是 WaitVisible：元素可能存在但被折叠/透明，
// 那种情况我们仍然应该往里填，让平台自己决定要不要显示。
const jsPick = `(function(sels){
  for (var i = 0; i < sels.length; i++) {
    try { if (document.querySelector(sels[i])) return sels[i]; } catch (e) {}
  }
  return "";
})(%s)`

func pick(ctx context.Context, sels []string) (string, error) {
	if len(sels) == 0 {
		return "", fmt.Errorf("没有候选选择器")
	}
	b, _ := json.Marshal(sels)
	var got string
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(jsPick, string(b)), &got)); err != nil {
		return "", err
	}
	return got, nil
}

// typeInto 先点再输入。
//
// 直接 SendKeys 有时候不生效：编辑器的输入框要先聚焦才监听键盘事件。
// 输入完再读一遍，读不到就当失败——静默失败是最难查的问题。
func typeInto(ctx context.Context, sel, text string) error {
	if text == "" {
		return nil
	}
	err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(sel, chromedp.ByQuery),
		chromedp.Focus(sel, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.SendKeys(sel, text, chromedp.ByQuery),
	)
	if err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)

	var got string
	probe := fmt.Sprintf(`(function(){
      var el = document.querySelector(%q);
      if (!el) return "";
      return (el.value !== undefined && el.value !== null) ? el.value : (el.innerText || el.textContent || "");
    })()`, sel)
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		return nil // 读不回来不算失败：可能只是跨域 iframe，实际已经填上了
	}
	if strings.TrimSpace(got) == "" {
		// 键盘事件没被接住，退回直接写内容 + 派发 input 事件。
		// Quill / ProseMirror 这类编辑器认 input 事件，页面上的字数统计会跟着动。
		setter := fmt.Sprintf(`(function(){
          var el = document.querySelector(%q);
          if (!el) return false;
          if (el.value !== undefined && el.tagName !== 'DIV') {
            el.value = %q;
          } else {
            el.innerText = %q;
          }
          el.dispatchEvent(new Event('input', {bubbles: true}));
          el.dispatchEvent(new Event('change', {bubbles: true}));
          return true;
        })()`, sel, text, text)
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(setter, &ok)); err != nil || !ok {
			return fmt.Errorf("输入框拒绝了输入")
		}
	}
	return nil
}

// jsClickText 按文字点按钮。
const jsClickText = `(function(txts){
  var els = document.querySelectorAll('button, div[role="button"], a, span');
  for (var i = 0; i < els.length; i++) {
    var t = (els[i].innerText || '').trim();
    if (!t || t.length > 12) continue;
    if (txts.indexOf(t) >= 0) { els[i].click(); return t; }
  }
  return "";
})(%s)`

// clickByText 在页面上找写着这几个字的按钮并点它。
func clickByText(ctx context.Context, texts []string) (string, error) {
	if len(texts) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(texts)
	var got string
	err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(jsClickText, string(b)), &got))
	return got, err
}

// clickByTextWait 反复找，直到出现或超时。
//
// 平台的按钮都是异步挂上来的，一次点不中就放弃会得到两种结果：
// 用户看到「找不到 XXX」但页面其实好好的，或者（更糟）后面全跑偏了。
func clickByTextWait(ctx context.Context, texts []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if txt, err := clickByText(ctx, texts); err == nil && txt != "" {
			return txt, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("等了 %s 也没点到（试过 %s）", timeout, strings.Join(texts, " / "))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// tidyUp 关掉平台自己弹出来的遮挡层，返回关掉了几个。
//
// 平台每次改版都会加一个「新功能提示」：图片可编辑啦、试试智能标题、
// 开通某某服务。它们盖在编辑区上方，用户看到的画面是
// 「填完了，但点不到发布」——程序把 90% 的活干了，却把最后两步
// 变成手动关弹窗，这跟没干一样。
//
// 只认白名单里的几行字，不做「点掉所有关闭按钮」那种通用处理：
// 误点发布或删稿都不可撤回，宁可漏关一个，也不能点错一下。
//
// titleSel 是标题输入框。要把焦点从正文挪过去：小红书的话题联想
// 挂在正文编辑器上，input.blur() 之后那个建议面板不会收，仍然压着发布按钮。
func tidyUp(ctx context.Context, titleSel string) int {
	if titleSel != "" {
		// 点标题框是最像人的做法：确认一下标题，顺手把联想框关掉。
		_ = chromedp.Run(ctx, chromedp.Click(titleSel, chromedp.ByQuery))
		time.Sleep(300 * time.Millisecond)
	}
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const a = document.activeElement; if (a && a.blur) a.blur(); })()`, nil),
		chromedp.KeyEvent("\x1b"),
	)
	time.Sleep(400 * time.Millisecond)

	n := 0
	for _, lb := range []string{"我知道了", "知道了"} {
		if txt, err := clickByText(ctx, []string{lb}); err == nil && txt != "" {
			n++
			// 点掉一个之后版面会重排，下一个弹窗的位置也变了，
			// 所以每点一次都重新找，不能一口气点预先算好的坐标。
			time.Sleep(500 * time.Millisecond)
		}
	}
	return n
}

func currentURL(ctx context.Context) (string, error) {
	var u string
	err := chromedp.Run(ctx, chromedp.Location(&u))
	return u, err
}
