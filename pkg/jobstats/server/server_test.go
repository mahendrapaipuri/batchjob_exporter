package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/mahendrapaipuri/batchjob_monitor/pkg/jobstats/base"
)

func setupServer() *JobstatsServer {
	logger := log.NewNopLogger()
	server, _, _ := NewJobstatsServer(&Config{Logger: logger})
	server.maxQueryPeriod = time.Duration(time.Hour * 168)
	server.grafana = &GrafanaConfig{available: false, Admins: getMockAdminUsers}
	server.Accounts = getMockAccounts
	server.Jobs = getMockJobs
	return server
}

func getMockAccounts(dbTable string, user string, logger log.Logger) ([]base.Account, error) {
	return []base.Account{{ID: "foo"}, {ID: "bar"}}, nil
}

func getMockJobs(
	query Query,
	logger log.Logger,
) ([]base.JobStats, error) {
	return []base.JobStats{{Jobid: 1000, Usr: "user"}, {Jobid: 10001, Usr: "user"}}, nil
}

func getMockAdminUsers(url string, client *http.Client, logger log.Logger) ([]string, error) {
	return []string{"adm1", "adm2"}, nil
}

// Test admin users sync
func TestAdminUsersSync(t *testing.T) {
	server := setupServer()
	server.grafana.available = true
	server.grafana.teamMembersEndpoint, _ = url.Parse("http://localhost:9090")
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)

	// Start recorder
	w := httptest.NewRecorder()
	server.accounts(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Expected admin users
	adminUsers, _ := getMockAdminUsers("", nil, server.logger)
	if !reflect.DeepEqual(adminUsers, server.adminUsers) {
		t.Errorf("expected %v got %v", adminUsers, server.adminUsers)
	}
}

// Test /api/accounts when no user header found
func TestAccountsHandlerNoUserHeader(t *testing.T) {
	server := setupServer()
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)

	// Start recorder
	w := httptest.NewRecorder()
	server.accounts(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Unmarshal byte into structs.
	var response base.AccountsResponse
	json.Unmarshal(data, &response)

	if response.Status != "error" {
		t.Errorf("expected error status got %v", response.Status)
	}
	if response.ErrorType != "user_error" {
		t.Errorf("expected user_error type got %v", response.ErrorType)
	}
	if !reflect.DeepEqual(response.Data, []base.Account{}) {
		t.Errorf("expected empty data got %v", response.Data)
	}
}

// Test /api/accounts when header found
func TestAccountsHandlerWithUserHeader(t *testing.T) {
	server := setupServer()
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	// Add user header
	req.Header.Set("X-Grafana-User", "foo")

	// Start recorder
	w := httptest.NewRecorder()
	server.accounts(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Expected result
	expectedAccounts, _ := getMockAccounts("mockDB", "foo", server.logger)

	// Unmarshal byte into structs.
	var response base.AccountsResponse
	json.Unmarshal(data, &response)

	if response.Status != "success" {
		t.Errorf("expected success status got %v", response.Status)
	}
	if !reflect.DeepEqual(response.Data, expectedAccounts) {
		t.Errorf("expected %v got %v", expectedAccounts, response.Data)
	}
}

// Test /api/jobs when no user header found
func TestJobsHandlerNoUserHeader(t *testing.T) {
	server := setupServer()
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)

	// Start recorder
	w := httptest.NewRecorder()
	server.jobs(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Unmarshal byte into structs.
	var response base.JobsResponse
	json.Unmarshal(data, &response)

	if response.Status != "error" {
		t.Errorf("expected error status got %v", response.Status)
	}
	if response.ErrorType != "user_error" {
		t.Errorf("expected user_error type got %v", response.ErrorType)
	}
	if !reflect.DeepEqual(response.Data, []base.JobStats{}) {
		t.Errorf("expected empty data got %v", response.Data)
	}
}

// Test /api/jobs when user header found
func TestJobsHandlerWithUserHeader(t *testing.T) {
	server := setupServer()
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	// Add user header
	currentUser := "foo"
	req.Header.Set("X-Grafana-User", currentUser)

	// Start recorder
	w := httptest.NewRecorder()
	server.jobs(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Expected result
	expectedJobs, _ := getMockJobs(Query{}, server.logger)

	// Unmarshal byte into structs.
	var response base.JobsResponse
	json.Unmarshal(data, &response)

	if response.Status != "success" {
		t.Errorf("expected success status got %v", response.Status)
	}
	if !reflect.DeepEqual(response.Data, expectedJobs) {
		t.Errorf("expected %v got %v", expectedJobs, response.Data)
	}
}

// Test /api/jobs when user header and impersonated user header found
func TestJobsHandlerWithUserHeaderAndAdmin(t *testing.T) {
	server := setupServer()
	server.adminUsers = []string{"admin"}
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	// Add user header
	req.Header.Set("X-Grafana-User", server.adminUsers[0])
	req.Header.Set("X-Dashboard-User", "foo")

	// Start recorder
	w := httptest.NewRecorder()
	server.jobs(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Expected result
	expectedJobs, _ := getMockJobs(Query{}, server.logger)

	// Unmarshal byte into structs.
	var response base.JobsResponse
	json.Unmarshal(data, &response)

	if response.Status != "success" {
		t.Errorf("expected success status got %v", response.Status)
	}
	if !reflect.DeepEqual(response.Data, expectedJobs) {
		t.Errorf("expected %v got %v", expectedJobs, response.Data)
	}
}

// Test /api/jobs when from/to query parameters are malformed
func TestJobsHandlerWithMalformedQueryParams(t *testing.T) {
	server := setupServer()
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	// Add user header
	req.Header.Set("X-Grafana-User", "foo")
	// Add from query parameter
	q := req.URL.Query()
	q.Add("from", "10-12-2023")
	req.URL.RawQuery = q.Encode()

	// Start recorder
	w := httptest.NewRecorder()
	server.jobs(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Unmarshal byte into structs.
	var response base.JobsResponse
	json.Unmarshal(data, &response)

	if response.Status != "error" {
		t.Errorf("expected error status got %v", response.Status)
	}
	if response.ErrorType != "data_error" {
		t.Errorf("expected data_error type got %v", response.ErrorType)
	}
	if !reflect.DeepEqual(response.Data, []base.JobStats{}) {
		t.Errorf("expected empty data got %v", response.Data)
	}
}

// Test /api/jobs when from/to query parameters exceed max time window
func TestJobsHandlerWithQueryWindowExceeded(t *testing.T) {
	server := setupServer()
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	// Add user header
	req.Header.Set("X-Grafana-User", "foo")
	// Add from query parameter
	q := req.URL.Query()
	q.Add("from", "1672527600")
	q.Add("to", "1685570400")
	req.URL.RawQuery = q.Encode()

	// Start recorder
	w := httptest.NewRecorder()
	server.jobs(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Unmarshal byte into structs.
	var response base.JobsResponse
	json.Unmarshal(data, &response)

	if response.Status != "error" {
		t.Errorf("expected error status got %v", response.Status)
	}
	if response.Error != "Maximum query window exceeded" {
		t.Errorf("expected Maximum time window exceeded got %v", response.Error)
	}
	if !reflect.DeepEqual(response.Data, []base.JobStats{}) {
		t.Errorf("expected empty data got %v", response.Data)
	}
}

// Test /api/jobs when from/to query parameters exceed max time window but when jobuuids
// are present
func TestJobsHandlerWithJobuuidsQueryParams(t *testing.T) {
	server := setupServer()
	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	// Add user header
	req.Header.Set("X-Grafana-User", "foo")
	// Add from query parameter
	q := req.URL.Query()
	q.Add("jobuuid", "foo-bar")
	req.URL.RawQuery = q.Encode()

	// Start recorder
	w := httptest.NewRecorder()
	server.jobs(w, req)
	res := w.Result()
	defer res.Body.Close()

	// Get body
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}

	// Expected result
	expectedJobs, _ := getMockJobs(Query{}, server.logger)

	// Unmarshal byte into structs.
	var response base.JobsResponse
	json.Unmarshal(data, &response)

	if response.Status != "success" {
		t.Errorf("expected success status got %v", response.Status)
	}
	if !reflect.DeepEqual(response.Data, expectedJobs) {
		t.Errorf("expected %v got %v", expectedJobs, response.Data)
	}
}
