package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/shuliakovsky/mezzium/pkg/health"
	"github.com/shuliakovsky/mezzium/pkg/networks"
	"github.com/shuliakovsky/mezzium/pkg/registry"
	"go.uber.org/zap"
)

type Admin struct {
	Reg      *registry.Registry
	Checker  *health.Checker
	AdminKey string
	Logger   *zap.Logger
}

func NewAdmin(reg *registry.Registry, checker *health.Checker, key string, logger *zap.Logger) *Admin {
	return &Admin{Reg: reg, Checker: checker, AdminKey: key, Logger: logger}
}

func (a *Admin) auth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("x-admin-key") != a.AdminKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// ===== хендлеры =====

// POST /admin/networks
func (a *Admin) AddNetwork(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	start := LogRequest(a.Logger, "admin_add_network", r.Method, r.URL.Path, bodyBytes)

	if !a.auth(w, r) {
		return
	}
	var nc networks.NetworkConfig
	if err := json.Unmarshal(bodyBytes, &nc); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if nc.Route == "" || nc.Protocol == "" || len(nc.Nodes) == 0 {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}
	// обрезаем / из начала
	nc.Route = strings.Trim(nc.Route, "/")

	best := a.Checker.UpdateNetwork(nc.Protocol, nc.Nodes)
	if len(best) == 0 {
		http.Error(w, "no healthy nodes", http.StatusBadRequest)
		return
	}
	a.Reg.AddNetwork(nc, best)
	a.Logger.Info("admin_add_network", zap.String("route", nc.Route), zap.Int("healthy_nodes", len(best)))
	resp := map[string]any{"status": "added", "healthyNodes": best}
	writeJSON(w, http.StatusOK, resp)
	respBytes, _ := json.Marshal(resp)
	LogResponse(a.Logger, "admin_add_network", http.StatusOK, respBytes, start)
}

// POST /admin/{network}/nodes
func (a *Admin) AddNode(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	start := LogRequest(a.Logger, "admin_add_node", r.Method, r.URL.Path, bodyBytes)

	if !a.auth(w, r) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/"), "/")
	if len(parts) < 2 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	network := parts[0]
	var node networks.Node
	if err := json.Unmarshal(bodyBytes, &node); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	best := a.Checker.UpdateNetwork(a.Reg.ProtocolOf(network), []networks.Node{node})
	if len(best) == 0 {
		http.Error(w, "node not healthy", http.StatusBadRequest)
		return
	}
	a.Reg.AddNode(network, node)
	a.Reg.AppendBest(network, best[0])
	a.Logger.Info("admin_add_node", zap.String("network", network), zap.String("url", node.URL))
	resp := map[string]any{"status": "added", "node": best[0]}
	writeJSON(w, http.StatusOK, resp)
	respBytes, _ := json.Marshal(resp)
	LogResponse(a.Logger, "admin_add_node", http.StatusOK, respBytes, start)
}

// GET /admin/{network}/nodes
func (a *Admin) ListNodes(w http.ResponseWriter, r *http.Request) {
	start := LogRequest(a.Logger, "admin_list_nodes", r.Method, r.URL.Path, nil)

	if !a.auth(w, r) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/"), "/")
	if len(parts) < 2 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	network := parts[0]
	all := a.Reg.All()
	st, ok := all[network]
	if !ok {
		http.Error(w, "unknown network", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, st.All)
	respBytes, _ := json.Marshal(st.All)
	LogResponse(a.Logger, "admin_list_nodes", http.StatusOK, respBytes, start)
}

// DELETE /admin/{network}/nodes
func (a *Admin) DeleteNode(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	start := LogRequest(a.Logger, "admin_delete_node", r.Method, r.URL.Path, bodyBytes)

	if !a.auth(w, r) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/"), "/")
	if len(parts) < 2 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	network := parts[0]
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil || payload.URL == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	all := a.Reg.All()
	st, ok := all[network]
	if !ok {
		http.Error(w, "unknown network", http.StatusNotFound)
		return
	}
	// фильтруем Best
	newBest := make([]registry.NodeWithPing, 0, len(st.Best))
	for _, n := range st.Best {
		if n.URL != payload.URL {
			newBest = append(newBest, n)
		}
	}
	a.Reg.SetBest(network, newBest)
	// фильтруем All
	newAll := make([]networks.Node, 0, len(st.All))
	for _, n := range st.All {
		if n.URL != payload.URL {
			newAll = append(newAll, n)
		}
	}
	a.Reg.State[network].All = newAll

	a.Logger.Info("admin_delete_node", zap.String("network", network), zap.String("url", payload.URL))
	resp := map[string]any{"status": "removed", "url": payload.URL}
	writeJSON(w, http.StatusOK, resp)
	respBytes, _ := json.Marshal(resp)
	LogResponse(a.Logger, "admin_delete_node", http.StatusOK, respBytes, start)
}

// POST /admin/networks/bulk
func (a *Admin) AddNetworksBulk(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	start := LogRequest(a.Logger, "admin_add_networks_bulk", r.Method, r.URL.Path, bodyBytes)

	if !a.auth(w, r) {
		return
	}
	var configs []networks.NetworkConfig
	if err := json.Unmarshal(bodyBytes, &configs); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(configs) == 0 {
		http.Error(w, "empty list", http.StatusBadRequest)
		return
	}

	result := make([]map[string]any, 0, len(configs))
	for _, nc := range configs {
		route := strings.Trim(nc.Route, "/")
		route = strings.ToLower(strings.Trim(route, "/"))

		// базовая проверка
		if route == "" || nc.Protocol == "" || len(nc.Nodes) == 0 {
			result = append(result, map[string]any{
				"route":  route,
				"status": "skipped",
				"reason": "missing required fields",
			})
			continue
		}

		// дубликат
		if a.Reg.Exists(route) {
			result = append(result, map[string]any{
				"route":  route,
				"status": "skipped",
				"reason": "already exists",
			})
			continue
		}

		// health‑check
		best := a.Checker.UpdateNetwork(nc.Protocol, nc.Nodes)
		if len(best) == 0 {
			result = append(result, map[string]any{
				"route":  route,
				"status": "failed",
				"reason": "no healthy nodes",
			})
			continue
		}

		nc.Route = route
		a.Reg.AddNetwork(nc, best)
		a.Logger.Info("admin_bulk_add_network", zap.String("route", route), zap.Int("healthy_nodes", len(best)))
		result = append(result, map[string]any{
			"route":        route,
			"status":       "added",
			"healthyNodes": len(best),
		})
	}

	writeJSON(w, http.StatusOK, result)
	respBytes, _ := json.Marshal(result)
	LogResponse(a.Logger, "admin_add_networks_bulk", http.StatusOK, respBytes, start)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
