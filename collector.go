package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	log "github.com/cihub/seelog"
	"github.com/prometheus/client_golang/prometheus"
	"io/ioutil"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	namespace   = "ipmi"
)

var (
	ipmiDCMICurrentPowerRegex = regexp.MustCompile(`^Current Power\s*:\s*(?P<value>[0-9.]*)\s*Watts.*`)
	ipmiChassisPowerRegex     = regexp.MustCompile(`^System Power\s*:\s(?P<value>.*)`)
	ipmiChassisDriveRegex     = regexp.MustCompile(`^Drive Fault\s*:\s(?P<value>.*)`)
	ipmiChassisCollingRegex   = regexp.MustCompile(`^Cooling/fan fault\s*:\s(?P<value>.*)`)
)

type collector struct{}

type sensorData struct {
	ID    int64
	Name  string
	Type  string
	State string
	Value float64
	Unit  string
	Event string
}

var (
	sensorStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "sensor", "state"),
		"Indicates the severity of the state reported by an IPMI sensor (0=nominal, 1=warning, 2=critical).",
		[]string{"id", "name", "type", "host"},
		nil,
	)

	sensorValueDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "sensor", "value"),
		"Generic data read from an IPMI sensor of unknown type, relying on labels for context.",
		[]string{"id", "name", "type", "host"},
		nil,
	)

	fanSpeedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "fan_speed", "rpm"),
		"Fan speed in rotations per minute.",
		[]string{"id", "name", "host"},
		nil,
	)

	fanSpeedStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "fan_speed", "state"),
		"Reported state of a fan speed sensor (0=nominal, 1=warning, 2=critical).",
		[]string{"id", "name", "host"},
		nil,
	)

	temperatureDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "temperature", "celsius"),
		"Temperature reading in degree Celsius.",
		[]string{"id", "name", "host"},
		nil,
	)

	temperatureStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "temperature", "state"),
		"Reported state of a temperature sensor (0=nominal, 1=warning, 2=critical).",
		[]string{"id", "name", "host"},
		nil,
	)

	voltageDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "voltage", "volts"),
		"Voltage reading in Volts.",
		[]string{"id", "name", "host"},
		nil,
	)

	voltageStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "voltage", "state"),
		"Reported state of a voltage sensor (0=nominal, 1=warning, 2=critical).",
		[]string{"id", "name", "host"},
		nil,
	)

	currentDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "current", "amperes"),
		"Current reading in Amperes.",
		[]string{"id", "name", "host"},
		nil,
	)

	currentStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "current", "state"),
		"Reported state of a current sensor (0=nominal, 1=warning, 2=critical).",
		[]string{"id", "name", "host"},
		nil,
	)

	powerDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "power", "watts"),
		"Power reading in Watts.",
		[]string{"id", "name", "host"},
		nil,
	)

	powerStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "power", "state"),
		"Reported state of a power sensor (0=nominal, 1=warning, 2=critical).",
		[]string{"id", "name", "host"},
		nil,
	)

	powerConsumption = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "dcmi", "power_consumption_watts"),
		"Current power consumption in Watts.",
		[]string{"host"},
		nil,
	)

	chassisPowerState = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "chassis", "power_state"),
		"Current power state (1=on, 0=off).",
		[]string{"host"},
		nil,
	)

	chassisDriveFault = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "chassis", "dirve_fault"),
		"Current drive fault (1=false, 0=true).",
		[]string{"host"},
		nil,
	)

	chassisCoolingFault = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "chassis", "cooling_fault"),
		"Current cooling fault (1=false, 0=true).",
		[]string{"host"},
		nil,
	)

	upDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "up"),
		"'1' if a scrape of the IPMI device was successful, '0' otherwise.",
		[]string{"collector", "host"},
		nil,
	)

	durationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "scrape_duration", "seconds"),
		"Returns how long the scrape took to complete in seconds.",
		[]string{"host"},
		nil,
	)
)

func ipmiOutput(name string, args []string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Error(fmt.Sprint(err) + ":" + stderr.String())
		return nil, errors.New(stderr.String())
	}
	return out.Bytes(), err
}

func splitMonitoringOutput(impiOutput []byte) ([]sensorData, error) {
	var result []sensorData

	r := csv.NewReader(bytes.NewReader(impiOutput))
	records, err := r.ReadAll()
	for _, line := range records {
		//line = strings.Fields(line[0])
		line = strings.Split(line[0], "|")
		for i := 0; i < len(line); i++ {
			line[i] = strings.Trim(line[i], " ")
		}
		var data sensorData
		data.ID, err = strconv.ParseInt(line[0], 10, 64)
		if err != nil {
			continue
		}
		if len(strings.Fields(line[1])) > 1 {
			data.Name = strings.ReplaceAll(line[1], " ", "_")
			data.Name = strings.ReplaceAll(data.Name, "/", "")
		} else {
			data.Name = line[1]
		}
		if strings.Index(data.Name, "-") == 2 {
			data.Name = data.Name[3:]
			data.Name = strings.ReplaceAll(data.Name, "-", "_")
		}
		data.Type = line[2]
		data.State = line[3]
		value := line[4]
		if value != "N/A" {
			data.Value, err = strconv.ParseFloat(value, 64)
			if err != nil {
				return result, err
			}
		} else {
			data.Value = math.NaN()
		}

		data.Unit = line[5]
		data.Event = strings.Trim(line[6], "'")

		result = append(result, data)
	}
	return result, err
}

func getValue(ipmiOutput []byte, regex *regexp.Regexp) (string, error) {
	for _, line := range strings.Split(string(ipmiOutput), "\n") {
		match := regex.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		for i, name := range regex.SubexpNames() {
			if name != "value" {
				continue
			}
			return match[i], nil
		}
	}
	return "", fmt.Errorf("Could not find value in output: %s", string(ipmiOutput))
}

func getCurrentPowerConsumption(ipmiOutput []byte) (float64, error) {
	value, err := getValue(ipmiOutput, ipmiDCMICurrentPowerRegex)
	if err != nil {
		return -1, err
	}
	return strconv.ParseFloat(value, 64)
}

func getChassis(ipmiOutput []byte, reg *regexp.Regexp) (float64, error) {
	value, err := getValue(ipmiOutput, reg)
	if err != nil {
		return -1, err
	}
	if value == "on" || value == "false" {
		return 1, err
	}
	return 0, err
}

// Describe implements Prometheus.Collector.
func (c collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- sensorStateDesc
	ch <- sensorValueDesc
	ch <- fanSpeedDesc
	ch <- temperatureDesc
	ch <- powerConsumption
	ch <- upDesc
	ch <- durationDesc
}

func collectTypedSensor(desc, stateDesc *prometheus.Desc, state float64, data sensorData, target ipmiTarget) []prometheus.Metric{
	var sensorMetrics [] prometheus.Metric
	sensorMetrics = append(sensorMetrics, prometheus.MustNewConstMetric(
		desc,
		prometheus.GaugeValue,
		data.Value,
		strconv.FormatInt(data.ID, 10),
		data.Name,
		target.Host,
	))
	sensorMetrics = append(sensorMetrics, prometheus.MustNewConstMetric(
		stateDesc,
		prometheus.GaugeValue,
		state,
		strconv.FormatInt(data.ID, 10),
		data.Name,
		target.Host,
	))
	return sensorMetrics
}

func collectGenericSensor(state float64, data sensorData, target ipmiTarget) []prometheus.Metric {
	var genericMetrics [] prometheus.Metric
	genericMetrics = append(genericMetrics, prometheus.MustNewConstMetric(
		sensorValueDesc,
		prometheus.GaugeValue,
		data.Value,
		strconv.FormatInt(data.ID, 10),
		data.Name,
		data.Type,
		target.Host,
	))
	genericMetrics = append(genericMetrics, prometheus.MustNewConstMetric(
		sensorStateDesc,
		prometheus.GaugeValue,
		state,
		strconv.FormatInt(data.ID, 10),
		data.Name,
		data.Type,
		target.Host,
	))
	return genericMetrics
}

func readFile(filename string) ([]byte, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Error("File reading error", err.Error())
	}
	return data, err
}

func collectMonitoring(target ipmiTarget) (int, error, []prometheus.Metric) {
	var monitorMetrics [] prometheus.Metric
	output, err := ipmiOutput("ipmimonitoring", []string{
		"-D", config.Global.Drive,
		"-h", target.Host,
		"-u", target.User,
		"-p", target.Pwd,
	})
	//output, err := readFile("./file/hpipmi.txt")
	if err != nil {
		log.Errorf("Failed to collect ipmimonitoring data from %s: %s", target.Host, err)
		return 0, err, nil
	}
	results, err := splitMonitoringOutput(output)
	if err != nil {
		log.Errorf("Failed to parse ipmimonitoring data from %s: %s", target.Host, err)
		return 0, err, nil
	}
	for _, data := range results {
		var state float64

		switch data.State {
		case "Nominal":
			state = 0
		case "Warning":
			state = 1
		case "Critical":
			state = 2
		case "N/A":
			state = math.NaN()
		default:
			log.Errorf("Unknown sensor state: '%s'\n", data.State)
			state = math.NaN()
		}

		log.Debugf("Got values: %v\n", data)

		switch data.Unit {
		case "RPM":
			monitorMetrics = append(monitorMetrics,
				collectTypedSensor(fanSpeedDesc, fanSpeedStateDesc, state, data, target)...)
		case "C":
			monitorMetrics = append(monitorMetrics,
				collectTypedSensor(temperatureDesc, temperatureStateDesc, state, data, target)...)
		case "A":
			monitorMetrics = append(monitorMetrics,
				collectTypedSensor(currentDesc, currentStateDesc, state, data, target)...)
		case "V":
			monitorMetrics = append(monitorMetrics,
				collectTypedSensor(voltageDesc, voltageStateDesc, state, data, target)...)
		case "W":
			monitorMetrics = append(monitorMetrics,
				collectTypedSensor(powerDesc, powerStateDesc, state, data, target)...)
		default:
			monitorMetrics = append(monitorMetrics,
				collectGenericSensor(state, data, target)...)
		}
	}
	return 1, nil, monitorMetrics
}

func collectDCMI(target ipmiTarget) (int, error, prometheus.Metric){
	output, err := ipmiOutput("ipmi-dcmi", []string{
		"-D", config.Global.Drive,
		"-h", target.Host,
		"-u", target.User,
		"-p", target.Pwd,
	})
	//output, err := readFile("./file/hpdcmi.txt")
	if err != nil {
		log.Debugf("Failed to collect ipmi-dcmi data from %s: %s", target.Host, err)
		return 0, err, nil
	}
	currentPowerConsumption, err := getCurrentPowerConsumption(output)
	if err != nil {
		log.Errorf("Failed to parse ipmi-dcmi data from %s: %s", target.Host, err)
		return 0, err,nil
	}
	return 1, nil, prometheus.MustNewConstMetric(
		powerConsumption,
		prometheus.GaugeValue,
		currentPowerConsumption,
		target.Host,
	)
}

func collectChassisState(target ipmiTarget) (int, error, []prometheus.Metric) {
	var chassMetrics [] prometheus.Metric
	output, err := ipmiOutput("ipmi-chassis", []string{
		"-D", config.Global.Drive,
		"-h", target.Host,
		"-u", target.User,
		"-p", target.Pwd,
	})
	//output, err := readFile("./file/sugonchass.txt")
	if err != nil {
		log.Debugf("Failed to collect ipmi-chassis data from %s: %s", target.Host, err)
		return 0, err,nil
	}
	currentChassisPowerState, err := getChassis(output, ipmiChassisPowerRegex)
	if err != nil {
		log.Errorf("Failed to parse ipmi-chassis data from %s: %s", target.Host, err)
		return 0, err,nil
	}
	chassMetrics = append(chassMetrics, prometheus.MustNewConstMetric(
		chassisPowerState,
		prometheus.GaugeValue,
		currentChassisPowerState,
		target.Host,
	))

	currentChassisDriveFault, err := getChassis(output, ipmiChassisDriveRegex)
	if err != nil {
		log.Errorf("Failed to parse ipmi-chassis data from %s: %s", target.Host, err)
		return 0, err,chassMetrics
	}
	chassMetrics = append(chassMetrics, prometheus.MustNewConstMetric(
		chassisDriveFault,
		prometheus.GaugeValue,
		currentChassisDriveFault,
		target.Host,
	))

	currentChassisCoolingFault, err := getChassis(output, ipmiChassisCollingRegex)
	if err != nil {
		log.Errorf("Failed to parse ipmi-chassis data from %s: %s", target.Host, err)
		return 0, err,chassMetrics
	}
	chassMetrics =append(chassMetrics, prometheus.MustNewConstMetric(
		chassisCoolingFault,
		prometheus.GaugeValue,
		currentChassisCoolingFault,
		target.Host,
	))

	return 1, nil,chassMetrics
}

func markCollectorUp(name string, up int, target ipmiTarget) prometheus.Metric{
	return prometheus.MustNewConstMetric(
		upDesc,
		prometheus.GaugeValue,
		float64(up),
		name,
		target.Host,
	)
}

func IpmiCollect(target ipmiTarget) []prometheus.Metric {
	var ipmiMetrics [] prometheus.Metric
	start := time.Now()
	duration := time.Since(start).Seconds()
	log.Debugf("Scrape of target %s took %f seconds.", target.Host, duration)
	durationMetrics := prometheus.MustNewConstMetric(
		durationDesc,
		prometheus.GaugeValue,
		duration,
		target.Host,
	)
	ipmiMetrics = append(ipmiMetrics, durationMetrics)

	for _, collector := range config.Global.Collector {
		var up int
		var collectMetcics []prometheus.Metric
		var dcmiMetric prometheus.Metric
		var chassMetrics []prometheus.Metric
		log.Debugf("Running collector: %s", collector)
		switch collector {
		case "ipmimonitoring":
			up, _,collectMetcics = collectMonitoring(target)
			ipmiMetrics = append(ipmiMetrics, collectMetcics...)
		case "ipmi-dcmi":
			up, _,dcmiMetric = collectDCMI(target)
			ipmiMetrics = append(ipmiMetrics, dcmiMetric)
		case "ipmi-chassis":
			up, _,chassMetrics  = collectChassisState(target)
			ipmiMetrics = append(ipmiMetrics, chassMetrics...)
		}
		ipmiMetrics = append(ipmiMetrics, markCollectorUp(collector, up, target))
	}
	log.Info("ipmiMetrics:",len(ipmiMetrics))
	return ipmiMetrics
}

// Collect implements Prometheus.Collector.
func (c collector) Collect(ch chan<- prometheus.Metric) {
	log.Info("get metrics data:",len(metrics))
	lock.RLock()
	for _, metric := range metrics {
		ch <- metric
	}
	lock.RUnlock()
}
