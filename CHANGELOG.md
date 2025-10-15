# 更新日志 (Changelog)

## [v2.0] - 2025-10-15

### 🎉 重大优化：人类行为模拟 v2.0

基于真实用户行为数据，对浏览推荐页功能进行深度优化，大幅提升行为自然度和安全性。

#### ✨ 新增特性

- **分段滚动机制**：将滚动分成 2-4 段，每段间隔 0.2-1.2 秒
- **回滚行为**：7-18% 概率触发向上回看，模拟真实"哎，刚才那个是什么"的行为
- **优化停留时间**：从 0.8-2s 延长至 3-6s（中位数），更符合真实浏览节奏
- **自然退出机制**：使用 ESC 键（60%）或点击遮罩层（40%）关闭笔记，保留滚动位置，不刷新页面
- **URL 解析功能**：新增 `parseNoteURL()` 函数，从链接中正确提取 feedID 和 xsecToken

#### 🐛 Bug 修复

- **修复笔记信息提取失败**：采用与项目其他功能一致的方式，从 `window.__INITIAL_STATE__` 获取笔记数据
- **统一数据获取方式**：现在浏览功能与 `feeds.go`、`search.go` 等其他 MCP 功能使用相同的数据来源
- **提升可靠性**：直接从页面初始状态对象获取准确的 feedID 和 xsecToken，避免 DOM 解析的不确定性

#### 🔧 参数优化

| 参数 | 旧版本 | 新版本 | 说明 |
|------|--------|--------|------|
| 滚动段时长 | 0.3-0.8s | 0.6-2.5s | 更自然的滚动速度变化 |
| 短暂停 | 无 | 0.2-1.2s | 模拟视觉处理和思考时间 |
| 回滚概率 | 0% | 7-18% | 新增回看行为 |
| 停留时间 | 0.8-2s | 3-6s | 更真实的浏览停留 |

#### 📝 实现细节

```go
// 新增函数
func (b *BrowseAction) humanLikeScrollWithBacktrack(page *rod.Page) error
func (b *BrowseAction) closeNoteModal(page *rod.Page) error
```

**关键改进：**

1. **分段滚动**
   ```go
   segments := rand.Intn(3) + 2 // 2-4段
   for i := 0; i < segments; i++ {
       page.Mouse.MustScroll(0, float64(segmentScroll))
       time.Sleep(randomDuration(200, 1200)) // 段间停顿
   }
   ```

2. **回滚机制**
   ```go
   backtrackProbability := rand.Intn(12) + 7 // 7-18%
   if rand.Intn(100) < backtrackProbability {
       backtrackAmount := -(scrollAmount * (rand.Intn(20) + 20) / 100)
       page.Mouse.MustScroll(0, float64(backtrackAmount))
   }
   ```

3. **延长停留**
   ```go
   time.Sleep(randomDuration(3000, 6000)) // 3-6秒
   ```

4. **自然退出**
   ```go
   if closeMethod < 6 { // 60% 使用 ESC 键
       page.MustElement("body").MustKeyActions().Press(input.Escape).MustDo()
   } else { // 40% 点击遮罩层
       mask.Click(proto.InputMouseButtonLeft, 1)
   }
   ```

#### 📚 新增文档

- `docs/BEHAVIOR_OPTIMIZATION.md` - 详细的优化原理和技术说明
- 更新 `docs/BROWSE_FEATURE.md` - 添加 v2.0 优化说明
- 更新 `README.md` - 添加优化参数说明

#### 🎯 效果对比

**优化前（v1.0）**
- ⚠️ 滚动连续且快速（0.3-0.8s）
- ⚠️ 停留时间偏短（0.8-2s）
- ⚠️ 无回滚行为
- ⚠️ 被检测风险：中等

**优化后（v2.0）**
- ✅ 分段滚动，更自然（0.6-2.5s）
- ✅ 停留时间合理（3-6s）
- ✅ 7-18% 回滚概率
- ✅ 被检测风险：较低

#### 🔄 兼容性

- ✅ 完全向下兼容，无需修改现有调用代码
- ✅ API 参数保持不变
- ✅ 自动应用新的优化逻辑

#### 💡 使用建议

**轻度浏览（新账号）**
```json
{
  "duration": 5,
  "click_probability": 20,
  "interact_probability": 30
}
```

**中度浏览（日常使用）**
```json
{
  "duration": 10,
  "click_probability": 40,
  "interact_probability": 50
}
```

**活跃浏览（老账号）**
```json
{
  "duration": 15,
  "click_probability": 60,
  "interact_probability": 70
}
```

---

## [v1.0] - 2025-10-14

### 🎉 初始版本

#### ✨ 核心功能

- ✅ 小红书图文笔记发布
- ✅ 小红书视频笔记发布
- ✅ 搜索笔记功能
- ✅ 点赞、收藏、评论功能
- ✅ 获取推荐流、热门笔记
- ✅ 查看笔记详情
- ✅ 用户资料查询
- ✅ 浏览推荐页（基础版）

#### 🔌 集成支持

- Claude Code CLI
- Cursor
- VSCode
- AnythingLLM
- n8n
- CherryStudio

#### 📦 部署方式

- Docker 部署
- macOS 原生部署
- Windows 部署

#### 🔐 安全特性

- Cookie 管理
- 登录状态检测
- 防检测基础机制

---

## 版本说明

### 版本号规则

- **主版本号**：重大功能更新或架构变更
- **次版本号**：新功能添加或重要优化
- **修订号**：Bug 修复和小改进

### 图标说明

- 🎉 重大更新
- ✨ 新增特性
- 🔧 优化改进
- 🐛 Bug 修复
- 📝 文档更新
- 🔒 安全修复
- ⚠️ 重要提示
- 💡 使用建议

---

## 反馈和建议

如有问题或建议，欢迎：
- 提交 Issue
- 发起 Pull Request
- 联系项目维护者

**最后更新**：2025-10-15

