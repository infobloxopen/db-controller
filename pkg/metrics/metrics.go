package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	UsersCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "users_created_total",
			Help: "Number of created users ",
		}, []string{"username", "dburl"},
	)
	UsersDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "users_deleted_total",
			Help: "Number of deleted users ",
		},
	)
	UsersCreatedErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "users_create_errors_total",
			Help: "Number of users created with errors",
		}, []string{"reason", "username", "dburl", "error"},
	)
	UsersDeletedErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "users_deleted_errors_total",
			Help: "Number of users deleted with errors",
		}, []string{"reason"},
	)
	UsersCreateTime = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "user_create_time_seconds",
		Help: "Histogram of user creation time in seconds",
	})
	UsersUpdated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "users_updated_total",
			Help: "Number of updated users ",
		},
	)
	UsersUpdatedErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "users_update_errors_total",
			Help: "Number of users updated with errors",
		}, []string{"reason"},
	)
	UsersUpdateTime = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "user_update_time_seconds",
		Help: "Histogram of user updating time in seconds",
	})
	DBProvisioningErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "database_provisioning_errors_total",
			Help: "Number of failed database provisioning",
		}, []string{"reason"},
	)
	DBCreated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "databases_created_total",
			Help: "Number of created databases ",
		},
	)
	PasswordRotated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "password_rotated_total",
			Help: "Number of rotated passwords",
		},
	)
	PasswordRotatedErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "password_rotate_errors_total",
			Help: "Number of passwords rotated with errors",
		}, []string{"reason"},
	)
	PasswordRotateTime = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "password_rotation_time_seconds",
		Help: "Histogram of password rotation time in seconds",
	})

	// -----------------------------------------------------------------------
	// Database controller metrics

	TotalDatabaseClaims = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbcontroller_total_database_claims",
			Help: "Total number of database claims",
		},
		[]string{"namespace", "app_id", "db_tye", "db_version"},
	)
	ExistingSourceClaims = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbcontroller_existing_source_claims",
			Help: "Number of database claims using existing source",
		},
		[]string{"namespace", "use_existing_source", "app_id"},
	)
	ErrorStateClaims = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbcontroller_error_state_claims",
			Help: "Number of database claims in error state",
		},
		[]string{"namespace", "app_id", "error"},
	)
	MigrationStateClaims = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbcontroller_migration_state_claims",
			Help: "Number of database claims in each migration state",
		},
		[]string{"namespace", "migration_state", "app_id"},
	)
	ActiveDBState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbcontroller_active_db_state",
			Help: "State of active databases",
		},
		[]string{"namespace", "db_state", "app_id"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(UsersCreated, UsersDeleted, UsersCreatedErrors, UsersCreateTime)
	metrics.Registry.MustRegister(UsersUpdated, UsersUpdatedErrors, UsersDeletedErrors, UsersUpdateTime)
	metrics.Registry.MustRegister(DBCreated, DBProvisioningErrors)
	metrics.Registry.MustRegister(PasswordRotated, PasswordRotatedErrors, PasswordRotateTime)
	metrics.Registry.MustRegister(TotalDatabaseClaims)
	metrics.Registry.MustRegister(ExistingSourceClaims)
	metrics.Registry.MustRegister(ErrorStateClaims)
	metrics.Registry.MustRegister(MigrationStateClaims)
	metrics.Registry.MustRegister(ActiveDBState)
}
