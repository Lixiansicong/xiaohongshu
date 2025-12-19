package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/recommendation"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

func resetCookiesFiles() error {
	basePath := cookies.GetCookiesFilePath()
	dir := filepath.Dir(basePath)
	base := filepath.Base(basePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = "cookies"
	}

	// 1) 删除主 cookies 文件
	if err := os.Remove(basePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	// 2) 删除同目录下派生的 instance cookies 文件
	pattern := filepath.Join(dir, fmt.Sprintf("%s_*%s", name, ext))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, p := range matches {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// 这个 CLI 程序用于直接从命令行运行推荐页浏览任务（支持多实例），
// 复用服务层的浏览逻辑，而不依赖 MCP 客户端。
func main() {
	rand.Seed(time.Now().UnixNano())

	var (
		headless            bool
		binPath             string
		duration            int
		minScrolls          int
		maxScrolls          int
		clickProbability    int
		interactProbability int
		likeOnlyProbability int
		instances           int
		withoutComment      bool
		resetCookies        bool
	)

	flag.BoolVar(&headless, "headless", false, "是否无头模式，默认 false（有界面，便于扫码登录）")
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径（可选，不传则使用 ROD_BROWSER_BIN 环境变量）")
	flag.IntVar(&duration, "duration", xiaohongshu.DefaultBrowseDurationMinutes,
		fmt.Sprintf("浏览时长（分钟），默认 %d 分钟", xiaohongshu.DefaultBrowseDurationMinutes))
	flag.IntVar(&minScrolls, "min-scrolls", xiaohongshu.DefaultMinScrollsPerRound,
		fmt.Sprintf("每轮最小滚动次数，默认 %d 次", xiaohongshu.DefaultMinScrollsPerRound))
	flag.IntVar(&maxScrolls, "max-scrolls", xiaohongshu.DefaultMaxScrollsPerRound,
		fmt.Sprintf("每轮最大滚动次数，默认 %d 次", xiaohongshu.DefaultMaxScrollsPerRound))
	flag.IntVar(&clickProbability, "click-probability", xiaohongshu.DefaultClickProbability,
		fmt.Sprintf("点击笔记的概率(0-100)，默认 %d", xiaohongshu.DefaultClickProbability))
	flag.IntVar(&interactProbability, "interact-probability", xiaohongshu.DefaultInteractProbability,
		fmt.Sprintf("在笔记中互动的概率(0-100)，默认 %d", xiaohongshu.DefaultInteractProbability))
	flag.IntVar(&likeOnlyProbability, "like-only-probability", xiaohongshu.DefaultLikeOnlyProbability,
		fmt.Sprintf("互动时仅点赞不收藏的概率(0-100)，默认 %d", xiaohongshu.DefaultLikeOnlyProbability))
	flag.IntVar(&instances, "instances", 1, "浏览器实例数量，1 表示单实例，大于 1 表示多实例并行")
	flag.BoolVar(&withoutComment, "without-comment", true, "是否关闭评论，仅点赞/收藏/浏览")
	flag.BoolVar(&resetCookies, "reset-cookies", false, "启动前清理 cookies 文件并重新登录")

	flag.Parse()

	if resetCookies {
		if err := resetCookiesFiles(); err != nil {
			logrus.Fatalf("failed to reset cookies: %v", err)
		}
		logrus.Infof("cookies 已清理（含 instance 派生文件），将重新登录")
	}

	if headless {
		logrus.Warn("当前以无头模式运行，首次登录时可能无法扫码，建议第一次使用时 headless=false")
	}

	// 初始化全局配置（主要是给可能复用的浏览器管理器使用）
	configs.InitHeadless(headless)
	configs.SetBinPath(binPath)
	browser.GetGlobalManager().SetConfig(headless, binPath)

	// 构建浏览配置
	cfg := xiaohongshu.BrowseConfig{
		Duration:            duration,
		MinScrolls:          minScrolls,
		MaxScrolls:          maxScrolls,
		ClickProbability:    clickProbability,
		InteractProbability: interactProbability,
		LikeOnlyProbability: likeOnlyProbability,
	}

	if withoutComment {
		disable := false
		cfg.EnableComment = &disable
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(duration+5)*time.Minute)
	defer cancel()

	// 如果实例数 <= 1，则视为单实例，内部仍然使用并行逻辑但只启动一个实例
	if instances <= 1 {
		instances = 1
		logrus.Infof("开始单实例浏览推荐页，时长=%d 分钟", duration)
		results, err := recommendation.RunParallelBrowse(ctx, cfg, instances)
		if err != nil {
			logrus.WithError(err).Error("单实例浏览过程中出现错误")
		}

		var stats *xiaohongshu.BrowseStats
		for _, res := range results {
			if res != nil && res.Stats != nil {
				stats = res.Stats
				break
			}
		}
		if stats == nil {
			logrus.Fatal("浏览失败：未获得任何实例的统计结果，请检查登录状态或网络情况")
		}

		fmt.Printf("单实例浏览完成：\n- 浏览时长: %v\n- 滚动次数: %d\n- 点击笔记: %d 个\n- 点赞: %d 次\n- 收藏: %d 次\n- 评论: %d 次\n- 浏览笔记: %d 个\n",
			stats.Duration, stats.ScrollCount, stats.ClickCount,
			stats.LikeCount, stats.FavoriteCount, stats.CommentCount,
			len(stats.ViewedNotes),
		)
		return
	}

	// 多实例并行浏览逻辑
	logrus.Infof("开始并行浏览推荐页，实例数=%d，时长=%d 分钟", instances, duration)
	results, err := recommendation.RunParallelBrowse(ctx, cfg, instances)
	if err != nil {
		logrus.WithError(err).Error("并行浏览过程中出现错误")
	}

	var successCount int
	for _, res := range results {
		if res == nil {
			continue
		}
		if res.Stats != nil {
			successCount++
			fmt.Printf("实例 %s 浏览完成：\n- 浏览时长: %v\n- 滚动次数: %d\n- 点击笔记: %d 个\n- 点赞: %d 次\n- 收藏: %d 次\n- 评论: %d 次\n- 浏览笔记: %d 个\n\n",
				res.InstanceID,
				res.Stats.Duration,
				res.Stats.ScrollCount,
				res.Stats.ClickCount,
				res.Stats.LikeCount,
				res.Stats.FavoriteCount,
				res.Stats.CommentCount,
				len(res.Stats.ViewedNotes),
			)
		} else {
			fmt.Printf("实例 %s 失败：%s\n", res.InstanceID, res.Error)
		}
	}

	if successCount == 0 {
		logrus.Fatal("所有实例均未成功完成浏览，请检查登录状态或网络情况")
	}
}
