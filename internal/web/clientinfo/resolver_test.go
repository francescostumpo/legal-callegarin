package clientinfo

import (
	"net/http/httptest"
	"testing"
)

func TestResolveUsesRemoteAddressWhenNoProxyIsTrusted(t *testing.T) {
	request := httptest.NewRequest("GET", "http://studio.example.test/path", nil)
	request.RemoteAddr = "192.0.2.10:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.Header.Set("X-Forwarded-Proto", "https")

	identity, err := Resolve(request, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := identity.Address.String(); got != "192.0.2.10" {
		t.Fatalf("address = %q", got)
	}
	if identity.Scheme != "http" || identity.Forwarded {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestResolveRemovesExactlyTrustedHopsFromRight(t *testing.T) {
	tests := []struct {
		name       string
		remote     string
		forwarded  string
		proto      string
		hops       int
		want       string
		wantScheme string
	}{
		{name: "one ingress hop", remote: "10.0.0.4:1234", forwarded: "198.51.100.8", proto: "https", hops: 1, want: "198.51.100.8", wantScheme: "https"},
		{name: "spoofed prefix is not selected", remote: "10.0.0.4:1234", forwarded: "192.0.2.99, 198.51.100.8", proto: "http, https", hops: 1, want: "198.51.100.8", wantScheme: "https"},
		{name: "single ingress proto applies despite spoofed address prefix", remote: "10.0.0.4:1234", forwarded: "192.0.2.99, 198.51.100.8", proto: "https", hops: 1, want: "198.51.100.8", wantScheme: "https"},
		{name: "two trusted hops", remote: "[2001:db8::3]:1234", forwarded: "2001:db8::1, 2001:db8::2", proto: "https, http", hops: 2, want: "2001:db8::1", wantScheme: "https"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://studio.example.test/path", nil)
			request.RemoteAddr = testCase.remote
			request.Header.Set("X-Forwarded-For", testCase.forwarded)
			request.Header.Set("X-Forwarded-Proto", testCase.proto)
			identity, err := Resolve(request, testCase.hops)
			if err != nil {
				t.Fatal(err)
			}
			if got := identity.Address.String(); got != testCase.want || identity.Scheme != testCase.wantScheme || !identity.Forwarded {
				t.Fatalf("identity = %#v, want address=%s scheme=%s", identity, testCase.want, testCase.wantScheme)
			}
		})
	}
}

func TestResolveFailsClosedToRemoteForMalformedOrInsufficientChain(t *testing.T) {
	for name, forwarded := range map[string]string{
		"malformed":    "not-an-ip",
		"empty member": "198.51.100.8, ",
		"insufficient": "",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://studio.example.test/path", nil)
			request.RemoteAddr = "192.0.2.10:4321"
			request.Header.Set("X-Forwarded-For", forwarded)
			request.Header.Set("X-Forwarded-Proto", "https")
			identity, err := Resolve(request, 1)
			if err != nil {
				t.Fatal(err)
			}
			if got := identity.Address.String(); got != "192.0.2.10" || identity.Scheme != "http" || identity.Forwarded {
				t.Fatalf("identity = %#v", identity)
			}
		})
	}
}

func TestResolveRejectsInvalidRemoteAddress(t *testing.T) {
	request := httptest.NewRequest("GET", "http://studio.example.test/path", nil)
	request.RemoteAddr = "not-an-address"
	if _, err := Resolve(request, 0); err == nil {
		t.Fatal("Resolve error = nil")
	}
}
