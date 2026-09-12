//go:build e2e

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"
)

func TestE2ETLSConfigUsesAnEphemeralLoopbackCertificate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	configuration, err := newE2ETLSConfig("127.0.0.1:4173", now, bytes.NewReader(bytes.Repeat([]byte{0x42}, 4096)))
	if err != nil {
		t.Fatal(err)
	}
	if configuration.MinVersion != tls.VersionTLS13 || len(configuration.Certificates) != 1 {
		t.Fatalf("TLS configuration = %#v", configuration)
	}
	certificate, err := x509.ParseCertificate(configuration.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := certificate.VerifyHostname("127.0.0.1"); err != nil {
		t.Fatalf("certificate does not cover loopback address: %v", err)
	}
	if !certificate.NotBefore.Before(now) || !certificate.NotAfter.After(now) {
		t.Fatalf("certificate validity = %s to %s", certificate.NotBefore, certificate.NotAfter)
	}
}

func TestE2ETLSServerUsesOnlyInMemoryCertificate(t *testing.T) {
	t.Parallel()

	server := &recordingTLSServer{}
	wrapper := e2eTLSServer{server: server}
	if err := wrapper.ListenAndServe(); !errors.Is(err, errE2ETLSStopped) {
		t.Fatalf("ListenAndServe() error = %v", err)
	}
	if server.certificateFile != "" || server.keyFile != "" {
		t.Fatalf("TLS files = (%q, %q), want empty", server.certificateFile, server.keyFile)
	}
	if err := wrapper.Shutdown(context.Background()); err != nil || !server.shutdownCalled {
		t.Fatalf("Shutdown() error = %v, called = %t", err, server.shutdownCalled)
	}
}

var errE2ETLSStopped = errors.New("E2E TLS server stopped")

type recordingTLSServer struct {
	certificateFile string
	keyFile         string
	shutdownCalled  bool
}

func (server *recordingTLSServer) ListenAndServeTLS(certificateFile, keyFile string) error {
	server.certificateFile = certificateFile
	server.keyFile = keyFile
	return errE2ETLSStopped
}

func (server *recordingTLSServer) Shutdown(context.Context) error {
	server.shutdownCalled = true
	return nil
}

var _ interface {
	ListenAndServeTLS(string, string) error
	Shutdown(context.Context) error
} = (*recordingTLSServer)(nil)
