package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTLSConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TLSConfig
		wantErr bool
	}{
		{"disabled", TLSConfig{Enabled: false}, false},
		{"missing cert", TLSConfig{Enabled: true, KeyFile: "key.pem"}, true},
		{"missing key", TLSConfig{Enabled: true, CertFile: "cert.pem"}, true},
		{"valid", TLSConfig{Enabled: true, CertFile: "cert.pem", KeyFile: "key.pem"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTLSConfig_LoadTLSConfig(t *testing.T) {
	// Generate self-signed cert for testing
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	if err := generateTestCert(certFile, keyFile); err != nil {
		t.Fatalf("generate test cert: %v", err)
	}

	cfg := TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	tlsCfg, err := cfg.LoadTLSConfig()
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}
	if tlsCfg.MinVersion != 0x0303 { // tls.VersionTLS12
		t.Errorf("expected MinVersion TLS 1.2, got %x", tlsCfg.MinVersion)
	}
}

func TestTLSConfig_LoadTLSConfig_Disabled(t *testing.T) {
	cfg := TLSConfig{Enabled: false}
	tlsCfg, err := cfg.LoadTLSConfig()
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if tlsCfg != nil {
		t.Error("expected nil tls.Config when disabled")
	}
}

func TestTLSConfig_LoadTLSConfig_BadFiles(t *testing.T) {
	cfg := TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}
	_, err := cfg.LoadTLSConfig()
	if err == nil {
		t.Error("expected error for nonexistent files")
	}
}

// generateTestCert creates a self-signed certificate for testing.
func generateTestCert(certFile, keyFile string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyOut, err := os.Create(keyFile)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return nil
}
