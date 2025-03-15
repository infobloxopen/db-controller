package providers

import (
	"context"
	"fmt"
	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	MasterPasswordSuffix    = "-master"
	MasterPasswordSecretKey = "password"
	SnapshotSource          = "Snapshot"
	AwsPostgres             = "postgres"
	AwsAuroraPostgres       = "aurora-postgresql"
)

type AWSProvider struct {
	k8sClient client.Client
	config    *viper.Viper
	serviceNS string
}

func newAWSProvider(k8sClient client.Client, config *viper.Viper, serviceNS string) Provider {
	return &AWSProvider{
		k8sClient: k8sClient,
		config:    config,
		serviceNS: serviceNS,
	}
}

func (p *AWSProvider) CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	logger := log.FromContext(ctx)
	logger.Info("provisioning crossplane aws database", "DatabaseSpec", spec)

	switch spec.DbType {
	case AwsAuroraPostgres:
		err := p.createAuroraDB(ctx, spec)
		if err != nil {
			return false, err
		}
		instanceReady, err := p.isDBInstanceReady(ctx, spec.ResourceName)
		if err != nil {
			return false, err
		}
		clusterReady, err := p.isDBClusterReady(ctx, spec.ResourceName)
		if err != nil {
			return false, err
		}
		logger.Info("checking provisioned instance readiness", "instanceReady", instanceReady, "clusterReady", clusterReady)
		if basefun.GetMultiAZEnabled(p.config) {
			instance2Ready, err := p.isDBInstanceReady(ctx, spec.ResourceName+"-2")
			if err != nil {
				return false, err
			}
			logger.Info("checking provisioned instance readiness", "instance2Ready", instance2Ready)
			return instanceReady && instance2Ready && clusterReady, nil
		}
		return instanceReady && clusterReady, nil
	case AwsPostgres:
		err := p.createPostgres(ctx, spec)
		if err != nil {
			return false, err
		}
		instanceReady, err := p.isDBInstanceReady(ctx, spec.ResourceName)
		if err != nil {
			return false, err
		}
		logger.Info("checking provisioned instance readiness", "instanceReady", instanceReady)
		return instanceReady, nil
	}
	return false, fmt.Errorf("%w: %s must be one of %s", v1.ErrInvalidDBType, spec.DbType, []v1.DatabaseType{v1.Postgres, v1.AuroraPostgres})
}

// DeleteDatabase attempt to delete all crossplane resources related to the provided spec
// optionally
func (p *AWSProvider) DeleteDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	if spec.TagInactive {
		// it takes some time to propagate the tag to the underlying aws resource
		if tagged, err := p.addInactiveOperationalTag(ctx, spec); err != nil || !tagged {
			return tagged, err
		}
	}

	deletionPolicy := client.PropagationPolicy(metav1.DeletePropagationBackground)
	paramGroupName := getParameterGroupName(spec)
	// Delete DBClusterParameterGroup if it exists
	dbClusterParamGroup := &crossplaneaws.DBClusterParameterGroup{}
	clusterParamGroupKey := client.ObjectKey{Name: paramGroupName}
	if err := p.k8sClient.Get(ctx, clusterParamGroupKey, dbClusterParamGroup); err == nil {
		if err := p.k8sClient.Delete(ctx, dbClusterParamGroup, deletionPolicy); err != nil {
			return false, err
		}
	}

	// Delete DBParameterGroup if it exists
	dbParameterGroup := &crossplaneaws.DBParameterGroup{}
	paramGroupKey := client.ObjectKey{Name: paramGroupName}
	if err := p.k8sClient.Get(ctx, paramGroupKey, dbParameterGroup); err == nil {
		if err := p.k8sClient.Delete(ctx, dbParameterGroup, deletionPolicy); err != nil {
			return false, err
		}
	}

	// Delete DBCluster if it exists
	dbCluster := &crossplaneaws.DBCluster{}
	clusterKey := client.ObjectKey{Name: spec.ResourceName}
	if err := p.k8sClient.Get(ctx, clusterKey, dbCluster); err == nil {
		if err := p.k8sClient.Delete(ctx, dbCluster, deletionPolicy); err != nil {
			return false, err
		}
	}

	// Delete DBInstance if it exists
	instanceKey := client.ObjectKey{Name: spec.ResourceName}
	dbInstance := &crossplaneaws.DBInstance{}
	if err := p.k8sClient.Get(ctx, instanceKey, dbInstance); err == nil {
		if err := p.k8sClient.Delete(ctx, dbInstance, deletionPolicy); err != nil {
			return false, err
		}
	}

	// Delete secondary DBInstance if it exists
	dbInstance2 := &crossplaneaws.DBInstance{}
	instance2Key := client.ObjectKey{Name: spec.ResourceName + "-2"}
	if err := p.k8sClient.Get(ctx, instance2Key, dbInstance2); err == nil {
		if err := p.k8sClient.Delete(ctx, dbInstance2, deletionPolicy); err != nil {
			return false, err
		}
	}

	return true, nil
}

func (p *AWSProvider) GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error) {
	panic("not implemented")
}

func (p *AWSProvider) createPostgres(ctx context.Context, params DatabaseSpec) error {
	logger := log.FromContext(ctx)

	// Handle database parameter group
	paramGroupName := getParameterGroupName(params)
	dbParamGroup := &crossplaneaws.DBParameterGroup{}

	err := p.k8sClient.Get(ctx, client.ObjectKey{Name: paramGroupName}, dbParamGroup)
	if errors.IsNotFound(err) {
		logger.Info("Creating Postgres Instance parameter group", "paramGroupName", paramGroupName)
		if createErr := p.k8sClient.Create(ctx, p.postgresDBInstance(params)); createErr != nil {
			return fmt.Errorf("failed to create DB parameter group: %w", createErr)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get DB parameter group: %w", err)
	}

	// Handle database instance
	dbInstance := &crossplaneaws.DBInstance{}
	instanceKey := client.ObjectKey{Name: params.ResourceName}

	err = p.k8sClient.Get(ctx, instanceKey, dbInstance)
	if errors.IsNotFound(err) {
		dbInstance = p.postgresDBInstance(params)

		// Create master password secret before creating the DB instance
		if passwordErr := ManageMasterPassword(ctx, dbInstance.Spec.ForProvider.CustomDBInstanceParameters.MasterUserPasswordSecretRef, p.k8sClient); passwordErr != nil {
			return fmt.Errorf("failed to manage master password for DB instance %s: %w", params.ResourceName, passwordErr)
		}

		logger.Info("Creating Postgres db instance", "dbInstance", params.ResourceName)
		if createErr := p.k8sClient.Create(ctx, dbInstance); createErr != nil {
			return fmt.Errorf("failed to create DB instance %s: %w", params.ResourceName, createErr)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get DB instance %s: %w", params.ResourceName, err)
	}

	if !dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		return fmt.Errorf("can not create Cloud DB instance %s it is being deleted", params.ResourceName)
	}

	if updateErr := p.updateDBInstance(ctx, params, dbInstance); updateErr != nil {
		return fmt.Errorf("failed to update DB instance: %w", updateErr)
	}

	return nil
}

func (p *AWSProvider) createAuroraDB(ctx context.Context, params DatabaseSpec) error {
	logger := log.FromContext(ctx)
	// Handle database instance parameter group
	paramGroupName := getParameterGroupName(params)
	dbParamGroup := &crossplaneaws.DBParameterGroup{}
	if err := p.k8sClient.Get(ctx, client.ObjectKey{Name: paramGroupName}, dbParamGroup); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating Aurora DB Instance parameter group", "paramGroupName", paramGroupName)
			if err := p.k8sClient.Create(ctx, p.auroraInstanceParamGroup(params)); err != nil {
				return fmt.Errorf("failed to create DB parameter group: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get DB parameter group: %w", err)
		}
	}

	// Handle database cluster parameter group
	dbClusterParamGroup := &crossplaneaws.DBClusterParameterGroup{}
	if err := p.k8sClient.Get(ctx, client.ObjectKey{Name: paramGroupName}, dbClusterParamGroup); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating Aurora DB Cluster parameter group", "paramGroupName", paramGroupName)
			if err := p.k8sClient.Create(ctx, p.auroraClusterParamGroup(params)); err != nil {
				return fmt.Errorf("failed to create DB parameter group: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get DB parameter group: %w", err)
		}
	}

	// Handle aurora database cluster creation
	dbCluster := &crossplaneaws.DBCluster{}
	clusterKey := client.ObjectKey{Name: params.ResourceName}
	if err := p.k8sClient.Get(ctx, clusterKey, dbCluster); err != nil {
		if errors.IsNotFound(err) {
			dbCluster = p.auroraDBCluster(params)
			// Create master password secret before creating the DB instance
			passwordErr := ManageMasterPassword(ctx, dbCluster.Spec.ForProvider.CustomDBClusterParameters.MasterUserPasswordSecretRef, p.k8sClient)
			if passwordErr != nil {
				return fmt.Errorf("failed to manage master password: %w", passwordErr)
			}
			logger.Info("Creating Aurora DB Cluster", "name", params.ResourceName)
			if err := p.k8sClient.Create(ctx, dbCluster); err != nil {
				return fmt.Errorf("failed to create DB instance: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get DB instance: %w", err)
		}
	}

	// Handle primary database instance
	primaryDbInstance := &crossplaneaws.DBInstance{}
	primaryInstanceKey := client.ObjectKey{Name: params.ResourceName}
	if err := p.k8sClient.Get(ctx, primaryInstanceKey, primaryDbInstance); err != nil {
		if errors.IsNotFound(err) {
			primaryDbInstance = p.auroraDBInstance(params, false)
			logger.Info("Creating Aurora DB Instance", "name", params.ResourceName)
			if err := p.k8sClient.Create(ctx, primaryDbInstance); err != nil {
				return fmt.Errorf("failed to create DB instance: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get DB instance: %w", err)
		}
	}

	if !primaryDbInstance.ObjectMeta.DeletionTimestamp.IsZero() || !dbCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return fmt.Errorf("can not create Cloud DB instance %s it is being deleted", params.ResourceName)
	}

	err := p.updateAuroraDBCluster(ctx, params, dbCluster)
	if err != nil {
		return fmt.Errorf("failed to update DB cluster: %v", err)
	}

	err = p.updateDBInstance(ctx, params, primaryDbInstance)
	if err != nil {
		return fmt.Errorf("failed to update primary DB instance: %v", err)
	}

	// Handle secondary database instance
	if basefun.GetMultiAZEnabled(p.config) {
		secondaryDbInstance := &crossplaneaws.DBInstance{}
		secondaryInstanceKey := client.ObjectKey{Name: params.ResourceName + "-2"}
		if err := p.k8sClient.Get(ctx, secondaryInstanceKey, secondaryDbInstance); err != nil {
			if errors.IsNotFound(err) {
				secondaryDbInstance = p.auroraDBInstance(params, true)
				logger.Info("Creating Aurora DB Secondary Instance", "name", params.ResourceName+"-2")
				if err := p.k8sClient.Create(ctx, secondaryDbInstance); err != nil {
					return fmt.Errorf("failed to create DB instance: %w", err)
				}
			} else {
				return fmt.Errorf("failed to get DB instance: %w", err)
			}
		}

		if !secondaryDbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
			return fmt.Errorf("can not create Cloud DB instance %s it is being deleted", params.ResourceName)
		}

		err := p.updateDBInstance(ctx, params, secondaryDbInstance)
		if err != nil {
			return fmt.Errorf("failed to update secondary DB instance: %v", err)
		}
	}

	return nil
}

func (p *AWSProvider) postgresDBInstance(params DatabaseSpec) *crossplaneaws.DBInstance {
	ms64 := int64(params.MinStorageGB)
	multiAZ := basefun.GetMultiAZEnabled(p.config)

	var maxStorageVal *int64
	if params.MaxStorageGB == 0 {
		maxStorageVal = nil
	} else {
		maxStorageVal = &params.MaxStorageGB
	}

	var restoreFrom *crossplaneaws.RestoreDBInstanceBackupConfiguration
	if params.SnapshotID != nil {
		restoreFrom = &crossplaneaws.RestoreDBInstanceBackupConfiguration{
			Snapshot: &crossplaneaws.SnapshotRestoreBackupConfiguration{
				SnapshotIdentifier: params.SnapshotID,
			},
			Source: ptr.To(SnapshotSource),
		}
	}

	p.configureDBTags(&params)

	return &crossplaneaws.DBInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:   params.ResourceName,
			Labels: params.Labels,
		},
		Spec: crossplaneaws.DBInstanceSpec{
			ForProvider: crossplaneaws.DBInstanceParameters{
				CACertificateIdentifier: params.CACertificateIdentifier,
				Region:                  basefun.GetRegion(p.config),
				CustomDBInstanceParameters: crossplaneaws.CustomDBInstanceParameters{
					ApplyImmediately:  ptr.To(true),
					SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
					VPCSecurityGroupIDRefs: []xpv1.Reference{
						{Name: basefun.GetVpcSecurityGroupIDRefs(p.config)},
					},
					DBSubnetGroupNameRef: &xpv1.Reference{
						Name: basefun.GetDbSubnetGroupNameRef(p.config),
					},
					AutogeneratePassword: true,
					MasterUserPasswordSecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      params.ResourceName + MasterPasswordSuffix,
							Namespace: p.serviceNS,
						},
						Key: MasterPasswordSecretKey,
					},
					EngineVersion: GetEngineVersion(params, p.config),
					RestoreFrom:   restoreFrom,
					DBParameterGroupNameRef: &xpv1.Reference{
						Name: getParameterGroupName(params),
					},
				},
				Engine:                          &params.DbType,
				MultiAZ:                         &multiAZ,
				DBInstanceClass:                 &params.InstanceClass,
				AllocatedStorage:                &ms64,
				MaxAllocatedStorage:             maxStorageVal,
				MasterUsername:                  &params.MasterUsername,
				PubliclyAccessible:              &params.PubliclyAccessible,
				EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
				EnablePerformanceInsights:       &params.EnablePerfInsight,
				EnableCloudwatchLogsExports:     params.EnableCloudwatchLogsExport,
				BackupRetentionPeriod:           &params.BackupRetentionDays,
				StorageEncrypted:                ptr.To(true),
				StorageType:                     &params.StorageType,
				Port:                            &params.Port,
				PreferredMaintenanceWindow:      params.PreferredMaintenanceWindow,
				Tags: ConvertFromProviderTags(params.Tags, func(tag ProviderTag) *crossplaneaws.Tag {
					return &crossplaneaws.Tag{Key: &tag.Key, Value: &tag.Value}
				}),
			},
			ResourceSpec: xpv1.ResourceSpec{
				WriteConnectionSecretToReference: &xpv1.SecretReference{
					Name:      params.ResourceName,
					Namespace: p.serviceNS,
				},
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) postgresDBParameterGroup(params DatabaseSpec) *crossplaneaws.DBParameterGroup {
	logical := "rds.logical_replication"
	one := "1"
	immediate := "immediate"
	reboot := "pending-reboot"
	forceSsl := "rds.force_ssl"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	pgName := getParameterGroupName(params)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := params.DatabaseName
	desc := "custom PG for " + pgName

	return &crossplaneaws.DBParameterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: pgName,
		},
		Spec: crossplaneaws.DBParameterGroupSpec{
			ForProvider: crossplaneaws.DBParameterGroupParameters{
				Region:      basefun.GetRegion(p.config),
				Description: &desc,
				CustomDBParameterGroupParameters: crossplaneaws.CustomDBParameterGroupParameters{
					DBParameterGroupFamilySelector: &crossplaneaws.DBParameterGroupFamilyNameSelector{
						Engine:        params.DbType,
						EngineVersion: GetEngineVersion(params, p.config),
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
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) updateDBInstance(ctx context.Context, params DatabaseSpec, dbInstance *crossplaneaws.DBInstance) error {
	// Create a patch snapshot from current DBInstance
	patchDBInstance := client.MergeFrom(dbInstance.DeepCopy())

	if params.DbType == AwsPostgres {
		multiAZ := basefun.GetMultiAZEnabled(p.config)
		ms64 := int64(params.MinStorageGB)
		dbInstance.Spec.ForProvider.AllocatedStorage = &ms64

		var maxStorageVal *int64
		if params.MaxStorageGB == 0 {
			maxStorageVal = nil
		} else {
			maxStorageVal = &params.MaxStorageGB
		}
		dbInstance.Spec.ForProvider.MaxAllocatedStorage = maxStorageVal
		dbInstance.Spec.ForProvider.EnableCloudwatchLogsExports = params.EnableCloudwatchLogsExport
		dbInstance.Spec.ForProvider.MultiAZ = &multiAZ
	}
	enablePerfInsight := params.EnablePerfInsight
	dbInstance.Spec.ForProvider.EnablePerformanceInsights = &enablePerfInsight
	dbInstance.Spec.DeletionPolicy = params.DeletionPolicy
	dbInstance.Spec.ForProvider.CACertificateIdentifier = params.CACertificateIdentifier
	if params.DbType == AwsAuroraPostgres {
		dbInstance.Spec.ForProvider.EnableCloudwatchLogsExports = nil
	}
	// Compute a json patch based on the changed DBInstance
	dbInstancePatchData, err := patchDBInstance.Data(dbInstance)
	if err != nil {
		return err
	}
	// an empty json patch will be {}, we can assert that no update is required if len == 2
	// we could also just apply the empty patch if additional call to apiserver isn't an issue
	if len(dbInstancePatchData) <= 2 {
		return nil
	}

	if params.PreferredMaintenanceWindow != nil {
		dbInstance.Spec.ForProvider.PreferredMaintenanceWindow = params.PreferredMaintenanceWindow
	}

	err = p.k8sClient.Patch(ctx, dbInstance, patchDBInstance)
	if err != nil {
		return err
	}

	return nil
}

func (p *AWSProvider) auroraDBInstance(params DatabaseSpec, isSecondInstance bool) *crossplaneaws.DBInstance {
	dbHostname := params.ResourceName
	if isSecondInstance {
		dbHostname = dbHostname + "-2"
	}

	p.configureDBTags(&params)

	return &crossplaneaws.DBInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:   dbHostname,
			Labels: params.Labels,
		},
		Spec: crossplaneaws.DBInstanceSpec{
			ForProvider: crossplaneaws.DBInstanceParameters{
				CACertificateIdentifier: params.CACertificateIdentifier,
				Region:                  basefun.GetRegion(p.config),
				CustomDBInstanceParameters: crossplaneaws.CustomDBInstanceParameters{
					ApplyImmediately:  ptr.To(true),
					SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
					EngineVersion:     GetEngineVersion(params, p.config),
					DBParameterGroupNameRef: &xpv1.Reference{
						Name: getParameterGroupName(params),
					},
				},
				Engine:                      &params.DbType,
				DBInstanceClass:             &params.InstanceClass,
				PubliclyAccessible:          &params.PubliclyAccessible,
				DBClusterIdentifier:         &params.ResourceName,
				EnablePerformanceInsights:   &params.EnablePerfInsight,
				EnableCloudwatchLogsExports: nil,
				PreferredMaintenanceWindow:  params.PreferredMaintenanceWindow,
				Tags: ConvertFromProviderTags(params.Tags, func(tag ProviderTag) *crossplaneaws.Tag {
					return &crossplaneaws.Tag{Key: &tag.Key, Value: &tag.Value}
				}),
			},
			ResourceSpec: xpv1.ResourceSpec{
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}

}

func (p *AWSProvider) auroraDBCluster(params DatabaseSpec) *crossplaneaws.DBCluster {
	var auroraBackupRetentionPeriod *int64
	if params.BackupRetentionDays != 0 {
		auroraBackupRetentionPeriod = &params.BackupRetentionDays
	} else {
		auroraBackupRetentionPeriod = nil
	}

	p.configureDBTags(&params)

	return &crossplaneaws.DBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: params.ResourceName,
		},
		Spec: crossplaneaws.DBClusterSpec{
			ForProvider: crossplaneaws.DBClusterParameters{
				Region:                basefun.GetRegion(p.config),
				BackupRetentionPeriod: auroraBackupRetentionPeriod,
				CustomDBClusterParameters: crossplaneaws.CustomDBClusterParameters{
					SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
					VPCSecurityGroupIDRefs: []xpv1.Reference{
						{Name: basefun.GetVpcSecurityGroupIDRefs(p.config)},
					},
					DBSubnetGroupNameRef: &xpv1.Reference{
						Name: basefun.GetDbSubnetGroupNameRef(p.config),
					},
					AutogeneratePassword: true,
					MasterUserPasswordSecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      params.ResourceName + MasterPasswordSuffix,
							Namespace: p.serviceNS,
						},
						Key: MasterPasswordSecretKey,
					},
					DBClusterParameterGroupNameRef: &xpv1.Reference{
						Name: getParameterGroupName(params),
					},
					EngineVersion: GetEngineVersion(params, p.config),
				},
				Engine: &params.DBVersion,
				Tags: ConvertFromProviderTags(params.Tags, func(tag ProviderTag) *crossplaneaws.Tag {
					return &crossplaneaws.Tag{Key: &tag.Key, Value: &tag.Value}
				}),
				MasterUsername:                  &params.MasterUsername,
				EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
				StorageEncrypted:                ptr.To(true),
				StorageType:                     &params.StorageType,
				Port:                            &params.Port,
				EnableCloudwatchLogsExports:     params.EnableCloudwatchLogsExport,
				IOPS:                            nil,
				PreferredMaintenanceWindow:      params.PreferredMaintenanceWindow,
			},
			ResourceSpec: xpv1.ResourceSpec{
				WriteConnectionSecretToReference: &xpv1.SecretReference{
					Name:      params.ResourceName,
					Namespace: p.serviceNS,
				},
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) auroraClusterParamGroup(params DatabaseSpec) *crossplaneaws.DBClusterParameterGroup {
	logical := "rds.logical_replication"
	one := "1"
	immediate := "immediate"
	reboot := "pending-reboot"
	forceSsl := "rds.force_ssl"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	pgName := getParameterGroupName(params)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := params.DatabaseName
	desc := "custom PG for " + pgName

	return &crossplaneaws.DBClusterParameterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: pgName,
		},
		Spec: crossplaneaws.DBClusterParameterGroupSpec{
			ForProvider: crossplaneaws.DBClusterParameterGroupParameters{
				Region:      basefun.GetRegion(p.config),
				Description: &desc,
				CustomDBClusterParameterGroupParameters: crossplaneaws.CustomDBClusterParameterGroupParameters{
					DBParameterGroupFamilySelector: &crossplaneaws.DBParameterGroupFamilyNameSelector{
						Engine:        params.DbType,
						EngineVersion: GetEngineVersion(params, p.config),
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
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) auroraInstanceParamGroup(params DatabaseSpec) *crossplaneaws.DBParameterGroup {
	immediate := "immediate"
	reboot := "pending-reboot"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	pgName := getParameterGroupName(params)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := params.DatabaseName
	desc := "custom PG for " + pgName

	return &crossplaneaws.DBParameterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: pgName,
		},
		Spec: crossplaneaws.DBParameterGroupSpec{
			ForProvider: crossplaneaws.DBParameterGroupParameters{
				Region:      basefun.GetRegion(p.config),
				Description: &desc,
				CustomDBParameterGroupParameters: crossplaneaws.CustomDBParameterGroupParameters{
					DBParameterGroupFamilySelector: &crossplaneaws.DBParameterGroupFamilyNameSelector{
						Engine:        params.DbType,
						EngineVersion: GetEngineVersion(params, p.config),
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
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) updateAuroraDBCluster(ctx context.Context, params DatabaseSpec, dbCluster *crossplaneaws.DBCluster) error {
	// Create a patch snapshot from current DBCluster
	patchDBCluster := client.MergeFrom(dbCluster.DeepCopy())
	p.configureDBTags(&params)
	// Update DBCluster
	dbCluster.Spec.ForProvider.Tags = ConvertFromProviderTags(params.Tags, func(tag ProviderTag) *crossplaneaws.Tag {
		return &crossplaneaws.Tag{Key: &tag.Key, Value: &tag.Value}
	})
	if params.BackupRetentionDays != 0 {
		dbCluster.Spec.ForProvider.BackupRetentionPeriod = &params.BackupRetentionDays
	}
	dbCluster.Spec.ForProvider.StorageType = &params.StorageType
	dbCluster.Spec.DeletionPolicy = params.DeletionPolicy

	// Compute a json patch based on the changed RDSInstance
	dbClusterPatchData, err := patchDBCluster.Data(dbCluster)
	if err != nil {
		return err
	}
	// an empty json patch will be {}, we can assert that no update is required if len == 2
	// we could also just apply the empty patch if additional call to apiserver isn't an issue
	if len(dbClusterPatchData) <= 2 {
		return nil
	}

	err = p.k8sClient.Patch(ctx, dbCluster, patchDBCluster)
	if err != nil {
		return err
	}

	return nil
}

func (p *AWSProvider) configureDBTags(params *DatabaseSpec) {
	backupPolicy := params.BackupPolicy
	if backupPolicy == "" {
		backupPolicy = basefun.GetDefaultBackupPolicy(p.config)
	}

	params.Tags = MergeTags(params.Tags, []ProviderTag{OperationalTAGActive, {Key: BackupPolicyKey, Value: backupPolicy}})
}

func (p *AWSProvider) isDBInstanceReady(ctx context.Context, instanceName string) (bool, error) {
	dbInstance := &crossplaneaws.DBInstance{}
	instanceKey := client.ObjectKey{Name: instanceName}
	if err := p.k8sClient.Get(ctx, instanceKey, dbInstance); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return isReady(dbInstance.Status.Conditions)
}

func (p *AWSProvider) isDBClusterReady(ctx context.Context, clusterName string) (bool, error) {
	dbCluster := &crossplaneaws.DBCluster{}
	clusterKey := client.ObjectKey{Name: clusterName}
	if err := p.k8sClient.Get(ctx, clusterKey, dbCluster); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return isReady(dbCluster.Status.Conditions)
}

func (p *AWSProvider) markClusterAsInactive(ctx context.Context, name string) (bool, error) {
	dbCluster := &crossplaneaws.DBCluster{}
	err := p.k8sClient.Get(ctx, client.ObjectKey{
		Name: name,
	}, dbCluster)
	if err != nil {
		return false, err
	}

	if isInactiveAtProvider(dbCluster.Status.AtProvider.TagList) {
		return true, nil
	}

	patchDBInstance := client.MergeFrom(dbCluster.DeepCopy())
	dbCluster.Spec.ForProvider.Tags = changeToInactive(dbCluster.Spec.ForProvider.Tags)

	err = p.k8sClient.Patch(ctx, dbCluster, patchDBInstance)
	if err != nil {
		return false, err
	}
	return false, nil
}

func (p *AWSProvider) markInstanceAsInactive(ctx context.Context, name string) (bool, error) {
	dbInstance := &crossplaneaws.DBInstance{}
	err := p.k8sClient.Get(ctx, client.ObjectKey{
		Name: name,
	}, dbInstance)
	if err != nil {
		return false, err
	}

	if isInactiveAtProvider(dbInstance.Status.AtProvider.TagList) {
		return true, nil
	}

	patchDBInstance := client.MergeFrom(dbInstance.DeepCopy())
	dbInstance.Spec.ForProvider.Tags = changeToInactive(dbInstance.Spec.ForProvider.Tags)

	err = p.k8sClient.Patch(ctx, dbInstance, patchDBInstance)
	if err != nil {
		return false, err
	}
	return false, nil
}

func isInactiveAtProvider(tags []*crossplaneaws.Tag) bool {
	for _, tag := range tags {
		if *tag.Key == OperationalTAGInactive.Key && *tag.Value == OperationalTAGInactive.Value {
			return true
		}
	}
	return false
}

func changeToInactive(tags []*crossplaneaws.Tag) []*crossplaneaws.Tag {
	found := false
	for _, tag := range tags {
		if *tag.Key == OperationalTAGInactive.Key && *tag.Value == OperationalTAGInactive.Value {
			found = true
			break
		}
		if *tag.Key == OperationalTAGInactive.Key && *tag.Value == OperationalTAGActive.Value {
			found = true
			*tag.Value = OperationalTAGInactive.Value
		}
	}
	if !found {
		tags = append(tags, &crossplaneaws.Tag{Key: &OperationalTAGInactive.Key, Value: &OperationalTAGInactive.Value})
	}
	return tags
}

// addInactiveOperationalTag marks the instance with operational-status: inactive tag,
// it returns ErrTagNotPropagated if the tag is not yet propagated to the cloud resource
func (p *AWSProvider) addInactiveOperationalTag(ctx context.Context, spec DatabaseSpec) (bool, error) {
	for _, tag := range spec.Tags {
		if tag.Key == OperationalTAGInactive.Key || tag.Value == OperationalTAGInactive.Value {
			return true, nil
		}
	}

	if tagged, err := p.markInstanceAsInactive(ctx, spec.ResourceName); err != nil || !tagged {
		return false, err
	}

	if spec.DbType == AwsAuroraPostgres {
		if tagged, err := p.markClusterAsInactive(ctx, spec.ResourceName); err != nil || !tagged {
			return false, err
		}
	}

	return true, nil
}
