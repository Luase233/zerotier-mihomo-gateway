# Windows desktop and tray / Windows 界面与托盘

[简体中文 README](README.md) · [English README](README.en.md)

## 简体中文

v0.3.0 包含独立的 Windows Forms 界面，使用 Windows 自带 .NET Framework 4.x（验证环境为 Windows 11）。桌面界面本身不新增网络监听端口，也不替换正在运行的网关进程。可选的[手机控制服务](MOBILE.md)会单独监听配置的 ZeroTier 私有地址。

- **运行面板：** 心跳状态、活动连接、收发包速率、当前代理进程累计拒绝/错误计数。
- **图表：** 切换收发包速率和活动连接，最多显示打开界面后最近 10 分钟的数据。网关约每 5 秒写入统计，界面每 2 秒刷新。这里是包/秒，不是 Mbps；拒绝计数不等于丢包率。
- **托盘：** 右下角通知区域的桥形图标；双击打开面板，右键启动、停止、打开日志或退出界面。Windows 可能将图标放在 `^` 隐藏图标区域，可自行拖到常显区域。
- **关闭：** 关闭或最小化窗口会收起到托盘；“退出界面”仅退出界面，网关继续运行。
- **语言：** 右上角切换简体中文 / English，偏好保存在当前用户的 `%LOCALAPPDATA%\ZeroBridgeMihomo\language.txt`。
- **启停保护：** 先在手机关闭 Enable Default Route，再勾选确认框并通过 Windows UAC。操作调用现有 `scripts\Manage.ps1`；停止可能恢复原 NAT，启动仍受脚本预检约束。完成后检查状态，再决定是否开启客户端 Default Route。界面不会自动设置客户端。

“心跳正常”只说明本地 supervisor 状态新鲜，不等于已通过端到端联网检测。状态或统计超过 20 秒未更新时，界面显示异常或停止显示速率。图表不持久化，进程更换会重置；没有新样本时不会伪造流量。

### 构建、运行与登录自启

先按 README 配置并安装网关及其管理脚本。界面安装器只添加桌面程序和当前用户的登录任务，不配置网络、不替换机器配置或启停脚本。

```powershell
& .\scripts\Test-Desktop.ps1
& .\scripts\Build-Desktop.ps1
& .\bin\zerobridge-desktop.exe --root C:\ZeroTierGateway

# 管理员 PowerShell；更新前先从托盘退出旧界面。
& .\scripts\Install-Desktop.ps1 -GatewayRoot C:\ZeroTierGateway
```

安装后可运行 `C:\ZeroTierGateway\bin\zerobridge-desktop.exe`。本次发布提供源码，需要先执行构建命令；安装器要求已有网关安装，不是完整网络安装器。

「手机控制」按钮显示配对二维码、连接信息和最近 20 条指令；主面板显示最新指令结果。二维码包含访问密钥，不要将真实二维码或 `mobile.json` 提交到仓库。详见[手机控制说明](MOBILE.md)。

计划任务 `ZeroBridgeMihomoTray` 使用当前用户、交互登录、普通权限和无限运行时间；下次登录时以托盘方式启动。若管理员凭据属于另一账户，请显式传入 `-UserId '计算机名\实际用户'`。原 `ZeroTierGateway` SYSTEM 开机任务和 High 优先级保持独立。界面不能在用户登录前显示。

可使用 `--minimized` 直接收起到托盘、`--readonly` 禁用启停。重复启动相同网关目录的界面会唤醒已有窗口。删除 `ZeroBridgeMihomoTray` 计划任务即可取消界面登录自启，不影响网关任务。

本项目仅提供本地转发与协议转换，不提供 VPN、代理节点、账号或订阅服务。参见 [DISCLAIMER.md](DISCLAIMER.md)。

## English

v0.3.0 includes a standalone Windows Forms dashboard using Windows' .NET Framework 4.x (validated on Windows 11). The desktop UI itself opens no network listener and does not replace the running gateway process. The optional [mobile service](MOBILE.md) separately listens on the configured private ZeroTier address.

- **Dashboard:** supervisor heartbeat, active connections, packet rates, and the current proxy process's cumulative rejected/error counters.
- **Charts:** switch between packet rates and active connections, retaining up to ten minutes collected while the UI runs. Gateway statistics arrive about every five seconds; the UI polls every two seconds. Rates are packets/s, not Mbps; rejected packets are not a packet-loss measurement.
- **Tray:** a bridge icon in the notification area. Double-click to open; right-click for Start, Stop, logs and Exit UI. Windows may place it under the `^` overflow menu; users can drag it into the visible area.
- **Close:** closing or minimizing hides to the tray. Exit UI leaves the gateway running.
- **Language:** switch Chinese / English at the upper right. The preference is stored in `%LOCALAPPDATA%\ZeroBridgeMihomo\language.txt`.
- **Start/Stop:** first disable Enable Default Route on the phone, check the confirmation box, and approve Windows UAC. The UI calls existing `scripts\Manage.ps1`; stopping may restore original NAT and starting remains subject to its preflight checks. Check status before enabling the client's default route again. The UI does not configure the client.

A healthy heartbeat describes fresh local supervisor state, not an end-to-end connectivity check. State or statistics older than twenty seconds are flagged or withheld from rate display. History is not persisted and resets when the proxy changes; missing samples are never represented as invented traffic.

### Build, run and start at logon

Install and configure the gateway and management scripts following the README first. The desktop installer adds only its executable and the user's logon task; it does not configure networking or replace machine configuration or management scripts.

```powershell
& .\scripts\Test-Desktop.ps1
& .\scripts\Build-Desktop.ps1
& .\bin\zerobridge-desktop.exe --root C:\ZeroTierGateway

# Administrator PowerShell; exit the previous UI from its tray before updating.
& .\scripts\Install-Desktop.ps1 -GatewayRoot C:\ZeroTierGateway
```

After installation, run `C:\ZeroTierGateway\bin\zerobridge-desktop.exe`. This release provides source, so build it first. The installer requires an existing gateway installation and is not a complete network installer.

The Mobile control button shows the pairing QR, connection information and the last twenty commands; the main dashboard shows the latest result. The QR contains an access credential: do not commit a real QR or `mobile.json`. See the [mobile guide](MOBILE.md).

The `ZeroBridgeMihomoTray` task runs as the user at interactive logon, with limited privileges and no time limit, starting minimized. If elevation uses a different administrator account, supply `-UserId 'COMPUTER\actual-user'`. The separate SYSTEM `ZeroTierGateway` boot task retains its High priority. The UI cannot appear before a user logs on.

Use `--minimized` to start in the tray, or `--readonly` to disable controls. Launching again for the same root brings the existing window forward. Delete the `ZeroBridgeMihomoTray` task to disable UI autostart without affecting the gateway task.

This project provides local forwarding and protocol conversion only, not VPN services, proxy nodes, accounts or subscriptions. See [DISCLAIMER.md](DISCLAIMER.md).
