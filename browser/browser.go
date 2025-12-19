package browser

import (
	"runtime"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/headless_browser"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

type browserConfig struct {
	binPath    string
	cookiePath string
}

type Option func(*browserConfig)

func WithBinPath(binPath string) Option {
	return func(c *browserConfig) {
		c.binPath = binPath
	}
}

// WithCookiesPath 指定新浏览器实例启动时要使用的 cookies 文件路径。
func WithCookiesPath(path string) Option {
	return func(c *browserConfig) {
		c.cookiePath = path
	}
}

func NewBrowser(headless bool, options ...Option) *headless_browser.Browser {
	cfg := &browserConfig{}
	for _, opt := range options {
		opt(cfg)
	}

	opts := []headless_browser.Option{
		headless_browser.WithHeadless(headless),
	}
	if cfg.binPath != "" {
		opts = append(opts, headless_browser.WithChromeBinPath(cfg.binPath))
	}

	// 加载 cookies
	cookiePath := cfg.cookiePath
	if cookiePath == "" {
		cookiePath = cookies.GetCookiesFilePath()
	}
	cookieLoader := cookies.NewLoadCookie(cookiePath)

	if data, err := cookieLoader.LoadCookies(); err == nil {
		opts = append(opts, headless_browser.WithCookies(string(data)))
		logrus.WithField("cookies_path", cookiePath).Debug("loaded cookies from file successfully")
	} else {
		logrus.WithField("cookies_path", cookiePath).Warnf("failed to load cookies: %v", err)
	}

	return headless_browser.New(opts...)
}

// ConfigurePage 配置页面，应用针对特定环境的补丁（如 Windows UA 修复）
func ConfigurePage(page *rod.Page) {
	// 针对 Windows 环境修复 User-Agent
	// 因为 headless_browser 内部使用了 stealth 库，默认会将 UA 伪装成 Mac Chrome
	// 这会导致小红书识别设备为 Mac，且可能导致 Windows 下的一些兼容性问题
	if runtime.GOOS == "windows" {
		
		ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

		// 1. 通过协议层覆盖 UA
		// 忽略错误，因为如果页面已经关闭这可能会失败，但不影响主流程
		_ = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
			UserAgent: ua,
			Platform:  "Windows",
		})

		// 2. 注入 JS 脚本覆盖 navigator 属性，防止 stealth 库或页面脚本检测到不一致
		_, err := page.EvalOnNewDocument(`
			Object.defineProperty(navigator, 'platform', {
				get: () => 'Win32'
			});
			Object.defineProperty(navigator, 'userAgent', {
				get: () => '` + ua + `'
			});
			// 同时也覆盖 vendor，保持一致性
			Object.defineProperty(navigator, 'vendor', {
				get: () => 'Google Inc.'
			});
		`)
		if err != nil {
			logrus.Warnf("failed to set user agent script: %v", err)
		}

		logrus.Info("已修正 Windows 环境下的 User-Agent 设置")
	}
}
