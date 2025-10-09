package networks

type Node struct {
	URL      string            `yaml:"url" json:"url"`
	Priority int               `yaml:"priority" json:"priority"`
	Headers  map[string]string `yaml:"headers" json:"headers"`
	Tor      bool              `yaml:"tor" json:"tor"`
}

type NetworkConfig struct {
	Route     string     `yaml:"route" json:"route"`
	Protocol  string     `yaml:"protocol" json:"protocol"` // evm|btc
	Nodes     []Node     `yaml:"nodes" json:"nodes"`
	TimeoutMs int        `yaml:"timeoutMs" json:"timeoutMs"`
	Gas       *GasConfig `yaml:"gas" json:"gas"`
}

type GasConfig struct {
	ExternalURL     string `yaml:"externalUrl" json:"externalUrl"`         // опционально
	MinPriorityGwei int64  `yaml:"minPriorityGwei" json:"minPriorityGwei"` // опционально (дефолт зададим в oracle)
	TTLSeconds      int    `yaml:"ttlSeconds" json:"ttlSeconds"`           // опционально (дефолт зададим в oracle)
}
