# Third-party components

- [tun2socks](https://github.com/xjasonlyu/tun2socks) v2.7.0, MIT, verified against the pinned module's LICENSE file. Only its in-process core stack is reused. Its original copyright notice is retained in LICENSE alongside this project's notice.
- [gVisor](https://github.com/google/gvisor), Apache-2.0. Exact netstack version is pinned in go.mod/go.sum.
- [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) and [golang.org/x/net](https://pkg.go.dev/golang.org/x/net), Go BSD-style licenses; exact versions are pinned.
- [WinDivert 2.2.2-A](https://github.com/basil00/WinDivert/releases/tag/v2.2.2), LGPLv3/GPLv2 dual licensing. This repository includes its accompanying license and official archive/file hashes, but no DLL or driver binaries. Fetch-Runtime.ps1 downloads the official release for local use. Source is available from the upstream repository/tag.
- [Go](https://go.dev/), BSD-style license. The compiler/toolchain is not included.

Upstream projects do not endorse or provide support for this integration. Redistribution of separately built binaries and bundled dependencies requires satisfying their applicable licenses.
