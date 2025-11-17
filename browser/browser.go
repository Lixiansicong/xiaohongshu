package browser

import (
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
