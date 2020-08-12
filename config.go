package main

// IPMIConfig is the Go representation of a module configuration in the yaml
// config file.

type ipmiTarget struct {
	Host   string
	User   string
	Pwd    string
}

type Config struct {
	Global struct{
		Address string
		Drive         string
		Collector   []string
	}
	Targets []ipmiTarget
}
