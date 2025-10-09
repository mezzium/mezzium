package networks

import (
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAll_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	yml := `
route: /foo
protocol: evm
nodes:
  - url: https://example.com
    priority: 1
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "foo.yaml"), []byte(yml), 0644))
	logger := zap.NewNop()
	cfgs, err := LoadAll(dir, logger)
	require.NoError(t, err)
	require.Contains(t, cfgs, "foo")
	require.Equal(t, "evm", cfgs["foo"].Protocol)
	require.Equal(t, "/foo", cfgs["foo"].Route)
}
