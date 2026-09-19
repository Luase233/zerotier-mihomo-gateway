# 项目使用声明 / Scope of use

[简体中文 README](README.md) · [English README](README.en.md)

## 简体中文

**ZeroBridge Mihomo 仅提供本地 IPv4 流量转发与协议转换功能。** 它把指定客户端的转发数据包转换成 SOCKS5 连接，交给使用者已经配置的代理程序处理。

- 本项目不提供、不运营也不销售 VPN 服务、代理服务、代理节点、订阅、账号、带宽或网络出口；不附带任何上述服务的访问凭据。
- 使用者须自行取得、授权并配置 ZeroTier 网络、Mihomo / Clash 及上游服务。相关网络可达性、服务质量、账号和费用由使用者与相应服务提供方处理。
- 本项目不承诺匿名性、隐私保护、绕过访问限制或任何特定网络访问结果。第三方组件的加密或网络能力不等同于本项目提供 VPN 服务。
- 使用者应仅在拥有或已获授权的设备和网络中使用，并遵守适用法律、网络管理要求及第三方服务条款，不得用于未获授权或违法的网络活动。
- 当前版本为实验性软件，已知边界见 README 与 VALIDATION.md，包括 IPv6 不受代理、进程或驱动故障以及启动阶段没有持久阻断保证。
- 本项目按 [MIT 许可证](LICENSE) 以“现状”提供；许可证中的无担保和责任条款适用。第三方依赖保留各自许可证。本声明说明功能与服务范围，不代表额外担保，也不替代许可证条款。
- 本项目与 ZeroTier、Mihomo、Clash、WinDivert、tun2socks 等上游项目无官方隶属或背书关系。相关名称仅用于描述兼容组件和技术用途。

## English

**ZeroBridge Mihomo provides local IPv4 traffic forwarding and protocol conversion only.** It converts the selected client's forwarded packets into SOCKS5 connections for a proxy the user has already configured.

- This project does not provide, operate or sell VPN services, proxy services, proxy nodes, subscriptions, accounts, bandwidth or Internet egress. No credentials for such services are included.
- Users must obtain, authorize and configure their own ZeroTier network, Mihomo / Clash instance and upstream services. Connectivity, service quality, accounts and charges are matters between users and the relevant providers.
- The project makes no promise of anonymity, privacy protection, bypassing access restrictions or any particular network-access result. Encryption or networking capabilities of third-party components do not mean this project provides a VPN service.
- Use only on devices and networks you own or are authorized to administer, in compliance with applicable laws, network policies and third-party service terms. Do not use it for unauthorized or unlawful network activity.
- The current version is experimental. README and VALIDATION.md describe limitations including the absence of IPv6 proxying and persistent blocking guarantees during process/driver failures or startup.
- The project is provided “as is” under the [MIT License](LICENSE), including its warranty disclaimer and liability provisions. Third-party dependencies retain their own licenses. This statement explains functional and service scope; it creates no additional warranty and does not replace the license terms.
- This project is not officially affiliated with or endorsed by ZeroTier, Mihomo, Clash, WinDivert, tun2socks or other upstream projects. Their names identify compatible components and technical purposes only.
