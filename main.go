package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	//configFile = kingpin.Flag(
	//	"config.file",
	//	"Path to configuration file.",
	//).String()
	configFile = "ipmi_remote.yml"
	executablesPath = kingpin.Flag(
		"freeipmi.path",
		"Path to FreeIPMI executables (default: rely on $PATH).",
	).String()
	listenAddress = kingpin.Flag(
		"web.listen-address",
		"Address to listen on for web interface and telemetry.",
	).Default(":9290").String()

	sc = &SafeConfig{
		C: &Config{},
	}
	reloadCh chan chan error
)

func remoteIPMIHandler(w http.ResponseWriter, r *http.Request) {
	ips := []string{"1.2.3.4","2.3.4.5","3.4.5.6"}
	registry := prometheus.NewRegistry()
	remoteCollector := collector{targets: ips, module: "ipmi", config: sc}
	registry.MustRegister(remoteCollector)
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func updateConfiguration(w http.ResponseWriter, r *http.Request) {
	log.Info("刷新配置。。。")
	switch r.Method {
	case "POST":
		rc := make(chan error)
		reloadCh <- rc
		if err := <-rc; err != nil {
			http.Error(w, fmt.Sprintf("failed to reload config: %s", err), http.StatusInternalServerError)
		}
	default:
		log.Errorf("Only POST requests allowed for %s", r.URL)
		w.Header().Set("Allow", "POST")
		http.Error(w, "Only POST requests allowed", http.StatusMethodNotAllowed)
	}
}

func main() {
	//log.AddFlags(kingpin.CommandLine)
	//kingpin.HelpFlag.Short('h')
	//kingpin.Version(version.Print("ipmi_exporter"))
	//kingpin.Parse()
	log.Infoln("Starting ipmi_exporter")

	// Bail early if the config is bad.
	if err := sc.ReloadConfig(configFile); err != nil {
		log.Fatalf("Error parsing config file: %s", err)
	}

	hup := make(chan os.Signal)
	reloadCh = make(chan chan error)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-hup:
				if err := sc.ReloadConfig(configFile); err != nil {
					log.Errorf("Error reloading config: %s", err)
				}
			case rc := <-reloadCh:
				if err := sc.ReloadConfig(configFile); err != nil {
					log.Errorf("Error reloading config: %s", err)
					rc <- err
				} else {
					rc <- nil
				}
			}
		}
	}()

	//localCollector := collector{target: targetLocal, module: "default", config: sc}
	//prometheus.MustRegister(&localCollector)

	http.Handle("/metrics", promhttp.Handler())       // Regular metrics endpoint for local IPMI metrics.
	http.HandleFunc("/ipmi", remoteIPMIHandler)       // Endpoint to do IPMI scrapes.
	http.HandleFunc("/-/reload", updateConfiguration) // Endpoint to reload configuration.

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
            <head>
            <title>IPMI Exporter</title>
            <style>
            label{
            display:inline-block;
            width:75px;
            }
            form label {
            margin: 10px;
            }
            form input {
            margin: 10px;
            }
            </style>
            </head>
            <body>
            <h1>IPMI Exporter</h1>
            <form action="/ipmi">
            <label>Target:</label> <input type="text" name="target" placeholder="X.X.X.X" value="1.2.3.4"><br>
            <input type="submit" value="Submit">
			</form>
			<p><a href="/metrics">Local metrics</a></p>
			<p><a href="/config">Config</a></p>
            </body>
            </html>`))
	})

	log.Infof("Listening on %s", *listenAddress)
	err := http.ListenAndServe(":9290", nil)
	if err != nil {
		log.Fatal(err)
	}
}
