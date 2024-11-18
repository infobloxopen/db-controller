package metrics

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// StartUpdater starts a metrics updater that updates the metrics every minute.
func StartUpdater(ctx context.Context, client client.Client) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	logr := log.FromContext(ctx).WithName("metrics-updater")

	for {
		select {
		case <-ctx.Done():
			logr.Info("shutting down metrics updater")
			return
		case <-ticker.C:
			updateMetrics(ctx, logr, client)
		}
	}
}

func updateMetrics(ctx context.Context, log logr.Logger, client client.Client) {

	var databaseClaims v1.DatabaseClaimList
	if err := client.List(ctx, &databaseClaims); err != nil {
		log.Error(err, "unable to list database claims")
		return
	}

	metrics.TotalDatabaseClaims.Reset()
	metrics.ErrorStateClaims.Reset()
	metrics.MigrationStateClaims.Reset()
	metrics.ActiveDBState.Reset()
	metrics.ExistingSourceClaims.Reset()

	for _, dbClaim := range databaseClaims.Items {
		metrics.TotalDatabaseClaims.WithLabelValues(dbClaim.Namespace).Inc()

		if dbClaim.Status.Error != "" {
			metrics.ErrorStateClaims.WithLabelValues(dbClaim.Namespace).Inc()
		}

		if dbClaim.Status.MigrationState != "" {
			metrics.MigrationStateClaims.WithLabelValues(dbClaim.Namespace, dbClaim.Status.MigrationState).Inc()
		}

		if dbClaim.Status.ActiveDB.DbState != "" {
			metrics.ActiveDBState.WithLabelValues(dbClaim.Namespace, string(dbClaim.Status.ActiveDB.DbState)).Inc()
		}

		if dbClaim.Spec.UseExistingSource != nil && *dbClaim.Spec.UseExistingSource {
			metrics.ExistingSourceClaims.WithLabelValues(dbClaim.Namespace, "true").Inc()
		} else {
			metrics.ExistingSourceClaims.WithLabelValues(dbClaim.Namespace, "false").Inc()
		}
	}
}
