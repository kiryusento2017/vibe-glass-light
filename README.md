# Claude Code Light 🚦

Windows 桌面液态玻璃红绿灯挂件，实时显示 [Claude Code](https://claude.ai/code) 工作状态。**上班 vibe coding 时余光一扫就知道 AI 在干嘛。**

> 单文件 `.exe`，扔 U 盘即插即用，不装任何运行时。

## 效果

一块悬浮在桌面上的**液态玻璃胶囊**，透过它能看到真实桌面被折射扭曲。中间三盏红绿灯：

| 灯 | 含义 | Claude Code 状态 |
|---|---|---|
| 🔴 红灯闪烁 | 正在执行工具 | PreToolUse |
| 🟡 黄灯闪烁 | 正在思考 | UserPromptSubmit / PostToolUse |
| 🟢 绿灯常亮 | 空闲等待 | Stop |
| ⚫ 全灭 | 未运行 | Claude Code 进程不在 |

按住玻璃会**果冻形变**（横向变窄、纵向拉长），拖得越快越窄，松手 Q 弹回弹——所有弹簧参数可在 `glass-tuning.json` 里热调。

## 安装与使用

1. 从 [Releases](../../releases) 下载 `claude-traffic-light.exe`
2. 双击运行（首次启动自动在 `~/.claude/settings.json` 里安装状态 hook）
3. 打开 Claude Code，开始 vibe coding
4. 右键挂件 → 菜单（隐藏/固定位置/退出）

**如何调参**：编辑 exe 同目录的 `glass-tuning.json`（首次运行自动生成），保存即实时生效，无需重启。

## 架构

```
main.go             入口：单实例互斥 → 加载配置 → 安装 hook → 创建窗口 → 启动监测
hookinstall.go      把状态 hook 幂等合并进 ~/.claude/settings.json
config/             配置读写（config.json 窗口位置 + glass-tuning.json 视觉热重载）
state/              四态枚举（灰/绿/黄/红）及优先级
watcher/            每 100ms 读 hook 状态文件 + 每 3s 检测 claude.exe 进程
ui/
  window.go           DComp 透明置顶窗、消息循环、自接管鼠标拖动、弹簧形变状态机
  render.go           D3D11 渲染管线：device/swapchain/shader 编译 + 每帧绘制
  glass.hlsl          像素 shader：超椭圆 SDF 限定形状、shuding 折射核、三灯叠加
  capture.go          Desktop Duplication 抓取桌面纹理（GPU 常驻）
  com.go              D3D11/DXGI/DComp COM 绑定
  win32.go            Win32 API 绑定与常量
  physics.go          二阶弹簧物理（每帧 Euler 积分，驱动形变）
```

### 状态探测：Claude Code Hooks

不轮询 transcript。靠 4 个生命周期 hook 实时推送：

```
UserPromptSubmit → 黄（思考中）
PostToolUse      → 黄（思考中）
PreToolUse       → 红（执行中）
Stop             → 绿（空闲）
```

Hook 以 exec form 安装（`command`=exe 路径 + `args`，直接 spawn 不经 shell），每个 hook 写状态文件 `~/.claude/agent-light-state`，watcher 每 100ms 读取。

自动灭灯：每 3s 用 `CreateToolhelp32Snapshot` 检测 `claude.exe` 进程，不在则切灰色。

### 渲染管线

```
Desktop Duplication 抓整屏桌面纹理（GPU 常驻）
  → HLSL 超椭圆 SDF + shuding 折射核（中心清晰、边缘强折射）
  → box blur / 高光 / 反射亮边 / 投影（可热调）
  → DXGI swapchain → DirectComposition 透明置顶窗
WDA_EXCLUDEFROMCAPTURE 排除自身捕获，断开反馈循环
```

### 弹簧形变

二阶弹簧（刚度 K + 阻尼 C）每帧 Euler 积分，主线程鼠标事件设目标，渲染线程每帧推进：

- **按下**：横向窄 + 纵向长（`pressX/pressY`）
- **稳态**：稍窄稍高（`steadyX/steadyY`）
- **拖动**：按速度叠加变窄（`dragK`，下限 `dragMin`）
- **松手**：回到稳态 + 基于速度的过冲冲量（`releaseImpulse`）

所有参数在 `glass-tuning.json` 中可热调。

## 构建

需要 Go 工具链 + Windows：

```powershell
# 普通构建（带控制台，调试用）
go build -o claude-traffic-light.exe .

# 发布构建（无控制台窗口）
go build -ldflags="-H windowsgui" -o claude-traffic-light.exe .

# 测试
go test ./...
```

## 技术栈

Go + D3D11 + DirectComposition + HLSL（无 WebView2、无 Electron、无 CGO）

## 许可

MIT
