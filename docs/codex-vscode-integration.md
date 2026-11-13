# Codex VS Code 远控接入调查

调查时间：2026-11-12。此文是已验证入口与后续方案，尚未实现 VS Code Provider 或修改用户的 VS Code 设置。

## 结论

本机 `openai.chatgpt` 扩展 `26.1106.51428` 没有发现通过环境变量指定本地 app-server WebSocket 后端的入口。不能把 Codex App 的 `CODEX_APP_SERVER_WS_URL` 直接套到 VS Code 扩展上。

扩展提供 `chatgpt.cliExecutable`，可作为 stdio 桥接程序的接入点。官方将其标为开发用途，并提示覆盖捆绑可执行文件可能影响功能；它不构成稳定的第三方远控接口。

## 证据

官方文档：[Developer settings](https://learn.chatgpt.com/docs/developer-settings) 的 Editor settings reference 列出 `chatgpt.cliExecutable`；该设置属于 VS Code 的 `chatgpt.*` 配置，不属于 Codex `config.toml`。[Environment variables](https://learn.chatgpt.com/docs/config-file/environment-variables) 未列出 IDE WebSocket 后端地址配置。

本机检查文件：

- `/Users/levi/.vscode/extensions/openai.chatgpt-26.1106.51428-darwin-arm64/package.json`
- `/Users/levi/.vscode/extensions/openai.chatgpt-26.1106.51428-darwin-arm64/out/extension.js`

`extension.js` SHA-256：`30fb21db73fff04c505dc1463dc562205c2321fea52a615d8c2658fd356d157e`。

扩展的 `startCodexProcess()` 固定传入以下参数：

```text
-c features.code_mode_host=true
-c features.windows_sandbox_service=true
app-server --analytics-default-enabled
```

其 `VHe()` 函数使用 `child_process.spawn`，固定 `stdio: ["pipe", "pipe", "pipe"]`；`NK()` 优先读取 `cliExecutable`，否则使用扩展捆绑的 Codex。扩展发送 `initialize` 到子进程 stdin，从 stdout 解析 JSONL，向 Webview 报告本地传输为 `stdio`。

在扩展 JavaScript 中未发现 `CODEX_APP_SERVER_WS_URL`、`--listen` 或本地 app-server URL 设置；捆绑的 Codex 二进制也不包含 `CODEX_APP_SERVER_WS_URL` 字符串。这些仅是本版本的实现证据，不能证明未来版本不会增加入口。

## 隔离验证

1. 使用临时 `CODEX_HOME` 与临时工作目录启动扩展捆绑的 Codex，参数与扩展一致；设置 `CODEX_APP_SERVER_WS_URL` 指向测试 TCP listener。通过 stdin 发送 `initialize`，成功从 stdout 得到结果，测试 listener 没有收到连接。没有发送模型 turn，也没有读取或恢复真实会话。
2. 在 Node VM 中运行从扩展原样提取的启动函数与路径选择函数，模拟 VS Code 设置和 `spawn`。`chatgpt.cliExecutable` 成功选择测试桥接程序，参数保持传入值，stdio 仍为三条管道。环境变量虽被继承，但不会改变扩展的传输选择。

第二项验证了替换入口，不是完整 VS Code 扩展端到端验证。没有修改用户设置或重启用户的 VS Code / Codex App。

## 建议的初始化方案

在用户执行“启用 VS Code 远控”时，配置一个本机 Go 可执行程序：

```json
{
  "chatgpt.cliExecutable": "/absolute/path/to/agenthub-codex-bridge"
}
```

这是方案示意；当前项目尚未提供该用途的可执行程序。Windows 需要原生 `.exe`。

```text
VS Code 扩展 ── stdio JSONL ── Go 桥接程序 ──┐
                                         ├── 同一个窗口对应的 app-server
Agent Hub VS Code Provider ── 本机连接 ─────┘
```

桥接程序需保留扩展的启动配置，使用匹配版本的 Codex 创建可连接的本地后端，并把扩展的 stdio 流转发过去。向 Agent Hub 登记准确的后端实例、端点和生命周期。持久设置生效后，用户手动打开 VS Code 也会经过这个入口；现有窗口需重新加载扩展后才能接入。不能借此自动接管之前已经启动的私有 stdio 进程。

每个 VS Code 窗口保留独立后端实例。App、CLI、VS Code 可以在 Agent Hub 聚合展示，但不根据相同 `CODEX_HOME` 或相同会话创建来源推断它们属于同一个运行实例。

接入前需要验证：

- 扩展初始化、上下文相关 RPC 和协议版本兼容；升级后重新发现捆绑 CLI。
- 多连接的事件订阅，以及外部发起 turn 时扩展是否更新 UI。
- 审批请求按连接路由并保证只处理一次，不能简单广播所有 RPC。
- 多窗口、扩展重载、桥接退出时的实例清理和原设置恢复。
- VS Code Remote SSH / WSL / Dev Container 场景中，桥接程序、Provider 与后端所在主机的一致性；本机设置不会自动提供远端 Agent Hub。

本机端点只向本机开放；移动端与 Web 继续通过 Agent Hub 配对后的 WebRTC / 中继访问，不直接暴露 app-server 到公网。

## 与会话隔离工作的关系

会话创建来源仅作展示；可控性必须依据实际接入的后端判断。共享的 `thread/list` 历史不能直接作为当前后端正在控制的会话清单。当前 MVP 的宽泛历史列表仍需改造，不能把这份调查文档视为已完成隔离。

未接入的 VS Code 实例应标为“已安装、需初始化后接入”，不将其共享历史当作 CLI 当前会话操作。历史会话跨后端恢复应是显式操作，并区别于控制原客户端的运行中会话。
