package settings

import "testing"

func TestRejectUnsafeConfig(t *testing.T) {
	c := Config{SourceIP: "10.147.20.2", WindowsIP: "10.147.20.1", InterfaceIndex: 18, Proxy: "127.0.0.1:7897", DNS: "1.1.1.1:53", MTU: 1280, MaxFlows: 256, IdleTimeoutSeconds: 120, WinDivertDLL: "runtime/WinDivert.dll"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.SourceIP = "0.0.0.0" }, func(c *Config) { c.SourceIP = c.WindowsIP }, func(c *Config) { c.Proxy = "8.8.8.8:1080" }, func(c *Config) { c.DNS = "127.0.0.1:53" }, func(c *Config) { c.MTU = 65535 }} {
		bad := c
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatalf("unsafe config accepted: %+v", bad)
		}
	}
}
