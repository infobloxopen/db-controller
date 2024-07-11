package controller

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	_ "github.com/lib/pq"
)

func getEphemeralPort() int {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	defer l.Close() // nolint:errcheck
	return l.Addr().(*net.TCPAddr).Port
}

func RunDB() (*sql.DB, string, func()) {
	port := getEphemeralPort()

	// Run PostgreSQL in Docker
	cmd := exec.Command("docker", "run", "-d", "-p", fmt.Sprintf("%d:5432", port), "-e", "POSTGRES_PASSWORD=postgres", "postgres:15")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	container := string(out[:len(out)-1]) // remove newline

	// Exercise hotload
	//hotload.RegisterSQLDriver("pgx", stdlib.GetDefaultDriver())
	dsn := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port)
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

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	// try to connect to the database for 10 seconds
	for i := 0; i < 10; i++ {
		err = conn.PingContext(ctx)
		if err == nil {
			break
		}
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
		time.Sleep(time.Second)
	}
	cancel()
	if err != nil {

		cmd := exec.Command("docker", "logs", container)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			log.Println("docker logs failed:", err)
		} else {
			log.Println("docker logs:", string(out))
		}

		panic(err)
	}

	return conn, dsn, func() {
		_ = os.Remove(f.Name())
		cmd := exec.Command("docker", "rm", "-f", container)
		cmd.Stderr = os.Stderr
		// cmd.Stdout = os.Stdout
		_ = cmd.Run()
	}
}
