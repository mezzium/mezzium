package oracle

type Tier struct {
	MaxPriorityFee int64 `json:"maxPriorityFee"`
	MaxFee         int64 `json:"maxFee"`
	GasPrice       int64 `json:"gasPrice"`
}
type simpleErr string
