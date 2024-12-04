package dockerdb

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

var debugLevel = 1

// This does not write to stdout or stderr
var logger logr.Logger

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

func StartNetwork(networkName string) func() {

	var errBuf bytes.Buffer

	// List any containers attached to the network and stop then remove them
	cmd := exec.Command("docker", "ps", "-q", "-a", "--filter", fmt.Sprintf("network=%s", networkName))
	logger.V(debugLevel).Info(cmd.String())
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		logger.Error(err, errBuf.String())
		os.Exit(1)
	}
	logger.V(debugLevel).Info(string(out))

	if len(out) > 0 {
		// Containers are attached to the network, remove them
		cmd = exec.Command("docker", "rm", "-f", string(out))
		logger.V(debugLevel).Info("removing containers attached to network", "cmd", cmd.String())
		cmd.Stderr = &errBuf
		bs, err := cmd.Output()
		if err != nil {
			logger.Error(err, "failed to remove containers attached to network", "buf", errBuf.String())
			os.Exit(1)
		}
		logger.V(debugLevel).Info(string(bs))
	}

	// Check if network already exists
	cmd = exec.Command("docker", "network", "inspect", "pgctl")
	cmd.Stderr = &errBuf
	logger.V(debugLevel).Info(cmd.String())
	err = cmd.Run()
	if err != nil {
		// Daemons report different errors for network not found
		if !strings.Contains(errBuf.String(), "not found") &&
			!strings.Contains(errBuf.String(), "No such network") {
			logger.Error(err, "unhandled_err", "stderr", errBuf.String())
			os.Exit(1)
		}
		// Network does not exist
		cmd = exec.Command("docker", "network", "create", "pgctl")
		cmd.Stderr = &errBuf
		if err := cmd.Run(); err != nil {
			logger.Error(err, "failed to create network", "stderr", errBuf.String())
			os.Exit(1)
		}
	}

	return func() {
		now := time.Now()
		defer func() {
			logger.V(debugLevel).Info("network_cleanup_took", "duration", time.Since(now))
		}()

		var errBuf bytes.Buffer
		// Find all the containers attached to the network and unlink them
		cmd := exec.Command("docker", "network", "inspect", "-f", "{{range .Containers}}{{.Name}} {{end}}", "pgctl")
		cmd.Stderr = &errBuf
		out, err := cmd.Output()
		if err != nil {
			logger.Error(err, "stderr", errBuf.String())
			os.Exit(1)
		}
		ctrstr := string(out)
		if len(ctrstr) > 0 {

			// given this output, parse it into separate container names ' lucid_sanderson nifty_euclid'
			// and then remove them from the network

			// Unlink all containers from the network
			containers := strings.Split(ctrstr, " ")
			for _, container := range containers {
				container = strings.TrimSpace(container)
				if container == "" {
					continue
				}
				cmd = exec.Command("docker", "network", "disconnect", "pgctl", container)
				logger.V(debugLevel).Info(cmd.String())
				cmd.Stderr = &errBuf
				buf, err := cmd.Output()
				if err != nil && !strings.Contains(errBuf.String(), "marked for removal") {
					logger.Error(err, "failed to disconnect container", "container", container, "stderr", errBuf.String())
				}
				logger.V(debugLevel).Info(string(buf))
			}
		}

		err = RetryFn(nil, func() error {
			cmd = exec.Command("docker", "network", "rm", "pgctl")
			cmd.Stderr = &errBuf
			return cmd.Run()
		}, time.Second, 15*time.Second)
		if err != nil {
			logger.Error(err, "failed to retry removing network, please remove manually")
		}
	}
}

// Config to run a PostgreSQL database in a Docker container.
type Config struct {
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
func Run(log logr.Logger, cfg Config) (*sql.DB, string, func()) {
	port := getEphemeralPort()

	ctx, cancel := context.WithCancel(context.Background())

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
	cmd := exec.CommandContext(ctx, "docker", append(args, ctrArgs...)...)
	log.V(debugLevel).Info(cmd.String())

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		log.Error(err, "failed to run docker container")
		log.Info(cmd.String())
		log.Info("stderr:" + stderr.String())
		os.Exit(1)
	}
	log.V(debugLevel).Info(string(out))
	container := string(out[:len(out)-1]) // remove newline

	connectLogger(ctx, log, container)

	// Exercise hotload
	//hotload.RegisterSQLDriver("pgx", stdlib.GetDefaultDriver())
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", url.QueryEscape(cfg.Username), url.QueryEscape(cfg.Password), GetOutboundIP(), port, cfg.Database)
	log.V(debugLevel).Info(dsn)
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
	}, 100*time.Millisecond, 10*time.Second)

	// Fake some roles for testing
	_, err = conn.Exec(`
CREATE ROLE rds_superuser WITH INHERIT LOGIN;
CREATE ROLE alloydbsuperuser WITH INHERIT LOGIN`)
	if err != nil {
		panic(err)
	}

	if err != nil {
		log.Error(err, "failed to connect to database")

		cmd = exec.Command("docker", "logs", container)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			log.Error(err, "failed to get logs")
		}
		log.Info(string(out))
		os.Exit(1)
	}
	// TODO: change this to debug logging, just timing jenkins for now
	log.Info("db_connected", "dsn", dsn, "duration", time.Since(now))

	return conn, dsn, func() {
		// Cleanup container on close, dont exit without trying all steps first
		now := time.Now()
		defer func() {
			cancel()
			log.V(debugLevel).Info("container_cleanup_took", "duration", time.Since(now))
		}()

		err := os.Remove(f.Name())
		if err != nil {
			log.Error(err, "failed to remove temp file")
		}

		cmd := exec.Command("docker", "rm", "-f", container)
		// This take 10 seconds to run, and we don't care if
		// it was successful. So use Start() to not wait for
		// it to finish.
		log.V(debugLevel).Info(cmd.String())
		if err := cmd.Start(); err != nil {
			log.Error(err, "failed to remove container")
		}
	}
}

func connectLogger(ctx context.Context, logger logr.Logger, containerID string) {

	log := logger.WithName("testdb")
	// Connect to the container's logs
	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", containerID)

	log.Info("connecting to container logs", "cmd", cmd.String())

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error(err, "failed to get stdout pipe")
		os.Exit(1)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Error(err, "failed to get stderr pipe")
		os.Exit(1)
	}

	go logStdouterr(stdout, log)
	go logStdouterr(stderr, log)

	err = cmd.Start()
	if err != nil {
		logger.Error(err, "failed to start command")
		return
	}
}

func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}

func logStdouterr(out io.ReadCloser, logger logr.Logger) {
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		// This is very noisy and left off by default
		// Consider wiring this up to a test verbosity setting
		//logger.V(1).Info(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		logger.Error(err, "Error reading command output")
	}
}
