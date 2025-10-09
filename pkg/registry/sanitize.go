package registry

import (
	"github.com/shuliakovsky/mezzium/pkg/secrets"
)

func SanitizeNodes(nodes []NodeWithPing) []NodeWithPing {
	clean := make([]NodeWithPing, len(nodes))
	for i, n := range nodes {
		n.Headers = secrets.RedactHeaders(n.Headers)
		clean[i] = n
	}
	return clean
}
