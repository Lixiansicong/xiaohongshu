package browser

import (
	"sync"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/headless_browser"
)

// Manager 浏览器实例管理器，确保同一时间只有一个浏览器实例在使用
type Manager struct {
	mu       sync.Mutex
	cond     *sync.Cond // 条件变量，用于等待浏览器释放
	browser  *headless_browser.Browser
	headless bool
	binPath  string
	inUse    bool // 标记浏览器是否正在使用中
}

var (
	globalManager     *Manager
	globalManagerOnce sync.Once
)

// GetGlobalManager 获取全局浏览器管理器（单例）
func GetGlobalManager() *Manager {
	globalManagerOnce.Do(func() {
		m := &Manager{}
		m.cond = sync.NewCond(&m.mu)
		globalManager = m
	})
	return globalManager
}

// SetConfig 设置浏览器配置
func (m *Manager) SetConfig(headless bool, binPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.headless = headless
	m.binPath = binPath
}

// AcquireBrowser 获取浏览器实例（会阻塞直到浏览器可用）
// 返回浏览器实例和一个 release 函数，使用完毕后必须调用 release 函数释放浏览器
func (m *Manager) AcquireBrowser() (*headless_browser.Browser, func()) {
	m.mu.Lock()
	
	// 如果浏览器正在使用中，等待其释放
	for m.inUse {
		logrus.Info("⏳ 浏览器正在使用中，等待释放...")
		m.cond.Wait() // 释放锁并等待信号，被唤醒后会重新获得锁
		logrus.Info("✓ 浏览器已释放，继续执行")
	}
	
	// 如果浏览器实例不存在或已关闭，创建新实例
	if m.browser == nil {
		logrus.Info("创建新的浏览器实例...")
		m.browser = NewBrowser(m.headless, WithBinPath(m.binPath))
		logrus.Info("✓ 浏览器实例创建成功")
	}
	
	m.inUse = true
	browser := m.browser
	
	// 返回释放函数
	release := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.inUse = false
		logrus.Debug("浏览器实例已释放，可供其他操作使用")
		m.cond.Signal() // 唤醒一个等待的 goroutine
	}
	
	m.mu.Unlock()
	return browser, release
}

// CloseBrowser 关闭并清理浏览器实例
func (m *Manager) CloseBrowser() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.browser != nil {
		logrus.Info("关闭浏览器实例...")
		m.browser.Close()
		m.browser = nil
		m.inUse = false
	}
}

// NewPageWithRelease 获取一个新的页面，并返回页面和释放函数
// 这是一个便捷方法，组合了获取浏览器和创建页面的操作
func (m *Manager) NewPageWithRelease() (*rod.Page, func()) {
	browser, releaseBrowser := m.AcquireBrowser()
	
	page := browser.NewPage()
	
	// 组合释放函数：先关闭页面，再释放浏览器
	release := func() {
		if page != nil {
			page.Close()
		}
		releaseBrowser()
	}
	
	return page, release
}

