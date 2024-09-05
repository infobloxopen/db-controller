package databaseclaim

import (
	"context"
	"fmt"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"

	"k8s.io/apimachinery/pkg/api/errors"
)

func (r *DatabaseClaimReconciler) manageCloudHostAWS(ctx context.Context, dbClaim *v1.DatabaseClaim) (bool, error) {
	dbHostIdentifier := r.Input.DbHostIdentifier

	switch dbClaim.Spec.Type {
	case v1.AuroraPostgres:
		return r.manageAuroraDBInstances(ctx, dbHostIdentifier, dbClaim, false)
	case v1.Postgres:
		return r.managePostgresDBInstanceAWS(ctx, dbHostIdentifier, dbClaim)
	}

	return false, fmt.Errorf("%w: %q must be one of %s", ErrInvalidDBType, dbClaim.Spec.Type, []v1.DatabaseType{v1.Postgres, v1.AuroraPostgres})

}

func (r *DatabaseClaimReconciler) manageAuroraDBInstances(ctx context.Context, dbHostIdentifier string, dbClaim *v1.DatabaseClaim, isSecondIns bool) (bool, error) {

	if basefun.GetCloud(r.Config.Viper) == "aws" {
		_, err := r.manageDBClusterAWS(ctx, dbHostIdentifier, dbClaim)
		if err != nil {
			return false, err
		}
	}

	log.FromContext(ctx).Info("dbcluster is ready. proceeding to manage dbinstance")
	firstInsReady, err := r.manageAuroraDBInstance(ctx, dbHostIdentifier, dbClaim, false)
	if err != nil {
		return false, err
	}
	secondInsReady := true
	if basefun.GetMultiAZEnabled(r.Config.Viper) {
		secondInsReady, err = r.manageAuroraDBInstance(ctx, dbHostIdentifier, dbClaim, true)
		if err != nil {
			return false, err
		}
	}
	return firstInsReady && secondInsReady, nil
}

func (r *DatabaseClaimReconciler) manageDBClusterAWS(ctx context.Context, dbHostName string,
	dbClaim *v1.DatabaseClaim) (bool, error) {

	logr := log.FromContext(ctx)

	pgName, err := r.manageClusterParamGroup(ctx, dbClaim)
	if err != nil {
		logr.Error(err, "parameter group setup failed")
		return false, err
	}

	serviceNS, err := r.getServiceNamespace()
	if err != nil {
		return false, err
	}

	dbSecretCluster := xpv1.SecretReference{
		Name:      dbHostName,
		Namespace: serviceNS,
	}

	dbMasterSecretCluster := xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{
			Name:      dbHostName + masterSecretSuffix,
			Namespace: serviceNS,
		},
		Key: masterPasswordKey,
	}
	dbCluster := &crossplaneaws.DBCluster{}
	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}

	params := &r.Input.HostParams
	restoreFromSource := defaultRestoreFromSource
	encryptStrg := true

	var auroraBackupRetentionPeriod *int64
	if r.Input.BackupRetentionDays != 0 {
		auroraBackupRetentionPeriod = &r.Input.BackupRetentionDays
	} else {
		auroraBackupRetentionPeriod = nil
	}

	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbCluster)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return false, err
		}

		logr.Info("creating_crossplane_dbcluster", "name", dbHostName)
		validationError := params.CheckEngineVersion()
		if validationError != nil {
			logr.Error(validationError, "invalid_db_version")
			return false, validationError
		}
		dbCluster = &crossplaneaws.DBCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: dbHostName,
				// TODO - Figure out the proper labels for resource
				// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
			},
			Spec: crossplaneaws.DBClusterSpec{
				ForProvider: crossplaneaws.DBClusterParameters{
					Region:                basefun.GetRegion(r.Config.Viper),
					BackupRetentionPeriod: auroraBackupRetentionPeriod,
					CustomDBClusterParameters: crossplaneaws.CustomDBClusterParameters{
						SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
						VPCSecurityGroupIDRefs: []xpv1.Reference{
							{Name: basefun.GetVpcSecurityGroupIDRefs(r.Config.Viper)},
						},
						DBSubnetGroupNameRef: &xpv1.Reference{
							Name: basefun.GetDbSubnetGroupNameRef(r.Config.Viper),
						},
						AutogeneratePassword:        true,
						MasterUserPasswordSecretRef: &dbMasterSecretCluster,
						DBClusterParameterGroupNameRef: &xpv1.Reference{
							Name: pgName,
						},
						EngineVersion: &params.EngineVersion,
					},
					Engine: &params.Engine,
					Tags:   DBClaimTags(dbClaim.Spec.Tags).DBTags(),
					// Items from Config
					MasterUsername:                  &params.MasterUsername,
					EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
					StorageEncrypted:                &encryptStrg,
					StorageType:                     &params.StorageType,
					Port:                            &params.Port,
					EnableCloudwatchLogsExports:     r.Input.EnableCloudwatchLogsExport,
					IOPS:                            nil,
					PreferredMaintenanceWindow:      dbClaim.Spec.PreferredMaintenanceWindow,
				},
				ResourceSpec: xpv1.ResourceSpec{
					WriteConnectionSecretToReference: &dbSecretCluster,
					ProviderConfigReference:          &providerConfigReference,
					DeletionPolicy:                   params.DeletionPolicy,
				},
			},
		}
		if r.mode == M_UseNewDB && dbClaim.Spec.RestoreFrom != "" {
			snapshotID := dbClaim.Spec.RestoreFrom
			dbCluster.Spec.ForProvider.CustomDBClusterParameters.RestoreFrom = &crossplaneaws.RestoreDBClusterBackupConfiguration{
				Snapshot: &crossplaneaws.SnapshotRestoreBackupConfiguration{
					SnapshotIdentifier: &snapshotID,
				},
				Source: &restoreFromSource,
			}
		}
		//create master password secret, before calling create on DBInstance
		err := r.manageMasterPassword(ctx, dbCluster.Spec.ForProvider.CustomDBClusterParameters.MasterUserPasswordSecretRef)
		if err != nil {
			return false, err
		}
		logr.Info("creating crossplane DBCluster resource", "DBCluster", dbCluster.Name)
		if err := r.Client.Create(ctx, dbCluster); err != nil {
			logr.Error(err, "crossplane_dbcluster_create")
			return false, err
		}

	}
	if !dbCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud DB cluster %s it is being deleted", dbHostName)
		logr.Error(err, "dbCluster", "dbHostIdentifier", dbHostName)
		return false, err
	}
	_, err = r.updateDBClusterAWS(ctx, dbClaim, dbCluster)
	if err != nil {
		return false, err
	}

	return r.isResourceReady(dbCluster.Status.ResourceStatus)
}

func (r *DatabaseClaimReconciler) managePostgresDBInstanceAWS(ctx context.Context, dbHostName string, dbClaim *v1.DatabaseClaim) (bool, error) {
	logr := log.FromContext(ctx)
	serviceNS, err := r.getServiceNamespace()
	if err != nil {
		return false, err
	}
	dbSecretInstance := xpv1.SecretReference{
		Name:      dbHostName,
		Namespace: serviceNS,
	}

	dbMasterSecretInstance := xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{
			Name:      dbHostName + masterSecretSuffix,
			Namespace: serviceNS,
		},
		Key: masterPasswordKey,
	}

	pgName, err := r.managePostgresParamGroup(ctx, dbClaim)
	if err != nil {
		logr.Error(err, "parameter group setup failed")
		return false, err
	}
	// Infrastructure Config
	region := basefun.GetRegion(r.Config.Viper)
	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}
	restoreFromSource := defaultRestoreFromSource
	dbInstance := &crossplaneaws.DBInstance{}

	params := &r.Input.HostParams
	ms64 := int64(params.MinStorageGB)
	multiAZ := basefun.GetMultiAZEnabled(r.Config.Viper)
	trueVal := true

	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)

	var maxStorageVal *int64

	if params.MaxStorageGB == 0 {
		maxStorageVal = nil
	} else {
		maxStorageVal = &params.MaxStorageGB
	}

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			validationError := params.CheckEngineVersion()
			if validationError != nil {
				return false, validationError
			}
			dbInstance = &crossplaneaws.DBInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name: dbHostName,
					// TODO - Figure out the proper labels for resource
					// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
				},
				Spec: crossplaneaws.DBInstanceSpec{
					ForProvider: crossplaneaws.DBInstanceParameters{
						CACertificateIdentifier: &r.Input.CACertificateIdentifier,
						Region:                  region,
						CustomDBInstanceParameters: crossplaneaws.CustomDBInstanceParameters{
							ApplyImmediately:  &trueVal,
							SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
							VPCSecurityGroupIDRefs: []xpv1.Reference{
								{Name: basefun.GetVpcSecurityGroupIDRefs(r.Config.Viper)},
							},
							DBSubnetGroupNameRef: &xpv1.Reference{
								Name: basefun.GetDbSubnetGroupNameRef(r.Config.Viper),
							},
							DBParameterGroupNameRef: &xpv1.Reference{
								Name: pgName,
							},
							AutogeneratePassword:        true,
							MasterUserPasswordSecretRef: &dbMasterSecretInstance,
							EngineVersion:               &params.EngineVersion,
						},
						Engine:              &params.Engine,
						MultiAZ:             &multiAZ,
						DBInstanceClass:     &params.InstanceClass,
						AllocatedStorage:    &ms64,
						MaxAllocatedStorage: maxStorageVal,
						Tags:                ReplaceOrAddTag(DBClaimTags(dbClaim.Spec.Tags).DBTags(), operationalStatusTagKey, operationalStatusActiveValue),
						// Items from Config
						MasterUsername:                  &params.MasterUsername,
						PubliclyAccessible:              &params.PubliclyAccessible,
						EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
						EnablePerformanceInsights:       &r.Input.EnablePerfInsight,
						EnableCloudwatchLogsExports:     r.Input.EnableCloudwatchLogsExport,
						BackupRetentionPeriod:           &r.Input.BackupRetentionDays,
						StorageEncrypted:                &trueVal,
						StorageType:                     &params.StorageType,
						Port:                            &params.Port,
						PreferredMaintenanceWindow:      dbClaim.Spec.PreferredMaintenanceWindow,
					},
					ResourceSpec: xpv1.ResourceSpec{
						WriteConnectionSecretToReference: &dbSecretInstance,
						ProviderConfigReference:          &providerConfigReference,
						DeletionPolicy:                   params.DeletionPolicy,
					},
				},
			}
			if r.mode == M_UseNewDB && dbClaim.Spec.RestoreFrom != "" {
				snapshotID := dbClaim.Spec.RestoreFrom
				dbInstance.Spec.ForProvider.CustomDBInstanceParameters.RestoreFrom = &crossplaneaws.RestoreDBInstanceBackupConfiguration{
					Snapshot: &crossplaneaws.SnapshotRestoreBackupConfiguration{
						SnapshotIdentifier: &snapshotID,
					},
					Source: &restoreFromSource,
				}
			}

			//create master password secret, before calling create on DBInstance
			err := r.manageMasterPassword(ctx, dbInstance.Spec.ForProvider.CustomDBInstanceParameters.MasterUserPasswordSecretRef)
			if err != nil {
				return false, err
			}
			//create DBInstance
			logr.Info("creating crossplane DBInstance resource", "DBInstance", dbInstance.Name)
			if err := r.Client.Create(ctx, dbInstance); err != nil {
				return false, err
			}
		} else {
			//not errors.IsNotFound(err) {
			return false, err
		}
	}
	// Deletion is long running task check that is not being deleted.
	if !dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud DB instance %s it is being deleted", dbHostName)
		logr.Error(err, "DBInstance", "dbHostIdentifier", dbHostName)
		return false, err
	}

	_, err = r.updateDBInstance(ctx, dbClaim, dbInstance)
	if err != nil {
		return false, err
	}
	return r.isResourceReady(dbInstance.Status.ResourceStatus)
}

func (r *DatabaseClaimReconciler) updateDBClusterAWS(ctx context.Context, dbClaim *v1.DatabaseClaim, dbCluster *crossplaneaws.DBCluster) (bool, error) {

	logr := log.FromContext(ctx)

	// Create a patch snapshot from current DBCluster
	patchDBCluster := client.MergeFrom(dbCluster.DeepCopy())

	// Update DBCluster
	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)
	dbCluster.Spec.ForProvider.Tags = DBClaimTags(dbClaim.Spec.Tags).DBTags()
	if r.Input.BackupRetentionDays != 0 {
		dbCluster.Spec.ForProvider.BackupRetentionPeriod = &r.Input.BackupRetentionDays
	}
	dbCluster.Spec.ForProvider.StorageType = &r.Input.HostParams.StorageType
	dbCluster.Spec.DeletionPolicy = r.Input.HostParams.DeletionPolicy

	// Compute a json patch based on the changed RDSInstance
	dbClusterPatchData, err := patchDBCluster.Data(dbCluster)
	if err != nil {
		return false, err
	}
	// an empty json patch will be {}, we can assert that no update is required if len == 2
	// we could also just apply the empty patch if additional call to apiserver isn't an issue
	if len(dbClusterPatchData) <= 2 {
		return false, nil
	}
	logr.Info("updating crossplane DBCluster resource", "DBCluster", dbCluster.Name)
	err = r.Client.Patch(ctx, dbCluster, patchDBCluster)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *DatabaseClaimReconciler) manageAuroraDBInstance(ctx context.Context, dbHostName string, dbClaim *v1.DatabaseClaim, isSecondIns bool) (bool, error) {
	logr := log.FromContext(ctx)
	// Infrastructure Config
	region := basefun.GetRegion(r.Config.Viper)
	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}
	pgName, err := r.manageAuroraPostgresParamGroup(ctx, dbClaim)
	if err != nil {
		logr.Error(err, "parameter group setup failed")
		return false, err
	}
	dbClusterIdentifier := dbHostName
	if isSecondIns {
		dbHostName = dbHostName + "-2"
	}
	dbInstance := &crossplaneaws.DBInstance{}

	params := &r.Input.HostParams
	trueVal := true
	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			logr.Info("aurora db instance not found. creating now")
			validationError := params.CheckEngineVersion()
			if validationError != nil {
				return false, validationError
			}
			dbInstance = &crossplaneaws.DBInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name: dbHostName,
					// TODO - Figure out the proper labels for resource
					// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
				},
				Spec: crossplaneaws.DBInstanceSpec{
					ForProvider: crossplaneaws.DBInstanceParameters{
						CACertificateIdentifier: &r.Input.CACertificateIdentifier,
						Region:                  region,
						CustomDBInstanceParameters: crossplaneaws.CustomDBInstanceParameters{
							ApplyImmediately:  &trueVal,
							SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
							EngineVersion:     &params.EngineVersion,
						},
						DBParameterGroupName: &pgName,
						Engine:               &params.Engine,
						DBInstanceClass:      &params.InstanceClass,
						Tags:                 ReplaceOrAddTag(DBClaimTags(dbClaim.Spec.Tags).DBTags(), operationalStatusTagKey, operationalStatusActiveValue),
						// Items from Config
						PubliclyAccessible:          &params.PubliclyAccessible,
						DBClusterIdentifier:         &dbClusterIdentifier,
						EnablePerformanceInsights:   &r.Input.EnablePerfInsight,
						EnableCloudwatchLogsExports: nil,
						PreferredMaintenanceWindow:  dbClaim.Spec.PreferredMaintenanceWindow,
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}

			logr.Info("creating crossplane DBInstance resource", "DBInstance", dbInstance.Name)

			r.Client.Create(ctx, dbInstance)
		} else {
			//not errors.IsNotFound(err) {
			return false, err
		}
	}

	// Deletion is long running task check that is not being deleted.
	if !dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud DB instance %s it is being deleted", dbHostName)
		logr.Error(err, "DBInstance", "dbHostIdentifier", dbHostName)
		return false, err
	}

	_, err = r.updateDBInstance(ctx, dbClaim, dbInstance)
	if err != nil {
		return false, err
	}

	return r.isResourceReady(dbInstance.Status.ResourceStatus)
}

func (r *DatabaseClaimReconciler) managePostgresParamGroup(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {

	logr := log.FromContext(ctx)

	logical := "rds.logical_replication"
	one := "1"
	immediate := "immediate"
	reboot := "pending-reboot"
	forceSsl := "rds.force_ssl"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	params := &r.Input.HostParams
	pgName := r.getParameterGroupName(dbClaim)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := r.Input.MasterConnInfo.DatabaseName
	desc := "custom PG for " + pgName

	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}

	dbParamGroup := &crossplaneaws.DBParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)
	if err != nil {
		if errors.IsNotFound(err) {
			validationError := params.CheckEngineVersion()
			if validationError != nil {
				return pgName, validationError
			}
			dbParamGroup = &crossplaneaws.DBParameterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: pgName,
				},
				Spec: crossplaneaws.DBParameterGroupSpec{
					ForProvider: crossplaneaws.DBParameterGroupParameters{
						Region:      basefun.GetRegion(r.Config.Viper),
						Description: &desc,
						CustomDBParameterGroupParameters: crossplaneaws.CustomDBParameterGroupParameters{
							DBParameterGroupFamilySelector: &crossplaneaws.DBParameterGroupFamilyNameSelector{
								Engine:        params.Engine,
								EngineVersion: &params.EngineVersion,
							},
							Parameters: []crossplaneaws.CustomParameter{
								{ParameterName: &logical,
									ParameterValue: &one,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &forceSsl,
									ParameterValue: &one,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &transactionTimeout,
									ParameterValue: &transactionTimeoutValue,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &sharedLib,
									ParameterValue: &sharedLibValue,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &cron,
									ParameterValue: &cronValue,
									ApplyMethod:    &reboot,
								},
							},
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}
			logr.Info("creating crossplane DBParameterGroup resource", "DBParameterGroup", dbParamGroup.Name)

			err = r.Client.Create(ctx, dbParamGroup)
			if err != nil {
				return pgName, err
			}

		} else {
			//not errors.IsNotFound(err) {
			return pgName, err
		}
	}
	return pgName, nil
}
func (r *DatabaseClaimReconciler) manageAuroraPostgresParamGroup(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {

	logr := log.FromContext(ctx)

	immediate := "immediate"
	reboot := "pending-reboot"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	params := &r.Input.HostParams
	pgName := r.getParameterGroupName(dbClaim)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := r.Input.MasterConnInfo.DatabaseName
	desc := "custom PG for " + pgName

	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}

	dbParamGroup := &crossplaneaws.DBParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)
	if err != nil {
		if errors.IsNotFound(err) {
			validationError := params.CheckEngineVersion()
			if validationError != nil {
				return pgName, validationError
			}
			dbParamGroup = &crossplaneaws.DBParameterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: pgName,
				},
				Spec: crossplaneaws.DBParameterGroupSpec{
					ForProvider: crossplaneaws.DBParameterGroupParameters{
						Region:      basefun.GetRegion(r.Config.Viper),
						Description: &desc,
						CustomDBParameterGroupParameters: crossplaneaws.CustomDBParameterGroupParameters{
							DBParameterGroupFamilySelector: &crossplaneaws.DBParameterGroupFamilyNameSelector{
								Engine:        params.Engine,
								EngineVersion: &params.EngineVersion,
							},
							Parameters: []crossplaneaws.CustomParameter{
								{ParameterName: &transactionTimeout,
									ParameterValue: &transactionTimeoutValue,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &sharedLib,
									ParameterValue: &sharedLibValue,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &cron,
									ParameterValue: &cronValue,
									ApplyMethod:    &reboot,
								},
							},
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}
			logr.Info("creating crossplane DBParameterGroup resource", "DBParameterGroup", dbParamGroup.Name)

			err = r.Client.Create(ctx, dbParamGroup)
			if err != nil {
				return pgName, err
			}

		} else {
			//not errors.IsNotFound(err) {
			return pgName, err
		}
	}
	return pgName, nil
}

func (r *DatabaseClaimReconciler) manageClusterParamGroup(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {

	logr := log.FromContext(ctx)

	logical := "rds.logical_replication"
	one := "1"
	immediate := "immediate"
	reboot := "pending-reboot"
	forceSsl := "rds.force_ssl"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	params := &r.Input.HostParams
	pgName := r.getParameterGroupName(dbClaim)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := r.Input.MasterConnInfo.DatabaseName
	desc := "custom PG for " + pgName

	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}

	dbParamGroup := &crossplaneaws.DBClusterParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)
	if err != nil {
		if errors.IsNotFound(err) {
			validationError := params.CheckEngineVersion()
			if validationError != nil {
				return pgName, validationError
			}
			dbParamGroup = &crossplaneaws.DBClusterParameterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: pgName,
				},
				Spec: crossplaneaws.DBClusterParameterGroupSpec{
					ForProvider: crossplaneaws.DBClusterParameterGroupParameters{
						Region:      basefun.GetRegion(r.Config.Viper),
						Description: &desc,
						CustomDBClusterParameterGroupParameters: crossplaneaws.CustomDBClusterParameterGroupParameters{
							DBParameterGroupFamilySelector: &crossplaneaws.DBParameterGroupFamilyNameSelector{
								Engine:        params.Engine,
								EngineVersion: &params.EngineVersion,
							},
							Parameters: []crossplaneaws.CustomParameter{
								{ParameterName: &logical,
									ParameterValue: &one,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &forceSsl,
									ParameterValue: &one,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &transactionTimeout,
									ParameterValue: &transactionTimeoutValue,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &sharedLib,
									ParameterValue: &sharedLibValue,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &cron,
									ParameterValue: &cronValue,
									ApplyMethod:    &reboot,
								},
							},
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}
			logr.Info("creating crossplane DBParameterGroup resource", "DBParameterGroup", dbParamGroup.Name)

			err = r.Client.Create(ctx, dbParamGroup)
			if err != nil {
				return pgName, err
			}

		} else {
			//not errors.IsNotFound(err) {
			return pgName, err
		}
	}
	return pgName, nil
}

func (r *DatabaseClaimReconciler) deleteExternalResourcesAWS(ctx context.Context, dbClaim *v1.DatabaseClaim) error {
	// delete any external resources associated with the dbClaim
	// Only RDS Instance are managed for now
	reclaimPolicy := basefun.GetDefaultReclaimPolicy(r.Config.Viper)

	if reclaimPolicy == "delete" {
		dbHostName := r.getDynamicHostName(dbClaim)
		pgName := r.getParameterGroupName(dbClaim)

		// Delete
		if err := r.deleteCloudDatabaseAWS(dbHostName, ctx); err != nil {
			return err
		}
		return r.deleteParameterGroupAWS(ctx, pgName)

	}
	// else reclaimPolicy == "retain" nothing to do!

	return nil
}

func (r *DatabaseClaimReconciler) deleteCloudDatabaseAWS(dbHostName string, ctx context.Context) error {

	logr := log.FromContext(ctx)
	dbInstance := &crossplaneaws.DBInstance{}
	dbCluster := &crossplaneaws.DBCluster{}

	if basefun.GetMultiAZEnabled(r.Config.Viper) {
		err := r.Client.Get(ctx, client.ObjectKey{
			Name: dbHostName + "-2",
		}, dbInstance)
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			} // else not found - no action required
		} else if dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, dbInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
				logr.Info("unable delete crossplane DBInstance resource", "DBInstance", dbHostName+"-2")
				return err
			} else {
				logr.Info("deleted crossplane DBInstance resource", "DBInstance", dbHostName+"-2")
			}
		}
	}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		} // else not found - no action required
	} else if dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			logr.Info("unable delete crossplane DBInstance resource", "DBInstance", dbHostName)
			return err
		} else {
			logr.Info("deleted crossplane DBInstance resource", "DBInstance", dbHostName)
		}
	}

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbCluster)

	if err != nil {
		if errors.IsNotFound(err) {
			return nil //nothing to delete
		} else {
			return err
		}
	}

	if dbCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbCluster, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			logr.Info("unable delete crossplane DBCluster resource", "DBCluster", dbHostName)
			return err
		} else {
			logr.Info("deleted crossplane DBCluster resource", "DBCluster", dbHostName)
		}
	}

	return nil
}

func (r *DatabaseClaimReconciler) deleteParameterGroupAWS(ctx context.Context, pgName string) error {

	logr := log.FromContext(ctx)

	dbParamGroup := &crossplaneaws.DBParameterGroup{}
	dbClusterParamGroup := &crossplaneaws.DBClusterParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)

	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		} // else not found - no action required
	} else if dbParamGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbParamGroup, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			logr.Info("unable delete crossplane dbParamGroup resource", "dbParamGroup", dbParamGroup)
			return err
		} else {
			logr.Info("deleted crossplane dbParamGroup resource", "dbParamGroup", dbParamGroup)
		}
	}

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbClusterParamGroup)

	if err != nil {
		if errors.IsNotFound(err) {
			return nil //nothing to delete
		} else {
			return err
		}
	}

	if dbClusterParamGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbClusterParamGroup, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			logr.Info("unable delete crossplane DBCluster resource", "dbClusterParamGroup", dbClusterParamGroup)
			return err
		} else {
			logr.Info("deleted crossplane DBCluster resource", "dbClusterParamGroup", dbClusterParamGroup)
		}
	}

	return nil
}

func (r *DatabaseClaimReconciler) updateDBInstance(ctx context.Context, dbClaim *v1.DatabaseClaim, dbInstance *crossplaneaws.DBInstance) (bool, error) {

	logr := log.FromContext(ctx)

	// Create a patch snapshot from current DBInstance
	patchDBInstance := client.MergeFrom(dbInstance.DeepCopy())

	// Update DBInstance
	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)
	dbInstance.Spec.ForProvider.Tags = ReplaceOrAddTag(DBClaimTags(dbClaim.Spec.Tags).DBTags(), operationalStatusTagKey, operationalStatusActiveValue)
	params := &r.Input.HostParams
	if dbClaim.Spec.Type == v1.Postgres {
		multiAZ := basefun.GetMultiAZEnabled(r.Config.Viper)
		ms64 := int64(params.MinStorageGB)
		dbInstance.Spec.ForProvider.AllocatedStorage = &ms64

		var maxStorageVal *int64
		if params.MaxStorageGB == 0 {
			maxStorageVal = nil
		} else {
			maxStorageVal = &params.MaxStorageGB
		}

		dbInstance.Spec.ForProvider.MaxAllocatedStorage = maxStorageVal
		dbInstance.Spec.ForProvider.EnableCloudwatchLogsExports = r.Input.EnableCloudwatchLogsExport
		dbInstance.Spec.ForProvider.MultiAZ = &multiAZ
	}
	enablePerfInsight := r.Input.EnablePerfInsight
	dbInstance.Spec.ForProvider.EnablePerformanceInsights = &enablePerfInsight
	dbInstance.Spec.DeletionPolicy = params.DeletionPolicy
	dbInstance.Spec.ForProvider.CACertificateIdentifier = &r.Input.CACertificateIdentifier
	if dbClaim.Spec.Type == v1.AuroraPostgres {
		dbInstance.Spec.ForProvider.EnableCloudwatchLogsExports = nil
	}

	// Compute a json patch based on the changed DBInstance
	dbInstancePatchData, err := patchDBInstance.Data(dbInstance)
	if err != nil {
		return false, err
	}
	// an empty json patch will be {}, we can assert that no update is required if len == 2
	// we could also just apply the empty patch if additional call to apiserver isn't an issue
	if len(dbInstancePatchData) <= 2 {
		return false, nil
	}
	logr.Info("updating crossplane DBInstance resource", "DBInstance", dbInstance.Name)
	err = r.Client.Patch(ctx, dbInstance, patchDBInstance)
	if err != nil {
		return false, err
	}

	return true, nil
}

func ReplaceOrAddTag(tags []*crossplaneaws.Tag, key string, value string) []*crossplaneaws.Tag {
	for _, tag := range tags {
		if *tag.Key == key {
			if *tag.Value != value {
				tag.Value = &value
				return tags
			} else {
				return tags
			}
		}
	}
	tags = append(tags, &crossplaneaws.Tag{Key: &key, Value: &value})
	return tags
}

func (r *DatabaseClaimReconciler) operationalTaggingForDbParamGroup(ctx context.Context, logr logr.Logger, dbParamGroupName string) error {
	dbParameterGroup := &crossplaneaws.DBParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbParamGroupName,
	}, dbParameterGroup)

	if err != nil {
		return err
	}
	operationalTagForProviderPresent := false
	for _, tag := range dbParameterGroup.Spec.ForProvider.Tags {
		if *tag.Key == operationalStatusTagKey && *tag.Value == operationalStatusInactiveValue {
			operationalTagForProviderPresent = true
		}
	}
	if !operationalTagForProviderPresent {
		patchDBParameterGroup := client.MergeFrom(dbParameterGroup.DeepCopy())

		dbParameterGroup.Spec.ForProvider.Tags = ReplaceOrAddTag(dbParameterGroup.Spec.ForProvider.Tags, operationalStatusTagKey, operationalStatusInactiveValue)

		err := r.Client.Patch(ctx, dbParameterGroup, patchDBParameterGroup)
		if err != nil {
			logr.Error(err, "Error updating operational tags for  crossplane db param group ")
			return err
		}
	}
	return nil
}

func (r *DatabaseClaimReconciler) operationalTaggingForDbClusterParamGroup(ctx context.Context, logr logr.Logger, dbParamGroupName string) error {
	dbClusterParamGroup := &crossplaneaws.DBClusterParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbParamGroupName,
	}, dbClusterParamGroup)

	if err != nil {
		logr.Error(err, "Error getting crossplane db cluster param group for old DB ")
		return err
	}

	operationalTagForProviderPresent := false
	for _, tag := range dbClusterParamGroup.Spec.ForProvider.Tags {
		if *tag.Key == operationalStatusTagKey && *tag.Value == operationalStatusInactiveValue {
			operationalTagForProviderPresent = true
		}
	}

	if !operationalTagForProviderPresent {
		patchDBClusterParameterGroup := client.MergeFrom(dbClusterParamGroup.DeepCopy())

		dbClusterParamGroup.Spec.ForProvider.Tags = ReplaceOrAddTag(dbClusterParamGroup.Spec.ForProvider.Tags, operationalStatusTagKey, operationalStatusInactiveValue)

		err := r.Client.Patch(ctx, dbClusterParamGroup, patchDBClusterParameterGroup)
		if err != nil {
			logr.Error(err, "Error updating operational tags for  crossplane db cluster param group ")
			return err
		}
	}
	return nil
}

func (r *DatabaseClaimReconciler) operationalTaggingForDbCluster(ctx context.Context, logr logr.Logger, dbHostName string) error {
	dbCluster := &crossplaneaws.DBCluster{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbCluster)

	if err != nil {
		return err
	}
	operationalTagForProviderPresent := false
	for _, tag := range dbCluster.Spec.ForProvider.Tags {
		if *tag.Key == operationalStatusTagKey && *tag.Value == operationalStatusInactiveValue {
			operationalTagForProviderPresent = true
		}
	}

	if !operationalTagForProviderPresent {
		patchDBClusterParameterGroup := client.MergeFrom(dbCluster.DeepCopy())

		dbCluster.Spec.ForProvider.Tags = ReplaceOrAddTag(dbCluster.Spec.ForProvider.Tags, operationalStatusTagKey, operationalStatusInactiveValue)

		err := r.Client.Patch(ctx, dbCluster, patchDBClusterParameterGroup)
		if err != nil {
			logr.Error(err, "Error updating operational tags for  crossplane db cluster   ")
			return err
		}
	}
	return nil

}

func (r *DatabaseClaimReconciler) operationalTaggingForDbInstance(ctx context.Context, logr logr.Logger, dbHostName string) (bool, error) {

	dbInstance := &crossplaneaws.DBInstance{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)

	if err != nil {
		return false, err
	} else {
		operationalTagForProviderPresent := false
		operationalTagAtProviderPresent := false
		// Checking whether tags are already requested
		for _, tag := range dbInstance.Spec.ForProvider.Tags {
			if *tag.Key == operationalStatusTagKey && *tag.Value == operationalStatusInactiveValue {
				operationalTagForProviderPresent = true
			}
		}
		// checking whether tags have got updated on AWS (This will be done by chekcing tags at AtProvider)
		for _, tag := range dbInstance.Status.AtProvider.TagList {
			if *tag.Key == operationalStatusTagKey && *tag.Value == operationalStatusInactiveValue {
				operationalTagAtProviderPresent = true
			}
		}

		if !operationalTagForProviderPresent {
			patchDBInstance := client.MergeFrom(dbInstance.DeepCopy())

			dbInstance.Spec.ForProvider.Tags = ReplaceOrAddTag(dbInstance.Spec.ForProvider.Tags, operationalStatusTagKey, operationalStatusInactiveValue)

			err := r.Client.Patch(ctx, dbInstance, patchDBInstance)
			if err != nil {
				logr.Error(err, "Error patching  crossplane dbInstance for old DB to add operational tags")
				return false, err
			}
		} else if operationalTagForProviderPresent && !operationalTagAtProviderPresent {
			logr.Info("could not find operational tags of DBInstance on AWS. These are already requested. Needs to requeue")
			return false, nil
		} else {
			logr.Info("operational tags of DBInstance on AWS found")
			return true, nil
		}

	}
	return false, nil
}

func HasOperationalTag(tags []*crossplaneaws.Tag) bool {

	for _, tag := range tags {
		if *tag.Key == operationalStatusTagKey && *tag.Value == operationalStatusInactiveValue {
			return true
		}
	}
	return false
}
