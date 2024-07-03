package common

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-kit/log"
	"github.com/mahendrapaipuri/ceems/pkg/grafana"
	"github.com/prometheus/common/config"
)

type mockConfig struct {
	Field1 string `yaml:"field1"`
	Field2 string `yaml:"field2"`
}

func TestSanitizeFloat(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{
			name:     "With +Inf",
			input:    math.Inf(0),
			expected: 0,
		},
		{
			name:     "With -Inf",
			input:    math.Inf(-1),
			expected: 0,
		},
		{
			name:     "With NaN",
			input:    math.NaN(),
			expected: 0,
		},
	}

	for _, test := range tests {
		got := SanitizeFloat(test.input)
		if got != test.expected {
			t.Errorf("%s: expected %f, got %f", test.name, test.expected, got)
		}
	}
}

func TestGetUuid(t *testing.T) {
	expected := "d808af89-684c-6f3f-a474-8d22b566dd12"
	got, err := GetUUIDFromString([]string{"foo", "1234", "bar567"})
	if err != nil {
		t.Errorf("Failed to generate UUID due to %s", err)
	}

	// Check if UUIDs match
	if expected != got {
		t.Errorf("Mismatched UUIDs. Expected %s Got %s", expected, got)
	}
}

func TestMakeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := `
---
field1: foo
field2: bar`
	configPath := filepath.Join(tmpDir, "config.yml")
	os.WriteFile(configPath, []byte(configFile), 0600)

	// Check error when no file path is provided
	if _, err := MakeConfig[mockConfig](""); err == nil {
		t.Errorf("expected error due to missing file path, got none")
	}

	// Check if config file is correctly read
	expected := &mockConfig{Field1: "foo", Field2: "bar"}
	cfg, err := MakeConfig[mockConfig](configPath)
	if err != nil {
		t.Errorf("failed to read config file %s", err)
	}
	if !reflect.DeepEqual(expected, cfg) {
		t.Errorf("expected config %#v, got %#v", expected, cfg)
	}
}

func TestGetFreePort(t *testing.T) {
	_, _, err := GetFreePort()
	if err != nil {
		t.Errorf("failed to find free port: %s", err)
	}
}

func TestGrafanaClient(t *testing.T) {
	// Start mock server
	expected := "dummy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teamMembers := []grafana.GrafanaTeamsReponse{
			{
				Login: r.Header.Get("Authorization"),
			},
		}
		if err := json.NewEncoder(w).Encode(&teamMembers); err != nil {
			w.Write([]byte("KO"))
		}
	}))
	defer server.Close()

	// Make config file
	config := &GrafanaWebConfig{
		URL: server.URL,
		HTTPClientConfig: config.HTTPClientConfig{
			Authorization: &config.Authorization{
				Type:        "Bearer",
				Credentials: config.Secret(expected),
			},
		},
	}

	// Create grafana client
	var client *grafana.Grafana
	var err error
	if client, err = CreateGrafanaClient(config, log.NewNopLogger()); err != nil {
		t.Errorf("failed to create Grafana client: %s", err)
	}

	teamMembers, err := client.TeamMembers([]string{"1"})
	if err != nil {
		t.Errorf("failed to fetch team members: %s", err)
	}
	if teamMembers[0] != fmt.Sprintf("Bearer %s", expected) {
		t.Errorf("expected %s, got %s", fmt.Sprintf("Bearer %s", expected), teamMembers[0])
	}
}

func TestComputeExternalURL(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{
			input: "",
			valid: true,
		},
		{
			input: "http://proxy.com/prometheus",
			valid: true,
		},
		{
			input: "'https://url/prometheus'",
			valid: false,
		},
		{
			input: "'relative/path/with/quotes'",
			valid: false,
		},
		{
			input: "http://alertmanager.company.com",
			valid: true,
		},
		{
			input: "https://double--dash.de",
			valid: true,
		},
		{
			input: "'http://starts/with/quote",
			valid: false,
		},
		{
			input: "ends/with/quote\"",
			valid: false,
		},
	}

	for _, test := range tests {
		_, err := ComputeExternalURL(test.input, "0.0.0.0:9090")
		if test.valid {
			if err != nil {
				t.Errorf("no error expected, got %s", err)
			}
		} else {
			if err == nil {
				t.Errorf("error expected, got none")
			}
		}
	}
}
