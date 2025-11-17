package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
	"strings"
)

// MCP 工具处理函数

// handleCheckLoginStatus 处理检查登录状态
func (s *AppServer) handleCheckLoginStatus(ctx context.Context) *MCPToolResult {
	logrus.Info("MCP: 检查登录状态")

	status, err := s.xiaohongshuService.CheckLoginStatus(ctx)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "检查登录状态失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	resultText := fmt.Sprintf("登录状态检查成功: %+v", status)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleGetLoginQrcode 处理获取登录二维码请求。
// 返回二维码图片的 Base64 编码和超时时间，供前端展示扫码登录。
func (s *AppServer) handleGetLoginQrcode(ctx context.Context) *MCPToolResult {
	logrus.Info("MCP: 获取登录扫码图片")

	result, err := s.xiaohongshuService.GetLoginQrcode(ctx)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取登录扫码图片失败: " + err.Error()}},
			IsError: true,
		}
	}

	if result.IsLoggedIn {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "你当前已处于登录状态"}},
		}
	}

	now := time.Now()
	deadline := func() string {
		d, err := time.ParseDuration(result.Timeout)
		if err != nil {
			return now.Format("2006-01-02 15:04:05")
		}
		return now.Add(d).Format("2006-01-02 15:04:05")
	}()

	// 已登录：文本 + 图片
	contents := []MCPContent{
		{Type: "text", Text: "请用小红书 App 在 " + deadline + " 前扫码登录 👇"},
		{
			Type:     "image",
			MimeType: "image/png",
			Data:     strings.TrimPrefix(result.Img, "data:image/png;base64,"),
		},
	}
	return &MCPToolResult{Content: contents}
}

// handlePublishContent 处理发布内容
func (s *AppServer) handlePublishContent(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 发布内容")

	// 解析参数
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	imagePathsInterface, _ := args["images"].([]interface{})
	tagsInterface, _ := args["tags"].([]interface{})

	var imagePaths []string
	for _, path := range imagePathsInterface {
		if pathStr, ok := path.(string); ok {
			imagePaths = append(imagePaths, pathStr)
		}
	}

	var tags []string
	for _, tag := range tagsInterface {
		if tagStr, ok := tag.(string); ok {
			tags = append(tags, tagStr)
		}
	}

	logrus.Infof("MCP: 发布内容 - 标题: %s, 图片数量: %d, 标签数量: %d", title, len(imagePaths), len(tags))

	// 构建发布请求
	req := &PublishRequest{
		Title:   title,
		Content: content,
		Images:  imagePaths,
		Tags:    tags,
	}

	// 执行发布
	result, err := s.xiaohongshuService.PublishContent(ctx, req)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	resultText := fmt.Sprintf("内容发布成功: %+v", result)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handlePublishVideo 处理发布视频内容（仅本地单个视频文件）
func (s *AppServer) handlePublishVideo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 发布视频内容（本地）")

	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	videoPath, _ := args["video"].(string)
	tagsInterface, _ := args["tags"].([]interface{})

	var tags []string
	for _, tag := range tagsInterface {
		if tagStr, ok := tag.(string); ok {
			tags = append(tags, tagStr)
		}
	}

	if videoPath == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: 缺少本地视频文件路径",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 发布视频 - 标题: %s, 标签数量: %d", title, len(tags))

	// 构建发布请求
	req := &PublishVideoRequest{
		Title:   title,
		Content: content,
		Video:   videoPath,
		Tags:    tags,
	}

	// 执行发布
	result, err := s.xiaohongshuService.PublishVideo(ctx, req)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	resultText := fmt.Sprintf("视频发布成功: %+v", result)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleListFeeds 处理获取Feeds列表
func (s *AppServer) handleListFeeds(ctx context.Context) *MCPToolResult {
	logrus.Info("MCP: 获取Feeds列表")

	result, err := s.xiaohongshuService.ListFeeds(ctx)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feeds列表失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取Feeds列表成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleSearchFeeds 处理搜索Feeds
func (s *AppServer) handleSearchFeeds(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 搜索Feeds")

	// 解析参数
	keyword, ok := args["keyword"].(string)
	if !ok || keyword == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "搜索Feeds失败: 缺少关键词参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 搜索Feeds - 关键词: %s", keyword)

	result, err := s.xiaohongshuService.SearchFeeds(ctx, keyword)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "搜索Feeds失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("搜索Feeds成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleGetFeedDetail 处理获取Feed详情
func (s *AppServer) handleGetFeedDetail(ctx context.Context, args map[string]any) *MCPToolResult {
	logrus.Info("MCP: 获取Feed详情")

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 获取Feed详情 - Feed ID: %s", feedID)

	result, err := s.xiaohongshuService.GetFeedDetail(ctx, feedID, xsecToken)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取Feed详情成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleUserProfile 获取用户主页
func (s *AppServer) handleUserProfile(ctx context.Context, args map[string]any) *MCPToolResult {
	logrus.Info("MCP: 获取用户主页")

	// 解析参数
	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: 缺少user_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 获取用户主页 - User ID: %s", userID)

	result, err := s.xiaohongshuService.UserProfile(ctx, userID, xsecToken)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取用户主页，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleLikeFeed 处理点赞/取消点赞
func (s *AppServer) handleLikeFeed(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少feed_id参数"}}, IsError: true}
	}
	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少xsec_token参数"}}, IsError: true}
	}
	unlike, _ := args["unlike"].(bool)

	var res *ActionResult
	var err error

	if unlike {
		res, err = s.xiaohongshuService.UnlikeFeed(ctx, feedID, xsecToken)
	} else {
		res, err = s.xiaohongshuService.LikeFeed(ctx, feedID, xsecToken)
	}

	if err != nil {
		action := "点赞"
		if unlike {
			action = "取消点赞"
		}
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: action + "失败: " + err.Error()}}, IsError: true}
	}

	action := "点赞"
	if unlike {
		action = "取消点赞"
	}
	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s成功 - Feed ID: %s", action, res.FeedID)}}}
}

// handleFavoriteFeed 处理收藏/取消收藏
func (s *AppServer) handleFavoriteFeed(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少feed_id参数"}}, IsError: true}
	}
	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少xsec_token参数"}}, IsError: true}
	}
	unfavorite, _ := args["unfavorite"].(bool)

	var res *ActionResult
	var err error

	if unfavorite {
		res, err = s.xiaohongshuService.UnfavoriteFeed(ctx, feedID, xsecToken)
	} else {
		res, err = s.xiaohongshuService.FavoriteFeed(ctx, feedID, xsecToken)
	}

	if err != nil {
		action := "收藏"
		if unfavorite {
			action = "取消收藏"
		}
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: action + "失败: " + err.Error()}}, IsError: true}
	}

	action := "收藏"
	if unfavorite {
		action = "取消收藏"
	}
	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s成功 - Feed ID: %s", action, res.FeedID)}}}
}

// handlePostComment 处理发表评论到Feed
func (s *AppServer) handlePostComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 发表评论到Feed")

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少content参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 发表评论 - Feed ID: %s, 内容长度: %d", feedID, len(content))

	// 发表评论
	result, err := s.xiaohongshuService.PostCommentToFeed(ctx, feedID, xsecToken, content)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 返回成功结果，只包含feed_id
	resultText := fmt.Sprintf("评论发表成功 - Feed ID: %s", result.FeedID)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}


// buildBrowseConfigFromArgs 从 MCP 参数构建 BrowseConfig，可选择是否强制禁用评论
func buildBrowseConfigFromArgs(args map[string]interface{}, forceDisableComment bool) xiaohongshu.BrowseConfig {
	duration, _ := args["duration"].(float64)
	minScrolls, _ := args["min_scrolls"].(float64)
	maxScrolls, _ := args["max_scrolls"].(float64)
	clickProbability, _ := args["click_probability"].(float64)
	interactProbability, _ := args["interact_probability"].(float64)
	likeOnlyProbability, _ := args["like_only_probability"].(float64)

	var comments []string
	if commentsInterface, ok := args["comments"].([]interface{}); ok {
		for _, c := range commentsInterface {
			if commentStr, ok := c.(string); ok {
				comments = append(comments, commentStr)
			}
		}
	}

	var enableComment *bool
	if forceDisableComment {
		v := false
		enableComment = &v
	} else {
		if enableCommentVal, ok := args["enable_comment"].(bool); ok {
			enableComment = &enableCommentVal
		}
	}

	return xiaohongshu.BrowseConfig{
		Duration:            int(duration),
		MinScrolls:          int(minScrolls),
		MaxScrolls:          int(maxScrolls),
		ClickProbability:    int(clickProbability),
		InteractProbability: int(interactProbability),
		LikeOnlyProbability: int(likeOnlyProbability),
		EnableComment:       enableComment,
		Comments:            comments,
	}
}

// handleBrowseRecommendations 处理浏览推荐页
func (s *AppServer) handleBrowseRecommendations(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 开始浏览推荐页")

	config := buildBrowseConfigFromArgs(args, false)

	logrus.Infof("MCP: 浏览配置 - 时长: %d分钟, 点击概率: %d%%, 互动概率: %d%%",
		config.Duration, config.ClickProbability, config.InteractProbability)

	// 执行浏览
	stats, err := s.xiaohongshuService.BrowseRecommendations(ctx, config)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "浏览推荐页失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出
	resultText := fmt.Sprintf(`浏览推荐页完成！

📊 统计信息:
- 浏览时长: %v
- 滚动次数: %d
- 点击笔记: %d 个
- 点赞: %d 次
- 收藏: %d 次
- 评论: %d 次
- 浏览笔记: %d 个`,
		stats.Duration.Round(time.Second),
		stats.ScrollCount,
		stats.ClickCount,
		stats.LikeCount,
		stats.FavoriteCount,
		stats.CommentCount,
		len(stats.ViewedNotes),
	)

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleBrowseRecommendationsWithoutComment 处理浏览推荐页（不进行评论）
func (s *AppServer) handleBrowseRecommendationsWithoutComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 开始浏览推荐页（无评论模式）")

	config := buildBrowseConfigFromArgs(args, true)

	logrus.Infof("MCP: 无评论浏览配置 - 时长: %d分钟, 点击概率: %d%%, 互动概率: %d%%",
		config.Duration, config.ClickProbability, config.InteractProbability)

	// 执行浏览
	stats, err := s.xiaohongshuService.BrowseRecommendationsWithoutComment(ctx, config)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "浏览推荐页失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出
	resultText := fmt.Sprintf(`浏览推荐页完成（无评论模式）！

📊 统计信息:
- 浏览时长: %v
- 滚动次数: %d
- 点击笔记: %d 个
- 点赞: %d 次
- 收藏: %d 次
- 评论: %d 次
- 浏览笔记: %d 个`,
		stats.Duration.Round(time.Second),
		stats.ScrollCount,
		stats.ClickCount,
		stats.LikeCount,
		stats.FavoriteCount,
		stats.CommentCount,
		len(stats.ViewedNotes),
	)

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleParallelBrowseRecommendations 处理并行浏览推荐页（多浏览器实例）
func (s *AppServer) handleParallelBrowseRecommendations(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 开始并行浏览推荐页（多实例）")

	config := buildBrowseConfigFromArgs(args, false)

	// 并行实例数量
	instancesFloat, _ := args["instances"].(float64)
	instances := int(instancesFloat)
	if instances <= 0 {
		instances = 3
	}

	logrus.Infof("MCP: 并行浏览配置 - 实例数: %d, 时长: %d分钟, 点击概率: %d%%, 互动概率: %d%%",
		instances, config.Duration, config.ClickProbability, config.InteractProbability)

	// 执行并行浏览
	results, err := s.xiaohongshuService.ParallelBrowseRecommendations(ctx, config, instances)
	if err != nil {
		logrus.WithError(err).Error("并行浏览推荐页失败")
	}

	var sb strings.Builder
	sb.WriteString("并行浏览推荐页完成。\n\n")
	sb.WriteString(fmt.Sprintf("配置: 实例数=%d, 时长=%d分钟, 点击概率=%d%%, 互动概率=%d%%\n\n",
		instances, config.Duration, config.ClickProbability, config.InteractProbability))

	for _, res := range results {
		if res == nil {
			continue
		}
		if res.Stats != nil {
			stats := res.Stats
			sb.WriteString(fmt.Sprintf(
				"实例 %s:\n- 浏览时长: %v\n- 滚动次数: %d\n- 点击笔记: %d 个\n- 点赞: %d 次\n- 收藏: %d 次\n- 评论: %d 次\n- 浏览笔记: %d 个\n\n",
				res.InstanceID,
				stats.Duration.Round(time.Second),
				stats.ScrollCount,
				stats.ClickCount,
				stats.LikeCount,
				stats.FavoriteCount,
				stats.CommentCount,
				len(stats.ViewedNotes),
			))
		} else {
			sb.WriteString(fmt.Sprintf("实例 %s: 失败 - %s\n\n", res.InstanceID, res.Error))
		}
	}

	// 如果所有实例都失败了，将整体标记为错误
	isError := err != nil
	if !isError {
		allFailed := true
		for _, res := range results {
			if res != nil && res.Stats != nil {
				allFailed = false
				break
			}
		}
		if allFailed {
			isError = true
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: sb.String(),
		}},
		IsError: isError,
	}
}
