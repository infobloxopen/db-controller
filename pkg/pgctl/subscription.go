package pgctl

import (
	"database/sql"
	"fmt"
)

// SubscriptionStatus represents the status of a PostgreSQL subscription
type SubscriptionStatus struct {
	TableName  string
	SRSubState string
	State      string
	LSN        sql.NullString
	CurrentLSN sql.NullString
	LagSize    sql.NullString
}

// getSubscriptionStatus returns the status of a PostgreSQL subscription
func getSubscriptionStatus(db *sql.DB, subscriptionName string) (*SubscriptionStatus, error) {
	// Query to get subscription status information
	query := `SELECT 
    sr.srrelid::regclass as table_name,
    sr.srsubstate,
    CASE sr.srsubstate
        WHEN 'i' THEN 'Initializing'
        WHEN 'd' THEN 'Data Copying'
        WHEN 's' THEN 'Synchronized'
        WHEN 'r' THEN 'Ready'
        WHEN 'f' THEN 'Failed'
    END as state,
    sr.srsublsn as lsn,
    pg_last_wal_receive_lsn()::text as current_lsn,
    pg_size_pretty(pg_wal_lsn_diff(pg_last_wal_receive_lsn(), sr.srsublsn)) as lag_size
FROM pg_subscription s
JOIN pg_subscription_rel sr ON s.oid = sr.srsubid
WHERE s.subname = $1;
`

	var status SubscriptionStatus
	err := db.QueryRow(query, subscriptionName).Scan(&status.TableName, &status.SRSubState, &status.State, &status.LSN, &status.CurrentLSN, &status.LagSize)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("subscription '%s' does not exist", subscriptionName)
	} else if err != nil {
		return nil, err
	}

	return &status, nil
}
