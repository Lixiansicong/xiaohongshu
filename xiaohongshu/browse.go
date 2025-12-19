package xiaohongshu

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

func isRodSessionNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Session with given id not found") || strings.Contains(msg, "-32001")
}

const (
	DefaultBrowseDurationMinutes = 60
	DefaultMinScrollsPerRound    = 1
	DefaultMaxScrollsPerRound    = 3
	DefaultClickProbability      = 100
	DefaultInteractProbability   = 60
	DefaultLikeOnlyProbability   = 30
)

// BrowseConfig 浏览配置
type BrowseConfig struct {
	// 浏览时长（分钟）
	Duration int `json:"duration"`
	InstanceID string `json:"instance_id,omitempty"`
	// 滚动次数范围
	MinScrolls int `json:"min_scrolls"`
	MaxScrolls int `json:"max_scrolls"`
	// 点击笔记的概率 (0-100)
	ClickProbability int `json:"click_probability"`
	// 在笔记中互动的概率 (0-100) - 兼容保留（当前 follow-based 模式不使用）
	InteractProbability int `json:"interact_probability"`
	// 触发互动时，仅点赞不收藏的概率 (0-100) - 兼容保留（当前 follow-based 模式不使用）
	LikeOnlyProbability int `json:"like_only_probability,omitempty"`
	// 是否启用评论（nil=默认启用, true=启用, false=禁用）- 兼容保留（当前 follow-based 模式不使用）
	EnableComment *bool `json:"enable_comment,omitempty"`
	// 评论内容列表（可选，如果为空则自动从评论区获取）- 兼容保留（当前 follow-based 模式不使用）
	Comments []string `json:"comments,omitempty"`
}

// BrowseAction 浏览动作
type BrowseAction struct {
	page   *rod.Page
	config BrowseConfig
	ten    *tenTimesManager
}

type tenTimesManager struct {
	instanceID string
	forceAll   bool
	targets    map[string]struct{}
	completed  map[string]struct{}
	state      map[string]*tenNoteState
	active     *tenTask
	activeNotVisible int
	queue      []*tenTask
	enqueued   map[string]struct{}

	timesPath     string
	completedPath string
	statePath     string
}

type tenNoteState struct {
	Author    string    `json:"author"`
	FeedID    string    `json:"feed_id"`
	Title     string    `json:"title,omitempty"`
	Count     int       `json:"count"`
	Completed bool      `json:"completed"`
	UpdatedAt time.Time `json:"updated_at"`
}

type tenTask struct {
	Author    string
	FeedID    string
	XsecToken string
	Title     string
}

// BrowseStats 浏览统计
type BrowseStats struct {
	Duration      time.Duration       `json:"duration"`
	ScrollCount   int                 `json:"scroll_count"`
	ClickCount    int                 `json:"click_count"`
	LikeCount     int                 `json:"like_count"`
	FavoriteCount int                 `json:"favorite_count"`
	CommentCount  int                 `json:"comment_count"`
	ViewedNotes   []string            `json:"viewed_notes"`
	viewedSet     map[string]struct{} `json:"-"`
}

// NewBrowseAction 创建浏览动作
func NewBrowseAction(page *rod.Page, config BrowseConfig) *BrowseAction {
	// 设置默认值（仅当调用方未显式配置时才使用默认值）
	if config.Duration == 0 {
		config.Duration = DefaultBrowseDurationMinutes // 默认浏览时长
	}
	if config.MinScrolls == 0 {
		config.MinScrolls = DefaultMinScrollsPerRound // 每轮最小滚动次数
	}
	if config.MaxScrolls == 0 {
		config.MaxScrolls = DefaultMaxScrollsPerRound // 每轮最大滚动次数
	}
	if config.ClickProbability == 0 {
		config.ClickProbability = DefaultClickProbability // 点击概率
	}
	if config.InteractProbability == 0 {
		config.InteractProbability = DefaultInteractProbability // 互动概率（兼容保留）
	}
	if config.LikeOnlyProbability <= 0 || config.LikeOnlyProbability > 100 {
		config.LikeOnlyProbability = DefaultLikeOnlyProbability // 仅点赞概率（兼容保留）
	}

	tenMgr, err := newTenTimesManager(config.InstanceID)
	if err != nil {
		logrus.WithError(err).WithField("instance", config.InstanceID).Warn("ten-times 初始化失败，将回退正常模式")
		tenMgr = nil
	}

	return &BrowseAction{page: page, config: config, ten: tenMgr}
}

// StartBrowse 开始浏览推荐页
func (b *BrowseAction) StartBrowse(ctx context.Context) (*BrowseStats, error) {
	logrus.Info("开始模拟人类浏览小红书推荐页")

	stats := &BrowseStats{
		ViewedNotes: make([]string, 0),
		viewedSet:   make(map[string]struct{}),
	}
	startTime := time.Now()

	// 导航到推荐页
	// 为首次导航创建一个独立的超时 context
	navCtx, navCancel := context.WithTimeout(ctx, 60*time.Second)
	defer navCancel()
	navPage := b.page.Context(navCtx)
	navPage.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	waitForExploreReady(navPage, 6*time.Second) // 等待页面完全加载
	pause(300, 700)

	// 浏览时长
	browseUntil := time.Now().Add(time.Duration(b.config.Duration) * time.Minute)

	// 初始化已浏览笔记集合（支持从历史切片恢复）
	if len(stats.ViewedNotes) > 0 {
		for _, id := range stats.ViewedNotes {
			stats.viewedSet[id] = struct{}{}
		}
	}

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
				if b.ten != nil && (b.ten.active != nil || len(b.ten.queue) > 0) {
					delaySeconds := rand.Intn(31) + 30 // 30-60s
					nextRefreshTime = time.Now().Add(time.Duration(delaySeconds) * time.Second)
					logrus.WithFields(logrus.Fields{
						"instance": b.config.InstanceID,
						"delay_s":  delaySeconds,
					}).Info("ten-times 任务进行中，延后刷新推荐页")
				} else {
				logrus.Info("刷新推荐页以获取新内容")
				// 为刷新操作创建一个独立的超时 context
				refreshCtx, refreshCancel := context.WithTimeout(ctx, 60*time.Second)
				refreshPage := b.page.Context(refreshCtx)
				refreshPage.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
				refreshCancel()                                 // 导航完成后立即取消 context
				waitForExploreReady(refreshPage, 6*time.Second) // 刷新后等待加载
				pause(300, 700)

				// 设置下一次刷新时间：2-5分钟后
				nextRefreshTime = time.Now().Add(randomRefreshInterval())
				remainingMinutes := time.Until(browseUntil).Minutes()
				if remainingMinutes > 0 {
					logrus.Infof("页面已刷新，计划在 %.1f 分钟后再次刷新（剩余浏览时间: %.1f 分钟）",
						time.Until(nextRefreshTime).Minutes(), remainingMinutes)
				}
				}
			}

			// 执行一轮浏览
			if err := b.browseRound(ctx, stats); err != nil {
				logrus.Warnf("浏览出错: %v", err)
				pause(1200, 2500)
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
		if b.ten != nil {
			processed, err := b.processTenTimesIfNeeded(ctx, stats)
			if err != nil {
				logrus.WithError(err).WithField("instance", b.config.InstanceID).Warn("ten-times 处理异常，继续正常浏览")
			} else if processed {
				pause(700, 1100)
				continue
			}
		}

		// 模拟人类滚动（包含回滚机制）
		if err := b.humanLikeScrollWithBacktrack(page); err != nil {
			return err
		}
		stats.ScrollCount++

		// 停留时间：中位数 1-2s（减少停留时间，使浏览更自然）
		pause(1000, 2000)

		if b.ten != nil {
			processed, err := b.processTenTimesIfNeeded(ctx, stats)
			if err != nil {
				logrus.WithError(err).WithField("instance", b.config.InstanceID).Warn("ten-times 处理异常，继续正常浏览")
			} else if processed {
				pause(700, 1100)
				continue
			}
		}

		// 根据概率决定是否点击笔记
		if rand.Intn(100) < b.config.ClickProbability {
			if err := b.clickAndViewNote(ctx, stats); err != nil {
				logrus.Warnf("点击笔记出错: %v", err)
				// 返回推荐页 - 创建独立的超时 context
				recoverCtx, recoverCancel := context.WithTimeout(ctx, 60*time.Second)
				recoverPage := page.Context(recoverCtx)
				recoverPage.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
				recoverCancel()
				waitForExploreReady(recoverPage, 6*time.Second)
				pause(300, 700)
			}
		}
	}

	return nil
}

func (b *BrowseAction) processTenTimesIfNeeded(ctx context.Context, stats *BrowseStats) (bool, error) {
	if b.ten == nil {
		return false, nil
	}

	page := b.page.Context(ctx)

	if b.ten.active != nil {
		didInteract, done, err := b.executeTenInteractionStep(ctx, stats, b.ten.active)
		if err != nil {
			return true, err
		}
		if done {
			b.ten.active = nil
			b.ten.activeNotVisible = 0
		}
		if !didInteract && !done {
			b.ten.activeNotVisible++
			if b.ten.activeNotVisible >= 6 {
				logrus.WithFields(logrus.Fields{"instance": b.config.InstanceID, "feed_id": b.ten.active.FeedID}).Warn("ten-times active 目标长时间不可见，回退正常浏览")
				b.ten.active = nil
				b.ten.activeNotVisible = 0
				return false, nil
			}
		} else if didInteract {
			b.ten.activeNotVisible = 0
		}
		return true, nil
	}

	feeds, _ := b.getFeedsFromPage(page)
	feedMap := make(map[string]Feed, len(feeds))
	for _, f := range feeds {
		if f.ID != "" {
			feedMap[f.ID] = f
		}
	}

	cards, err := b.getVisibleNoteCards(page)
	if err != nil {
		return false, err
	}

	for _, card := range cards {
		feedID, xsecToken, err := b.extractNoteInfo(card)
		if err != nil || feedID == "" {
			continue
		}
		st := b.ten.ensureState(feedID)
		if st.Completed || st.Count >= 10 {
			continue
		}
		if _, ok := b.ten.enqueued[feedID]; ok {
			continue
		}

		authorName, nameErr := b.extractAuthorNameFromCard(card)
		authorSource := "dom"
		if nameErr != nil {
			logrus.WithError(nameErr).WithFields(logrus.Fields{"feed_id": feedID, "instance": b.config.InstanceID}).Debug("提取博主名失败")
			authorSource = "initial_state"
		}
		if strings.TrimSpace(authorName) == "" {
			authorSource = "initial_state"
		}

		if authorName == "" {
			if f, ok := feedMap[feedID]; ok {
				authorName = normalizeNickname(f.NoteCard.User)
			}
		}
		authorName = strings.TrimSpace(authorName)
		matchedTarget := b.ten.forceAll
		if !matchedTarget && authorName != "" {
			_, matchedTarget = b.ten.targets[authorName]
		}
		logrus.WithFields(logrus.Fields{
			"instance":       b.config.InstanceID,
			"feed_id":        feedID,
			"author":         authorName,
			"author_source":  authorSource,
			"matched_target": matchedTarget,
		}).Info("ten-times 扫描卡片作者")
		if authorName == "" {
			continue
		}

		if !matchedTarget {
			continue
		}
		if _, ok := b.ten.completed[completedKey(authorName, feedID)]; ok {
			st.Completed = true
			if st.Count < 10 {
				st.Count = 10
			}
			continue
		}

		title := ""
		if t, tErr := b.extractTitleFromCard(card); tErr == nil {
			title = strings.TrimSpace(t)
		}
		if f, ok := feedMap[feedID]; ok {
			if title == "" {
				title = strings.TrimSpace(f.NoteCard.DisplayTitle)
			}
			if title == "" {
				title = strings.TrimSpace(f.NoteCard.Type)
			}
			if authorName == "" {
				authorName = normalizeNickname(f.NoteCard.User)
			}
		}

		b.ten.queue = append(b.ten.queue, &tenTask{Author: authorName, FeedID: feedID, XsecToken: xsecToken, Title: title})
		b.ten.enqueued[feedID] = struct{}{}
		logrus.WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": authorName, "feed_id": feedID}).Info("ten-times 入队目标笔记")
	}

	if len(b.ten.queue) == 0 {
		return false, nil
	}

	task := b.ten.queue[0]
	b.ten.queue = b.ten.queue[1:]
	delete(b.ten.enqueued, task.FeedID)
	b.ten.active = task
	b.ten.activeNotVisible = 0

	didInteract, done, err := b.executeTenInteractionStep(ctx, stats, task)
	if err != nil {
		return true, err
	}
	if done {
		b.ten.active = nil
		b.ten.activeNotVisible = 0
	}
	if didInteract {
		b.ten.activeNotVisible = 0
	}
	return true, nil
}

func (b *BrowseAction) executeTenInteractionStep(ctx context.Context, stats *BrowseStats, task *tenTask) (didInteract bool, done bool, err error) {
	if b.ten == nil {
		return false, false, nil
	}
	if task == nil || strings.TrimSpace(task.FeedID) == "" {
		return false, false, nil
	}

	select {
	case <-ctx.Done():
		return false, false, ctx.Err()
	default:
	}

	st := b.ten.ensureState(task.FeedID)
	st.Author = strings.TrimSpace(task.Author)
	st.FeedID = strings.TrimSpace(task.FeedID)
	if st.Title == "" {
		st.Title = task.Title
	}

	if _, ok := b.ten.completed[completedKey(st.Author, st.FeedID)]; ok {
		st.Completed = true
		if st.Count < 10 {
			st.Count = 10
		}
	}

	if st.Completed || st.Count >= 10 {
		return false, true, nil
	}

	page := b.page.Context(ctx)

	card, findErr := b.findVisibleCardByFeedID(page, task.FeedID)
	if findErr != nil || card == nil {
		logrus.WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID, "count": st.Count}).Info("ten-times 当前目标不可见，继续滚动等待")
		return false, false, nil
	}

	var isRealVideo bool
	var openErr error
	for attempt := 0; attempt < 2; attempt++ {
		isRealVideo, openErr = b.openNoteFromCard(page, card, task.FeedID, stats)
		if openErr == nil {
			break
		}
		pause(600, 1100)
	}
	if openErr != nil {
		logrus.WithError(openErr).WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID}).Warn("ten-times 打开笔记失败，将稍后重试")
		return false, false, nil
	}

	logrus.WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID, "round": st.Count + 1}).Info("ten-times 开始互动")

	if browseErr := b.browseNoteToBottom(page, task.FeedID, isRealVideo); browseErr != nil {
		logrus.WithError(browseErr).WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID}).Warn("ten-times 浏览到底失败")
	}

	if st.Count == 0 {
		logrus.WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID}).Info("ten-times 首次互动：尝试点赞+收藏")
		b.forceLikeAndFavoriteInModal(page, task.FeedID, stats)
	}

	if err := b.closeNoteModal(page); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID}).Warn("ten-times 关闭弹窗失败")
		fallbackCtx, fallbackCancel := context.WithTimeout(ctx, 60*time.Second)
		fallbackPage := page.Context(fallbackCtx)
		fallbackPage.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
		fallbackCancel()
		waitForExploreReady(fallbackPage, 6*time.Second)
	} else {
		waitForNoteModalClosed(page, 3*time.Second)
	}
	pause(900, 1300)

	st.Count++
	st.UpdatedAt = time.Now()
	if st.Count >= 10 {
		st.Completed = true
	}
	if err := b.ten.saveState(); err != nil {
		logrus.WithError(err).WithField("instance", b.config.InstanceID).Warn("ten-times 保存状态失败")
	}

	logrus.WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID, "count": st.Count}).Info("ten-times 单次互动完成")

	if st.Completed {
		if err := b.ten.appendCompleted(task.Author, task.FeedID, st.Title); err != nil {
			logrus.WithError(err).WithField("instance", b.config.InstanceID).Warn("ten-times 写入 completed 失败")
		}
		logrus.WithFields(logrus.Fields{"instance": b.config.InstanceID, "author": task.Author, "feed_id": task.FeedID}).Info("ten-times 十次互动完成")
		return true, true, nil
	}

	return true, false, nil
}

func (b *BrowseAction) findVisibleCardByFeedID(page *rod.Page, feedID string) (*rod.Element, error) {
	cards, err := b.getVisibleNoteCards(page)
	if err != nil {
		return nil, err
	}
	for _, card := range cards {
		id, _, _ := b.extractNoteInfo(card)
		if id == feedID {
			return card, nil
		}
	}
	return nil, fmt.Errorf("not visible")
}

func (b *BrowseAction) extractAuthorNameFromCard(card *rod.Element) (string, error) {
	if card == nil {
		return "", fmt.Errorf("nil card")
	}

	selectors := []struct {
		anchorSel string
		nameSel   string
	}{
		{anchorSel: "a.author", nameSel: "span.name"},
		{anchorSel: "a.author", nameSel: ".name"},
		{anchorSel: "a[class*='author']", nameSel: "span.name"},
		{anchorSel: "a[class*='author']", nameSel: "span[class*='name']"},
		{anchorSel: "a[class*='author']", nameSel: "[class*='name']"},
	}

	for _, sel := range selectors {
		anchor, err := card.Element(sel.anchorSel)
		if err != nil || anchor == nil {
			continue
		}
		nameEl, err := anchor.Element(sel.nameSel)
		if err == nil && nameEl != nil {
			text, textErr := nameEl.Text()
			if textErr == nil {
				text = strings.TrimSpace(text)
				if isReasonableAuthorName(text) {
					return text, nil
				}
			}
		}

		text := ""
		_ = rod.Try(func() {
			text = anchor.MustEval(`() => (this.textContent || "").trim()`).String()
		})
		text = strings.TrimSpace(text)
		if isReasonableAuthorName(text) {
			return text, nil
		}
	}

	return "", fmt.Errorf("author not found")
}

func (b *BrowseAction) extractTitleFromCard(card *rod.Element) (string, error) {
	if card == nil {
		return "", fmt.Errorf("nil card")
	}

	selectors := []string{
		"a.title span",
		"a.title",
		".title span",
		".title",
		"a[class*='title'] span",
		"a[class*='title']",
	}

	for _, sel := range selectors {
		el, err := card.Element(sel)
		if err != nil || el == nil {
			continue
		}
		text, textErr := el.Text()
		if textErr != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if isReasonableNoteTitle(text) {
			return text, nil
		}
	}

	return "", fmt.Errorf("title not found")
}

func isReasonableNoteTitle(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len([]rune(s)) > 120 {
		return false
	}
	return true
}

func isReasonableAuthorName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len([]rune(s)) > 40 {
		return false
	}
	return true
}

func normalizeNickname(u User) string {
	if strings.TrimSpace(u.Nickname) != "" {
		return strings.TrimSpace(u.Nickname)
	}
	if strings.TrimSpace(u.NickName) != "" {
		return strings.TrimSpace(u.NickName)
	}
	return ""
}

func (b *BrowseAction) browseNoteToBottom(page *rod.Page, feedID string, isRealVideo bool) error {
	_ = feedID
	if isRealVideo {
		pause(4000, 9000)
	} else {
		pause(1200, 2400)
	}

	maxRounds := 18
	stable := 0
	for i := 0; i < maxRounds; i++ {
		before, after, err := scrollModalOnce(page)
		if err != nil {
			break
		}
		if after-before < 40 {
			stable++
		} else {
			stable = 0
		}
		pause(450, 900)
		if stable >= 2 {
			break
		}
	}

	if err := b.scrollCommentArea(page); err != nil {
		logrus.WithError(err).Debug("ten-times 评论区滚动失败")
	}
	return nil
}

func scrollModalOnce(page *rod.Page) (before int, after int, err error) {
	res := page.MustEval(`() => {
		const modal = document.querySelector('.note-detail-modal') ||
		             document.querySelector('.modal') ||
		             document.querySelector('[class*="detail"]');
		if (!modal) return { ok: false, before: 0, after: 0 };
		const candidates = [];
		candidates.push(modal);
		const scroller = modal.querySelector('.note-scroller') || modal.querySelector('[class*="scroller"]') || modal.querySelector('[class*="scroll"]');
		if (scroller) candidates.push(scroller);

		let el = null;
		for (const c of candidates) {
			if (!c) continue;
			const ch = c.clientHeight || 0;
			const sh = c.scrollHeight || 0;
			if (sh > ch + 10) { el = c; break; }
		}
		if (!el) return { ok: false, before: 0, after: 0 };
		const before = el.scrollTop || 0;
		const step = 600 + Math.floor(Math.random() * 350);
		el.scrollTop = Math.min(before + step, (el.scrollHeight || 0));
		const after = el.scrollTop || 0;
		return { ok: true, before, after };
	}`)

	ok := res.Get("ok").Bool()
	if !ok {
		return 0, 0, fmt.Errorf("no scrollable")
	}
	return res.Get("before").Int(), res.Get("after").Int(), nil
}

func newTenTimesManager(instanceID string) (*tenTimesManager, error) {
	timesPath, completedPath, statePath := resolveTenFiles(instanceID)

	forceAll := false
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("TEN_TIMES_FORCE_ALL"))); v != "" {
		switch v {
		case "1", "true", "yes", "on":
			forceAll = true
		}
	}

	var targets map[string]struct{}
	var err error
	if !forceAll {
		targets, err = readLinesAsSet(timesPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		if len(targets) == 0 {
			return nil, nil
		}
	} else {
		targets = make(map[string]struct{})
		logrus.WithField("instance", instanceID).Info("TEN_TIMES_FORCE_ALL 已开启：将对所有可见笔记执行十次互动")
	}

	// 每次运行时清空 ten_completed，确保本次运行输出的是“本次完成记录”。
	if err := clearTenCompletedFile(completedPath); err != nil {
		logrus.WithError(err).WithField("instance", instanceID).Warn("清空 ten_completed 失败，将继续运行但 completed 可能残留")
	}
	completedSet := make(map[string]struct{})

	state, err := loadTenState(statePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logrus.WithError(err).WithField("instance", instanceID).Warn("读取 ten_state 失败，将从空状态开始")
		}
		state = make(map[string]*tenNoteState)
	}

	return &tenTimesManager{
		instanceID:    instanceID,
		forceAll:      forceAll,
		targets:       targets,
		completed:     completedSet,
		state:         state,
		queue:         make([]*tenTask, 0, 8),
		enqueued:      make(map[string]struct{}),
		timesPath:     timesPath,
		completedPath: completedPath,
		statePath:     statePath,
	}, nil
}

func clearTenCompletedFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte{}, 0644)
}

func (m *tenTimesManager) ensureState(feedID string) *tenNoteState {
	if m.state == nil {
		m.state = make(map[string]*tenNoteState)
	}
	st, ok := m.state[feedID]
	if !ok || st == nil {
		st = &tenNoteState{FeedID: feedID}
		m.state[feedID] = st
	}
	if _, ok := m.completed[completedKey(st.Author, feedID)]; ok {
		st.Completed = true
		if st.Count < 10 {
			st.Count = 10
		}
	}
	return st
}

func (m *tenTimesManager) saveState() error {
	if m == nil {
		return nil
	}
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(m.statePath, data, 0644)
}

func (m *tenTimesManager) appendCompleted(author, feedID, title string) error {
	if m == nil {
		return nil
	}
	author = strings.TrimSpace(author)
	feedID = strings.TrimSpace(feedID)
	key := completedKey(author, feedID)
	if _, ok := m.completed[key]; ok {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(m.completedPath), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(m.completedPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	identifier := strings.TrimSpace(title)
	if identifier == "" {
		identifier = "(无标题)"
	}
	line := fmt.Sprintf("%s,%s\n", author, identifier)
	if _, err := f.WriteString(line); err != nil {
		return err
	}
	m.completed[key] = struct{}{}
	return nil
}

func completedKey(author, feedID string) string {
	return strings.TrimSpace(author) + "," + strings.TrimSpace(feedID)
}

func resolveTenFiles(instanceID string) (timesPath, completedPath, statePath string) {
	suffix := instanceNumberSuffix(instanceID)

	tryPaths := func(name string) []string {
		paths := make([]string, 0, 3)
		if wd, err := os.Getwd(); err == nil {
			paths = append(paths, filepath.Join(wd, name))
		}
		if exe, err := os.Executable(); err == nil {
			paths = append(paths, filepath.Join(filepath.Dir(exe), name))
		}
		paths = append(paths, name)
		return paths
	}

	resolveExistingOrFirst := func(name string) string {
		for _, p := range tryPaths(name) {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		if wd, err := os.Getwd(); err == nil {
			return filepath.Join(wd, name)
		}
		return name
	}

	if suffix != "" {
		timesPath = resolveExistingOrFirst(fmt.Sprintf("ten_times%s.txt", suffix))
		completedPath = resolveExistingOrFirst(fmt.Sprintf("ten_completed%s.txt", suffix))
		statePath = resolveExistingOrFirst(fmt.Sprintf("ten_state%s.json", suffix))
		return
	}

	timesPath = resolveExistingOrFirst("ten_times.txt")
	completedPath = resolveExistingOrFirst("ten_completed.txt")
	statePath = resolveExistingOrFirst("ten_state.json")
	return
}

func instanceNumberSuffix(instanceID string) string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return ""
	}
	re := regexp.MustCompile(`(\d+)$`)
	m := re.FindStringSubmatch(instanceID)
	if len(m) < 2 {
		return ""
	}
	_, err := strconv.Atoi(m[1])
	if err != nil {
		return ""
	}
	return m[1]
}

func readLinesAsSet(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	set := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		set[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

func readCompletedSet(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	set := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		set[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

func loadTenState(path string) (map[string]*tenNoteState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return make(map[string]*tenNoteState), nil
	}

	state := make(map[string]*tenNoteState)
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (b *BrowseAction) getNoteModalRoot(page *rod.Page) (*rod.Element, error) {
	selectors := []string{
		".note-detail-modal",
		".modal",
		"[class*='detail']",
	}

	for i := 0; i < 6; i++ {
		for _, sel := range selectors {
			var el *rod.Element
			err := rod.Try(func() {
				el = page.Timeout(500 * time.Millisecond).MustElement(sel)
			})
			if err != nil || el == nil {
				continue
			}
			if visible, _ := el.Visible(); visible {
				return el, nil
			}
		}
		time.Sleep(120 * time.Millisecond)
	}

	return nil, fmt.Errorf("未找到笔记详情弹窗容器")
}

func normalizeModalSelector(sel string) string {
	sel = strings.TrimPrefix(sel, ".note-detail-modal ")
	sel = strings.TrimPrefix(sel, ".modal ")
	return sel
}

func (b *BrowseAction) getInteractStateFromInitialState(page *rod.Page, feedID string) (liked bool, collected bool, err error) {
	// 注意：__INITIAL_STATE__ 可能存在循环引用，不能直接 JSON.stringify(window.__INITIAL_STATE__)
	// 这里仅提取我们需要的 liked/collected 字段。
	var result string
	evalErr := rod.Try(func() {
		result = page.Timeout(2*time.Second).MustEval(`(feedID) => {
			try {
				const s = window.__INITIAL_STATE__;
				if (!s || !s.note || !s.note.noteDetailMap) return "";
				const detail = s.note.noteDetailMap[feedID];
				if (!detail || !detail.note || !detail.note.interactInfo) return "";
				const ii = detail.note.interactInfo;
				return JSON.stringify({ liked: !!ii.liked, collected: !!ii.collected });
			} catch (e) {
				return "";
			}
		}`, feedID).String()
	})
	if evalErr != nil {
		return false, false, fmt.Errorf("读取 __INITIAL_STATE__ 失败: %w", evalErr)
	}
	if result == "" {
		return false, false, fmt.Errorf("__INITIAL_STATE__ not found or missing interactInfo")
	}

	var state struct {
		Liked     bool `json:"liked"`
		Collected bool `json:"collected"`
	}
	if err := json.Unmarshal([]byte(result), &state); err != nil {
		return false, false, err
	}

	return state.Liked, state.Collected, nil
}

type FollowStatus int

const (
	FollowStatusUnknown FollowStatus = iota
	FollowStatusNotFollowed
	FollowStatusFollowed
	FollowStatusMutual
)

func (b *BrowseAction) getFollowStatusInModal(page *rod.Page) (FollowStatus, error) {
	modal, err := b.getNoteModalRoot(page)
	if err != nil {
		// 允许 modal 识别失败，后续走 JS 全局扫描兜底
		modal = nil
	}

	selectors := []string{
		"button.follow-button",
		".follow-button",
		"button[class*='follow']",
	}

	var btn *rod.Element
	if modal != nil {
		for i := 0; i < 6 && btn == nil; i++ {
			for _, sel := range selectors {
				var el *rod.Element
				err := rod.Try(func() {
					el = modal.Timeout(500 * time.Millisecond).MustElement(sel)
				})
				if err != nil || el == nil {
					continue
				}
				if visible, _ := el.Visible(); !visible {
					continue
				}
				btn = el
				break
			}
			if btn == nil {
				time.Sleep(120 * time.Millisecond)
			}
		}
	}

	// 快速路径：如果找到了按钮，优先用按钮 text/class 判定
	if btn != nil {
		text, _ := btn.Text()
		text = strings.TrimSpace(text)
		cls, _ := btn.Attribute("class")
		classStr := ""
		if cls != nil {
			classStr = *cls
		}
		logrus.Infof("关注按钮命中(选择器)：text=%s class=%s", text, classStr)

		if strings.Contains(text, "互相关注") {
			return FollowStatusMutual, nil
		}
		if strings.Contains(text, "已关注") {
			return FollowStatusFollowed, nil
		}
		if strings.Contains(text, "关注") {
			return FollowStatusNotFollowed, nil
		}
		if strings.Contains(classStr, "outlined") {
			return FollowStatusFollowed, nil
		}
		if strings.Contains(classStr, "primary") {
			return FollowStatusNotFollowed, nil
		}
	}

	// 兜底：用 JS 扫描页面内可见 button，按文本“关注/已关注/互相关注”识别
	resultJSON := page.Timeout(2 * time.Second).MustEval(`() => {
		const normalizeText = (t) => (t || '').replace(/\s+/g, ' ').trim();
		const roots = [];
		const modal = document.querySelector('.note-detail-modal') || document.querySelector('.modal');
		if (modal) roots.push(modal);
		roots.push(document);

		const isVisible = (el) => {
			if (!el) return false;
			const rect = el.getBoundingClientRect();
			if (!rect || rect.width <= 0 || rect.height <= 0) return false;
			const style = window.getComputedStyle(el);
			if (!style) return false;
			if (style.visibility === 'hidden' || style.display === 'none' || parseFloat(style.opacity || '1') === 0) return false;
			return true;
		};

		const candidates = [];
		const seen = new Set();
		for (const root of roots) {
			const btns = root.querySelectorAll('button');
			for (const b of btns) {
				if (!isVisible(b)) continue;
				const text = normalizeText(b.innerText);
				const cls = (b.className || '').toString();
				if (!text && !cls) continue;
				if (!(text.includes('关注') || cls.includes('follow'))) continue;
				const key = text + '|' + cls;
				if (seen.has(key)) continue;
				seen.add(key);
				candidates.push({ text, class: cls });
			}
		}

		const pick = (pred) => candidates.find(c => pred(c));
		const mutual = pick(c => c.text.includes('互相关注'));
		const followed = pick(c => c.text.includes('已关注'));
		const notFollowed = pick(c => c.text === '关注' || c.text.includes('关注'));

		const chosen = mutual || followed || notFollowed || null;
		return JSON.stringify({
			found: !!chosen,
			text: chosen ? chosen.text : '',
			class: chosen ? chosen.class : '',
			candidates
		});
	}`).String()

	var scan struct {
		Found      bool   `json:"found"`
		Text       string `json:"text"`
		Class      string `json:"class"`
		Candidates []struct {
			Text  string `json:"text"`
			Class string `json:"class"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &scan); err != nil {
		return FollowStatusUnknown, fmt.Errorf("关注状态 JS 扫描结果解析失败: %w", err)
	}

	if !scan.Found {
		return FollowStatusUnknown, fmt.Errorf("未找到关注按钮")
	}

	logrus.Infof("关注按钮命中(JS)：text=%s class=%s", scan.Text, scan.Class)
	if strings.Contains(scan.Text, "互相关注") {
		return FollowStatusMutual, nil
	}
	if strings.Contains(scan.Text, "已关注") {
		return FollowStatusFollowed, nil
	}
	if strings.Contains(scan.Text, "关注") {
		return FollowStatusNotFollowed, nil
	}
	if strings.Contains(scan.Class, "outlined") {
		return FollowStatusFollowed, nil
	}
	if strings.Contains(scan.Class, "primary") {
		return FollowStatusNotFollowed, nil
	}

	return FollowStatusUnknown, fmt.Errorf("关注按钮状态未知: text=%s", scan.Text)
}

func isPressedLikeOrCollect(elem *rod.Element) bool {
	if elem == nil {
		return false
	}

	res, err := elem.Eval(`() => {
		try {
			const el = this;
			const aria = el.getAttribute('aria-pressed');
			if (aria === 'true') return true;

			const host = el.closest('.like-wrapper,.collect-wrapper') || el;
			const cls = (host.className || '').toString();
			if (/(^|\s)(active|selected|liked|collected|is-active)(\s|$)/i.test(cls)) return true;
			if (/(^|\s)(like-active|collect-active)(\s|$)/i.test(cls)) {
				const useEl = host.querySelector('use');
				const href = useEl ? (useEl.getAttribute('href') || useEl.getAttribute('xlink:href') || '') : '';
				// 小红书已点赞/已收藏的标记：#liked / #collected
				if (href === '#liked' || href === '#collected') return true;
				if (href && href !== '#like' && href !== '#collect') return true;
			}

			const useEl = host.querySelector('use');
			const href = useEl ? (useEl.getAttribute('href') || useEl.getAttribute('xlink:href') || '') : '';
			if (href) {
				if (href === '#liked' || href === '#collected') return true;
				if (href === '#like' || href === '#collect') return false;
				const lower = href.toLowerCase();
				if (/(like|collect)/.test(lower) && /(fill|filled|active|selected|liked|collected|on)/.test(lower)) return true;
			}
			const pressedHost = el.closest('[aria-pressed]');
			if (pressedHost && pressedHost.getAttribute('aria-pressed') === 'true') return true;
			const activeHost = el.closest('.active,.selected,.is-active,.liked,.collected');
			if (activeHost) return true;
			return false;
		} catch (e) {
			return false;
		}
	}`)
	if err != nil || res == nil {
		return false
	}
	return res.Value.Bool()
}

func (b *BrowseAction) getLikeCollectStateFromDOM(modal *rod.Element) (liked bool, collected bool, err error) {
	if modal == nil {
		return false, false, fmt.Errorf("modal is nil")
	}

	findVisible := func(selectors []string) (*rod.Element, error) {
		for _, sel := range selectors {
			var el *rod.Element
			err := rod.Try(func() {
				el = modal.Timeout(500 * time.Millisecond).MustElement(sel)
			})
			if err != nil || el == nil {
				continue
			}
			if visible, _ := el.Visible(); !visible {
				continue
			}
			return el, nil
		}
		return nil, fmt.Errorf("not found")
	}

	likeSelectors := []string{
		".interact-container .left .like-wrapper",
		".left .like-wrapper",
		".like-wrapper",
		".interact-container .left .like-lottie",
		".like-lottie",
		".reds-icon.like-icon",
		"[class*='like']",
	}
	collectSelectors := []string{
		"#note-page-collect-board-guide",
		".interact-container .left .collect-wrapper",
		".left .collect-wrapper",
		".collect-wrapper",
		".interact-container .left .reds-icon.collect-icon",
		".collect-icon",
		".reds-icon.collect-icon",
		"[class*='collect']",
	}

	likeEl, _ := findVisible(likeSelectors)
	collectEl, _ := findVisible(collectSelectors)
	if likeEl == nil && collectEl == nil {
		return false, false, fmt.Errorf("未找到点赞/收藏按钮")
	}

	if likeEl != nil {
		liked = isPressedLikeOrCollect(likeEl)
	}
	if collectEl != nil {
		collected = isPressedLikeOrCollect(collectEl)
	}

	return liked, collected, nil
}

func (b *BrowseAction) forceLikeAndFavoriteInModal(page *rod.Page, feedID string, stats *BrowseStats) {
	modal, modalErr := b.getNoteModalRoot(page)
	if modalErr != nil {
		logrus.Warnf("未找到弹窗容器，跳过点赞/收藏: %v", modalErr)
		return
	}
	likedDOM, collectedDOM, domErr := b.getLikeCollectStateFromDOM(modal)
	if domErr != nil {
		logrus.Warnf("互动状态(DOM)读取失败，将直接尝试点赞/收藏（仍会在 likeInModal/favoriteInModal 内做防撤销判断）: %v", domErr)
	}
	logrus.Infof("互动状态(DOM)：liked=%v collected=%v", likedDOM, collectedDOM)

	if !likedDOM {
		logrus.Info("准备点赞：当前未点赞")
		if err := b.likeInModal(page); err != nil {
			logrus.Warnf("点赞失败: %v", err)
		} else {
			logrus.Info("点赞完成")
			stats.LikeCount++
			pause(300, 900)
		}
	} else {
		logrus.Info("跳过点赞：已点赞（防撤销）")
	}

	// 点赞后 UI 可能会重绘，收藏前再刷新一次状态
	likedDOM, collectedDOM, _ = b.getLikeCollectStateFromDOM(modal)
	if !collectedDOM {
		logrus.Info("准备收藏：当前未收藏")
		if err := b.favoriteInModal(page); err != nil {
			logrus.Warnf("收藏失败: %v", err)
		} else {
			logrus.Info("收藏完成")
			stats.FavoriteCount++
			pause(400, 1100)
		}
	} else {
		logrus.Info("跳过收藏：已收藏（防撤销）")
	}
}

// humanLikeScrollWithBacktrack 模拟人类滚动（优化版，包含回滚机制）
// 基于真实用户行为参数：
// - 滚动段时长：0.6-2.5s
// - 插入短暂停：0.2-1.2s
// - 回滚概率：7-18%
func (b *BrowseAction) humanLikeScrollWithBacktrack(page *rod.Page) error {
	// 随机选择滚动方式
	scrollType := rand.Intn(3)

	// 主滚动动作 - 减少滚动距离，使浏览更自然
	scrollAmount := rand.Intn(500) + 400 // 400-900像素（减少滚动幅度）

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
			time.Sleep(randomDuration(150, 700))
		}

	case 1:
		// 使用键盘方向键
		times := rand.Intn(3) + 1 // 1-3次（减少滚动次数）
		for i := 0; i < times; i++ {
			page.MustElement("body").MustKeyActions().Press(input.ArrowDown).MustDo()
			// 插入短暂停：0.2-1.2s
			time.Sleep(randomDuration(150, 700))
		}

	case 2:
		// 使用 JavaScript 滚动
		page.MustEval(fmt.Sprintf(`() => window.scrollBy({top: %d, behavior: 'smooth'})`, scrollAmount))
		// 等待滚动动画完成
		time.Sleep(randomDuration(450, 800))
	}

	// 滚动段时长：0.6-2.5s
	time.Sleep(randomDuration(400, 1400))

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
				time.Sleep(randomDuration(150, 400))
			}
		} else {
			// 鼠标或JS回滚
			page.Mouse.MustScroll(0, float64(backtrackAmount))
		}

		// 回滚后的停顿
		time.Sleep(randomDuration(350, 1000))
	}

	return nil
}

// clickAndViewNote 点击并浏览笔记
func (b *BrowseAction) clickAndViewNote(ctx context.Context, stats *BrowseStats) error {
	page := b.page.Context(ctx)

	logrus.Debug("========== 开始点击笔记流程 ==========")

	// 步骤1&2: 获取笔记列表并选择要浏览的笔记
	selectedFeed, err := b.selectFeedForClick(page, stats)
	if err != nil {
		return err
	}
	feedID := selectedFeed.ID
	xsecToken := selectedFeed.XsecToken

	if feedID == "" || xsecToken == "" {
		logrus.Error("笔记信息不完整")
		return fmt.Errorf("笔记信息不完整")
	}

	// 步骤3: 记录选中笔记的基础信息（暂不在此阶段区分真视频/图文）
	logrus.Debugf("步骤3: 选中笔记 ID=%s, type=%s", feedID, selectedFeed.NoteCard.Type)

	// 步骤4&5: 在可见卡片中查找匹配的笔记
	selectedCard, err := b.selectCardForFeed(page, selectedFeed)
	if err != nil {
		return err
	}

	// 步骤6&7: 点击进入笔记并等待页面加载（基于加载耗时检测是否为真视频）
	var isRealVideo bool
	for attempt := 0; attempt < 2; attempt++ {
		isRealVideo, err = b.openNoteFromCard(page, selectedCard, feedID, stats)
		if err == nil {
			break
		}
		if attempt == 0 && isRodSessionNotFoundErr(err) {
			logrus.Warnf("检测到 Rod session 失效，尝试刷新并重试点击: %v", err)
			recoverCtx, recoverCancel := context.WithTimeout(ctx, 60*time.Second)
			recoverPage := page.Context(recoverCtx)
			recoverPage.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
			recoverCancel()
			page = recoverPage
			waitForExploreReady(recoverPage, 6*time.Second)
			pause(300, 700)

			selectedCard, err = b.selectCardForFeed(page, selectedFeed)
			if err != nil {
				return err
			}
			continue
		}
		return err
	}
	if err != nil {
		return err
	}

	logrus.Info("步骤7后: 检测作者关注状态")
	followStatus, followErr := b.getFollowStatusInModal(page)
	if followErr != nil {
		logrus.Warnf("关注状态检测失败，将按未关注处理并退出: %v", followErr)
		followStatus = FollowStatusNotFollowed
	}


	// false && (followStatus != FollowStatusFollowed && followStatus != FollowStatusMutual)临时禁用未关注直接退出逻辑
	// followStatus != FollowStatusFollowed && followStatus != FollowStatusMutual正常逻辑

	if followStatus != FollowStatusFollowed && followStatus != FollowStatusMutual {
		logrus.Info("检测到未关注，停留 2-4 秒后直接退出（不执行互动）")
		pause(2000, 4000)
		logrus.Info("步骤10: 关闭笔记弹窗")
		pause(300, 800)
		if err := b.closeNoteModal(page); err != nil {
			logrus.Warnf("关闭笔记弹窗失败，尝试刷新页面: %v", err)
			fallbackCtx, fallbackCancel := context.WithTimeout(ctx, 60*time.Second)
			fallbackPage := page.Context(fallbackCtx)
			fallbackPage.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
			fallbackCancel()
		} else {
			logrus.Info("笔记弹窗关闭成功")
		}
		waitForNoteModalClosed(page, 3*time.Second)
		pause(200, 1000)
		logrus.Info("========== 笔记点击流程完成 ==========")
		return nil
	}

	if isRealVideo {
		logrus.Info("步骤7判定当前笔记为真视频，将采用视频浏览策略")
	} else {
		logrus.Info("步骤7判定当前笔记为图文/动图，将采用图文浏览策略")
	}

	// 步骤8: 浏览笔记内容
	logrus.Info("步骤8: 浏览笔记内容")
	if err := b.browseNoteContent(page, feedID, isRealVideo); err != nil {
		logrus.Warnf("浏览笔记内容出错: %v", err)
	} else {
		logrus.Info("笔记内容浏览完成")
	}

	// 步骤9: 检查是否需要互动
	logrus.Info("检测到已关注/互关：执行 100% 点赞+收藏")
	b.forceLikeAndFavoriteInModal(page, feedID, stats)
	logrus.Info("笔记互动完成")

	// 步骤10: 关闭笔记弹窗（使用自然的方式）
	logrus.Info("步骤10: 关闭笔记弹窗")
	pause(300, 800)
	if err := b.closeNoteModal(page); err != nil {
		logrus.Warnf("关闭笔记弹窗失败，尝试刷新页面: %v", err)
		// 降级方案：刷新页面 - 创建独立的超时 context
		fallbackCtx, fallbackCancel := context.WithTimeout(ctx, 60*time.Second)
		fallbackPage := page.Context(fallbackCtx)
		fallbackPage.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
		fallbackCancel()
	} else {
		logrus.Info("笔记弹窗关闭成功")
	}
	waitForNoteModalClosed(page, 3*time.Second)
	pause(200, 1000)

	logrus.Info("========== 笔记点击流程完成 ==========")
	return nil
}

// selectFeedForClick 从页面数据和已浏览记录中选择要浏览的笔记
func (b *BrowseAction) selectFeedForClick(page *rod.Page, stats *BrowseStats) (Feed, error) {
	// 从 window.__INITIAL_STATE__ 获取笔记列表（与其他 MCP 功能保持一致）
	logrus.Debug("步骤1: 从页面获取笔记列表")
	feeds, err := b.getFeedsFromPage(page)
	if err != nil || len(feeds) == 0 {
		logrus.Errorf("获取笔记列表失败: %v", err)
		return Feed{}, fmt.Errorf("未找到笔记列表: %v", err)
	}
	logrus.Debugf("成功获取 %d 条笔记", len(feeds))

	// 基于已浏览去重，优先选择未浏览的笔记
	logrus.Debug("步骤2: 筛选未浏览的笔记")

	viewed := stats.viewedSet
	if viewed == nil {
		viewed = make(map[string]struct{}, len(stats.ViewedNotes))
		for _, id := range stats.ViewedNotes {
			viewed[id] = struct{}{}
		}
		stats.viewedSet = viewed
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
		logrus.Infof("选择未浏览笔记，剩余 %d 条未浏览", len(unviewed))
	} else {
		// 回退：都看过则仍随机一个，但尽量通过滚动引入新内容
		selectedFeed = feeds[rand.Intn(len(feeds))]
		logrus.Info("所有笔记都已浏览，随机选择一条")
	}

	return selectedFeed, nil
}

// selectCardForFeed 在可见卡片中查找与选中笔记对应的卡片，找不到则随机选择
func (b *BrowseAction) selectCardForFeed(page *rod.Page, selectedFeed Feed) (*rod.Element, error) {
	feedID := selectedFeed.ID

	// 获取对应的 DOM 元素并点击
	logrus.Debug("步骤4: 查找可见的笔记卡片")
	noteCards, err := b.getVisibleNoteCards(page)
	if err != nil || len(noteCards) == 0 {
		logrus.Errorf("查找笔记卡片失败: %v", err)
		return nil, fmt.Errorf("未找到可见笔记卡片")
	}
	logrus.Infof("找到 %d 个可见笔记卡片", len(noteCards))

	// 在可见卡片中寻找对应 feedID 的卡片；找不到则回退随机
	logrus.Debug("步骤5: 匹配笔记卡片")
	var selectedCard *rod.Element
	for _, card := range noteCards {
		id, token, _ := b.extractNoteInfo(card)
		if id == feedID || (id != "" && id == selectedFeed.ID) {
			selectedCard = card
			logrus.Infof("找到匹配的笔记卡片: ID=%s", id)
			break
		}
		_ = token // 保持一致性，后续如需校验可用
	}
	if selectedCard == nil {
		selectedCard = noteCards[rand.Intn(len(noteCards))]
		logrus.Info("未找到匹配的笔记卡片，随机选择一个")
	}

	return selectedCard, nil
}

// openNoteFromCard 点击笔记卡片并等待笔记页面加载（同时基于加载耗时检测是否为真视频）
func (b *BrowseAction) openNoteFromCard(page *rod.Page, selectedCard *rod.Element, feedID string, stats *BrowseStats) (bool, error) {
	// 点击进入笔记
	logrus.Info("步骤6: 点击笔记卡片")
	if err := selectedCard.Click(proto.InputMouseButtonLeft, 1); err != nil {
		logrus.Errorf("点击笔记失败: %v", err)
		return false, fmt.Errorf("点击笔记失败: %v", err)
	}
	logrus.Info("笔记点击成功")
	stats.ClickCount++
	stats.ViewedNotes = append(stats.ViewedNotes, feedID)
	if stats.viewedSet != nil {
		stats.viewedSet[feedID] = struct{}{}
	}

	// 步骤7: 等待笔记页面加载，并基于加载耗时检测是否为真视频
	logrus.Info("步骤7: 等待笔记页面加载")
	stepStart := time.Now()

	// 先进行一个短暂的固定等待，模拟首屏渲染
	pause(400, 900)

	maxWait := 4 * time.Second
	logrus.Debug("开始等待DOM稳定（最多 4 秒用于区分视频/图文）")
	waitStart := time.Now()

	var isRealVideo bool

	err := rod.Try(func() {
		page.Timeout(maxWait).MustWaitDOMStable()
	})
	elapsedWait := time.Since(waitStart)

	if err != nil {
		// 在 4 秒内仍未完成 => 判定为真视频
		isRealVideo = true
		logrus.Infof("笔记页面在 %v 内未完全加载，判定为真视频笔记", elapsedWait)
	} else {
		// 4 秒内加载完成 => 视为图文/动图
		isRealVideo = false
		logrus.Info("笔记页面加载完成")
		logrus.Debugf("笔记页面加载耗时: %v", elapsedWait)
	}

	logrus.Debugf("步骤7 总等待时间（含初始等待）: %v", time.Since(stepStart))

	return isRealVideo, nil
}

// getFeedsFromPage 从页面的 window.__INITIAL_STATE__ 获取笔记列表
// 这与项目中其他 MCP 功能（feeds.go, search.go）的实现方式完全一致
func (b *BrowseAction) getFeedsFromPage(page *rod.Page) ([]Feed, error) {
	logrus.Debug("### 开始从页面获取笔记数据")

	// 只提取我们需要的部分，避免循环引用问题
	// 直接访问 feed.feeds._value，而不是序列化整个 __INITIAL_STATE__
	result := page.MustEval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.feed &&
		    window.__INITIAL_STATE__.feed.feeds &&
		    window.__INITIAL_STATE__.feed.feeds._value) {
			// 只提取我们需要的字段，避免循环引用
			const feeds = window.__INITIAL_STATE__.feed.feeds._value;
			return JSON.stringify(feeds.map(feed => ({
				id: feed.id,
				type: feed.type,
				xsecToken: feed.xsecToken,
				noteCard: feed.noteCard
			})));
		}
		return "";
	}`).String()

	if result == "" {
		logrus.Error("### 未找到笔记数据")
		return nil, fmt.Errorf("__INITIAL_STATE__ not found")
	}

	logrus.Debugf("### 获取到的数据长度: %d bytes", len(result))

	// 解析笔记列表
	var feeds []Feed
	if err := json.Unmarshal([]byte(result), &feeds); err != nil {
		logrus.Errorf("### 解析笔记数据失败: %v", err)
		return nil, fmt.Errorf("failed to unmarshal feeds: %w", err)
	}

	logrus.Infof("### 成功解析 %d 条笔记数据", len(feeds))
	return feeds, nil
}

// getVisibleNoteCards 获取当前可见的笔记卡片
func (b *BrowseAction) getVisibleNoteCards(page *rod.Page) ([]*rod.Element, error) {
	logrus.Debug("*** 开始查找笔记卡片")

	// 小红书的笔记卡片选择器
	cards, err := page.Elements("section.note-item")
	if err != nil {
		logrus.Errorf("*** 查找笔记卡片失败: %v", err)
		return nil, err
	}
	logrus.Infof("*** 找到 %d 个笔记卡片（包括不可见的）", len(cards))

	visibleCards := make([]*rod.Element, 0)
	for i, card := range cards {
		// 检查卡片是否在可视区域
		visible, err := card.Visible()
		if err == nil && visible {
			// 进一步检查是否在视口内
			inViewport, _ := card.Eval(`() => {
				const rect = this.getBoundingClientRect();
				return rect.top >= 0 && rect.bottom <= window.innerHeight;
			}`)
			if inViewport != nil && inViewport.Value.Bool() {
				logrus.Debugf("*** 卡片 %d 在视口内", i)
				visibleCards = append(visibleCards, card)
			} else {
				logrus.Debugf("*** 卡片 %d 不在视口内", i)
			}
		} else {
			logrus.Debugf("*** 卡片 %d 不可见或检查失败: %v", i, err)
		}
	}

	logrus.Infof("*** 找到 %d 个可见的笔记卡片", len(visibleCards))
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

// browseNoteContent 浏览笔记内容（视频与图文策略不同）
func (b *BrowseAction) browseNoteContent(page *rod.Page, feedID string, isRealVideo bool) error {
	logrus.Debug(">>> 开始浏览笔记内容")

	// 模拟先看标题和文案
	logrus.Debug(">>> 阅读标题和内容")
	pause(1500, 3000)

	if isRealVideo {
		// 真视频：不做图片滚动，采用“完整观看/部分观看”策略
		if rand.Intn(100) < 10 {
			logrus.Info(">>> 当前笔记为真视频，本次选择完整观看视频，等待视频播放完成")
			_ = rod.Try(func() {
				page.Timeout(2 * time.Second).MustWaitDOMStable()
			})
			watchDuration := randomDuration(12000, 30000)
			logrus.Infof(">>> 视频完整观看结束，继续停留约 %.1f 秒", watchDuration.Seconds())
			time.Sleep(watchDuration)
		} else {
			// 90% 概率部分观看：停留 4-9 秒后进入后续操作
			watchDuration := randomDuration(4000, 9000)
			logrus.Infof(">>> 当前笔记为真视频，本次选择部分观看，停留约 %d 秒后进入后续操作", int(watchDuration.Seconds()))
			time.Sleep(watchDuration)
		}
	} else {
		// 图文/动图：先浏览图片，再轻微滚动正文
		logrus.Info(">>> 当前笔记为图文/动图，准备浏览图片和正文内容")

		imageCount, err := b.getNoteImageCount(page, feedID)
		if err != nil {
			logrus.Warnf(">>> 获取图片数量失败（可忽略）: %v", err)
		} else if imageCount > 0 {
			logrus.Infof(">>> 检测到该笔记共有 %d 张图片", imageCount)

			// 80% 概率执行图片轮播浏览
			if rand.Intn(100) < 80 {
				logrus.Info(">>> 开始浏览轮播图片")
				if err := b.browseNoteImages(page, imageCount); err != nil {
					logrus.Warnf(">>> 浏览轮播图片失败: %v", err)
				}
			}
		} else {
			logrus.Debug(">>> 未检测到图片列表，跳过图片浏览")
		}

		// 最后保留 1-2 次正文滚动，模拟浏览文案
		scrollTimes := rand.Intn(2) + 1
		logrus.Debugf(">>> 准备滚动 %d 次查看正文内容", scrollTimes)
		for i := 0; i < scrollTimes; i++ {
			page.Mouse.MustScroll(0, float64(rand.Intn(250)+150))
			pause(500, 1100)
		}
		logrus.Debug(">>> 正文内容滚动完成")
	}

	// 智能浏览评论区（无论视频还是图文，都有概率滚动评论区）
	if rand.Intn(100) < 70 { // 70% 概率浏览评论区
		logrus.Debug(">>> 准备浏览评论区")
		if err := b.scrollCommentArea(page); err != nil {
			logrus.Warnf(">>> 滚动评论区失败: %v", err)
		} else {
			logrus.Debug(">>> 评论区浏览完成")
		}
	} else {
		logrus.Debug(">>> 跳过评论区浏览")
	}

	logrus.Info(">>> 笔记内容浏览完毕")
	return nil
}

// browseNoteImages 基于轮播组件结构模拟依次查看图片，通过点击轮播箭头切换
func (b *BrowseAction) browseNoteImages(page *rod.Page, imageCount int) error {
	if imageCount <= 1 {
		logrus.Debug(">>> 仅检测到 1 张图片，停留浏览")
		pause(1000, 2200)
		return nil
	}

	logrus.Debugf(">>> 开始浏览轮播图片，总数=%d", imageCount)

	// 绝大多数情况下完整浏览所有图片，少数情况只看前几张，避免过于规律
	total := imageCount
	if imageCount > 2 && rand.Intn(100) < 30 {
		total = rand.Intn(imageCount-1) + 1
	}

	var sliderArea *rod.Element
	_ = rod.Try(func() {
		sliderArea = page.Timeout(2 * time.Second).MustElement(".slider-container .note-slider")
	})
	if sliderArea == nil {
		_ = rod.Try(func() {
			sliderArea = page.Timeout(2 * time.Second).MustElement(".note-slider")
		})
	}
	if sliderArea == nil {
		_ = rod.Try(func() {
			sliderArea = page.Timeout(2 * time.Second).MustElement(".slider-container")
		})
	}
	if sliderArea != nil {
		_ = rod.Try(func() {
			sliderArea.Timeout(1 * time.Second).MustHover()
		})
	}

	// 尝试找到轮播图右侧的箭头按钮（显式超时，避免 Element 默认等待太久导致卡死）
	var rightArrow *rod.Element
	var err error
	err = rod.Try(func() {
		rightArrow = page.Timeout(2 * time.Second).MustElement(".slider-container .arrow-controller.right")
	})
	if err != nil || rightArrow == nil {
		_ = rod.Try(func() {
			rightArrow = page.Timeout(2 * time.Second).MustElement(".arrow-controller.right")
		})
	}
	if rightArrow == nil {
		logrus.Warn(">>> 未找到轮播图右侧箭头（已快速超时），改为停留等待浏览")
		pause(1000, 2200)
		return nil
	}

	// 当前图片先停留一段时间
	pause(700, 1600)

	for i := 1; i < total; i++ {
		// 模拟用户在当前图片上停留一段时间再切下一张
		pause(700, 1800)

		if sliderArea != nil {
			if err := rod.Try(func() {
				sliderArea.Timeout(1 * time.Second).MustHover()
			}); err == nil {
				scrollAmount := rand.Intn(180) + 120
				page.Mouse.MustScroll(0, float64(scrollAmount))
				continue
			}
		}

		if err := rightArrow.Timeout(2*time.Second).Click(proto.InputMouseButtonLeft, 1); err != nil {
			logrus.Warnf(">>> 点击轮播图下一张失败，提前结束轮播浏览: %v", err)
			break
		}
	}

	return nil
}

// getNoteImageCount 仅通过笔记详情中的轮播组件 DOM 结构获取当前笔记的图片张数
func (b *BrowseAction) getNoteImageCount(page *rod.Page, _ string) (int, error) {
	count := page.MustEval(`() => {
		try {
			const sliderRoot = document.querySelector('.slider-container .note-slider') ||
					document.querySelector('.note-slider');
			if (!sliderRoot) return 0;

			// 1. 优先从 fraction 文本中解析总张数，例如 "5/6" => 6
			const fractionEl = sliderRoot.parentElement && sliderRoot.parentElement.querySelector('.fraction');
			if (fractionEl && fractionEl.textContent) {
				const text = fractionEl.textContent.trim();
				const m = text.match(/^\s*\d+\s*\/\s*(\d+)\s*$/);
				if (m) {
					const total = parseInt(m[1], 10);
					if (!Number.isNaN(total) && total > 0) {
						return total;
					}
				}
			}

			// 2. 退回到基于 .swiper-slide 结构的统计方式
			const wrapper = sliderRoot.querySelector('.swiper-wrapper');
			if (!wrapper) return 0;

			const slides = Array.from(wrapper.querySelectorAll('.swiper-slide'));
			if (!slides.length) return 0;

			const indexSet = new Set();
			for (const slide of slides) {
				// 跳过 swiper 复制出来的 slide
				if (slide.classList.contains('swiper-slide-duplicate')) continue;
				const idx = slide.getAttribute('data-index') || slide.getAttribute('data-swiper-slide-index');
				if (idx != null) indexSet.add(idx);
			}
			if (indexSet.size > 0) {
				return indexSet.size;
			}

			// 如果没有 index，就直接按“非 duplicate slide 数量”统计
			const filtered = slides.filter(s => !s.classList.contains('swiper-slide-duplicate'));
			if (filtered.length > 0) {
				return filtered.length;
			}

			return 0;
		} catch (e) {
			return 0;
		}
	}`).Int()

	if count == 0 {
		return 0, fmt.Errorf("未在 DOM 中发现轮播图组件或有效的图片")
	}
	return count, nil
}

// isCommentAreaVisible 检查评论区是否在视口中可见
func (b *BrowseAction) isCommentAreaVisible(page *rod.Page) (bool, error) {
	isVisible := page.MustEval(`() => {
		// 尝试多种评论区选择器
		const commentSelectors = [
			'.comment-container',
			'[class*="comment"]',
			'.comments-section',
			'.note-comments'
		];

		for (let selector of commentSelectors) {
			const commentArea = document.querySelector(selector);
			if (commentArea) {
				// 获取元素的位置信息
				const rect = commentArea.getBoundingClientRect();

				// 检查元素是否在视口中
				// 考虑到小红书的弹窗结构，我们需要检查相对于弹窗容器的位置
				const modal = document.querySelector('.note-detail-modal') ||
				              document.querySelector('.modal') ||
				              document.querySelector('[class*="detail"]');

				if (modal) {
					const modalRect = modal.getBoundingClientRect();
					// 检查评论区是否在弹窗的可见区域内
					const isVisibleInModal = rect.top >= modalRect.top &&
					                       rect.bottom <= modalRect.bottom &&
					                       rect.height > 0;

					if (isVisibleInModal) {
						return true;
					}
				} else {
					// 如果找不到弹窗，使用全局视口检查
					const isVisible = rect.top >= 0 &&
					                 rect.bottom <= window.innerHeight &&
					                 rect.height > 0;

					if (isVisible) {
						return true;
					}
				}
			}
		}

		return false;
	}`).Bool()

	return isVisible, nil
}

// scrollCommentArea 智能滚动评论区
// 自动检测评论区是否有评论，以及是否到达底部
func (b *BrowseAction) scrollCommentArea(page *rod.Page) error {
	logrus.Debug("开始浏览评论区")

	// 检查评论区是否有评论
	hasComments, err := b.hasComments(page)
	if err != nil {
		logrus.Warnf("检查评论区失败: %v", err)
		return fmt.Errorf("检查评论区失败: %v", err)
	}

	if !hasComments {
		logrus.Debug("评论区没有评论，跳过滚动")
		return nil
	}

	// 检查评论区是否在视口中可见
	commentVisible, err := b.isCommentAreaVisible(page)
	if err != nil {
		logrus.Warnf("检查评论区可见性失败: %v", err)
		// 如果无法检查可见性，继续执行原有逻辑
		commentVisible = false
	}

	// 如果评论区不可见，先滚动到评论区位置
	if !commentVisible {
		b.scrollCommentAreaIntoView(page)
	} else {
		logrus.Debug("评论区已在视口中可见")
	}

	logrus.Debug("评论区有评论，开始滚动")

	b.performCommentScrolling(page)

	return nil
}

// scrollCommentAreaIntoView 将评论区滚动到视口中，包含无法精确定位时的降级逻辑
func (b *BrowseAction) scrollCommentAreaIntoView(page *rod.Page) {
	logrus.Info("评论区不在视口中，先滚动到评论区位置")

	// 尝试找到评论区并滚动到其位置
	scrolledToComment := page.MustEval(`() => {
		// 尝试多种评论区选择器
		const commentSelectors = [
			'.comment-container',
			'[class*="comment"]',
			'.comments-section',
			'.note-comments'
		];

		for (let selector of commentSelectors) {
			const commentArea = document.querySelector(selector);
			if (commentArea) {
				// 滚动到评论区位置
				commentArea.scrollIntoView({ behavior: 'smooth', block: 'start' });
				return true;
			}
		}

		// 如果找不到评论区，尝试向下滚动一定距离
		window.scrollBy({ top: 400, behavior: 'smooth' });
		return false;
	}`).Bool()

	// 等待滚动完成
	pause(800, 1500)

	if !scrolledToComment {
		logrus.Warn("无法精确定位评论区，使用通用滚动")
		// 降级方案：通用滚动
		page.Mouse.MustScroll(0, float64(rand.Intn(400)+300))
		pause(700, 1500)
	}
}

// performCommentScrolling 在评论区内执行多次滚动并检测是否到底部
func (b *BrowseAction) performCommentScrolling(page *rod.Page) {
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
		pause(700, 1500)

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
				pause(500, 1000)
			}
			break
		}
	}
}

// 评论区相关选择器集中定义，便于统一维护
var (
	// commentItemSelectors 用于检测评论区是否有评论（元素层面）
	commentItemSelectors = []string{
		".comment-item",                     // 评论项
		".comments-container .comment-item", // 评论容器内的评论项
		"[class*='comment-item']",           // 包含comment-item的class
		".comment-list .item",               // 评论列表项
	}
)

// hasComments 检查评论区是否有评论
func (b *BrowseAction) hasComments(page *rod.Page) (bool, error) {
	// 使用统一定义的评论项选择器集合
	for _, selector := range commentItemSelectors {
		elements, err := page.Elements(selector)
		if err == nil && len(elements) > 0 {
			logrus.Debugf("通过选择器 %s 找到 %d 条评论", selector, len(elements))
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
	logrus.Info("<<< 准备关闭笔记弹窗")

	// 随机选择关闭方式，模拟真实用户习惯
	closeMethod := rand.Intn(10)

	if closeMethod < 6 { // 60% 概率使用 ESC 键
		logrus.Info("<<< 使用 ESC 键关闭笔记")
		page.MustElement("body").MustKeyActions().Press(input.Escape).MustDo()
		time.Sleep(randomDuration(300, 600))
		logrus.Info("<<< ESC 键已按下")
		return nil
	}

	// 40% 概率点击遮罩层（空白处）
	logrus.Info("<<< 尝试点击遮罩层关闭笔记")

	// 尝试多种可能的遮罩层选择器
	maskSelectors := []string{
		"div.close",             // 关闭按钮
		"div[class*='mask']",    // 遮罩层
		"div[class*='overlay']", // 覆盖层
		".note-detail-mask",     // 笔记详情遮罩
	}

	for _, selector := range maskSelectors {
		logrus.Debugf("<<< 尝试选择器: %s", selector)
		if mask, err := page.Element(selector); err == nil {
			if visible, _ := mask.Visible(); visible {
				logrus.Infof("<<< 找到可见的遮罩层: %s", selector)
				if err := mask.Click(proto.InputMouseButtonLeft, 1); err == nil {
					time.Sleep(randomDuration(300, 600))
					logrus.Info("<<< 遮罩层点击成功")
					return nil
				} else {
					logrus.Warnf("<<< 遮罩层点击失败: %v", err)
				}
			}
		}
	}

	// 如果找不到遮罩层，使用 ESC 作为降级方案
	logrus.Info("<<< 未找到遮罩层，使用 ESC 键作为降级方案")
	page.MustElement("body").MustKeyActions().Press(input.Escape).MustDo()
	time.Sleep(randomDuration(300, 600))
	logrus.Info("<<< ESC 键已按下（降级方案）")

	return nil
}

// likeInModal 在弹窗内进行点赞操作
func (b *BrowseAction) likeInModal(page *rod.Page) error {
	modal, err := b.getNoteModalRoot(page)
	if err != nil {
		return err
	}

	// 使用与详情页相同的选择器，但不跳转页面
	// 选择器来自 like_favorite.go 中的 SelectorLikeButton
	selector := ".interact-container .left .like-lottie"

	// 尝试多个可能的点赞按钮选择器（弹窗内可能略有不同）
	selectors := []string{
		selector,          // 详情页选择器
		".like-lottie",    // 简化选择器
		"[class*='like']", // 包含like的class（已限定在弹窗容器内）
	}

	for _, sel := range selectors {
		if elem, err := modal.Element(sel); err == nil {
			if visible, _ := elem.Visible(); visible {
				if isPressedLikeOrCollect(elem) {
					return nil
				}
				logrus.Debugf("使用选择器点赞: %s", sel)
				if err := elem.Timeout(2*time.Second).Click(proto.InputMouseButtonLeft, 1); err == nil {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("未找到点赞按钮")
}

// favoriteInModal 在弹窗内进行收藏操作
func (b *BrowseAction) favoriteInModal(page *rod.Page) error {
	modal, err := b.getNoteModalRoot(page)
	if err != nil {
		return err
	}

	// 使用与详情页相同的选择器，但不跳转页面
	// 选择器来自 like_favorite.go 中的 SelectorCollectButton
	selector := ".interact-container .left .reds-icon.collect-icon"

	// 尝试多个可能的收藏按钮选择器（弹窗内可能略有不同）
	selectors := []string{
		selector,             // 详情页选择器
		".collect-icon",      // 简化选择器
		"[class*='collect']", // 包含collect的class（已限定在弹窗容器内）
	}

	for _, sel := range selectors {
		if elem, err := modal.Element(sel); err == nil {
			if visible, _ := elem.Visible(); visible {
				if isPressedLikeOrCollect(elem) {
					return nil
				}
				logrus.Debugf("使用选择器收藏: %s", sel)
				if err := elem.Timeout(2*time.Second).Click(proto.InputMouseButtonLeft, 1); err == nil {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("未找到收藏按钮")
}

// randomDuration 生成随机时长（毫秒）
func randomDuration(min, max int) time.Duration {
	return time.Duration(rand.Intn(max-min+1)+min) * time.Millisecond
}

func pause(minMs, maxMs int) {
	time.Sleep(randomDuration(minMs, maxMs))
}

func waitForExploreReady(page *rod.Page, timeout time.Duration) {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		ok := false
		_ = rod.Try(func() {
			ok = page.Timeout(800 * time.Millisecond).MustEval(`() => document.querySelectorAll('section.note-item').length > 0`).Bool()
		})
		if ok {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func waitForNoteModalClosed(page *rod.Page, timeout time.Duration) {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		closed := true
		_ = rod.Try(func() {
			closed = page.Timeout(800 * time.Millisecond).MustEval(`() => {
				const m = document.querySelector('.note-detail-modal') || document.querySelector('.modal') || document.querySelector('[class*="detail"]');
				if (!m) return true;
				const rect = m.getBoundingClientRect();
				if (!rect || rect.width <= 0 || rect.height <= 0) return true;
				const s = window.getComputedStyle(m);
				if (!s) return false;
				if (s.display === 'none' || s.visibility === 'hidden' || parseFloat(s.opacity || '1') === 0) return true;
				return false;
			}`).Bool()
		})
		if closed {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
}

// randomRefreshInterval 生成随机的页面刷新间隔（1-3分钟）
// 模拟真实用户习惯：每隔几分钟刷新推荐页以获取新内容
func randomRefreshInterval() time.Duration {
	// 随机生成 1-3 分钟的间隔
	minutes := rand.Intn(3) + 1 // 1, 2, 3
	// 再加上一些随机秒数，让时间更自然 (0-30秒)
	seconds := rand.Intn(31)
	totalSeconds := minutes*60 + seconds
	return time.Duration(totalSeconds) * time.Second
}
