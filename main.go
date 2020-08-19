package main

import (
	"context"
	log "github.com/cihub/seelog"
	"github.com/jinzhu/configor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robfig/cron/v3"
	"github.com/takama/daemon"
	"net/http"
	"os"
	"sync"
	"time"
)

type Service struct {
	daemon.Daemon
}

var (
	config = Config{}
	gRWLock *sync.RWMutex
	metrics [] prometheus.Metric
)

func init() {
	gRWLock = new(sync.RWMutex)
	err := configor.Load(&config, "./config/config.yml")
	if err != nil{
		log.Error("Error parsing config file: %s", err)
	}
	defer log.Flush()
	logger,err :=log.LoggerFromConfigAsFile("./config/logconf.xml")
	if err != nil{
		log.Errorf("parse config.xml err: %v",err)
	}
	log.ReplaceLogger(logger)
}

func remoteIPMIHandler(w http.ResponseWriter, r *http.Request) {
	registry := prometheus.NewRegistry()
	remoteCollector := collector{}
	registry.MustRegister(remoteCollector)
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func flush()  {
	var targetMetrics []prometheus.Metric
	wg := sync.WaitGroup{}
	wg.Add(len(config.Targets))
	for i := 0; i < len(config.Targets); i++ {
		go func(i int) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second * time.Duration(config.Global.TimeOut))
			defer cancel()
			targetMetrics = append(targetMetrics,IpmiCollect(config.Targets[i])...)
			select {
			case <-ctx.Done():
				log.Error("收到超时信号,采集退出", config.Targets[i].Host)
			default:
				log.Info(config.Targets[i].Host,":指标采集完成",len(targetMetrics))
			}
			wg.Done()
		}(i)
	}
	wg.Wait()

	//统一写操作
	//gRWLock.Lock()
	metrics = []prometheus.Metric{}
	metrics = append(metrics,targetMetrics...)
	log.Info("metrics:",len(metrics))
	//gRWLock.Unlock()
}

func (service *Service) Manage ()  {
	//Create a cron manager
	log.Info("Create a cron manager")
	c := cron.New(cron.WithSeconds())
	//Run func every min
	c.AddFunc("*/"+ config.Global.Interval +" * * * * *",flush)
	c.Start()
	select {}
}

func main() {
	log.Info("Starting ipmi_exporter")
	srv,err := daemon.New("", "", daemon.SystemDaemon)
	if err != nil {
		log.Error("Error", err)
		os.Exit(1)
	}
	service := Service{srv}
	go service.Manage()

	http.HandleFunc("/metrics", remoteIPMIHandler)       // Endpoint to do IPMI scrapes.
	log.Info("Listening on %s", config.Global.Address)
	log.Info(config.Global.Address)
	err = http.ListenAndServe(config.Global.Address, nil)
	if err != nil {
		log.Error(err)
	}
}
