# Validation scope

This is an experimental implementation, not a security certification.

Validated in the original Windows deployment:

- Go unit/integration tests and go vet.
- Independent client netstack → gateway → mock SOCKS → generated packets: TCP, UDP, DNS rewriting, TCP retransmission, 9KB fragmented UDP at MTU1280, flow limits/expiry, and bounded close.
- Official WinDivert DLL checksum helper for IPv4 fragments, without opening a driver handle in the tests.
- Official runtime hash checks and valid Windows driver Authenticode signature.
- Actual driver capture smoke, guard/proxy readiness and unchanged Windows default routes.
- Existing Mihomo SOCKS TCP DNS, UDP DNS and HTTPS probe.
- iPhone cellular IPv4 HTTPS exit matched Mihomo; user confirmed video playback.
- On that cellular connection, IPv6-only endpoint worked with Default Route OFF and failed with ON. This is not IPv6 proxy support or a persistent IPv6 blocking guarantee.
- Native PowerShell5.1 lifecycle tests; actual SYSTEM task startup, High process priority and proxy restart while the independent guard remained alive.
- Supervisor fixtures: fresh startup, unavailable upstream with guard retained, replacement guard before retiring the old proxy, and previous-boot PID reuse avoidance.

Desktop/mobile validation for v0.3.0:

- Eleven desktop model checks, including stale heartbeat, partial JSON log tails, path boundaries and rate reset on process changes.
- Mobile server tests cover token/IP/Host/Origin boundaries, restricted endpoints, selector membership, duplicate commands, durable command records, readback confirmation and unconfirmed results.
- A 390×844 mobile browser viewport passed pairing, remembered access, search, confirmation cancellation and no horizontal overflow, with no page-script errors.
- The real local named-pipe controller was exercised by reselecting the existing node. The response and desktop audit agreed; the desktop UI displayed the confirmed command.
- The user subsequently confirmed that a physical iPhone could control proxy selection successfully. This is user-reported acceptance, not a comprehensive test across iOS versions.
- Mobile and tray logon tasks were installed with limited user privileges; the existing gateway proxy and guard remained running during installation.

Still not established:

- Long-term reliability, independent security audit, sustained adversarial/resource-exhaustion testing.
- Every client UDP application or actual client UDP large-packet path. Flow counts and TCP-capable video playback alone do not prove UDP replies.
- Actual Windows reboot acceptance; the boot task was manually invoked and its trigger verified.
- Persistent fail-closed behavior across both process failures, driver failure, startup gaps, reboot or IPv6 routing changes.
- General installation across arbitrary NAT, interfaces, proxy configurations or multi-user hosts.

Operational logs, machine identities, live IP addresses and original NAT backups are intentionally excluded. Sanitized test fixtures use example private addresses. Publication checks rebuild and test the source copy without changing the running installation.
