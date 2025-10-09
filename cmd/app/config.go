package main

import "os"

type config struct {
	PodIP        string
	PodName      string
	SharedSecret string
	BootstrapURL string
	TorSocks     string
	AdminKey     string
	Host         string
	Port         string
}

func loadConfig() config {
	return config{
		PodIP:        getEnv("POD_IP", "127.0.0.1"),
		PodName:      getEnv("POD_NAME", "dev-node"),
		SharedSecret: getEnv("SHARED_SECRET", "devsecret"),
		BootstrapURL: getEnv("BOOTSTRAP_URL", ""),
		TorSocks:     getEnv("TOR_SOCKS5", "127.0.0.1:9050"),
		AdminKey:     getEnv("ADMIN_API_KEY", "changeme"),
		Host:         getEnv("SERVER_HOST", "0.0.0.0"),
		Port:         getEnv("SERVER_PORT", "8080"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
