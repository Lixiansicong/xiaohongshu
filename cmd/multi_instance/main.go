package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "math/rand"
    "sync"
    "time"

    "github.com/go-rod/rod"
    "github.com/sirupsen/logrus"
    "github.com/xpzouying/xiaohongshu-mcp/browser"
    "github.com/xpzouying/xiaohongshu-mcp/configs"
    "github.com/xpzouying/xiaohongshu-mcp/cookies"
    "github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// 这个 CLI 程序用于直接从命令行运行推荐页浏览任务（支持多实例），
// 复用服务层的浏览逻辑，而不依赖 MCP 客户端。
func main() {
    rand.Seed(time.Now().UnixNano())

    var (
        headless           bool
        binPath            string
        duration           int
        minScrolls         int
        maxScrolls         int
        clickProbability   int
        interactProbability int
        instances          int
        withoutComment     bool
    )

    flag.BoolVar(&headless, "headless", false, "是否无头模式，默认 false（有界面，便于扫码登录）")
    flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径（可选，不传则使用 ROD_BROWSER_BIN 环境变量）")
    flag.IntVar(&duration, "duration", 10, "浏览时长（分钟），默认 10 分钟")
    flag.IntVar(&minScrolls, "min-scrolls", 2, "每轮最小滚动次数，默认 3 次")
    flag.IntVar(&maxScrolls, "max-scrolls", 6, "每轮最大滚动次数，默认 8 次")
    flag.IntVar(&clickProbability, "click-probability", 65, "点击笔记的概率(0-100)，默认 30")
    flag.IntVar(&interactProbability, "interact-probability", 60, "在笔记中互动的概率(0-100)，默认 50")
    flag.IntVar(&instances, "instances", 1, "浏览器实例数量，1 表示单实例，大于 1 表示多实例并行")
    flag.BoolVar(&withoutComment, "without-comment", true, "是否关闭评论，仅点赞/收藏/浏览")

    flag.Parse()

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
        results, err := parallelBrowseRecommendationsCLI(ctx, cfg, instances)
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
    results, err := parallelBrowseRecommendationsCLI(ctx, cfg, instances)
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


// ParallelInstanceResult 表示单个浏览器实例的浏览结果（CLI 版本）
type ParallelInstanceResult struct {
    InstanceID string                   `json:"instance_id"`
    Stats      *xiaohongshu.BrowseStats `json:"stats,omitempty"`
    Error      string                   `json:"error,omitempty"`
}

// parallelBrowseRecommendationsCLI 使用多个浏览器实例并行浏览推荐页（CLI 版本）
// 每个实例拥有独立的 cookies 文件和登录会话，登录等待时间统一为 60 秒
func parallelBrowseRecommendationsCLI(ctx context.Context, config xiaohongshu.BrowseConfig, instances int) ([]*ParallelInstanceResult, error) {
    if instances <= 0 {
        instances = 3
    }

    results := make([]*ParallelInstanceResult, instances)
    var wg sync.WaitGroup

    // 登录等待窗口：60 秒
    loginCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()

    for i := 0; i < instances; i++ {
        idx := i
        instanceID := fmt.Sprintf("instance%d", idx+1)
        results[idx] = &ParallelInstanceResult{InstanceID: instanceID}

        wg.Add(1)
        go func(res *ParallelInstanceResult) {
            defer wg.Done()

            cookiePath := cookies.GetInstanceCookiesFilePath(res.InstanceID)
            logrus.WithFields(logrus.Fields{
                "instance":     res.InstanceID,
                "cookies_path": cookiePath,
            }).Info("启动并行浏览实例 (CLI)")

            // 为当前实例创建独立浏览器
            b := browser.NewBrowser(configs.IsHeadless(),
                browser.WithBinPath(configs.GetBinPath()),
                browser.WithCookiesPath(cookiePath),
            )
            page := b.NewPage()
            defer func() {
                // 关闭页面和浏览器，避免资源泄漏
                if page != nil {
                    page.Close()
                }
                if b != nil {
                    b.Close()
                }
            }()

            loginAction := xiaohongshu.NewLogin(page)

            // 1. 先尝试使用已有 cookies 自动登录
            loggedIn, err := loginAction.CheckLoginStatus(ctx)
            if err == nil && loggedIn {
                logrus.WithField("instance", res.InstanceID).Info("检测到已登录，直接开始浏览 (CLI)")
                browseAction := xiaohongshu.NewBrowseAction(page, config)
                stats, browseErr := browseAction.StartBrowse(ctx)
                if browseErr != nil {
                    res.Error = browseErr.Error()
                    return
                }
                res.Stats = stats

                // 更新 cookies
                if err := saveCookiesToPath(page, cookiePath); err != nil {
                    logrus.WithError(err).WithField("instance", res.InstanceID).Warn("保存 cookies 失败 (CLI)")
                }
                return
            }

            // 2. 未登录时，进入登录等待窗口，等待用户扫码登录
            logrus.WithField("instance", res.InstanceID).Info("未登录，开始等待扫码登录 (CLI)")

            if ok := loginAction.WaitForLogin(loginCtx); !ok {
                if loginCtx.Err() != nil {
                    res.Error = "登录等待超时或被取消"
                } else {
                    res.Error = "登录失败"
                }
                return
            }

            logrus.WithField("instance", res.InstanceID).Info("扫码登录成功，开始浏览推荐页 (CLI)")
            // 登录成功后立即保存 cookies
            if err := saveCookiesToPath(page, cookiePath); err != nil {
                logrus.WithError(err).WithField("instance", res.InstanceID).Warn("保存 cookies 失败 (CLI)")
            }

            // 开始浏览推荐页
            browseAction := xiaohongshu.NewBrowseAction(page, config)
            stats, browseErr := browseAction.StartBrowse(ctx)
            if browseErr != nil {
                res.Error = browseErr.Error()
                return
            }
            res.Stats = stats

            // 浏览完成后再次保存 cookies，保证会话持久化
            if err := saveCookiesToPath(page, cookiePath); err != nil {
                logrus.WithError(err).WithField("instance", res.InstanceID).Warn("保存 cookies 失败 (CLI)")
            }
        }(results[idx])
    }

    wg.Wait()

    // 判断是否至少有一个实例成功登录并完成浏览
    var hasSuccess bool
    for _, res := range results {
        if res != nil && res.Stats != nil {
            hasSuccess = true
            break
        }
    }

    if !hasSuccess {
        return results, fmt.Errorf("60 秒内没有任何浏览器实例完成登录，任务已终止")
    }

    return results, nil
}

// saveCookiesToPath 将当前页面的 cookies 保存到指定文件路径（CLI 版本）
func saveCookiesToPath(page *rod.Page, cookiePath string) error {
    cks, err := page.Browser().GetCookies()
    if err != nil {
        return err
    }

    data, err := json.Marshal(cks)
    if err != nil {
        return err
    }

    cookieLoader := cookies.NewLoadCookie(cookiePath)
    return cookieLoader.SaveCookies(data)
}

