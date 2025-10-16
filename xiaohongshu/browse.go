package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

// BrowseConfig 浏览配置
type BrowseConfig struct {
	// 浏览时长（分钟）
	Duration int `json:"duration"`
	// 滚动次数范围
	MinScrolls int `json:"min_scrolls"`
	MaxScrolls int `json:"max_scrolls"`
	// 点击笔记的概率 (0-100)
	ClickProbability int `json:"click_probability"`
	// 在笔记中互动的概率 (0-100) - 点赞、收藏、评论一起执行
	InteractProbability int `json:"interact_probability"`
	// 评论内容列表（随机选择）
	Comments []string `json:"comments,omitempty"`
}

// BrowseAction 浏览动作
type BrowseAction struct {
	page   *rod.Page
	config BrowseConfig
}

// BrowseStats 浏览统计
type BrowseStats struct {
	Duration      time.Duration `json:"duration"`
	ScrollCount   int           `json:"scroll_count"`
	ClickCount    int           `json:"click_count"`
	LikeCount     int           `json:"like_count"`
	FavoriteCount int           `json:"favorite_count"`
	CommentCount  int           `json:"comment_count"`
	ViewedNotes   []string      `json:"viewed_notes"`
}

// NewBrowseAction 创建浏览动作
func NewBrowseAction(page *rod.Page, config BrowseConfig) *BrowseAction {
	// 设置默认值
	if config.Duration == 0 {
		config.Duration = 10 // 默认浏览10分钟
	}
	if config.MinScrolls == 0 {
		config.MinScrolls = 3
	}
	if config.MaxScrolls == 0 {
		config.MaxScrolls = 8
	}
	if config.ClickProbability == 0 {
		config.ClickProbability = 30 // 默认30%概率点击笔记
	}
	if config.InteractProbability == 0 {
		config.InteractProbability = 50 // 默认50%概率互动
	}

	return &BrowseAction{
		page:   page,
		config: config,
	}
}

// StartBrowse 开始浏览推荐页
func (b *BrowseAction) StartBrowse(ctx context.Context) (*BrowseStats, error) {
	logrus.Info("开始模拟人类浏览小红书推荐页")

	stats := &BrowseStats{
		ViewedNotes: make([]string, 0),
	}
	startTime := time.Now()

	// 导航到推荐页
	page := b.page.Context(ctx).Timeout(60 * time.Second)
	page.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	time.Sleep(randomDuration(1000, 2000)) // 等待页面完全加载

	// 浏览时长
	browseUntil := time.Now().Add(time.Duration(b.config.Duration) * time.Minute)

	// 刷新机制：模拟真实用户每隔几分钟刷新页面获取新内容
	// 随机设置第一次刷新时间：2-5分钟后
	nextRefreshTime := time.Now().Add(randomRefreshInterval())
	logrus.Infof("计划在 %.1f 分钟后刷新页面", time.Until(nextRefreshTime).Minutes())

	for time.Now().Before(browseUntil) {
		select {
		case <-ctx.Done():
			logrus.Info("浏览被取消")
			stats.Duration = time.Since(startTime)
			return stats, ctx.Err()
		default:
			// 检查是否到了刷新时间
			if time.Now().After(nextRefreshTime) {
				logrus.Info("刷新推荐页以获取新内容")
				page.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
				time.Sleep(randomDuration(1500, 3000)) // 刷新后等待加载

				// 设置下一次刷新时间：2-5分钟后
				nextRefreshTime = time.Now().Add(randomRefreshInterval())
				remainingMinutes := time.Until(browseUntil).Minutes()
				if remainingMinutes > 0 {
					logrus.Infof("页面已刷新，计划在 %.1f 分钟后再次刷新（剩余浏览时间: %.1f 分钟）",
						time.Until(nextRefreshTime).Minutes(), remainingMinutes)
				}
			}

			// 执行一轮浏览
			if err := b.browseRound(ctx, stats); err != nil {
				logrus.Warnf("浏览出错: %v", err)
				time.Sleep(randomDuration(2000, 4000))
				continue
			}
		}
	}

	stats.Duration = time.Since(startTime)
	logrus.Infof("浏览完成 - 统计: 滚动%d次, 点击%d个笔记, 点赞%d次, 收藏%d次, 评论%d次",
		stats.ScrollCount, stats.ClickCount, stats.LikeCount, stats.FavoriteCount, stats.CommentCount)

	return stats, nil
}

// browseRound 执行一轮浏览
func (b *BrowseAction) browseRound(ctx context.Context, stats *BrowseStats) error {
	page := b.page.Context(ctx)

	// 随机滚动次数
	scrollCount := rand.Intn(b.config.MaxScrolls-b.config.MinScrolls+1) + b.config.MinScrolls

	for i := 0; i < scrollCount; i++ {
		// 模拟人类滚动（包含回滚机制）
		if err := b.humanLikeScrollWithBacktrack(page); err != nil {
			return err
		}
		stats.ScrollCount++

		// 停留时间：中位数 3-6s
		time.Sleep(randomDuration(3000, 6000))

		// 根据概率决定是否点击笔记
		if rand.Intn(100) < b.config.ClickProbability {
			if err := b.clickAndViewNote(ctx, stats); err != nil {
				logrus.Warnf("点击笔记出错: %v", err)
				// 返回推荐页
				page.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
				time.Sleep(randomDuration(1000, 2000))
			}
		}
	}

	return nil
}

// humanLikeScroll 模拟人类滚动（保留旧版本作为备用）
func (b *BrowseAction) humanLikeScroll(page *rod.Page) error {
	// 随机选择滚动方式
	scrollType := rand.Intn(3)

	switch scrollType {
	case 0:
		// 使用鼠标滚轮
		scrollAmount := rand.Intn(700) + 600 // 600-1300像素（加长）
		page.Mouse.MustScroll(0, float64(scrollAmount))

	case 1:
		// 使用键盘方向键
		times := rand.Intn(3) + 1
		for i := 0; i < times; i++ {
			page.MustElement("body").MustKeyActions().Press(input.ArrowDown).MustDo()
			time.Sleep(randomDuration(100, 300))
		}

	case 2:
		// 使用 JavaScript 滚动
		scrollAmount := rand.Intn(800) + 700 // 700-1500像素（加长）
		page.MustEval(fmt.Sprintf(`() => window.scrollBy({top: %d, behavior: 'smooth'})`, scrollAmount))
	}

	time.Sleep(randomDuration(300, 800)) // 滚动后短暂停留
	return nil
}

// humanLikeScrollWithBacktrack 模拟人类滚动（优化版，包含回滚机制）
// 基于真实用户行为参数：
// - 滚动段时长：0.6-2.5s
// - 插入短暂停：0.2-1.2s
// - 回滚概率：7-18%
func (b *BrowseAction) humanLikeScrollWithBacktrack(page *rod.Page) error {
	// 随机选择滚动方式
	scrollType := rand.Intn(3)

	// 主滚动动作
	scrollAmount := rand.Intn(900) + 700 // 700-1600像素（加长）

	switch scrollType {
	case 0:
		// 使用鼠标滚轮（分段滚动更自然）
		segments := rand.Intn(3) + 2 // 2-4段
		totalScroll := scrollAmount
		for i := 0; i < segments; i++ {
			segmentScroll := totalScroll / segments
			if i == segments-1 {
				segmentScroll = totalScroll - (segmentScroll * (segments - 1))
			}
			page.Mouse.MustScroll(0, float64(segmentScroll))

			// 插入短暂停：0.2-1.2s
			time.Sleep(randomDuration(200, 1200))
		}

	case 1:
		// 使用键盘方向键
		times := rand.Intn(4) + 2 // 2-5次
		for i := 0; i < times; i++ {
			page.MustElement("body").MustKeyActions().Press(input.ArrowDown).MustDo()
			// 插入短暂停：0.2-1.2s
			time.Sleep(randomDuration(200, 1200))
		}

	case 2:
		// 使用 JavaScript 滚动
		page.MustEval(fmt.Sprintf(`() => window.scrollBy({top: %d, behavior: 'smooth'})`, scrollAmount))
		// 等待滚动动画完成
		time.Sleep(randomDuration(600, 1000))
	}

	// 滚动段时长：0.6-2.5s
	time.Sleep(randomDuration(600, 2500))

	// 回滚概率：7-18%
	backtrackProbability := rand.Intn(12) + 7 // 7-18
	if rand.Intn(100) < backtrackProbability {
		logrus.Debug("触发回滚行为")
		// 向上回滚一小段距离（通常是滚动距离的 20-40%）
		backtrackAmount := -(scrollAmount * (rand.Intn(20) + 20) / 100)

		if scrollType == 1 {
			// 键盘回滚
			times := rand.Intn(2) + 1
			for i := 0; i < times; i++ {
				page.MustElement("body").MustKeyActions().Press(input.ArrowUp).MustDo()
				time.Sleep(randomDuration(200, 500))
			}
		} else {
			// 鼠标或JS回滚
			page.Mouse.MustScroll(0, float64(backtrackAmount))
		}

		// 回滚后的停顿
		time.Sleep(randomDuration(500, 1500))
	}

	return nil
}

// clickAndViewNote 点击并浏览笔记
func (b *BrowseAction) clickAndViewNote(ctx context.Context, stats *BrowseStats) error {
	page := b.page.Context(ctx)

	// 从 window.__INITIAL_STATE__ 获取笔记列表（与其他 MCP 功能保持一致）
	feeds, err := b.getFeedsFromPage(page)
	if err != nil || len(feeds) == 0 {
		return fmt.Errorf("未找到笔记列表: %v", err)
	}

	// 基于已浏览去重，优先选择未浏览的笔记
	viewed := make(map[string]struct{}, len(stats.ViewedNotes))
	for _, id := range stats.ViewedNotes {
		viewed[id] = struct{}{}
	}
	unviewed := make([]Feed, 0, len(feeds))
	for _, f := range feeds {
		if f.ID == "" {
			continue
		}
		if _, ok := viewed[f.ID]; !ok {
			unviewed = append(unviewed, f)
		}
	}

	var selectedFeed Feed
	if len(unviewed) > 0 {
		selectedFeed = unviewed[rand.Intn(len(unviewed))]
	} else {
		// 回退：都看过则仍随机一个，但尽量通过滚动引入新内容
		selectedFeed = feeds[rand.Intn(len(feeds))]
	}
	feedID := selectedFeed.ID
	xsecToken := selectedFeed.XsecToken

	if feedID == "" || xsecToken == "" {
		return fmt.Errorf("笔记信息不完整")
	}

	logrus.Infof("点击笔记: %s", feedID)

	// 获取对应的 DOM 元素并点击
	noteCards, err := b.getVisibleNoteCards(page)
	if err != nil || len(noteCards) == 0 {
		return fmt.Errorf("未找到可见笔记卡片")
	}

	// 在可见卡片中寻找对应 feedID 的卡片；找不到则回退随机
	var selectedCard *rod.Element
	for _, card := range noteCards {
		id, token, _ := b.extractNoteInfo(card)
		if id == feedID || (id != "" && id == selectedFeed.ID) {
			selectedCard = card
			break
		}
		_ = token // 保持一致性，后续如需校验可用
	}
	if selectedCard == nil {
		selectedCard = noteCards[rand.Intn(len(noteCards))]
	}

	// 点击进入笔记
	if err := selectedCard.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("点击笔记失败: %v", err)
	}
	stats.ClickCount++
	stats.ViewedNotes = append(stats.ViewedNotes, feedID)

	// 等待笔记页面加载
	time.Sleep(randomDuration(1500, 3000))
	page.MustWaitDOMStable()

	// 浏览笔记内容
	if err := b.browseNoteContent(page); err != nil {
		logrus.Warnf("浏览笔记内容出错: %v", err)
	}

	// 根据概率决定是否互动
	if rand.Intn(100) < b.config.InteractProbability {
		if err := b.interactWithNote(ctx, feedID, xsecToken, stats); err != nil {
			logrus.Warnf("与笔记互动出错: %v", err)
		}
	}

	// 关闭笔记弹窗（使用自然的方式）
	time.Sleep(randomDuration(800, 1500))
	if err := b.closeNoteModal(page); err != nil {
		logrus.Warnf("关闭笔记弹窗失败，尝试刷新页面: %v", err)
		// 降级方案：刷新页面
		page.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	}
	time.Sleep(randomDuration(1000, 2000))

	return nil
}

// getFeedsFromPage 从页面的 window.__INITIAL_STATE__ 获取笔记列表
// 这与项目中其他 MCP 功能（feeds.go, search.go）的实现方式完全一致
func (b *BrowseAction) getFeedsFromPage(page *rod.Page) ([]Feed, error) {
	// 获取 window.__INITIAL_STATE__ 并转换为 JSON 字符串
	result := page.MustEval(`() => {
		if (window.__INITIAL_STATE__) {
			return JSON.stringify(window.__INITIAL_STATE__);
		}
		return "";
	}`).String()

	if result == "" {
		return nil, fmt.Errorf("__INITIAL_STATE__ not found")
	}

	// 解析 __INITIAL_STATE__
	var state FeedsResult
	if err := json.Unmarshal([]byte(result), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal __INITIAL_STATE__: %w", err)
	}

	// 返回 feed.feeds._value
	return state.Feed.Feeds.Value, nil
}

// getVisibleNoteCards 获取当前可见的笔记卡片
func (b *BrowseAction) getVisibleNoteCards(page *rod.Page) ([]*rod.Element, error) {
	// 小红书的笔记卡片选择器
	cards, err := page.Elements("section.note-item")
	if err != nil {
		return nil, err
	}

	visibleCards := make([]*rod.Element, 0)
	for _, card := range cards {
		// 检查卡片是否在可视区域
		visible, err := card.Visible()
		if err == nil && visible {
			// 进一步检查是否在视口内
			inViewport, _ := card.Eval(`() => {
				const rect = this.getBoundingClientRect();
				return rect.top >= 0 && rect.bottom <= window.innerHeight;
			}`)
			if inViewport != nil && inViewport.Value.Bool() {
				visibleCards = append(visibleCards, card)
			}
		}
	}

	return visibleCards, nil
}

// extractNoteInfo 从笔记卡片提取信息
func (b *BrowseAction) extractNoteInfo(card *rod.Element) (feedID, xsecToken string, err error) {
	// 查找笔记链接
	linkElem, err := card.Element("a.cover")
	if err != nil {
		return "", "", err
	}

	href, err := linkElem.Attribute("href")
	if err != nil || href == nil {
		return "", "", fmt.Errorf("未找到笔记链接")
	}

	// 优先从 data 属性获取
	dataID, _ := card.Attribute("data-note-id")
	dataToken, _ := card.Attribute("data-xsec-token")

	if dataID != nil && *dataID != "" {
		feedID = *dataID
	}
	if dataToken != nil && *dataToken != "" {
		xsecToken = *dataToken
	}

	// 如果属性中没有，从 URL 解析
	// URL 格式: /explore/68e66fef0000000004023fdb?xsec_token=ABc9MCVTGMXqvxLT8H-fHb_6DodO8iEoHByoltzPex20I=&xsec_source=pc_feed
	if feedID == "" || xsecToken == "" {
		parsedFeedID, parsedToken := parseNoteURL(*href)
		if feedID == "" && parsedFeedID != "" {
			feedID = parsedFeedID
		}
		if xsecToken == "" && parsedToken != "" {
			xsecToken = parsedToken
		}
	}

	if feedID == "" || xsecToken == "" {
		return "", "", fmt.Errorf("无法提取笔记信息，链接: %s", *href)
	}

	return feedID, xsecToken, nil
}

// parseNoteURL 从笔记 URL 中解析 feedID 和 xsecToken
// URL 格式: /explore/68e66fef0000000004023fdb?xsec_token=ABc9...&xsec_source=pc_feed
func parseNoteURL(urlStr string) (feedID, xsecToken string) {
	// 解析路径部分提取 feedID
	// /explore/68e66fef0000000004023fdb?... -> 68e66fef0000000004023fdb
	if strings.Contains(urlStr, "/explore/") {
		parts := strings.Split(urlStr, "/explore/")
		if len(parts) > 1 {
			// 提取问号前的部分
			pathPart := parts[1]
			if idx := strings.Index(pathPart, "?"); idx > 0 {
				feedID = pathPart[:idx]
			} else {
				feedID = pathPart
			}
		}
	}

	// 解析查询参数提取 xsec_token
	if strings.Contains(urlStr, "xsec_token=") {
		parts := strings.Split(urlStr, "xsec_token=")
		if len(parts) > 1 {
			tokenPart := parts[1]
			// 提取 & 前的部分（如果有的话）
			if idx := strings.Index(tokenPart, "&"); idx > 0 {
				xsecToken = tokenPart[:idx]
			} else {
				xsecToken = tokenPart
			}
		}
	}

	return feedID, xsecToken
}

// browseNoteContent 浏览笔记内容
func (b *BrowseAction) browseNoteContent(page *rod.Page) error {
	logrus.Debug("浏览笔记内容")

	// 模拟阅读标题和内容
	time.Sleep(randomDuration(2000, 4000))

	// 随机滚动查看图片或视频
	scrollTimes := rand.Intn(3) + 1
	for i := 0; i < scrollTimes; i++ {
		page.Mouse.MustScroll(0, float64(rand.Intn(300)+200))
		time.Sleep(randomDuration(800, 1500))
	}

	// 智能浏览评论区（无论视频还是图文，都有概率滚动评论区）
	if rand.Intn(100) < 70 { // 70% 概率浏览评论区
		if err := b.scrollCommentArea(page); err != nil {
			logrus.Warnf("滚动评论区失败: %v", err)
		}
	}

	return nil
}

// scrollCommentArea 智能滚动评论区
// 自动检测评论区是否有评论，以及是否到达底部
func (b *BrowseAction) scrollCommentArea(page *rod.Page) error {
	logrus.Info("开始浏览评论区")

	// 先滚动到评论区位置
	page.Mouse.MustScroll(0, float64(rand.Intn(400)+300))
	time.Sleep(randomDuration(1000, 2000))

	// 检查评论区是否有评论
	hasComments, err := b.hasComments(page)
	if err != nil {
		logrus.Warnf("检查评论区失败: %v", err)
		return fmt.Errorf("检查评论区失败: %v", err)
	}

	if !hasComments {
		logrus.Info("评论区没有评论，跳过滚动")
		return nil
	}

	logrus.Info("评论区有评论，开始滚动")

	// 随机滚动2-5次，每次检测是否到底部
	maxScrolls := rand.Intn(4) + 2 // 2-5次
	for i := 0; i < maxScrolls; i++ {
		// 获取滚动前的位置
		beforeScroll, err := b.getScrollPosition(page)
		if err != nil {
			logrus.Warnf("获取滚动位置失败: %v", err)
			break
		}

		// 执行滚动
		scrollAmount := rand.Intn(400) + 300 // 300-700像素
		page.Mouse.MustScroll(0, float64(scrollAmount))
		time.Sleep(randomDuration(1200, 2500))

		// 获取滚动后的位置
		afterScroll, err := b.getScrollPosition(page)
		if err != nil {
			logrus.Warnf("获取滚动位置失败: %v", err)
			break
		}

		// 检查是否已经到底部（滚动位置几乎没有变化）
		if afterScroll-beforeScroll < 50 { // 如果滚动距离小于50像素，认为到底了
			logrus.Info("评论区已滚动到底部")

			// 有30%概率回滚一下
			if rand.Intn(100) < 30 {
				logrus.Info("回滚评论区")
				backAmount := rand.Intn(300) + 200 // 回滚200-500像素
				page.Mouse.MustScroll(0, float64(-backAmount))
				time.Sleep(randomDuration(800, 1500))
			}
			break
		}
	}

	return nil
}

// hasComments 检查评论区是否有评论
func (b *BrowseAction) hasComments(page *rod.Page) (bool, error) {
	// 小红书评论区的选择器
	commentSelectors := []string{
		".comment-item",                     // 评论项
		".comments-container .comment-item", // 评论容器内的评论项
		"[class*='comment-item']",           // 包含comment-item的class
		".comment-list .item",               // 评论列表项
	}

	for _, selector := range commentSelectors {
		elements, err := page.Elements(selector)
		if err == nil && len(elements) > 0 {
			logrus.Infof("通过选择器 %s 找到 %d 条评论", selector, len(elements))
			return true, nil
		}
	}

	// 尝试通过JavaScript检查
	hasComments := page.MustEval(`() => {
		const commentContainers = document.querySelectorAll('[class*="comment"]');
		for (let container of commentContainers) {
			if (container.innerText && container.innerText.trim().length > 10) {
				return true;
			}
		}
		return false;
	}`).Bool()

	if hasComments {
		logrus.Info("通过JavaScript检测到评论")
	} else {
		logrus.Info("未检测到任何评论")
	}

	return hasComments, nil
}

// getScrollPosition 获取当前滚动位置
func (b *BrowseAction) getScrollPosition(page *rod.Page) (float64, error) {
	position := page.MustEval(`() => {
		// 尝试获取笔记详情弹窗内的滚动位置
		const modal = document.querySelector('.note-detail-modal') || 
		              document.querySelector('.modal') || 
		              document.querySelector('[class*="detail"]');
		if (modal) {
			const scrollContainer = modal.querySelector('[class*="scroll"]') || modal;
			return scrollContainer.scrollTop || window.pageYOffset || document.documentElement.scrollTop;
		}
		return window.pageYOffset || document.documentElement.scrollTop;
	}`).Num()

	return position, nil
}

// closeNoteModal 关闭笔记弹窗（模拟真实用户的退出行为）
// 小红书的笔记详情是悬浮在推荐页上的弹窗，不是新页面
// 真实用户会使用 ESC 键或点击空白处（遮罩层）来关闭
func (b *BrowseAction) closeNoteModal(page *rod.Page) error {
	// 随机选择关闭方式，模拟真实用户习惯
	closeMethod := rand.Intn(10)

	if closeMethod < 6 { // 60% 概率使用 ESC 键
		logrus.Debug("使用 ESC 键关闭笔记")
		page.MustElement("body").MustKeyActions().Press(input.Escape).MustDo()
		time.Sleep(randomDuration(300, 600))
		return nil
	}

	// 40% 概率点击遮罩层（空白处）
	logrus.Debug("点击遮罩层关闭笔记")

	// 尝试多种可能的遮罩层选择器
	maskSelectors := []string{
		"div.close",             // 关闭按钮
		"div[class*='mask']",    // 遮罩层
		"div[class*='overlay']", // 覆盖层
		".note-detail-mask",     // 笔记详情遮罩
	}

	for _, selector := range maskSelectors {
		if mask, err := page.Element(selector); err == nil {
			if visible, _ := mask.Visible(); visible {
				if err := mask.Click(proto.InputMouseButtonLeft, 1); err == nil {
					time.Sleep(randomDuration(300, 600))
					return nil
				}
			}
		}
	}

	// 如果找不到遮罩层，使用 ESC 作为降级方案
	logrus.Debug("未找到遮罩层，使用 ESC 键作为降级方案")
	page.MustElement("body").MustKeyActions().Press(input.Escape).MustDo()
	time.Sleep(randomDuration(300, 600))

	return nil
}

// interactWithNote 与笔记互动（点赞、收藏、评论一起执行）
// 注意：这里使用专门的弹窗内互动功能，不会跳转页面
// 与现有的详情页互动功能（like_favorite.go、comment_feed.go）使用相同的选择器
// 但直接在当前弹窗内操作，保持用户体验的自然性
func (b *BrowseAction) interactWithNote(ctx context.Context, feedID, xsecToken string, stats *BrowseStats) error {
	if feedID == "" || xsecToken == "" {
		return fmt.Errorf("缺少笔记信息")
	}

	logrus.Infof("开始与笔记互动: %s", feedID)
	page := b.page.Context(ctx)

	// 在弹窗内进行点赞（不跳转页面）
	if err := b.likeInModal(page); err != nil {
		logrus.Warnf("点赞失败: %v", err)
	} else {
		stats.LikeCount++
		logrus.Debug("点赞成功")
		// 点赞后延迟 0.5-3.0 秒再进行收藏
		time.Sleep(randomDuration(500, 3000))
	}

	// 在弹窗内进行收藏（不跳转页面）
	if err := b.favoriteInModal(page); err != nil {
		logrus.Warnf("收藏失败: %v", err)
	} else {
		stats.FavoriteCount++
		logrus.Debug("收藏成功")
		// 收藏后延迟 1-3 秒再进行评论
		time.Sleep(randomDuration(1000, 3000))
	}

	// 在弹窗内进行评论（不跳转页面）
	if rand.Intn(100) < 50 { // 50% 概率评论
		// 从评论区复制一条评论作为评论内容（最保守的方法）
		comment, err := b.getRandomCommentText(page)
		if err != nil || comment == "" {
			logrus.Warnf("获取评论文本失败，跳过评论: %v", err)
		} else {
			if err := b.commentInModal(page, comment); err != nil {
				logrus.Warnf("评论失败: %v", err)
			} else {
				stats.CommentCount++
				logrus.Debugf("评论成功: %s", comment)
				time.Sleep(randomDuration(800, 1500))
			}
		}
	}

	return nil
}

// getRandomCommentText 从评论区获取一条随机评论的文本内容
func (b *BrowseAction) getRandomCommentText(page *rod.Page) (string, error) {
	logrus.Info("尝试从评论区获取评论文本")

	// 小红书评论文本的选择器
	commentTextSelectors := []string{
		".comment-item .content",           // 评论内容
		".comment-item .text",              // 评论文本
		"[class*='comment'] .content",      // 包含comment的class的内容
		"[class*='comment-item'] .content", // 评论项的内容
		".comment-list .item .content",     // 评论列表项的内容
	}

	var allComments []*rod.Element

	// 尝试所有选择器
	for _, selector := range commentTextSelectors {
		elements, err := page.Elements(selector)
		if err == nil && len(elements) > 0 {
			allComments = append(allComments, elements...)
		}
	}

	// 如果没找到，尝试通过JavaScript获取
	if len(allComments) == 0 {
		commentTexts := page.MustEval(`() => {
			const comments = [];
			const commentElements = document.querySelectorAll('[class*="comment"]');
			
			for (let elem of commentElements) {
				// 查找包含评论文本的子元素
				const textElem = elem.querySelector('.content') || 
				                 elem.querySelector('.text') || 
				                 elem.querySelector('[class*="content"]');
				
				if (textElem && textElem.innerText) {
					const text = textElem.innerText.trim();
					// 过滤掉太短或太长的评论
					if (text.length >= 5 && text.length <= 100) {
						comments.push(text);
					}
				}
			}
			
			return comments;
		}`).Arr()

		if len(commentTexts) > 0 {
			// 随机选择一条评论
			randomIndex := rand.Intn(len(commentTexts))
			commentText := commentTexts[randomIndex].String()
			logrus.Infof("从JavaScript获取到评论: %s", commentText)
			return commentText, nil
		}

		logrus.Info("评论区没有找到评论文本")
		return "", fmt.Errorf("评论区没有找到评论文本")
	}

	// 从找到的评论元素中随机选择一个
	randomComment := allComments[rand.Intn(len(allComments))]

	// 获取评论文本
	commentText, err := randomComment.Text()
	if err != nil {
		return "", fmt.Errorf("获取评论文本失败: %v", err)
	}

	commentText = strings.TrimSpace(commentText)

	// 过滤掉太短或太长的评论
	if len(commentText) < 5 {
		logrus.Info("评论文本太短，重新获取")
		return "", fmt.Errorf("评论文本太短")
	}
	if len(commentText) > 100 {
		// 截取前100个字符
		runes := []rune(commentText)
		if len(runes) > 100 {
			commentText = string(runes[:100])
		}
	}

	logrus.Infof("从评论区获取到评论: %s", commentText)
	return commentText, nil
}

// likeInModal 在弹窗内进行点赞操作
func (b *BrowseAction) likeInModal(page *rod.Page) error {
	// 使用与详情页相同的选择器，但不跳转页面
	// 选择器来自 like_favorite.go 中的 SelectorLikeButton
	selector := ".interact-container .left .like-lottie"

	// 尝试多个可能的点赞按钮选择器（弹窗内可能略有不同）
	selectors := []string{
		selector, // 详情页选择器
		".note-detail-modal .interact-container .left .like-lottie", // 弹窗内选择器
		".modal .interact-container .left .like-lottie",             // 通用弹窗选择器
		".like-lottie",    // 简化选择器
		"[class*='like']", // 包含like的class
	}

	for _, sel := range selectors {
		if elem, err := page.Element(sel); err == nil {
			if visible, _ := elem.Visible(); visible {
				logrus.Debugf("使用选择器点赞: %s", sel)
				elem.MustClick()
				return nil
			}
		}
	}

	return fmt.Errorf("未找到点赞按钮")
}

// favoriteInModal 在弹窗内进行收藏操作
func (b *BrowseAction) favoriteInModal(page *rod.Page) error {
	// 使用与详情页相同的选择器，但不跳转页面
	// 选择器来自 like_favorite.go 中的 SelectorCollectButton
	selector := ".interact-container .left .reds-icon.collect-icon"

	// 尝试多个可能的收藏按钮选择器（弹窗内可能略有不同）
	selectors := []string{
		selector, // 详情页选择器
		".note-detail-modal .interact-container .left .reds-icon.collect-icon", // 弹窗内选择器
		".modal .interact-container .left .reds-icon.collect-icon",             // 通用弹窗选择器
		".collect-icon",      // 简化选择器
		"[class*='collect']", // 包含collect的class
	}

	for _, sel := range selectors {
		if elem, err := page.Element(sel); err == nil {
			if visible, _ := elem.Visible(); visible {
				logrus.Debugf("使用选择器收藏: %s", sel)
				elem.MustClick()
				return nil
			}
		}
	}

	return fmt.Errorf("未找到收藏按钮")
}

// commentInModal 在弹窗内进行评论操作
func (b *BrowseAction) commentInModal(page *rod.Page, content string) error {
	// 尝试多个可能的评论输入框选择器（弹窗内可能与详情页不同）
	inputSelectors := []string{
		"div.input-box div.content-edit span",                    // 详情页选择器（来自comment_feed.go）
		".note-detail-modal div.input-box div.content-edit span", // 弹窗内选择器
		".modal div.input-box div.content-edit span",             // 通用弹窗选择器
		".comment-input span",                                    // 简化选择器
		"[placeholder*='评论']",                                    // 通过placeholder查找
	}

	// 先点击评论输入框
	var inputElem *rod.Element
	for _, sel := range inputSelectors {
		if elem, err := page.Element(sel); err == nil {
			if visible, _ := elem.Visible(); visible {
				logrus.Debugf("找到评论输入框: %s", sel)
				elem.MustClick()
				inputElem = elem
				break
			}
		}
	}

	if inputElem == nil {
		return fmt.Errorf("未找到评论输入框")
	}

	time.Sleep(randomDuration(300, 600))

	// 查找实际的文本输入元素
	textInputSelectors := []string{
		"div.input-box div.content-edit p.content-input",                    // 详情页选择器
		".note-detail-modal div.input-box div.content-edit p.content-input", // 弹窗内选择器
		".modal div.input-box div.content-edit p.content-input",             // 通用弹窗选择器
		".content-input",     // 简化选择器
		"textarea",           // 通用textarea
		"input[type='text']", // 通用文本输入
	}

	var textElem *rod.Element
	for _, sel := range textInputSelectors {
		if elem, err := page.Element(sel); err == nil {
			if visible, _ := elem.Visible(); visible {
				logrus.Debugf("找到文本输入框: %s", sel)
				textElem = elem
				break
			}
		}
	}

	if textElem == nil {
		return fmt.Errorf("未找到文本输入框")
	}

	// 输入评论内容
	textElem.MustInput(content)
	time.Sleep(randomDuration(500, 1000))

	// 查找并点击提交按钮
	submitSelectors := []string{
		"div.bottom button.submit",                    // 详情页选择器
		".note-detail-modal div.bottom button.submit", // 弹窗内选择器
		".modal div.bottom button.submit",             // 通用弹窗选择器
		"button.submit",                               // 简化选择器
		"button[type='submit']",                       // 通用提交按钮
		"button:contains('发布')",                       // 通过文本查找
	}

	for _, sel := range submitSelectors {
		if elem, err := page.Element(sel); err == nil {
			if visible, _ := elem.Visible(); visible {
				logrus.Debugf("找到提交按钮: %s", sel)
				elem.MustClick()
				return nil
			}
		}
	}

	return fmt.Errorf("未找到提交按钮")
}

// randomDuration 生成随机时长（毫秒）
func randomDuration(min, max int) time.Duration {
	return time.Duration(rand.Intn(max-min+1)+min) * time.Millisecond
}

// randomRefreshInterval 生成随机的页面刷新间隔（2-5分钟）
// 模拟真实用户习惯：每隔几分钟刷新推荐页以获取新内容
func randomRefreshInterval() time.Duration {
	// 随机生成 2-5 分钟的间隔
	minutes := rand.Intn(4) + 2 // 2, 3, 4, 5
	// 再加上一些随机秒数，让时间更自然 (0-59秒)
	seconds := rand.Intn(60)
	totalSeconds := minutes*60 + seconds
	return time.Duration(totalSeconds) * time.Second
}
