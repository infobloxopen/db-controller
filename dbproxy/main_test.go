package dbproxy

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

var (
	testdb                           *sql.DB
	tempDir                          string
	testDSN                          string
	testDSNURIPath, testPasswordPath string
	testDSNPath                      string
)

func TestMain(m *testing.M) {
	var err error
	tempDir, err = os.MkdirTemp("", "dbproxy")
	if err != nil {
		panic(err)
	}

	var cleanupTestDB func()
	testdb, testDSN, cleanupTestDB = Run(RunConfig{
		Database:  "postgres",
		Username:  "postgres",
		Password:  "postgres",
		DockerTag: "15",
	})

	// Wrote various DSN formats to temp files
	testDSNURIPath = fmt.Sprintf("%s/uri_dsn.txt", tempDir)
	testPasswordPath = fmt.Sprintf("%s/password.txt", tempDir)
	testDSNPath = fmt.Sprintf("%s/dsn.txt", tempDir)

	if err := ioutil.WriteFile(testDSNURIPath, []byte(testDSN), 0644); err != nil {
		panic(err)
	}

	oldDSN, err := pq.ParseURL(testDSN)
	if err != nil {
		panic(err)
	}

	if err := ioutil.WriteFile(testDSNPath, []byte(oldDSN), 0644); err != nil {
		panic(err)
	}

	defer cleanupTestDB()

	m.Run()

}

func getEphemeralPort() int {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	defer l.Close() // nolint:errcheck
	return l.Addr().(*net.TCPAddr).Port
}

// generateRandomString creates a random string of the specified length
func generateRandomString(length int) (string, error) {
	// Create a byte slice to hold the random data
	randomBytes := make([]byte, length)

	// Read random data into the slice
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	// Encode the random data to a base64 string
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func randomWithPrefix(prefix string) string {
	random, err := generateRandomString(5)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s%s", prefix, random)
}

type RunConfig struct {
	HostName  string
	DockerTag string
	Username  string
	Password  string
	Database  string
	// Network, optional, is the name of the Docker network to attach the container to.
	Network string
}

// Run a PostgreSQL database in a Docker container and return a connection to it.
// The caller is responsible for calling the func() to prevent leaking containers.
func Run(cfg RunConfig) (*sql.DB, string, func()) {
	port := getEphemeralPort()

	// Required parameters
	if cfg.Database == "" {
		panic("database name is required")
	}

	// Optional parameters
	if cfg.Username == "" {
		cfg.Username = randomWithPrefix("user")
	}
	if cfg.Password == "" {
		cfg.Password = randomWithPrefix("pass")
	}

	args := []string{
		"run",
		"-d",
		"-p", fmt.Sprintf("%d:5432", port),
		"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", cfg.Password),
		"-e", fmt.Sprintf("POSTGRES_USER=%s", cfg.Username),
		"-e", fmt.Sprintf("POSTGRES_DB=%s", cfg.Database),
	}

	// Optional docker cli parameters
	if cfg.HostName != "" {
		args = append(args, "--hostname", cfg.HostName)
	}

	if cfg.Network != "" {
		args = append(args, "--network", cfg.Network)
	}

	if cfg.DockerTag == "" {
		cfg.DockerTag = "latest"
	}

	ctrArgs := []string{fmt.Sprintf("postgres:%s", cfg.DockerTag), "postgres", "-c", "wal_level=logical"}

	// Run PostgreSQL in Docker
	cmd := exec.Command("docker", append(args, ctrArgs...)...)
	logger.V(1).Info(cmd.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		logger.Error(err, "failed to run docker container")
		logger.Info(cmd.String())
		logger.Info("stderr", stderr.String())
		os.Exit(1)
	}
	logger.V(1).Info(string(out))
	container := string(out[:len(out)-1]) // remove newline

	// Exercise hotload
	//hotload.RegisterSQLDriver("pgx", stdlib.GetDefaultDriver())
	dsn := fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?sslmode=disable", url.QueryEscape(cfg.Username), url.QueryEscape(cfg.Password), port, cfg.Database)
	logger.V(1).Info(dsn)
	f, err := os.CreateTemp("", "dsn.txt")
	if err != nil {
		panic(err)
	}
	if _, err := f.WriteString(dsn); err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}

	// TODO: read from file
	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		panic(err)
	}

	now := time.Now()
	err = RetryFn(nil, func() error {
		return conn.Ping()
	}, 10*time.Millisecond, 30*time.Second)

	if err != nil {
		logger.Error(err, "failed to connect to database")

		cmd = exec.Command("docker", "logs", container)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			logger.Error(err, "failed to get logs")
		}
		logger.Info(string(out))
		os.Exit(1)
	}
	// TODO: change this to debug logging, just timing jenkins for now
	logger.Info("db_connected", "duration", time.Since(now))

	return conn, dsn, func() {
		// Cleanup container on close, dont exit without trying all steps first
		now := time.Now()
		defer func() {
			logger.V(1).Info("container_cleanup_took", "duration", time.Since(now))
		}()

		err := os.Remove(f.Name())
		if err != nil {
			logger.Error(err, "failed to remove temp file")
		}

		cmd := exec.Command("docker", "rm", "-f", container)
		// This take 10 seconds to run, and we don't care if
		// it was successful. So use Start() to not wait for
		// it to finish.
		logger.Info(cmd.String())
		if err := cmd.Start(); err != nil {
			logger.Error(err, "failed to remove container")
		}
	}
}

// RetryFn retries a function until it succeeds or the timeout is reached
func RetryFn(t *testing.T, mightFail func() error, retryInterval, timeout time.Duration) error {
	if t != nil {
		t.Helper()
	}

	var err error
	startTime := time.Now()

	for time.Since(startTime) < timeout {
		err = mightFail()
		if err == nil {
			return nil
		}
		time.Sleep(retryInterval)
	}

	// If we reach here, the function did not succeed within the timeout
	if t != nil {
		t.Logf("retry_did_not_succeed within %s: %v", timeout, err)
	}
	return err
}
