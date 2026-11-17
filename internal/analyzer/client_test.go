package analyzer

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestIsPublic(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{ip: "93.184.216.34", want: true},
		{ip: "8.8.8.8", want: true},
		{ip: "2606:2800:220:1:248:1893:25c8:1946", want: true},
		{ip: "127.0.0.1"},
		{ip: "127.8.8.8"},
		{ip: "::1"},
		{ip: "10.0.0.5"},
		{ip: "172.16.0.1"},
		{ip: "172.31.255.255"},
		{ip: "172.32.0.1", want: true},
		{ip: "192.168.1.1"},
		{ip: "169.254.169.254"}, // cloud metadata service
		{ip: "fe80::1"},
		{ip: "fc00::1"},
		{ip: "0.0.0.0"},
		{ip: "0.1.2.3"},
		{ip: "::"},
		{ip: "100.64.0.1"},
		{ip: "192.0.0.1"},
		{ip: "198.18.0.1"},
		{ip: "224.0.0.1"},
		{ip: "ff02::1"},
		{ip: "240.0.0.1"},
		{ip: "255.255.255.255"},
		{ip: "::ffff:127.0.0.1"}, // IPv4-mapped loopback
		{ip: "::ffff:8.8.8.8", want: true},
		{ip: "64:ff9b::7f00:1"}, // NAT64 form of 127.0.0.1
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isPublic(netip.MustParseAddr(tt.ip)); got != tt.want {
				t.Errorf("isPublic(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestPublicOnly(t *testing.T) {
	tests := []struct {
		address string
		blocked bool
	}{
		{address: "93.184.216.34:443"},
		{address: "[2606:2800:220:1:248:1893:25c8:1946]:443"},
		{address: "127.0.0.1:8080", blocked: true},
		{address: "[::1]:8080", blocked: true},
		{address: "10.0.0.5:80", blocked: true},
		{address: "not an address", blocked: true},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			err := publicOnly("tcp", tt.address, nil)
			if got := errors.Is(err, ErrPrivateAddress); got != tt.blocked {
				t.Errorf("publicOnly(%q) = %v, blocked = %v, want %v", tt.address, err, got, tt.blocked)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!DOCTYPE html><title>Local</title>")
	}))
	defer ts.Close()

	t.Run("private addresses refused", func(t *testing.T) {
		_, err := New(NewClient(time.Second, false)).Analyze(t.Context(), ts.URL)
		if !errors.Is(err, ErrPrivateAddress) {
			t.Errorf("err = %v, want ErrPrivateAddress", err)
		}
		if requests != 0 {
			t.Errorf("server got %d requests, want 0", requests)
		}
	})

	t.Run("private addresses allowed", func(t *testing.T) {
		res, err := New(NewClient(time.Second, true)).Analyze(t.Context(), ts.URL)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if res.Title != "Local" {
			t.Errorf("Title = %q, want %q", res.Title, "Local")
		}
	})
}
