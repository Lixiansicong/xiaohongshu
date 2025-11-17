package recommendation

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "time"

    "github.com/go-rod/rod"
    "github.com/sirupsen/logrus"
    "github.com/xpzouying/xiaohongshu-mcp/browser"
    "github.com/xpzouying/xiaohongshu-mcp/configs"
    "github.com/xpzouying/xiaohongshu-mcp/cookies"
    "github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// ParallelInstanceResult 表示单个浏览器实例的浏览结果
type ParallelInstanceResult struct {
    InstanceID string                   `json:"instance_id"`
    Stats      *xiaohongshu.BrowseStats `json:"stats,omitempty"`
    Error      string                   `json:"error,omitempty"`
}

// RunParallelBrowse 使用多个浏览器实例并行浏览推荐页
// 每个实例拥有独立的 cookies 文件和登录会话，登录等待时间统一为 60 秒
func RunParallelBrowse(ctx context.Context, config xiaohongshu.BrowseConfig, instances int) ([]*ParallelInstanceResult, error) {
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
            }).Info("启动并行浏览实例")

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
                logrus.WithField("instance", res.InstanceID).Info("检测到已登录，直接开始浏览")
                browseAction := xiaohongshu.NewBrowseAction(page, config)
                stats, browseErr := browseAction.StartBrowse(ctx)
                if browseErr != nil {
                    res.Error = browseErr.Error()
                    return
                }
                res.Stats = stats

                // 更新 cookies
                if err := SavePageCookiesToPath(page, cookiePath); err != nil {
                    logrus.WithError(err).WithField("instance", res.InstanceID).Warn("保存 cookies 失败")
                }
                return
            }

            // 2. 未登录时，进入登录等待窗口，等待用户扫码登录
            logrus.WithField("instance", res.InstanceID).Info("未登录，开始等待扫码登录")
            if ok := loginAction.WaitForLogin(loginCtx); !ok {
                if loginCtx.Err() != nil {
                    res.Error = "登录等待超时或被取消"
                } else {
                    res.Error = "登录失败"
                }
                return
            }

            logrus.WithField("instance", res.InstanceID).Info("扫码登录成功，开始浏览推荐页")
            // 登录成功后立即保存 cookies
            if err := SavePageCookiesToPath(page, cookiePath); err != nil {
                logrus.WithError(err).WithField("instance", res.InstanceID).Warn("保存 cookies 失败")
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
            if err := SavePageCookiesToPath(page, cookiePath); err != nil {
                logrus.WithError(err).WithField("instance", res.InstanceID).Warn("保存 cookies 失败")
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

// SavePageCookiesToPath 将当前页面的 cookies 保存到指定文件路径
func SavePageCookiesToPath(page *rod.Page, cookiePath string) error {
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

