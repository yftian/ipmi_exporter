package main

import (
	log "github.com/cihub/seelog"
	"github.com/jinzhu/configor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var (
	config = Config{}
)

func init() {
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

func main() {
	log.Info("Starting ipmi_exporter")

	hup := make(chan os.Signal)
	signal.Notify(hup, syscall.SIGHUP)

	http.HandleFunc("/metrics", remoteIPMIHandler)       // Endpoint to do IPMI scrapes.

	log.Info("Listening on %s", config.Global.Address)
	log.Info(config.Global.Address)
	err := http.ListenAndServe(config.Global.Address, nil)
	if err != nil {
		log.Error(err)
	}
}
