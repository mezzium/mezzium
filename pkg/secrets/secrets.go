package secrets

import (
	"os"
	"strings"
	"sync"
)

var (
	once          sync.Once
	sensitiveEnvs []string

	headerKeySet = map[string]struct{}{
		"x-api-key":           {},
		"authorization":       {},
		"proxy-authorization": {},
		"api-key":             {},
	}

	envNameSensitivePatterns = []string{
		"API_KEY", "TOKEN", "SECRET", "PASSWORD", "ACCESS_KEY", "PRIVATE_KEY",
	}
)

func initSensitiveEnvs() {
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name, val := parts[0], parts[1]
		up := strings.ToUpper(name)
		for _, pat := range envNameSensitivePatterns {
			if strings.Contains(up, pat) && val != "" {
				sensitiveEnvs = append(sensitiveEnvs, val)
				break
			}
		}
	}
}

func RedactHeaders(h map[string]string) map[string]string {
	if len(h) == 0 {
		return h
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		if _, ok := headerKeySet[strings.ToLower(k)]; ok {
			out[k] = "***"
			continue
		}
		out[k] = v
	}
	return out
}

func RedactString(s string) string {
	once.Do(initSensitiveEnvs)
	for _, val := range sensitiveEnvs {
		if val == "" {
			continue
		}
		s = strings.ReplaceAll(s, val, "[HIDDEN]")
	}
	return s
}
func ResetSensitiveEnvs() {
	sensitiveEnvs = nil
	once = sync.Once{}
}
