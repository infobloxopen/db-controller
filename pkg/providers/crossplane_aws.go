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
)

const (
	MasterPasswordSuffix    = "-master"
	MasterPasswordSecretKey = "password"
	SnapshotSource          = "Snapshot"
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
	switch spec.DbType {
	case v1.AuroraPostgres:
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
		if basefun.GetMultiAZEnabled(p.config) {
			instance2Ready, err := p.isDBInstanceReady(ctx, spec.ResourceName+"-2")
			if err != nil {
				return false, err
			}
			return instanceReady && instance2Ready && clusterReady, nil
		}

		return instanceReady && clusterReady, nil
	case v1.Postgres:
		err := p.createPostgres(ctx, spec)
		if err != nil {
			return false, err
		}
		instanceReady, err := p.isDBInstanceReady(ctx, spec.ResourceName)
		if err != nil {
			return false, err
		}
		return instanceReady, nil
	}
	return false, fmt.Errorf("%w: %q must be one of %s", v1.ErrInvalidDBType, spec.DbType, []v1.DatabaseType{v1.Postgres, v1.AuroraPostgres})
}

func (p *AWSProvider) DeleteDatabase(ctx context.Context, spec DatabaseSpec) error {
	deletionPolicy := client.PropagationPolicy(metav1.DeletePropagationBackground)
	paramGroupName := getParameterGroupName(spec.ResourceName, spec.HostParams.DBVersion, spec.DbType)
	// Delete DBClusterParameterGroup if it exists
	dbClusterParamGroup := &crossplaneaws.DBClusterParameterGroup{}
	clusterParamGroupKey := client.ObjectKey{Name: paramGroupName}
	if err := p.k8sClient.Get(ctx, clusterParamGroupKey, dbClusterParamGroup); err == nil {
		if err := p.k8sClient.Delete(ctx, dbClusterParamGroup, deletionPolicy); err != nil {
			return err
		}
	}

	// Delete DBParameterGroup if it exists
	dbParameterGroup := &crossplaneaws.DBParameterGroup{}
	paramGroupKey := client.ObjectKey{Name: paramGroupName}
	if err := p.k8sClient.Get(ctx, paramGroupKey, dbParameterGroup); err == nil {
		if err := p.k8sClient.Delete(ctx, dbParameterGroup, deletionPolicy); err != nil {
			return err
		}
	}

	// Delete DBCluster if it exists
	dbCluster := &crossplaneaws.DBCluster{}
	clusterKey := client.ObjectKey{Name: spec.ResourceName}
	if err := p.k8sClient.Get(ctx, clusterKey, dbCluster); err == nil {
		if err := p.k8sClient.Delete(ctx, dbCluster, deletionPolicy); err != nil {
			return err
		}
	}

	// Delete DBInstance if it exists
	instanceKey := client.ObjectKey{Name: spec.ResourceName}
	dbInstance := &crossplaneaws.DBInstance{}
	if err := p.k8sClient.Get(ctx, instanceKey, dbInstance); err == nil {
		if err := p.k8sClient.Delete(ctx, dbInstance, deletionPolicy); err != nil {
			return err
		}
	}

	// Delete secondary DBInstance if it exists
	dbInstance2 := &crossplaneaws.DBInstance{}
	instance2Key := client.ObjectKey{Name: spec.ResourceName + "-2"}
	if err := p.k8sClient.Get(ctx, instance2Key, dbInstance2); err == nil {
		if err := p.k8sClient.Delete(ctx, dbInstance2, deletionPolicy); err != nil {
			return err
		}
	}

	return nil
}

func (p *AWSProvider) GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error) {
	panic("not implemented")
}

func (p *AWSProvider) createPostgres(ctx context.Context, params DatabaseSpec) error {
	// Handle database parameter group
	paramGroupName := getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType)
	dbParamGroup := &crossplaneaws.DBParameterGroup{}

	if err := p.k8sClient.Get(ctx, client.ObjectKey{Name: paramGroupName}, dbParamGroup); err != nil {
		if errors.IsNotFound(err) {
			if err := p.k8sClient.Create(ctx, p.auroraInstanceParamGroup(params)); err != nil {
				return fmt.Errorf("failed to create DB parameter group: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get DB parameter group: %w", err)
		}
	}

	// Handle database instance
	dbInstance := &crossplaneaws.DBInstance{}
	instanceKey := client.ObjectKey{Name: params.ResourceName}

	if err := p.k8sClient.Get(ctx, instanceKey, dbInstance); err != nil {
		if errors.IsNotFound(err) {
			dbInstance = p.postgresDBInstance(params)

			// Create master password secret before creating the DB instance
			passwordErr := ManageMasterPassword(ctx, dbInstance.Spec.ForProvider.CustomDBInstanceParameters.MasterUserPasswordSecretRef, p.k8sClient)
			if passwordErr != nil {
				return fmt.Errorf("failed to manage master password: %w", passwordErr)
			}

			if err := p.k8sClient.Create(ctx, dbInstance); err != nil {
				return fmt.Errorf("failed to create DB instance: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get DB instance: %w", err)
		}
	}

	if !dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		return fmt.Errorf("can not create Cloud DB instance %s it is being deleted", params.ResourceName)
	}

	err := p.updateDBInstance(ctx, params, dbInstance)
	if err != nil {
		return err
	}

	return nil
}

func (p *AWSProvider) createAuroraDB(ctx context.Context, params DatabaseSpec) error {
	// Handle database instance parameter group
	paramGroupName := getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType)
	dbParamGroup := &crossplaneaws.DBParameterGroup{}
	if err := p.k8sClient.Get(ctx, client.ObjectKey{Name: paramGroupName}, dbParamGroup); err != nil {
		if errors.IsNotFound(err) {
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

	err := p.auroraUpdateDBCluster(ctx, params, dbCluster)
	if err != nil {
		return err
	}

	err = p.updateDBInstance(ctx, params, primaryDbInstance)
	if err != nil {
		return err
	}

	// Handle secondary database instance
	if basefun.GetMultiAZEnabled(p.config) {
		secondaryDbInstance := &crossplaneaws.DBInstance{}
		secondaryInstanceKey := client.ObjectKey{Name: params.ResourceName + "-2"}
		if err := p.k8sClient.Get(ctx, secondaryInstanceKey, secondaryDbInstance); err != nil {
			if errors.IsNotFound(err) {
				secondaryDbInstance = p.auroraDBInstance(params, true)
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
			return err
		}
	}

	return nil
}

func (p *AWSProvider) postgresDBInstance(params DatabaseSpec) *crossplaneaws.DBInstance {
	ms64 := int64(params.HostParams.MinStorageGB)
	multiAZ := basefun.GetMultiAZEnabled(p.config)

	var maxStorageVal *int64
	if params.HostParams.MaxStorageGB == 0 {
		maxStorageVal = nil
	} else {
		maxStorageVal = &params.HostParams.MaxStorageGB
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
					SkipFinalSnapshot: params.HostParams.SkipFinalSnapshotBeforeDeletion,
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
					EngineVersion: GetEngineVersion(params.HostParams, p.config),
					RestoreFrom:   restoreFrom,
					DBParameterGroupNameRef: &xpv1.Reference{
						Name: getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType),
					},
				},
				Engine:                          &params.HostParams.Type,
				MultiAZ:                         &multiAZ,
				DBInstanceClass:                 &params.HostParams.InstanceClass,
				AllocatedStorage:                &ms64,
				MaxAllocatedStorage:             maxStorageVal,
				MasterUsername:                  &params.HostParams.MasterUsername,
				PubliclyAccessible:              &params.HostParams.PubliclyAccessible,
				EnableIAMDatabaseAuthentication: &params.HostParams.EnableIAMDatabaseAuthentication,
				EnablePerformanceInsights:       &params.EnablePerfInsight,
				EnableCloudwatchLogsExports:     params.EnableCloudwatchLogsExport,
				BackupRetentionPeriod:           &params.BackupRetentionDays,
				StorageEncrypted:                ptr.To(true),
				StorageType:                     &params.HostParams.StorageType,
				Port:                            &params.HostParams.Port,
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
				DeletionPolicy: params.HostParams.DeletionPolicy,
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
	pgName := getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := params.MasterConnInfo.DatabaseName
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
						Engine:        params.HostParams.Type,
						EngineVersion: GetEngineVersion(params.HostParams, p.config),
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
				DeletionPolicy: params.HostParams.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) updateDBInstance(ctx context.Context, params DatabaseSpec, dbInstance *crossplaneaws.DBInstance) error {
	// Create a patch snapshot from current DBInstance
	patchDBInstance := client.MergeFrom(dbInstance.DeepCopy())

	if params.DbType == v1.Postgres {
		multiAZ := basefun.GetMultiAZEnabled(p.config)
		ms64 := int64(params.HostParams.MinStorageGB)
		dbInstance.Spec.ForProvider.AllocatedStorage = &ms64

		var maxStorageVal *int64
		if params.HostParams.MaxStorageGB == 0 {
			maxStorageVal = nil
		} else {
			maxStorageVal = &params.HostParams.MaxStorageGB
		}
		dbInstance.Spec.ForProvider.MaxAllocatedStorage = maxStorageVal
		dbInstance.Spec.ForProvider.EnableCloudwatchLogsExports = params.EnableCloudwatchLogsExport
		dbInstance.Spec.ForProvider.MultiAZ = &multiAZ
	}
	enablePerfInsight := params.EnablePerfInsight
	dbInstance.Spec.ForProvider.EnablePerformanceInsights = &enablePerfInsight
	dbInstance.Spec.DeletionPolicy = params.HostParams.DeletionPolicy
	dbInstance.Spec.ForProvider.CACertificateIdentifier = params.CACertificateIdentifier
	if params.DbType == v1.AuroraPostgres {
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
					SkipFinalSnapshot: params.HostParams.SkipFinalSnapshotBeforeDeletion,
					EngineVersion:     GetEngineVersion(params.HostParams, p.config),
					DBParameterGroupNameRef: &xpv1.Reference{
						Name: getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType),
					},
				},
				Engine:                      &params.HostParams.Type,
				DBInstanceClass:             &params.HostParams.InstanceClass,
				PubliclyAccessible:          &params.HostParams.PubliclyAccessible,
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
				DeletionPolicy: params.HostParams.DeletionPolicy,
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
					SkipFinalSnapshot: params.HostParams.SkipFinalSnapshotBeforeDeletion,
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
						Key: MasterPasswordSuffix,
					},
					DBClusterParameterGroupNameRef: &xpv1.Reference{
						Name: getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType),
					},
					EngineVersion: GetEngineVersion(params.HostParams, p.config),
				},
				Engine: &params.HostParams.Type,
				Tags: ConvertFromProviderTags(params.Tags, func(tag ProviderTag) *crossplaneaws.Tag {
					return &crossplaneaws.Tag{Key: &tag.Key, Value: &tag.Value}
				}),
				MasterUsername:                  &params.HostParams.MasterUsername,
				EnableIAMDatabaseAuthentication: &params.HostParams.EnableIAMDatabaseAuthentication,
				StorageEncrypted:                ptr.To(true),
				StorageType:                     &params.HostParams.StorageType,
				Port:                            &params.HostParams.Port,
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
				DeletionPolicy: params.HostParams.DeletionPolicy,
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
	pgName := getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := params.MasterConnInfo.DatabaseName
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
						Engine:        params.HostParams.Type,
						EngineVersion: GetEngineVersion(params.HostParams, p.config),
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
				DeletionPolicy: params.HostParams.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) auroraInstanceParamGroup(params DatabaseSpec) *crossplaneaws.DBParameterGroup {
	immediate := "immediate"
	reboot := "pending-reboot"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	pgName := getParameterGroupName(params.ResourceName, params.HostParams.DBVersion, params.DbType)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := params.MasterConnInfo.DatabaseName
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
						Engine:        params.HostParams.Type,
						EngineVersion: GetEngineVersion(params.HostParams, p.config),
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
				DeletionPolicy: params.HostParams.DeletionPolicy,
			},
		},
	}
}

func (p *AWSProvider) auroraUpdateDBCluster(ctx context.Context, params DatabaseSpec, dbCluster *crossplaneaws.DBCluster) error {
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
	dbCluster.Spec.ForProvider.StorageType = &params.HostParams.StorageType
	dbCluster.Spec.DeletionPolicy = params.HostParams.DeletionPolicy

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
		return false, err
	}
	return isReady(dbInstance.Status.Conditions)
}

func (p *AWSProvider) isDBClusterReady(ctx context.Context, clusterName string) (bool, error) {
	dbCluster := &crossplaneaws.DBCluster{}
	clusterKey := client.ObjectKey{Name: clusterName}
	if err := p.k8sClient.Get(ctx, clusterKey, dbCluster); err != nil {
		return false, err
	}
	return isReady(dbCluster.Status.Conditions)
}
