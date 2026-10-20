// Command ragentd 是运行在红米 AX6000 上的控制代理：
// 提供上网方式管理、终端管理、App 访问限制，并通过局域网 HTTP/WebSocket
// 与云端中继向 iOS App 暴露控制接口。
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ax6000/ragent/internal/api"
	"github.com/ax6000/ragent/internal/cloudlink"
	"github.com/ax6000/ragent/internal/config"
	"github.com/ax6000/ragent/internal/filter"
	"github.com/ax6000/ragent/internal/hosts"
	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/model"
)

func main() {
	cfg, st, err := config.Load()
	if err != nil {
		logx.Errorf("main", "load config: %v", err)
		os.Exit(1)
	}
	logx.SetLevelString(cfg.LogLevel)
	logx.Infof("main", "ragentd starting device_id=%s listen=%s", cfg.DeviceID, cfg.Listen)

	// 1) 过滤引擎（DPI + 域名），并启动内置 DNS 代理
	fm := filter.New(st, cfg.DNSPort)
	if err := fm.Start(); err != nil {
		logx.Errorf("main", "dns proxy: %v", err)
	}

	// 2) 事件总线
	hub := api.NewHub()

	// 3) 终端管理器（事件回调在 server 创建后生效）
	var srv *api.Server
	hm := hosts.New(st, fm, func(ev model.Event) {
		if srv != nil {
			srv.OnHostEvent(ev)
		}
	})
	fm.SetIPResolver(hm.LookupByIP)

	// 4) 本地 API
	srv = api.New(cfg, st, hm, fm, hub)

	// 5) 云端中继
	cloud := cloudlink.New(cfg, srv)
	srv.Notify = cloud.RegisterNotify
	hub.Forward = func(ev model.Event) {
		if err := cloud.SendEvent(ev); err != nil {
			logx.Debugf("cloud", "send event: %v", err)
		}
	}

	// 6) 恢复开机前已保存的过滤规则
	if err := fm.RestoreOnBoot(); err != nil {
		logx.Warnf("main", "restore rules: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 立即扫描一次，再进入周期扫描
	hm.ScanOnce()
	go hm.Loop(ctx, cfg.ScanInterval)

	if cfg.CloudEnabled && cfg.CloudURL != "" {
		go cloud.Run()
	} else {
		logx.Infof("main", "云端中继未启用，仅支持局域网管理")
	}

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		logx.Infof("main", "api listening on %s", cfg.Listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logx.Errorf("main", "http server: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logx.Infof("main", "shutting down")

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	cloud.Close()
	fm.Stop()
}
