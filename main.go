package main

import (
	"flag"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
)

func main() {
	var (
		headless  bool
		binPath   string // 浏览器二进制文件路径
		port      string
		stdioMode bool // 是否使用 STDIO 模式
	)
	flag.BoolVar(&headless, "headless", true, "是否无头模式")
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径")
	flag.StringVar(&port, "port", ":18060", "端口")
	flag.BoolVar(&stdioMode, "stdio", false, "使用 STDIO 模式（用于 MCP 客户端）")
	flag.Parse()

	if len(binPath) == 0 {
		binPath = os.Getenv("ROD_BROWSER_BIN")
	}

	configs.InitHeadless(headless)
	configs.SetBinPath(binPath)

	// 初始化服务
	xiaohongshuService := NewXiaohongshuService()

	// 创建应用服务器
	appServer := NewAppServer(xiaohongshuService)

	// 根据模式选择启动方式
	if stdioMode {
		// STDIO 模式：直接运行 MCP 服务器，不启动 HTTP 服务
		logrus.Info("启动 STDIO 模式 MCP 服务器")
		if err := appServer.StartSTDIO(); err != nil {
			logrus.Fatalf("failed to run STDIO server: %v", err)
		}
	} else {
		// HTTP 模式：启动 HTTP 服务器
		if err := appServer.Start(port); err != nil {
			logrus.Fatalf("failed to run server: %v", err)
		}
	}
}
