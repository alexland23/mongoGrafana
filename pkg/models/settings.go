package models

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type PluginSettings struct {
	// ConnectionString is a MongoDB URI, e.g. mongodb://localhost:27017 or
	// mongodb+srv://cluster0.example.net. Options such as tls, replicaSet and
	// authSource can be passed as URI parameters.
	ConnectionString string `json:"connectionString"`
	// Database is the default database queries run against. It can be
	// overridden per query.
	Database string `json:"database"`
	// Username enables authentication when set; the password comes from
	// secure JSON data.
	Username string `json:"username"`
	// TimeoutSeconds bounds each query's execution time. Defaults to 30.
	TimeoutSeconds int `json:"timeoutSeconds"`

	// TLSEnabled turns on TLS for the connection independent of any "tls"
	// URI parameter.
	TLSEnabled bool `json:"tlsEnabled"`
	// TLSSkipVerify disables server certificate verification. Insecure;
	// intended for self-signed certs in dev/test environments.
	TLSSkipVerify bool `json:"tlsSkipVerify"`
	// ReadPreference is one of primary, primaryPreferred, secondary,
	// secondaryPreferred, nearest. Empty means driver default (primary).
	ReadPreference string `json:"readPreference"`
	// TLSCACertPath, TLSClientCertPath and TLSClientKeyPath are paths to
	// PEM files on the machine running the plugin backend. When set, each
	// takes precedence over the corresponding pasted certificate content.
	TLSCACertPath     string `json:"tlsCaCertPath"`
	TLSClientCertPath string `json:"tlsClientCertPath"`
	TLSClientKeyPath  string `json:"tlsClientKeyPath"`
	// ConnectTimeoutSeconds bounds initial server connection time. Defaults to 10.
	ConnectTimeoutSeconds int `json:"connectTimeoutSeconds"`
	// MaxPoolSize caps the number of pooled connections per server. Defaults
	// to the driver default (100) when zero.
	MaxPoolSize uint64 `json:"maxPoolSize"`

	Secrets *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	Password string `json:"password"`
	// TLSCACert, TLSClientCert and TLSClientKey are PEM-encoded and only
	// used when TLSEnabled is set.
	TLSCACert     string `json:"tlsCaCert"`
	TLSClientCert string `json:"tlsClientCert"`
	TLSClientKey  string `json:"tlsClientKey"`
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	err := json.Unmarshal(source.JSONData, &settings)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
	}

	if settings.ConnectionString == "" {
		return nil, errors.New("connection string is required")
	}
	if settings.TimeoutSeconds <= 0 {
		settings.TimeoutSeconds = 30
	}
	if settings.ConnectTimeoutSeconds <= 0 {
		settings.ConnectTimeoutSeconds = 10
	}

	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)

	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		Password:      source["password"],
		TLSCACert:     source["tlsCaCert"],
		TLSClientCert: source["tlsClientCert"],
		TLSClientKey:  source["tlsClientKey"],
	}
}
