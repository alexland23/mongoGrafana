package plugin

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

	"github.com/alandave/mongo-db/pkg/models"
)

// generateTestCert returns a self-signed certificate/key pair, PEM-encoded,
// for exercising PEM parsing without depending on a real CA.
func generateTestCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}

func writeTempFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestReadPreferenceFromString(t *testing.T) {
	for _, mode := range []string{"primary", "primaryPreferred", "secondary", "secondaryPreferred", "nearest"} {
		if _, err := readPreferenceFromString(mode); err != nil {
			t.Errorf("readPreferenceFromString(%q) returned unexpected error: %v", mode, err)
		}
	}

	if _, err := readPreferenceFromString("bogus"); err == nil {
		t.Error("readPreferenceFromString(\"bogus\") expected an error, got nil")
	}
}

func TestBuildTLSConfigSkipVerify(t *testing.T) {
	config := &models.PluginSettings{
		TLSSkipVerify: true,
		Secrets:       &models.SecretPluginSettings{},
	}

	tlsConfig, err := buildTLSConfig(config)
	if err != nil {
		t.Fatalf("buildTLSConfig returned unexpected error: %v", err)
	}
	if !tlsConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
	if tlsConfig.RootCAs != nil {
		t.Error("expected RootCAs to be nil when no CA cert is configured")
	}
}

func TestBuildTLSConfigInvalidCACert(t *testing.T) {
	config := &models.PluginSettings{
		Secrets: &models.SecretPluginSettings{TLSCACert: "not a real cert"},
	}

	if _, err := buildTLSConfig(config); err == nil {
		t.Error("expected an error for an invalid CA certificate, got nil")
	}
}

func TestBuildTLSConfigInvalidClientKeyPair(t *testing.T) {
	config := &models.PluginSettings{
		Secrets: &models.SecretPluginSettings{
			TLSClientCert: "not a real cert",
			TLSClientKey:  "not a real key",
		},
	}

	if _, err := buildTLSConfig(config); err == nil {
		t.Error("expected an error for an invalid client cert/key pair, got nil")
	}
}

func TestBuildTLSConfigCACertFromPath(t *testing.T) {
	certPEM, _ := generateTestCert(t)
	path := writeTempFile(t, "ca.pem", certPEM)

	config := &models.PluginSettings{
		TLSCACertPath: path,
		Secrets:       &models.SecretPluginSettings{TLSCACert: "not a real cert"},
	}

	tlsConfig, err := buildTLSConfig(config)
	if err != nil {
		t.Fatalf("buildTLSConfig returned unexpected error: %v", err)
	}
	if tlsConfig.RootCAs == nil {
		t.Error("expected RootCAs to be set from the CA cert path")
	}
}

func TestBuildTLSConfigClientKeyPairFromPath(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t)
	certPath := writeTempFile(t, "client.pem", certPEM)
	keyPath := writeTempFile(t, "client-key.pem", keyPEM)

	config := &models.PluginSettings{
		TLSClientCertPath: certPath,
		TLSClientKeyPath:  keyPath,
		Secrets: &models.SecretPluginSettings{
			TLSClientCert: "not a real cert",
			TLSClientKey:  "not a real key",
		},
	}

	tlsConfig, err := buildTLSConfig(config)
	if err != nil {
		t.Fatalf("buildTLSConfig returned unexpected error: %v", err)
	}
	if len(tlsConfig.Certificates) != 1 {
		t.Errorf("expected 1 client certificate loaded from path, got %d", len(tlsConfig.Certificates))
	}
}

func TestBuildTLSConfigMissingPathFile(t *testing.T) {
	config := &models.PluginSettings{
		TLSCACertPath: filepath.Join(t.TempDir(), "does-not-exist.pem"),
		Secrets:       &models.SecretPluginSettings{},
	}

	if _, err := buildTLSConfig(config); err == nil {
		t.Error("expected an error when the CA cert path does not exist, got nil")
	}
}
