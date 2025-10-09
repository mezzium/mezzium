package main

import (
	"go.uber.org/zap"

	"github.com/shuliakovsky/mezzium/pkg/networks"
	"github.com/shuliakovsky/mezzium/pkg/registry"
)

func initRegistry(cfg config, logger *zap.Logger) *registry.Registry {
	cfgs, err := networks.LoadAll("configs/networks", logger)
	if err != nil {
		logger.Fatal("networks_load_error", zap.Error(err))
	}
	reg := registry.New()
	reg.InitFromConfigs(cfgs)
	return reg
}
