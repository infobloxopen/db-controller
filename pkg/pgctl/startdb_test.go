package pgctl

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func getEphemeralPort() int {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	defer l.Close() // nolint:errcheck
	return l.Addr().(*net.TCPAddr).Port
}

func StartNetwork() func() {

	// List any containers attached to the network and stop then remove them
	cmd := exec.Command("docker", "ps", "-q", "-a", "--filter", fmt.Sprintf("network=pgctl"))
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	if len(out) > 0 {
		cmd = exec.Command("docker", "rm", "-f", string(out))
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}

	// Check if network already exists
	cmd = exec.Command("docker", "network", "inspect", "pgctl")
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		// Network does not exist
		cmd = exec.Command("docker", "network", "create", "pgctl")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}

	return func() {
		now := time.Now()
		defer func() {
			log.Println("network_cleanup_took", time.Since(now))
		}()

		// Find all the containers attached to the network and unlink them
		cmd := exec.Command("docker", "network", "inspect", "-f", "{{range .Containers}}{{.Name}} {{end}}", "pgctl")
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			panic(err)
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
				log.Println(cmd.String())
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					log.Println("failed to disconnect container", container)
				}
			}

		}

		retryTest(nil, func() error {
			cmd = exec.Command("docker", "network", "rm", "pgctl")
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}, time.Second, 15*time.Second)
	}
}

type dbConfig struct {
	HostName  string
	DockerTag string
	Username  string
	Password  string
	Database  string
}

func RunDB(cfg dbConfig) (*sql.DB, string, func()) {
	port := getEphemeralPort()

	// Run PostgreSQL in Docker
	cmd := exec.Command("docker", "run", "--hostname", cfg.HostName,
		"-d",
		"-p", fmt.Sprintf("%d:5432", port),
		"--network", "pgctl",
		"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", cfg.Password),
		"-e", fmt.Sprintf("POSTGRES_USER=%s", cfg.Username),
		"-e", fmt.Sprintf("POSTGRES_DB=%s", cfg.Database),
		fmt.Sprintf("postgres:%s", cfg.DockerTag),
		"postgres", "-c", "wal_level=logical")
	log.Println(cmd.String())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	container := string(out[:len(out)-1]) // remove newline

	// Exercise hotload
	//hotload.RegisterSQLDriver("pgx", stdlib.GetDefaultDriver())
	dsn := fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?sslmode=disable", cfg.Username, cfg.Password, port, cfg.Database)
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

	// try to connect to the database for 10 seconds
	for i := 0; i < 10; i++ {
		err = conn.Ping()
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		panic(err)
	}

	return conn, dsn, func() {

		_ = os.Remove(f.Name())
		_ = container

		cmd = exec.Command("docker", "rm", "-f", container)
		cmd.Stderr = os.Stderr
		// This take 10 seconds to run, and we don't care if
		// it was successful. So use Start() to not wait for
		// it to finish.
		log.Println(cmd.String())
		if err := cmd.Start(); err != nil {
			panic(err)
		}
	}
}

func retryTest(t *testing.T, mightFail func() error, retryInterval, timeout time.Duration) {
	if t != nil {
		t.Helper()
	}

	var err error
	startTime := time.Now()

	for time.Since(startTime) < timeout {
		err = mightFail()
		if err == nil {
			return
		}
		if t != nil {
			t.Logf("retryTest: %s", err)
		} else {
			log.Printf("retryTest: %s", err)
		}
		time.Sleep(retryInterval)
	}

	// If we reach here, the function did not succeed within the timeout
	if t != nil {
		t.Fatalf("retry_did_not_succeed within %s: %v", timeout, err)
	} else {
		panic(fmt.Sprintf("retry_did_not_succeed within %s: %v", timeout, err))
	}
}
