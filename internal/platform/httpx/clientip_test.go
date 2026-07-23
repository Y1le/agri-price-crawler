package httpx_test

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func TestParseTrustedProxiesNormalizesIPsAndCIDRs(t *testing.T) {
	t.Parallel()

	got, err := httpx.ParseTrustedProxies([]string{
		" 192.0.2.9 ",
		"10.8.7.6/8",
		"::ffff:198.51.100.7",
		"::ffff:203.0.113.0/120",
		"2001:db8:1::abcd/48",
	})

	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("192.0.2.9/32"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("198.51.100.7/32"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("2001:db8:1::/48"),
	}, got)
}

func TestParseTrustedProxiesRejectsMalformedAndZonedEntries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"proxy.internal",
		"192.0.2.1:443",
		"192.0.2.999",
		"2001:db8::/129",
		"fe80::1%en0",
		"::ffff:192.0.2.1/80",
	} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, err := httpx.ParseTrustedProxies([]string{value})
			require.Error(t, err)
		})
	}
}

func TestClientIPIgnoresForwardingFromUntrustedImmediatePeer(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "198.51.100.20:3210"
	request.Header.Set("X-Forwarded-For", "203.0.113.1")

	got, err := httpx.ClientIP(request, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("198.51.100.20"), got)
}

func TestClientIPWalksForwardingChainRightToLeft(t *testing.T) {
	t.Parallel()

	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/8", "192.0.2.0/24"})
	require.NoError(t, err)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.9:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.42, 203.0.113.66, 192.0.2.7")

	got, err := httpx.ClientIP(request, trusted)

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("203.0.113.66"), got, "values left of the first untrusted hop are spoofable")
}

func TestClientIPAcceptsMultipleForwardedHeaderLines(t *testing.T) {
	t.Parallel()

	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/8", "192.0.2.0/24"})
	require.NoError(t, err)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.9:443"
	request.Header.Add("X-Forwarded-For", "198.51.100.42")
	request.Header.Add("X-Forwarded-For", "192.0.2.7")

	got, err := httpx.ClientIP(request, trusted)

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("198.51.100.42"), got)
}

func TestClientIPReturnsLeftmostWhenWholeChainIsTrusted(t *testing.T) {
	t.Parallel()

	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/8", "192.0.2.0/24"})
	require.NoError(t, err)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.9:443"
	request.Header.Set("X-Forwarded-For", "192.0.2.1, 192.0.2.2")

	got, err := httpx.ClientIP(request, trusted)

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("192.0.2.1"), got)
}

func TestClientIPNormalizesMappedRemoteAndHeaderAddresses(t *testing.T) {
	t.Parallel()

	trusted, err := httpx.ParseTrustedProxies([]string{"192.0.2.1"})
	require.NoError(t, err)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "[::ffff:192.0.2.1]:8443"
	request.Header.Set("X-Forwarded-For", "::ffff:198.51.100.8")

	got, err := httpx.ClientIP(request, trusted)

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("198.51.100.8"), got)
}

func TestClientIPRejectsMalformedRemoteAddress(t *testing.T) {
	t.Parallel()

	for _, remote := range []string{
		"198.51.100.8",
		"host.internal:123",
		"198.51.100.8:not-a-port",
		"198.51.100.8:70000",
		"[fe80::1%en0]:443",
	} {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = remote
		_, err := httpx.ClientIP(request, nil)
		require.Error(t, err, remote)
	}
}

func TestClientIPRejectsEntireMalformedForwardingChain(t *testing.T) {
	t.Parallel()

	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/8"})
	require.NoError(t, err)
	for _, forwarded := range []string{
		"",
		"unknown",
		"198.51.100.8:1234",
		"198.51.100.8,,192.0.2.1",
		"198.51.100.999",
		"fe80::1%en0",
	} {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = "10.0.0.1:443"
		request.Header["X-Forwarded-For"] = []string{forwarded}

		got, err := httpx.ClientIP(request, trusted)

		require.Error(t, err, forwarded)
		require.False(t, got.IsValid())
	}
}
