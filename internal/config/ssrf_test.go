package config

import (
	"testing"
)

func TestIsPrivateAddressPrivateIPs(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{"10.x", "10.0.0.1", true},
		{"10.x other", "10.255.255.255", true},
		{"172.16.x", "172.16.0.1", true},
		{"172.31.x", "172.31.255.255", true},
		{"192.168.x", "192.168.1.1", true},
		{"192.168.x other", "192.168.0.100", true},
		{"127.x", "127.0.0.1", true},
		{"127.x other", "127.0.0.100", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPrivateAddress(tt.address)
			if got != tt.want {
				t.Errorf("IsPrivateAddress(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestIsPrivateAddressPublicIPs(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{"8.8.8.8", "8.8.8.8", false},
		{"1.1.1.1", "1.1.1.1", false},
		{"203.0.113.1", "203.0.113.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPrivateAddress(tt.address)
			if got != tt.want {
				t.Errorf("IsPrivateAddress(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestIsPrivateAddressWithPort(t *testing.T) {
	got := IsPrivateAddress("192.168.1.1:8080")
	if !got {
		t.Error("IsPrivateAddress(\"192.168.1.1:8080\") = false, want true")
	}

	got = IsPrivateAddress("8.8.8.8:443")
	if got {
		t.Error("IsPrivateAddress(\"8.8.8.8:443\") = true, want false")
	}
}

func TestIsPrivateAddressWithScheme(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{"http private", "http://10.0.0.1", true},
		{"https private", "https://10.0.0.1", true},
		{"http public", "http://8.8.8.8", false},
		{"https public", "https://8.8.8.8", false},
		{"http private with port", "http://192.168.1.1:8080", true},
		{"https public with port", "https://1.1.1.1:443", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPrivateAddress(tt.address)
			if got != tt.want {
				t.Errorf("IsPrivateAddress(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestIsPrivateAddressIPv6(t *testing.T) {
	got := IsPrivateAddress("::1")
	if !got {
		t.Error("IsPrivateAddress(\"::1\") = false, want true")
	}

	got = IsPrivateAddress("fc00::1")
	if !got {
		t.Error("IsPrivateAddress(\"fc00::1\") = false, want true")
	}
}

func TestIsPrivateAddressInvalid(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{"empty", "", false},
		{"invalid", "not-an-ip", false},
		{"garbage", "!!!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPrivateAddress(tt.address)
			if got != tt.want {
				t.Errorf("IsPrivateAddress(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestValidateNoSSRFWithPrivateBackend(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Listen: ":8080"},
		Routes: []RouteConfig{
			{
				Match:   RouteMatchConfig{PathPrefix: "/api"},
				Backend: []BackendConfig{{Address: "192.168.1.1:8080"}},
			},
		},
	}

	err := ValidateNoSSRF(cfg)
	if err == nil {
		t.Error("expected error for private backend address")
	}
}

func TestValidateNoSSRFAllowPrivate(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Listen: ":8080"},
		ConfigStore: ConfigStoreConfig{
			AllowPrivateBackends: true,
		},
		Routes: []RouteConfig{
			{
				Match:   RouteMatchConfig{PathPrefix: "/api"},
				Backend: []BackendConfig{{Address: "192.168.1.1:8080"}},
			},
		},
	}

	err := ValidateNoSSRF(cfg)
	if err != nil {
		t.Errorf("expected no error when allow_private_backends=true, got %v", err)
	}
}

func TestValidateNoSSRFPublicBackend(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Listen: ":8080"},
		Routes: []RouteConfig{
			{
				Match:   RouteMatchConfig{PathPrefix: "/api"},
				Backend: []BackendConfig{{Address: "8.8.8.8:8080"}},
			},
		},
	}

	err := ValidateNoSSRF(cfg)
	if err != nil {
		t.Errorf("expected no error for public backend, got %v", err)
	}
}
