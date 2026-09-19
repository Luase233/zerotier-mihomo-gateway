# Changelog

[简体中文 README](README.md) · [English README](README.en.md)

## Unreleased / 未发布

- 项目更名为 **ZeroBridge Mihomo**，仓库与 Go 模块路径同步调整。
- 完善中英文 README、双语开机自启文档，以及“不提供 VPN 或代理服务”的功能范围声明。
- Renamed the project to **ZeroBridge Mihomo**, including repository and Go module paths.
- Added complete Chinese/English READMEs, bilingual autostart guides and an explicit statement that the project provides no VPN or proxy service.

## v0.1.0 — 2026-09-20

首个实验性预发布版本，尚不承诺接口兼容性或长期稳定性。

- WinDivert NETWORK_FORWARD 捕获指定客户端的 IPv4 转发流量。
- 进程内 TCP/IP 栈转换 TCP/UDP 为本机 Mihomo SOCKS5 连接，无 TUN 网卡。
- DNS 转发改写、TCP 重传、IPv4 分片、连接数和空闲超时限制。
- 独立 guard、启动预检、NAT 备份及恢复脚本。
- 可选 SYSTEM 开机任务、High 优先级与代理进程恢复。
- 示例配置、运行库下载与校验、协议测试和运维说明。

已验证本地协议测试、Windows 网关、手机蜂窝 IPv4 出口与视频，以及计划任务的进程恢复。IPv6 代理、持久防泄漏、真实重启验收与长期稳定性不在本版本保证范围内。

发布源码，不附带真实配置、日志、恢复备份、编译产物或第三方驱动二进制。

### English

First experimental prerelease; API compatibility and long-term reliability are not guaranteed.

- Capture one configured client's IPv4 forwarding traffic with WinDivert NETWORK_FORWARD.
- Convert TCP/UDP through an in-process TCP/IP stack to existing local Mihomo SOCKS5, without a TUN adapter.
- DNS rewriting, TCP retransmission, IPv4 fragmentation, flow limits and idle timeouts.
- Independent guard, startup checks, and NAT backup/restoration scripts.
- Optional SYSTEM startup task, High priority and proxy-process recovery.
- Example configuration, verified runtime download, protocol tests and operational documentation.

Local protocol tests, Windows forwarding, cellular IPv4 exit/video and scheduled-task process recovery were verified. IPv6 proxying, persistent leak prevention, actual reboot acceptance and long-term stability are not guaranteed. The source release excludes real configurations, logs, recovery backups, build outputs and third-party driver binaries. This project provides local forwarding only; it does not operate or supply VPN or proxy services.
