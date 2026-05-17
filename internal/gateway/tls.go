package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net"
	"os"
)

type TLSConfig struct {
	CertFile       string
	KeyFile        string
	ClientCAFile   string
	ClientVerify   bool
}

func LoadTLSCert(certFile, keyFile string) (*tls.Certificate, error) {
	if certFile == "" || keyFile == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	slog.Info("TLS certificate loaded", "cert", certFile)
	return &cert, nil
}

func NewTLSListener(addr string, certFile, keyFile string) (net.Listener, error) {
	return NewTLSListenerWithClientAuth(addr, TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
}

func NewTLSListenerWithClientAuth(addr string, cfg TLSConfig) (net.Listener, error) {
	cert, err := LoadTLSCert(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, err
	}

	if cert == nil {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	if cfg.ClientCAFile != "" {
		caCert, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, err
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			slog.Warn("failed to append client CA certificates", "file", cfg.ClientCAFile)
		}

		tlsConfig.ClientCAs = caCertPool

		if cfg.ClientVerify {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
			slog.Info("TLS client certificate verification enabled", "ca", cfg.ClientCAFile)
		} else {
			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
			slog.Info("TLS client certificate verification optional", "ca", cfg.ClientCAFile)
		}
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	return tls.NewListener(listener, tlsConfig), nil
}
