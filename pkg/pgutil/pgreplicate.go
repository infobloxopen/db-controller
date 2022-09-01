package pgutil

import (
	"database/sql"
	"fmt"

	"github.com/infobloxopen/db-controller/pkg/metrics"
	"github.com/lib/pq"
)

var (
	defaultPubName = "dbc_temp_pub_for_upgrade"
	defaultSubName = "dbc_temp_sub_for_upgrade"
)

func CreatePublication(db *sql.DB, pubName string) (string, error) {
	var exists bool
	if pubName == "" {
		pubName = defaultPubName
	}

	err := db.QueryRow("SELECT EXISTS(SELECT pubname FROM pg_catalog.pg_publication WHERE pubname = $1)", pubName).Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for publication name")
		return "", err
	}
	if !exists {
		// pc.log.Info("creating publication:", "with name", pubName)
		if _, err := db.Exec(fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", pq.QuoteIdentifier(pubName))); err != nil {
			// pc.log.Error(err, "could not create publication")
			metrics.DBProvisioningErrors.WithLabelValues("create publication error")
			return "", err
		}

		//		pc.log.Info("publication created", pubName)
	}

	//	defer db.Close()

	return pubName, nil
}
func DeletePublication(db *sql.DB, pubName string) (string, error) {
	var exists bool
	if pubName == "" {
		pubName = defaultPubName
	}

	err := db.QueryRow("SELECT EXISTS(SELECT pubname FROM pg_catalog.pg_publication WHERE pubname = $1)", pubName).Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for publication name")
		return "", err
	}
	if exists {
		// pc.log.Info("creating publication:", "with name", pubName)
		if _, err := db.Exec(fmt.Sprintf("DROP PUBLICATION %s", pq.QuoteIdentifier(pubName))); err != nil {
			// pc.log.Error(err, "could not create publication")
			return "", err
		}

		//		pc.log.Info("publication created", pubName)

	}

	//defer db.Close()

	return pubName, nil
}

func CreateSubscription(db *sql.DB, pubDbDsnURI string, pubName string, subName string) (string, error) {
	var exists bool
	if pubName == "" {
		pubName = defaultPubName
	}

	if subName == "" {
		subName = defaultSubName
	}

	err := db.QueryRow("SELECT EXISTS(SELECT subname FROM pg_catalog.pg_subscription WHERE subname = $1)", subName).Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for subsription name")
		return "", err
	}
	if !exists {
		// pc.log.Info("creating subsription:", "with name", pubName)
		if _, err := db.Exec(fmt.Sprintf("CREATE SUBSCRIPTION %s CONNECTION '%s' PUBLICATION %s WITH (enabled=false)",
			pq.QuoteIdentifier(subName), pubDbDsnURI, pq.QuoteIdentifier(pubName))); err != nil {
			// pc.log.Error(err, "could not create subsription")
			return "", err
		}

		//		pc.log.Info("subsription created", pubName)
	}

	//	defer db.Close()

	return subName, nil
}

func DeleteSubscription(db *sql.DB, subName string) (string, error) {
	var exists bool
	if subName == "" {
		subName = defaultSubName
	}

	err := db.QueryRow("SELECT EXISTS(SELECT subname FROM pg_catalog.pg_subscription WHERE subname = $1)", subName).Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for Subscription name")
		return "", err
	}
	if exists {
		// pc.log.Info("creating Subscription:", "with name", subName)
		if _, err := db.Exec(fmt.Sprintf("DROP SUBSCRIPTION %s", pq.QuoteIdentifier(subName))); err != nil {
			// pc.log.Error(err, "could not create Subscription")
			return "", err
		}
		//		pc.log.Info("Subscription created", subName)
	}
	//defer db.Close()
	return subName, nil
}

func EnableSubscription(db *sql.DB, subName string) (bool, error) {
	var exists bool
	if subName == "" {
		subName = defaultSubName
	}

	err := db.QueryRow("SELECT EXISTS(SELECT subname FROM pg_catalog.pg_subscription WHERE subname = $1)", subName).Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for Subscription name")
		return false, err
	}
	if exists {
		// pc.log.Info("creating Subscription:", "with name", subName)
		if _, err := db.Exec(fmt.Sprintf("ALTER SUBSCRIPTION %s ENABLE", pq.QuoteIdentifier(subName))); err != nil {
			// pc.log.Error(err, "could not create Subscription")
			return false, err
		}
	} else {
		return false, fmt.Errorf("unable to enable subscription. subscription not found - %s", subName)
	}

	//		pc.log.Info("Subscription created", subName)

	//defer db.Close()

	return true, nil
}

func DisableSubscription(db *sql.DB, subName string) (bool, error) {
	var exists bool
	if subName == "" {
		subName = defaultSubName
	}

	err := db.QueryRow("SELECT EXISTS(SELECT subname FROM pg_catalog.pg_subscription WHERE subname = $1)", subName).Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for Subscription name")
		return false, err
	}
	if exists {
		// pc.log.Info("creating Subscription:", "with name", subName)
		if _, err := db.Exec(fmt.Sprintf("ALTER SUBSCRIPTION %s DISABLE", pq.QuoteIdentifier(subName))); err != nil {
			// pc.log.Error(err, "could not create Subscription")
			return false, err
		}
	} else {
		return false, fmt.Errorf("unable to disable subscription. subscription not found - %s", subName)
	}

	//		pc.log.Info("Subscription created", subName)

	//defer db.Close()

	return true, nil
}

func ValidatePermission(db *sql.DB) (bool, error) {
	var exists bool

	err := db.QueryRow("SELECT EXISTS(select  rolname from pg_roles where rolsuper = 't' and rolreplication ='t' and rolname = (select session_user))").Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for Subscription name")
		return false, err
	}
	if !exists {
		// pc.log.Info("creating Subscription:", "with name", subName)
		return false, fmt.Errorf("db user does not have required super_user and/or replication role")
	}

	return true, nil
}
