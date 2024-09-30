package databaseclaim

import (
	"context"
	"fmt"
	"net/url"

	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func validateAndUpdateCredsDSN(ctx context.Context, adminDSN, userDSN string) error {
	logger := log.FromContext(ctx).WithValues("caller", "validateAndUpdateCredsDSN")
	dbClient, err := dbclient.New(dbclient.Config{
		Log:    logger,
		DBType: "postgres",
		DSN:    adminDSN,
	})
	if err != nil {
		logger.Error(err, "unable_create_dbclient")
		return err
	}

	return validateAndUpdateCreds(ctx, dbClient, userDSN)
}

func validateAndUpdateCreds(ctx context.Context, cli dbclient.Clienter, currentConnDSN string) error {
	logger := log.FromContext(ctx).WithValues("admin", cli.SanitizeDSN())
	// If we're given an empty DSN, ignore it
	if currentConnDSN == "" {
		return nil
	}

	if err := cli.Ping(); err != nil {
		logger.Error(err, "admin_unable_connect")
		return fmt.Errorf("unable_connect: %w", err)
	}

	logger.V(1).Info("verifying_existing_creds", "conn_dsn", currentConnDSN)
	curCli, err := dbclient.New(dbclient.Config{
		Log:    logger,
		DBType: "postgres",
		DSN:    currentConnDSN,
	})
	if err != nil {
		return fmt.Errorf("unable_create_dbclient: %w", err)
	}
	logger = logger.WithValues("user", currentConnDSN)

	err = curCli.DB.Ping()
	if err == nil {
		return nil
	}

	logger.Info("unable_connect_updating_user", "err", err)

	parsedDSN, err := url.Parse(currentConnDSN)
	if err != nil {
		return err
	}
	pw, ok := parsedDSN.User.Password()
	if !ok || len(pw) == 0 {
		return fmt.Errorf("password unavailable to update")
	}
	if err := cli.UpdatePassword(parsedDSN.User.Username(), pw); err != nil {
		return err
	}

	err = curCli.DB.Ping()
	if err == nil {
		logger.Info("user_able_connect_after_update!!")
		return nil
	}
	logger.Error(err, "user_unable_connect_after_update", "dsn", currentConnDSN)

	return fmt.Errorf("failed_to_ping_after_update: %w", err)
}
