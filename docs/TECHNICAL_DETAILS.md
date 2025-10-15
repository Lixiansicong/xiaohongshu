# 技术实现细节

## 数据获取方式

### 核心原理：`window.__INITIAL_STATE__`

小红书在页面加载时会将初始数据注入到 `window.__INITIAL_STATE__` 对象中，这是一个包含所有页面初始状态的 JavaScript 对象。

项目中**所有的数据获取功能都统一使用这个对象**作为数据源。

### 数据结构

```javascript
window.__INITIAL_STATE__ = {
  feed: {
    feeds: {
      _value: [
        {
          id: "68e66fef0000000004023fdb",        // 笔记 ID (feedID)
          xsecToken: "ABc9MCVTGMXqvxLT8H...",    // 安全令牌
          noteCard: {
            displayTitle: "笔记标题",
            user: {
              userId: "...",
              nickname: "..."
            },
            interactInfo: {
              liked: false,
              likedCount: "123",
              collected: false
            },
            cover: { ... }
          }
        },
        // ... 更多笔记
      ]
    }
  },
  search: {
    feeds: {
      _value: [ ... ]  // 搜索结果也是相同结构
    }
  }
}
```

### 统一实现方式

#### 1. 获取推荐流（`feeds.go`）

```go
func (f *FeedsListAction) GetFeedsList(ctx context.Context) ([]Feed, error) {
    result := page.MustEval(`() => {
        if (window.__INITIAL_STATE__) {
            return JSON.stringify(window.__INITIAL_STATE__);
        }
        return "";
    }`).String()
    
    var state FeedsResult
    json.Unmarshal([]byte(result), &state)
    
    return state.Feed.Feeds.Value, nil
}
```

#### 2. 搜索笔记（`search.go`）

```go
func (s *SearchAction) Search(ctx context.Context, keyword string) ([]Feed, error) {
    result := page.MustEval(`() => {
        if (window.__INITIAL_STATE__) {
            return JSON.stringify(window.__INITIAL_STATE__);
        }
        return "";
    }`).String()
    
    var searchResult SearchResult
    json.Unmarshal([]byte(result), &searchResult)
    
    return searchResult.Search.Feeds.Value, nil
}
```

#### 3. 浏览推荐页（`browse.go`）✅ 已统一

```go
func (b *BrowseAction) getFeedsFromPage(page *rod.Page) ([]Feed, error) {
    result := page.MustEval(`() => {
        if (window.__INITIAL_STATE__) {
            return JSON.stringify(window.__INITIAL_STATE__);
        }
        return "";
    }`).String()
    
    var state FeedsResult
    json.Unmarshal([]byte(result), &state)
    
    return state.Feed.Feeds.Value, nil
}
```

### 为什么使用 `window.__INITIAL_STATE__`？

#### ✅ 优势

1. **数据完整准确**
   - 包含所有必需字段（id, xsecToken, noteCard 等）
   - 数据结构清晰，易于解析

2. **稳定可靠**
   - 不依赖 DOM 结构，不受页面样式变化影响
   - 不需要复杂的选择器和 DOM 遍历

3. **性能优越**
   - 一次 JavaScript 执行即可获取所有数据
   - 避免多次 DOM 查询

4. **与项目一致**
   - 所有功能使用相同的数据获取方式
   - 代码风格统一，易于维护

#### ❌ 之前的错误做法

**从 DOM 元素或 URL 解析数据：**

```go
// ❌ 错误方式：从链接解析
href := "/explore/68e66fef0000000004023fdb?xsec_token=ABc9..."
// 需要复杂的字符串处理
feedID := parseFromURL(href)
xsecToken := parseFromURL(href)

// ❌ 问题：
// 1. 解析逻辑复杂，容易出错
// 2. URL 格式可能变化
// 3. xsecToken 可能包含特殊字符，解析困难
// 4. 与项目其他功能不一致
```

### 完整的浏览流程

#### 1. 导航到推荐页

```go
page.MustNavigate("https://www.xiaohongshu.com/explore")
page.MustWaitLoad()
```

#### 2. 从 `__INITIAL_STATE__` 获取笔记列表

```go
feeds, err := b.getFeedsFromPage(page)
// feeds 是 []Feed 类型，包含所有笔记信息
```

#### 3. 随机选择一个笔记

```go
selectedFeed := feeds[rand.Intn(len(feeds))]
feedID := selectedFeed.ID            // "68e66fef0000000004023fdb"
xsecToken := selectedFeed.XsecToken  // "ABc9MCVTGMXqvxLT8H..."
```

#### 4. 点击对应的 DOM 元素

```go
// 随机选择一个可见的笔记卡片
noteCards, _ := page.Elements("section.note-item")
selectedCard := noteCards[rand.Intn(len(noteCards))]
selectedCard.Click(proto.InputMouseButtonLeft, 1)
```

#### 5. 使用准确的 feedID 和 xsecToken 进行互动

```go
// 点赞（复用现有功能）
likeAction := NewLikeAction(page)
likeAction.Like(ctx, feedID, xsecToken)

// 收藏（复用现有功能）
favoriteAction := NewFavoriteAction(page)
favoriteAction.Favorite(ctx, feedID, xsecToken)

// 评论（复用现有功能）
commentAction := NewCommentFeedAction(page)
commentAction.PostComment(ctx, feedID, xsecToken, comment)
```

### Feed 数据结构

```go
// Feed 表示单个笔记
type Feed struct {
    XsecToken string   `json:"xsecToken"`  // 安全令牌
    ID        string   `json:"id"`         // 笔记 ID
    ModelType string   `json:"modelType"`  // 模型类型
    NoteCard  NoteCard `json:"noteCard"`   // 笔记卡片信息
    Index     int      `json:"index"`      // 索引
}

// NoteCard 表示笔记卡片信息
type NoteCard struct {
    Type         string       `json:"type"`          // 类型（video/normal）
    DisplayTitle string       `json:"displayTitle"`  // 显示标题
    User         User         `json:"user"`          // 用户信息
    InteractInfo InteractInfo `json:"interactInfo"`  // 互动信息
    Cover        Cover        `json:"cover"`         // 封面
    Video        *Video       `json:"video,omitempty"` // 视频（可选）
}
```

### 互动功能的 URL 构建

所有互动功能（点赞、收藏、评论）都需要访问笔记详情页：

```go
func makeFeedDetailURL(feedID, xsecToken string) string {
    return fmt.Sprintf(
        "https://www.xiaohongshu.com/explore/%s?xsec_token=%s&xsec_source=pc_feed",
        feedID,
        xsecToken,
    )
}
```

**示例 URL：**
```
https://www.xiaohongshu.com/explore/68e66fef0000000004023fdb?xsec_token=ABc9MCVTGMXqvxLT8H-fHb_6DodO8iEoHByoltzPex20I=&xsec_source=pc_feed
```

### 安全机制：`xsec_token`

`xsecToken` 是小红书的安全令牌，用于：

1. **防止 CSRF 攻击**
2. **验证请求来源**
3. **跟踪用户行为**

**特点：**
- 每个笔记有唯一的 token
- Token 可能包含特殊字符（`=`, `-`, `_` 等）
- Token 有时效性
- 必须与正确的 feedID 配对使用

### 代码复用

浏览功能完全复用了现有的互动功能：

```go
// xiaohongshu/like_favorite.go
type LikeAction struct { ... }
func (a *LikeAction) Like(ctx context.Context, feedID, xsecToken string) error

type FavoriteAction struct { ... }
func (a *FavoriteAction) Favorite(ctx context.Context, feedID, xsecToken string) error

// xiaohongshu/comment_feed.go
type CommentFeedAction struct { ... }
func (f *CommentFeedAction) PostComment(ctx context.Context, feedID, xsecToken, content string) error
```

**在 browse.go 中：**

```go
func (b *BrowseAction) interactWithNote(ctx context.Context, feedID, xsecToken string, stats *BrowseStats) error {
    // 复用点赞功能
    likeAction := NewLikeAction(b.page)
    likeAction.Like(ctx, feedID, xsecToken)
    
    // 复用收藏功能
    favoriteAction := NewFavoriteAction(b.page)
    favoriteAction.Favorite(ctx, feedID, xsecToken)
    
    // 复用评论功能
    commentAction := NewCommentFeedAction(b.page)
    commentAction.PostComment(ctx, feedID, xsecToken, comment)
}
```

### 错误处理

#### 1. `__INITIAL_STATE__` 不存在

```go
if result == "" {
    return nil, fmt.Errorf("__INITIAL_STATE__ not found")
}
```

**可能原因：**
- 页面未完全加载
- 小红书页面结构变化
- 网络错误

**解决方案：**
- 增加等待时间
- 重试机制
- 检查网络连接

#### 2. JSON 解析失败

```go
if err := json.Unmarshal([]byte(result), &state); err != nil {
    return nil, fmt.Errorf("failed to unmarshal __INITIAL_STATE__: %w", err)
}
```

**可能原因：**
- 数据结构变化
- 不完整的 JSON

**解决方案：**
- 更新数据结构定义
- 增加数据验证

#### 3. feedID 或 xsecToken 为空

```go
if feedID == "" || xsecToken == "" {
    return fmt.Errorf("笔记信息不完整")
}
```

**可能原因：**
- 数据源异常
- 字段名称变化

**解决方案：**
- 检查字段映射
- 增加日志记录

### 最佳实践

#### 1. 统一数据源

✅ **始终从 `window.__INITIAL_STATE__` 获取数据**

```go
// ✅ 正确
feeds, _ := getFeedsFromPage(page)
feedID := feeds[0].ID

// ❌ 错误
href, _ := element.Attribute("href")
feedID := parseFromURL(href)
```

#### 2. 数据验证

✅ **获取数据后立即验证**

```go
feeds, err := getFeedsFromPage(page)
if err != nil || len(feeds) == 0 {
    return fmt.Errorf("未找到笔记列表")
}

if feeds[0].ID == "" || feeds[0].XsecToken == "" {
    return fmt.Errorf("笔记信息不完整")
}
```

#### 3. 复用现有功能

✅ **使用已验证的功能模块**

```go
// ✅ 正确：复用现有功能
likeAction := NewLikeAction(page)
likeAction.Like(ctx, feedID, xsecToken)

// ❌ 错误：重新实现相同功能
// 自己写点赞逻辑...
```

#### 4. 保持代码一致性

✅ **与项目中其他功能保持一致的实现风格**

所有数据获取功能（`feeds.go`, `search.go`, `browse.go`）都使用相同的模式：
1. 导航到目标页面
2. 等待页面加载
3. 执行 JavaScript 获取 `__INITIAL_STATE__`
4. 解析 JSON 数据
5. 返回结构化数据

### 性能考虑

#### JavaScript 执行开销

```go
// 一次性获取所有数据（推荐）
feeds, _ := getFeedsFromPage(page)
for _, feed := range feeds {
    // 使用 feed.ID, feed.XsecToken
}

// ❌ 避免：多次执行 JavaScript
for i := 0; i < 10; i++ {
    // 每次都重新获取，开销大
    feeds, _ := getFeedsFromPage(page)
}
```

#### 数据缓存

浏览过程中，`__INITIAL_STATE__` 的数据不会变化（除非刷新页面），可以缓存使用。

### 调试技巧

#### 1. 在浏览器中查看数据

打开小红书网站 → 打开开发者工具 → Console：

```javascript
// 查看完整的初始状态
console.log(window.__INITIAL_STATE__)

// 查看笔记列表
console.log(window.__INITIAL_STATE__.feed.feeds._value)

// 查看第一个笔记
console.log(window.__INITIAL_STATE__.feed.feeds._value[0])
```

#### 2. 导出数据用于测试

```javascript
// 导出为 JSON 文件
copy(JSON.stringify(window.__INITIAL_STATE__, null, 2))
// 然后粘贴到文件中
```

#### 3. 日志记录

```go
logrus.Debugf("获取到 %d 个笔记", len(feeds))
logrus.Debugf("笔记 ID: %s, Token: %s", feedID, xsecToken[:20]+"...")
```

---

## 总结

通过统一使用 `window.__INITIAL_STATE__` 作为数据源，我们实现了：

✅ 数据获取的**准确性**和**可靠性**  
✅ 代码风格的**统一性**和**一致性**  
✅ 功能模块的**可复用性**和**可维护性**  
✅ 与项目其他部分的**兼容性**

这是小红书 MCP 项目的核心技术实现，所有新功能都应遵循这个模式。

---

**最后更新**：2025-10-15  
**作者**：xiaohongshu-mcp 团队


