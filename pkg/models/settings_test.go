package models

import (
	"encoding/json"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestLoadPluginSettingsDefaults(t *testing.T) {
	jsonData, err := json.Marshal(map[string]any{"connectionString": "mongodb://localhost:27017"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	settings, err := LoadPluginSettings(backend.DataSourceInstanceSettings{JSONData: jsonData})
	if err != nil {
		t.Fatalf("LoadPluginSettings returned unexpected error: %v", err)
	}

	if settings.TimeoutSeconds != 30 {
		t.Errorf("TimeoutSeconds = %d, want default 30", settings.TimeoutSeconds)
	}
	if settings.ConnectTimeoutSeconds != 10 {
		t.Errorf("ConnectTimeoutSeconds = %d, want default 10", settings.ConnectTimeoutSeconds)
	}
}

func TestLoadPluginSettingsTLSSecrets(t *testing.T) {
	jsonData, err := json.Marshal(map[string]any{
		"connectionString": "mongodb://localhost:27017",
		"tlsEnabled":       true,
		"readPreference":   "secondaryPreferred",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	settings, err := LoadPluginSettings(backend.DataSourceInstanceSettings{
		JSONData: jsonData,
		DecryptedSecureJSONData: map[string]string{
			"tlsCaCert":     "ca-pem",
			"tlsClientCert": "cert-pem",
			"tlsClientKey":  "key-pem",
		},
	})
	if err != nil {
		t.Fatalf("LoadPluginSettings returned unexpected error: %v", err)
	}

	if !settings.TLSEnabled {
		t.Error("TLSEnabled = false, want true")
	}
	if settings.ReadPreference != "secondaryPreferred" {
		t.Errorf("ReadPreference = %q, want secondaryPreferred", settings.ReadPreference)
	}
	if settings.Secrets.TLSCACert != "ca-pem" || settings.Secrets.TLSClientCert != "cert-pem" || settings.Secrets.TLSClientKey != "key-pem" {
		t.Errorf("unexpected TLS secrets: %+v", settings.Secrets)
	}
}
