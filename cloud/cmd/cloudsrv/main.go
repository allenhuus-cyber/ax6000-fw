// Command cloudsrv 是云端中继服务：
// 维持路由器长连接、转发 App 的控制请求、把设备事件转成 APNs 推送。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ax6000/cloud/internal/api"
	"github.com/ax6000/cloud/internal/apns"
	"github.com/ax6000/cloud/internal/config"
	"github.com/ax6000/cloud/internal/hub"
	"github.com/ax6000/cloud/internal/store"
)

func main() {
	cfgPath := flag.String("config", "/etc/ax6000-cloud/config.json", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		panic(err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		panic(err)
	}
	h := hub.New()

	// APNs 配置不完整时仅关闭推送，不影响中继功能
	push, err := apns.New(apns.Config{
		TeamID:  cfg.APNS.TeamID,
		KeyID:   cfg.APNS.KeyID,
		KeyPath: cfg.APNS.KeyPath,
		Topic:   cfg.APNS.Topic,
		Sandbox: cfg.APNS.Sandbox,
	})
	if err != nil {
		logf("APNs 未启用: %v", err)
	} else {
		logf("APNs 已启用 topic=%s sandbox=%v", cfg.APNS.Topic, cfg.APNS.Sandbox)
	}

	srv := api.New(cfg, st, h, push)
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		logf("cloudsrv listening on %s", cfg.Listen)
		var err error
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			err = httpSrv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logf("http server: %v", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logf("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	_ = st.Save()
}

func logf(format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf(ts+" "+format+"\n", args...)
}
