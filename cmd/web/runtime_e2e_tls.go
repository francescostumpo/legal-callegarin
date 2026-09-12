//go:build e2e

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/config"
)

type inMemoryTLSServer interface {
	ListenAndServeTLS(string, string) error
	Shutdown(context.Context) error
}

type e2eTLSServer struct {
	server inMemoryTLSServer
}

func (server e2eTLSServer) ListenAndServe() error {
	return server.server.ListenAndServeTLS("", "")
}

func (server e2eTLSServer) Shutdown(ctx context.Context) error {
	return server.server.Shutdown(ctx)
}

func serveRuntimeHTTP(ctx context.Context, cfg config.Config, handler http.Handler, logger *slog.Logger) error {
	tlsConfig, err := newE2ETLSConfig(cfg.HTTPAddress, time.Now().UTC(), rand.Reader)
	if err != nil {
		return err
	}
	server := newHTTPServer(cfg, handler, logger)
	server.TLSConfig = tlsConfig
	logServerStarting(logger, buildVersion, buildCommit)
	return serveServer(ctx, e2eTLSServer{server: server}, logger)
}

func newE2ETLSConfig(address string, now time.Time, random io.Reader) (*tls.Config, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("create E2E certificate: invalid listen address")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() || now.IsZero() || random == nil {
		return nil, errors.New("create E2E certificate: loopback IP, clock, and randomness are required")
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), random)
	if err != nil {
		return nil, errors.New("create E2E certificate: generate ephemeral key")
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "legal-callegarin-e2e-loopback"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{ip},
	}
	certificateDER, err := x509.CreateCertificate(random, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, errors.New("create E2E certificate: sign ephemeral certificate")
	}
	certificate := tls.Certificate{
		Certificate: [][]byte{certificateDER},
		PrivateKey:  privateKey,
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
	}, nil
}
