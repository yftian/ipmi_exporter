package main

import (
	"github.com/jinzhu/configor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
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
		log.Fatalf("Error parsing config file: %s", err)
	}
}

func remoteIPMIHandler(w http.ResponseWriter, r *http.Request) {
	registry := prometheus.NewRegistry()
	remoteCollector := collector{}
	registry.MustRegister(remoteCollector)
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func main() {
	log.Infoln("Starting ipmi_exporter")

	hup := make(chan os.Signal)
	signal.Notify(hup, syscall.SIGHUP)

	http.HandleFunc("/metrics", remoteIPMIHandler)       // Endpoint to do IPMI scrapes.

	log.Infof("Listening on %s", config.Global.Address)
	log.Info(config.Global.Address)
	err := http.ListenAndServe(config.Global.Address, nil)
	if err != nil {
		log.Fatal(err)
	}
}
