# 验证入口与记录

当前验证命令和范围选择见 [工程验证规范](engineering/validation.md)，持续集成及发布产物见 [CI 与 Release](ci-and-releases.md)。两个 Go module 分别运行检查；客户端统一由根目录 `scripts/build.sh` 构建，默认输出到根目录 `dist/`。

## 当前专题记录

- [Codex IPC 实现验收](research/codex-ipc-implementation-2026-11-17.md)：owner/follower、只读回退、写入去重和生命周期边界。
- [Codex IPC 验证](research/codex-ipc-validation-2026-11-17.md)：隔离检查证据与真实 App 验证限制。
- [IPC 与同步后续方案](research/codex-ipc-and-sync-followup-2026-11-17.md)：已完成与待验证内容分别记录。
- [性能原始数据](platform/benchmarks/2026-11-14-macos-memory.json)：特定构建与负载下的历史测量，不代表所有平台。
- [TCP P2P](tcp-p2p.md)：隔离 NAT 测试方法和连接限制。

已移除客户端原型的构建与验收记录，不能将其测试结果算作当前 NSGo 客户端覆盖。研究记录按各自日期解释；交叉构建、模拟器和假 Agent 互通不代替 Windows/Android/iOS 真机及真实厂商集成验证。

## 设备服务

Go 和 Worker 都需要按 [一致性规范](engineering/service-parity.md) 验证公共契约。Go 默认 HTTP/API/WebSocket 为 26330/TCP，TCP/UDP 探测均为 26331；端口可配置。服务包检查必须包含 Web 资产、身份持久化、探测和失败后恢复，不能只检查二进制启动。
