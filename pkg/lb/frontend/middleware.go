//go:build cgo
// +build cgo

package frontend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	ceems_api_base "github.com/mahendrapaipuri/ceems/pkg/api/base"
	ceems_api "github.com/mahendrapaipuri/ceems/pkg/api/http"
	"github.com/mahendrapaipuri/ceems/pkg/api/models"
	"github.com/mahendrapaipuri/ceems/pkg/lb/base"
	"github.com/prometheus/common/config"
)

// Headers.
const (
	grafanaUserHeader    = "X-Grafana-User"
	dashboardUserHeader  = "X-Dashboard-User"
	loggedUserHeader     = "X-Logged-User"
	adminUserHeader      = "X-Admin-User"
	ceemsClusterIDHeader = "X-Ceems-Cluster-Id"
)

// Allowed resources.
// No need to check caps as Prometheus do not allow
// capitalised query names.
//
// For Prometheus following resources are allowed
// - query
// - query_range
// - labels
// - values
// - series
//
// For Pyroscope following resources are allowed
// - SelectMergeStacktraces
// - LabelNames
// - LabelValues
//
// Not sure if we need to allow more resources for Pyroscope
// So of the resources that need to checked can be found here:
// https://github.com/grafana/pyroscope/blob/b4125b77f44444eb413244d5e56d73a04b1c1def/tools/k6/tests/reads.js#L37-L57
// Maybe we can make it configurable by using below values as default.
var (
	allowedTSDBResources = []string{
		"query",
		"query_range",
		"labels",
		"series",
		"values",
	}
	allowedPyroResources = []string{
		"SelectMergeStacktraces",
		"LabelNames",
		"LabelValues",
	}
)

var (
	// Regex to match path suffix to apply middleware.
	regexpURLPaths             = "/([/]*(%s)?/?)(?:$)"
	regexpAllowedTSDBResources = regexp.MustCompile(fmt.Sprintf(regexpURLPaths, strings.Join(allowedTSDBResources, "|")))
	regexpAllowedPyroResources = regexp.MustCompile(fmt.Sprintf(regexpURLPaths, strings.Join(allowedPyroResources, "|")))

	// Regex that will match unit's UUIDs
	// Dont use greedy matching to avoid capturing gpuuuid label
	// Use strict UUID allowable character set. They can be only letters, digits and hypen (-)
	// Playground: https://goplay.tools/snippet/kq_r_1SOgnG
	regexpUUID = regexp.MustCompile("(?:.*?)[^gpu](?:uuid|service_name)=[~]{0,1}\"(?P<uuid>[a-zA-Z0-9-|]+)\"(?:.*)")

	// Regex that will match cluster's ID.
	regexID = regexp.MustCompile("(?:.+?)ceems_id=[~]{0,1}\"(?P<id>[a-zA-Z0-9-|_]+)\"(?:.*)")
)

// ceems is the struct container for CEEMS API server.
type ceems struct {
	db     *sql.DB
	webURL *url.URL
	client *http.Client
}

func (c *ceems) verifyEndpoint() *url.URL {
	if c.webURL != nil {
		return c.webURL.JoinPath("/api/v1/units/verify")
	}

	return nil
}

func (c *ceems) clustersEndpoint() *url.URL {
	if c.webURL != nil {
		return c.webURL.JoinPath("/api/v1/clusters/admin")
	}

	return nil
}

func (c *ceems) usersEndpoint() *url.URL {
	if c.webURL != nil {
		return c.webURL.JoinPath("/api/v1/users/admin")
	}

	return nil
}

// adminUsers returns the list of admin users either pulling
// from DB or making an API request.
func (c *ceems) adminUsers(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	// Admin users
	var adminUsers []string

	var err error

	// Check if DB is available
	if c.db != nil {
		adminUsers, err = ceems_api.AdminUserNames(ctx, c.db)
		if err != nil {
			return nil, err
		}
	} else if c.webURL != nil {
		// If CEEMS URL is available make a API request
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.usersEndpoint().String(), nil)
		if err != nil {
			return nil, err
		}

		// Add role query parameter to request
		urlVals := url.Values{"role": []string{"admin"}}
		req.URL.RawQuery = urlVals.Encode()

		// Add necessary headers. Use CEEMS service account as user
		req.Header.Add(grafanaUserHeader, ceems_api_base.CEEMSServiceAccount)

		// Make request
		admins, err := ceemsAPIRequest[models.AdminUsers](req, c.client)
		if err != nil {
			return nil, err
		}

		for _, admin := range admins {
			for _, user := range admin.Users {
				if userString, ok := user.(string); ok {
					adminUsers = append(adminUsers, userString)
				}
			}
		}
	}

	return adminUsers, nil
}

// authenticationMiddleware implements the auth middleware for LB.
type authenticationMiddleware struct {
	logger        *slog.Logger
	ceems         *ceems
	clusterIDs    []string
	pathsACLRegex *regexp.Regexp
	parseRequest  func(*ReqParams, *http.Request) error
}

// newAuthMiddleware setups new auth middleware.
func newAuthMiddleware(c *Config) (*authenticationMiddleware, error) {
	var db *sql.DB

	var ceemsClient *http.Client

	var ceemsWebURL *url.URL

	var err error

	// Check if DB path exists and get pointer to DB connection
	if c.APIServer.Data.Path != "" {
		dbAbsPath, err := filepath.Abs(
			filepath.Join(c.APIServer.Data.Path, ceems_api_base.CEEMSDBName),
		)
		if err != nil {
			return nil, err
		}

		// Set DB pointer only if file exists. Else sql.Open will create an empty
		// file as if exists already
		if _, err := os.Stat(dbAbsPath); err == nil {
			dsn := fmt.Sprintf("file:%s?%s", dbAbsPath, "_mutex=no&mode=ro&_busy_timeout=5000")

			if db, err = sql.Open("sqlite3", dsn); err != nil {
				return nil, err
			}
		}
	}

	// Check if URL for CEEMS API exists
	if c.APIServer.Web.URL != "" {
		// Unwrap original error to avoid leaking sensitive passwords in output
		ceemsWebURL, err = url.Parse(c.APIServer.Web.URL)
		if err != nil {
			return nil, errors.Unwrap(err)
		}

		// Make a CEEMS API server client from client config
		if ceemsClient, err = config.NewClientFromConfig(c.APIServer.Web.HTTPClientConfig, "ceems_api_server"); err != nil {
			return nil, err
		}
	}

	// Setup middleware
	amw := &authenticationMiddleware{
		logger: c.Logger,
		ceems: &ceems{
			db:     db,
			webURL: ceemsWebURL,
			client: ceemsClient,
		},
	}

	// Setup parsing functions based on LB type
	switch c.LBType {
	case base.PromLB:
		amw.parseRequest = parseTSDBRequest
		amw.pathsACLRegex = regexpAllowedTSDBResources
	case base.PyroLB:
		amw.parseRequest = parsePyroRequest
		amw.pathsACLRegex = regexpAllowedPyroResources
	}

	return amw, nil
}

// isAdminUser returns true if user is in admin users list.
func (amw *authenticationMiddleware) isAdminUser(ctx context.Context, user string) bool {
	// Get current admin users
	adminUsers, err := amw.ceems.adminUsers(ctx)
	if err != nil {
		amw.logger.Error("Failed to fetch admin users", "err", err)

		return false
	}

	return slices.Contains(adminUsers, user)
}

// Check UUIDs in query belong to user or not.
func (amw *authenticationMiddleware) isUserUnit(
	ctx context.Context,
	user string,
	clusterIDs []string,
	uuids []string,
	starts []int64,
) bool {
	// Always prefer checking with DB connection directly if it is available
	// As DB query is way more faster than HTTP API request
	if amw.ceems.db != nil {
		return ceems_api.VerifyOwnership(ctx, user, clusterIDs, uuids, starts, amw.ceems.db, amw.logger)
	}

	// If CEEMS URL is available make a API request
	// Any errors in making HTTP request will fail the query. This can happen due
	// to deployment issues and by failing queries we make operators to look into
	// what is happening
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, amw.ceems.verifyEndpoint().String(), nil)
	if err != nil {
		amw.logger.Error("Failed to create new request for unit ownership verification",
			"user", user, "queried_uuids", strings.Join(uuids, ","), "err", err)

		return false
	}

	// Add uuids to request
	urlVals := url.Values{"uuid": uuids, "cluster_id": clusterIDs}
	for _, s := range starts {
		urlVals.Add("time", strconv.FormatInt(s, 10))
	}

	req.URL.RawQuery = urlVals.Encode()

	// Add necessary headers
	req.Header.Add(grafanaUserHeader, user)

	// Make request
	// If request failed, forbid the query. It can happen when CEEMS API server
	// goes offline and we should wait for it to come back online
	if resp, err := amw.ceems.client.Do(req); err != nil {
		amw.logger.Error("Failed to make request for unit ownership verification",
			"user", user, "queried_uuids", strings.Join(uuids, ","), "err", err)

		return false
	} else if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		amw.logger.Error("Unauthorised query", "user", user,
			"queried_uuids", strings.Join(uuids, ","), "status_code", resp.StatusCode)

		return false
	}

	return true
}

// Middleware function, which will be called for each request.
func (amw *authenticationMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var loggedUser string

		reqParams := &ReqParams{}

		var isAdmin bool

		var err error

		// Get cluster id from X-Ceems-Cluster-Id header
		// This is most important and request parameter that we need
		// to proxy request. Rest of them are optional
		reqParams.clusterID = r.Header.Get(ceemsClusterIDHeader)

		// Verify clusterID is in list of valid cluster IDs
		if !slices.Contains(amw.clusterIDs, reqParams.clusterID) {
			amw.logger.Error("ClusterID header not found. Bad request", "url", r.URL)

			// Write an error and stop the handler chain
			w.WriteHeader(http.StatusBadRequest)

			response := ceems_api.Response[any]{
				Status:    "error",
				ErrorType: "bad_request",
				Error:     "invalid cluster ID. Set cluster ID using X-Ceems-Cluster-Id header in Prometheus datasource.",
			}
			if err := json.NewEncoder(w).Encode(&response); err != nil {
				amw.logger.Error("Failed to encode response", "err", err)
				w.Write([]byte("KO"))
			}

			return
		}

		// If ceems url or db is not configured, pass through. There is nothing
		// to check here
		if amw.ceems.webURL == nil && amw.ceems.db == nil {
			goto end
		}

		// Check if user header exists
		// Remove any X-Admin-User header or X-Logged-User if passed
		r.Header.Del(adminUserHeader)
		r.Header.Del(loggedUserHeader)

		// Check if username header is available
		loggedUser = r.Header.Get(grafanaUserHeader)
		if loggedUser == "" {
			amw.logger.Error("Grafana user Header not found. Denying authentication", "url", r.URL)

			// Write an error and stop the handler chain
			w.WriteHeader(http.StatusUnauthorized)

			response := ceems_api.Response[any]{
				Status:    "error",
				ErrorType: "unauthorized",
				Error:     "no user header found. Make sure to set send_user_header = true in [dataproxy] section of Grafana configuration file.",
			}
			if err := json.NewEncoder(w).Encode(&response); err != nil {
				amw.logger.Error("Failed to encode response", "err", err)
				w.Write([]byte("KO"))
			}

			return
		}

		amw.logger.Debug("middleware", "logged_user", loggedUser, "url", r.URL)

		// Set logged user header
		r.Header.Set(loggedUserHeader, loggedUser)

		// Check if user is admin
		isAdmin = amw.isAdminUser(r.Context(), loggedUser)

		// Allow only white listed resources and forbid all others for normal users
		// Skip this check for admin users
		if !amw.pathsACLRegex.MatchString(r.URL.Path) && !isAdmin {
			amw.logger.Error("Forbidden resource", "logged_user", loggedUser, "resource", r.URL.Path)

			// Write an error and stop the handler chain
			w.WriteHeader(http.StatusForbidden)

			response := ceems_api.Response[any]{
				Status:    "error",
				ErrorType: "forbidden",
				Error:     "user do not have permissions to this resource",
			}
			if err := json.NewEncoder(w).Encode(&response); err != nil {
				amw.logger.Error("Failed to encode response", "err", err)
				w.Write([]byte("KO"))
			}

			return
		}

		// Clone request, parse query params and set them in request context
		// This will ensure we set query params in request's context always
		if err = amw.parseRequest(reqParams, r); err != nil {
			amw.logger.Error("Failed to parse query in the request", "logged_user", loggedUser, "err", err)
		}

		// By this time, we parsed the query and we do not need to do next
		// verification for admin users
		if isAdmin {
			goto end
		}

		// Check if user is querying for his/her own compute units by looking to DB
		// If the current user is admin, allow query
		if !amw.isUserUnit(
			r.Context(),
			loggedUser,
			[]string{reqParams.clusterID},
			reqParams.uuids,
			[]int64{reqParams.time},
		) {
			// Write an error and stop the handler chain
			w.WriteHeader(http.StatusForbidden)

			response := ceems_api.Response[any]{
				Status:    "error",
				ErrorType: "forbidden",
				Error:     "user do not have permissions to view unit metrics",
			}
			if err := json.NewEncoder(w).Encode(&response); err != nil {
				amw.logger.Error("Failed to encode response", "err", err)
				w.Write([]byte("KO"))
			}

			return
		}

	end:
		// Set query params to request's context before passing down request
		r = setQueryParams(r, reqParams)

		// Pass down the request to the next middleware (or final handler)
		next.ServeHTTP(w, r)
	})
}
