package main

import (
	"github.com/shuliakovsky/mezzium/pkg/secrets"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/shuliakovsky/mezzium/pkg/health"
	"github.com/shuliakovsky/mezzium/pkg/metrics"
	"github.com/shuliakovsky/mezzium/pkg/registry"
)

func initHealthChecker(cfg config, reg *registry.Registry, logger *zap.Logger) *health.Checker {
	return health.New(cfg.TorSocks, logger, reg)
}

func runInitialHealth(reg *registry.Registry, checker *health.Checker, logger *zap.Logger) {
	limits := map[string]*limiter{
		"tatum.io":      newLimiter(1),
		"alchemyapi.io": newLimiter(1),
	}
	defaultLimiter := newLimiter(5)

	var wg sync.WaitGroup
	for name, st := range reg.All() {
		wg.Add(1)
		go func(name string, st *registry.NetworkState) {
			defer wg.Done()

			lim := pickLimiter(st, limits, defaultLimiter)

			lim.acquire()
			defer lim.release()

			best := checker.UpdateNetwork(st.Protocol, st.All)
			reg.SetBest(name, best)

			metrics.TotalNodes.WithLabelValues(name).Set(float64(len(st.All) + len(st.Discovered)))
			metrics.HealthyNodes.WithLabelValues(name).Set(float64(len(best)))

			logger.Info("health_initialized", zap.String("network", name), zap.Int("best_count", len(best)))
		}(name, st)
	}
	wg.Wait()
}

func startHealthLoop(reg *registry.Registry, checker *health.Checker, logger *zap.Logger) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			reg.PruneAndMerge(10 * time.Minute)

			limits := map[string]*limiter{
				"tatum.io":      newLimiter(1),
				"alchemyapi.io": newLimiter(1),
				"alchemy.com":   newLimiter(1),
			}
			defaultLimiter := newLimiter(5)

			var wg sync.WaitGroup
			for name, st := range reg.All() {
				wg.Add(1)
				go func(name string, st *registry.NetworkState) {
					defer wg.Done()

					lim := pickLimiter(st, limits, defaultLimiter)

					lim.acquire()
					defer lim.release()

					best := checker.UpdateNetwork(st.Protocol, st.All)
					reg.SetBest(name, best)
					metrics.TotalNodes.WithLabelValues(name).Set(float64(len(st.All) + len(st.Discovered)))
					metrics.HealthyNodes.WithLabelValues(name).Set(float64(len(best)))

					logger.Info("health_update", zap.String("network", name), zap.Int("best_count", len(best)))
				}(name, st)
			}
			wg.Wait()

			// drop nodes marked as fatal during checks
			for _, url := range checker.DrainDropURLs() {
				reg.RemoveNodeEverywhere(url)
				logger.Warn("unhealthy_node_dropped", zap.String("url", secrets.RedactString(url)))

			}
		}
	}()
}

func pickLimiter(st *registry.NetworkState, limits map[string]*limiter, def *limiter) *limiter {
	lim := def
	if len(st.All) > 0 {
		for domain, l := range limits {
			if strings.Contains(st.All[0].URL, domain) {
				lim = l
				break
			}
		}
	}
	return lim
}
