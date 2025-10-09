package docs

import (
	"embed"
	"github.com/swaggo/swag"
	"log"
	"net/http"
	"os"
	"strings"
)

//go:embed swagger.json
var swaggerFS embed.FS

var SwaggerInfo = &swag.Spec{
	Version:          "1.0",
	Host:             resolveSwaggerHost(),
	BasePath:         "/",
	Schemes:          []string{"http"},
	Title:            "Mezzium API",
	Description:      "RPC API Definition",
	InfoInstanceName: "swagger",
}

func init() {
	data, err := swaggerFS.ReadFile("swagger.json")
	if err != nil {
		log.Fatalf("failed to load swagger.json: %v", err)
	}
	SwaggerInfo.SwaggerTemplate = string(data)
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}

func JSONHandler(w http.ResponseWriter, r *http.Request) {
	data, err := swaggerFS.ReadFile("swagger.json")
	if err != nil {
		http.Error(w, "swagger spec not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func resolveSwaggerHost() string {
	rawHost := getEnv("SWAGGER_HOST", "")
	if rawHost != "" {
		if strings.Contains(rawHost, ":") {
			return rawHost
		}
		port := getEnv("SERVER_PORT", "8080")
		if port != "80" && port != "443" {
			return rawHost + ":" + port
		}
		return rawHost
	}

	host := getEnv("SERVER_HOST", "0.0.0.0")
	port := getEnv("SERVER_PORT", "8080")
	if port != "80" && port != "443" {
		return host + ":" + port
	}
	return host
}
