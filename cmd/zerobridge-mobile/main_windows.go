package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Luase233/zerobridge-mihomo/internal/mobile"
	"github.com/Microsoft/go-winio"
	"github.com/skip2/go-qrcode"
)

func main() {
	configPath := flag.String("config", `C:\ZeroTierGateway\mobile.json`, "Local mobile control configuration")
	check := flag.Bool("check", false, "Check the named-pipe controller without changing it")
	flag.Parse()
	b, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	var cfg mobile.Config
	if err = json.Unmarshal(b, &cfg); err != nil {
		log.Fatal(err)
	}
	if err = cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return winio.DialPipeContext(ctx, cfg.Pipe)
	}, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	controller := mobile.Controller{Client: &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}
	if *check {
		state, e := controller.Read(context.Background())
		if e != nil {
			log.Fatal(e)
		}
		fmt.Printf("Controller available; mode=%s; groups=%d\n", state.Mode, len(state.Groups))
		return
	}
	server, err := mobile.New(cfg, controller)
	if err != nil {
		log.Fatal(err)
	}
	if err := qrcode.WriteFile("http://"+cfg.Listen+"/#token="+url.QueryEscape(cfg.Token), qrcode.Medium, 360, filepath.Join(filepath.Dir(cfg.StateFile), "pairing.png")); err != nil {
		log.Fatal("Cannot create local pairing QR: ", err)
	}
	var listener net.Listener
	for {
		listener, err = net.Listen("tcp4", cfg.Listen)
		if err == nil {
			break
		}
		log.Print("Waiting for configured ZeroTier address/port")
		time.Sleep(5 * time.Second)
	}
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if e := server.Refresh(ctx); e != nil {
				log.Printf("Desktop synchronization unavailable: %v", e)
			}
			cancel()
			time.Sleep(5 * time.Second)
		}
	}()
	log.Printf("ZeroBridge Mobile v0.3.0 listening on %s", cfg.Listen)
	srv := &http.Server{Handler: server, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	log.Fatal(srv.Serve(listener))
}
