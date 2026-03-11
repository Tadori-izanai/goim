package main

import (
	"flag"
	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/Terry-Mao/goim/internal/gateway/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Terry-Mao/goim/internal/gateway/conf"
	log "github.com/golang/glog"
)

func main() {
	flag.Parse()
	if err := conf.Init(); err != nil {
		panic(err)
	}

	log.Infof("goim-api [addr: %s] start", conf.Conf.HTTPServer.Addr)

	srv := gateway.New(conf.Conf)
	httpSrv := http.New(conf.Conf.HTTPServer, srv)

	// signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	for {
		s := <-c
		log.Infof("goim-api get a signal %s", s.String())
		switch s {
		case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
			srv.Close()
			httpSrv.Close()
			log.Infof("goim-api exit")
			log.Flush()
			return
		case syscall.SIGHUP:
		default:
			return
		}
	}
}
