package health

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/shuliakovsky/mezzium/pkg/networks"
	"github.com/shuliakovsky/mezzium/pkg/registry"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	_ = godotenv.Load(".env")
	os.Exit(m.Run())
}

func newTestChecker() *Checker {
	logger, _ := zap.NewDevelopment()
	reg := registry.New()
	return New("", logger, reg)
}

func TestCheckEVM_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x10"}`))
	}))
	defer srv.Close()

	h := newTestChecker()
	ok, ping := h.checkEVM(networks.Node{URL: srv.URL}, 2*time.Second)
	require.True(t, ok, "EVM node should be alive")
	require.GreaterOrEqual(t, ping, int64(0), "ping should be non-negative")
}

func TestCheckBTC_AllVariants(t *testing.T) {
	h := newTestChecker()

	t.Run("Blockstream variant", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/blocks/tip/height", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("123456"))
		}))
		defer srv.Close()

		ok, _ := h.checkBTC(networks.Node{URL: srv.URL + "/api"}, 2*time.Second)
		require.True(t, ok)
	})

	t.Run("Tatum gateway variant", func(t *testing.T) {
		apiKey := os.Getenv("TATUM_API_KEY")
		if apiKey == "" {
			t.Skip("TATUM_API_KEY not set in environment")
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, apiKey, r.Header.Get("x-api-key"))
			body, _ := io.ReadAll(r.Body)
			require.Contains(t, string(body), `"method":"getblockcount"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":123456}`))
		}))
		defer srv.Close()

		tatumURL := strings.Replace(srv.URL, "127.0.0.1", "gateway.tatum.io", 1)

		ok, _ := h.checkBTC(networks.Node{
			URL:     tatumURL,
			Headers: map[string]string{"x-api-key": apiKey},
		}, 2*time.Second)
		require.True(t, ok, "Tatum gateway node should be alive")
	})
}

func TestUpdateNetwork_Prioritizes(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer fast.Close()

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer slow.Close()

	nodes := []networks.Node{
		{URL: fast.URL, Priority: 1},
		{URL: slow.URL, Priority: 2},
	}

	h := newTestChecker()
	res := h.UpdateNetwork("evm", nodes)
	require.Equal(t, 2, len(res))
	require.Equal(t, nodes[0].URL, res[0].URL, "fast node should be first")
}
