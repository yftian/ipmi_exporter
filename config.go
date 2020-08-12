package main

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
