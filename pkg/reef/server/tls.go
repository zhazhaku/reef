package server

import (
	"crypto/tls"
	"fmt"
)

// TLSConfig holds TLS configuration for the Reef server.
type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

// Validate checks that required TLS fields are present.
func (c *TLSConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.CertFile == "" {
		return fmt.Errorf("tls.cert_file is required when TLS is enabled")
	}
	if c.KeyFile == "" {
		return fmt.Errorf("tls.key_file is required when TLS is enabled")
	}
	return nil
}

// LoadTLSConfig creates a tls.Config from the TLSConfig.
func (c *TLSConfig) LoadTLSConfig() (*tls.Config, error) {
	if !c.Enabled {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
