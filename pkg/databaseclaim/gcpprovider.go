package databaseclaim

import (
	"context"
	"fmt"
	"strings"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	_ "github.com/lib/pq"
	crossplanegcp "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"

	"k8s.io/apimachinery/pkg/api/errors"
)

func (r *DatabaseClaimReconciler) manageCloudHostGCP(ctx context.Context, dbClaim *v1.DatabaseClaim) (bool, error) {
	dbHostIdentifier := r.Input.DbHostIdentifier

	if dbClaim.Spec.Type == v1.Postgres {
		_, err := r.manageDBClusterGCP(ctx, dbHostIdentifier, dbClaim)
		if err != nil {
			return false, err
		}
	}

	log.FromContext(ctx).Info("dbcluster is ready. proceeding to manage dbinstance")
	insReady, err := r.managePostgresDBInstanceGCP(ctx, dbHostIdentifier, dbClaim)
	if err != nil {
		return false, err
	}

	return insReady, nil
}

func (r *DatabaseClaimReconciler) manageDBClusterGCP(ctx context.Context, dbHostName string,
	dbClaim *v1.DatabaseClaim) (bool, error) {

	logr := log.FromContext(ctx)

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

	dbCluster := &crossplanegcp.Cluster{}
	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}

	params := &r.Input.HostParams

	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbCluster)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			logr.Error(err, "error != notfound retrieving cluster")
			return false, err
		}

		logr.Info("creating_crossplane_dbcluster", "name", dbHostName)
		validationError := params.CheckEngineVersion()
		if validationError != nil {
			logr.Error(validationError, "invalid_db_version")
			return false, validationError
		}
		dbCluster = &crossplanegcp.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: dbHostName,
				//Labels: map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
			},
			Spec: crossplanegcp.ClusterSpec{
				ForProvider: crossplanegcp.ClusterParameters{
					// Annotations to allow client tools to store small amount of arbitrary data. This is distinct from labels. https://google.aip.dev/128
					// An object containing a list of "key": value pairs. Example: { "name": "wrench", "mass": "1.3kg", "count": "3" }.
					// +kubebuilder:validation:Optional
					// +mapType=granular
					//Annotations: map[string]*string{"meta.upbound.io/resource-type": ptr.To("alloydb/v1beta1/cluster")}, //map[string]*string `json:"annotations,omitempty" tf:"annotations,omitempty"`

					// The automated backup policy for this cluster. AutomatedBackupPolicy is disabled by default.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					AutomatedBackupPolicy: &crossplanegcp.AutomatedBackupPolicyParameters{ // `json:"automatedBackupPolicy,omitempty" tf:"automated_backup_policy,omitempty"`
						Enabled: ptr.To(true),
					},

					// The type of cluster. If not set, defaults to PRIMARY.
					// Default value is PRIMARY.
					// Possible values are: PRIMARY, SECONDARY.
					// +kubebuilder:validation:Optional
					//ClusterType: "PRIMARY", // *string `json:"clusterType,omitempty" tf:"cluster_type,omitempty"`

					// The continuous backup config for this cluster.
					// If no policy is provided then the default policy will be used. The default policy takes one backup a day and retains backups for 14 days.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					//ContinuousBackupConfig *ContinuousBackupConfigParameters `json:"continuousBackupConfig,omitempty" tf:"continuous_backup_config,omitempty"`

					// The database engine major version. This is an optional field and it's populated at the Cluster creation time. This field cannot be changed after cluster creation.
					// +kubebuilder:validation:Optional
					DatabaseVersion: getAlloyDBVersion(&params.EngineVersion), // *string `json:"databaseVersion,omitempty" tf:"database_version,omitempty"`

					// Policy to determine if the cluster should be deleted forcefully.
					// Deleting a cluster forcefully, deletes the cluster and all its associated instances within the cluster.
					// Deleting a Secondary cluster with a secondary instance REQUIRES setting deletion_policy = "FORCE" otherwise an error is returned. This is needed as there is no support to delete just the secondary instance, and the only way to delete secondary instance is to delete the associated secondary cluster forcefully which also deletes the secondary instance.
					// +kubebuilder:validation:Optional
					DeletionPolicy: ptr.To(string(params.DeletionPolicy)), // *string `json:"deletionPolicy,omitempty" tf:"deletion_policy,omitempty"`

					// User-settable and human-readable display name for the Cluster.
					// +kubebuilder:validation:Optional
					DisplayName: &dbHostName, // *string `json:"displayName,omitempty" tf:"display_name,omitempty"`

					// EncryptionConfig describes the encryption config of a cluster or a backup that is encrypted with a CMEK (customer-managed encryption key).
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					// EncryptionConfig: &crossplanegcp.ClusterEncryptionConfigParameters{ // *ClusterEncryptionConfigParameters `json:"encryptionConfig,omitempty" tf:"encryption_config,omitempty"`
					// 	KMSKeyName: "",
					// }

					// For Resource freshness validation (https://google.aip.dev/154)
					// +kubebuilder:validation:Optional
					//Etag *string `json:"etag,omitempty" tf:"etag,omitempty"`

					// Initial user to setup during cluster creation.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					InitialUser: &crossplanegcp.InitialUserParameters{ //  *InitialUserParameters `json:"initialUser,omitempty" tf:"initial_user,omitempty"`
						PasswordSecretRef: dbMasterSecretCluster,
						User:              &params.MasterUsername,
					},

					// User-defined labels for the alloydb cluster.
					// Note: This field is non-authoritative, and will only manage the labels present in your configuration.
					// Please refer to the field effective_labels for all of the labels present on the resource.
					// +kubebuilder:validation:Optional
					// +mapType=granular
					//Labels: map[string]*string{"app.kubernetes.io/managed-by": ptr.To("db-controller")}, // map[string]*string `json:"labels,omitempty" tf:"labels,omitempty"`

					// The location where the alloydb cluster should reside.
					// +kubebuilder:validation:Required
					Location: ptr.To(basefun.GetRegion(r.Config.Viper)), // *string `json:"location" tf:"location,omitempty"`

					// MaintenanceUpdatePolicy defines the policy for system updates.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					// MaintenanceUpdatePolicy: &crossplanegcp.MaintenanceUpdatePolicyParameters{ //  *MaintenanceUpdatePolicyParameters `json:"maintenanceUpdatePolicy,omitempty" tf:"maintenance_update_policy,omitempty"`

					// 	MaintenanceWindows: []crossplanegcp.MaintenanceWindowsParameters{
					// 		{
					// 			StartTime: &crossplanegcp.StartTimeParameters{

					// 				Hours:   ptr.To(float64(1)), //TODO: parse the maintenancewindow to this format
					// 				Minutes: ptr.To(float64(0)),
					// 			},
					// 			Day: ptr.To("SATURDAY"),
					// 		},
					// 	},
					// },

					// The relative resource name of the VPC network on which the instance can be accessed. It is specified in the following form:
					// "projects/{projectNumber}/global/networks/{network_id}".
					// +crossplane:generate:reference:type=github.com/upbound/provider-gcp/apis/compute/v1beta1.Network
					// +crossplane:generate:reference:extractor=github.com/crossplane/upjet/pkg/resource.ExtractResourceID()
					// +kubebuilder:validation:Optional
					Network: (map[bool]*string{true: ptr.To(basefun.GetNetwork(r.Config.Viper)), false: nil})[basefun.GetNetwork(r.Config.Viper) != ""], // *string `json:"network,omitempty" tf:"network,omitempty"`

					// Metadata related to network configuration.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					NetworkConfig: &crossplanegcp.NetworkConfigParameters{ // *NetworkConfigParameters `json:"networkConfig,omitempty" tf:"network_config,omitempty"`
						Network:          (map[bool]*string{true: ptr.To(basefun.GetNetwork(r.Config.Viper)), false: nil})[basefun.GetNetwork(r.Config.Viper) != ""],
						AllocatedIPRange: ptr.To(basefun.GetAllocatedIpRange(r.Config.Viper)),
					},

					// Reference to a Network in compute to populate network.
					// +kubebuilder:validation:Optional
					//NetworkRef *v1.Reference `json:"networkRef,omitempty" tf:"-"`

					// Selector for a Network in compute to populate network.
					// +kubebuilder:validation:Optional
					//NetworkSelector *v1.Selector `json:"networkSelector,omitempty" tf:"-"`

					// The ID of the project in which the resource belongs.
					// If it is not provided, the provider project is used.
					// +kubebuilder:validation:Optional
					//Project *string `json:"project,omitempty" tf:"project,omitempty"`

					// Configuration for Private Service Connect (PSC) for the cluster.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					PscConfig: &crossplanegcp.PscConfigParameters{ // *PscConfigParameters `json:"pscConfig,omitempty" tf:"psc_config,omitempty"`
						PscEnabled: ptr.To(true),
					},

					// The source when restoring from a backup. Conflicts with 'restore_continuous_backup_source', both can't be set together.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					//RestoreBackupSource *RestoreBackupSourceParameters `json:"restoreBackupSource,omitempty" tf:"restore_backup_source,omitempty"`

					// The source when restoring via point in time recovery (PITR). Conflicts with 'restore_backup_source', both can't be set together.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					//RestoreContinuousBackupSource *RestoreContinuousBackupSourceParameters `json:"restoreContinuousBackupSource,omitempty" tf:"restore_continuous_backup_source,omitempty"`

					// Configuration of the secondary cluster for Cross Region Replication. This should be set if and only if the cluster is of type SECONDARY.
					// Structure is documented below.
					// +kubebuilder:validation:Optional
					//SecondaryConfig *SecondaryConfigParameters `json:"secondaryConfig,omitempty" tf:"secondary_config,omitempty"`
				},
				ResourceSpec: xpv1.ResourceSpec{
					WriteConnectionSecretToReference: &dbSecretCluster,
					ProviderConfigReference:          &providerConfigReference,
					DeletionPolicy:                   params.DeletionPolicy,
				},
			},
		}

		//create master password secret, before calling create on DBInstance
		// err := r.manageMasterPassword(ctx, dbCluster.Spec.ForProvider.CustomDBClusterParameters.MasterUserPasswordSecretRef)
		// if err != nil {
		// 	return false, err
		// }
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
	_, err = r.updateDBClusterGCP(ctx, dbClaim, dbCluster)
	if err != nil {
		return false, err
	}

	return r.isResourceReady(dbCluster.Status.ResourceStatus)
}

// https://cloud.google.com/alloydb/docs/reference/rest/v1beta/DatabaseVersion
func getAlloyDBVersion(engineVersion *string) *string {
	if strings.HasPrefix(*engineVersion, "14") {
		return ptr.To("POSTGRES_14")
	}
	return ptr.To("POSTGRES_15")
}

func (r *DatabaseClaimReconciler) managePostgresDBInstanceGCP(ctx context.Context, dbHostName string, dbClaim *v1.DatabaseClaim) (bool, error) {
	logr := log.FromContext(ctx)
	serviceNS, err := r.getServiceNamespace()
	if err != nil {
		return false, err
	}
	// dbSecretInstance := xpv1.SecretReference{
	// 	Name:      dbHostName,
	// 	Namespace: serviceNS,
	// }

	dbMasterSecretInstance := xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{
			Name:      dbHostName + masterSecretSuffix,
			Namespace: serviceNS,
		},
		Key: masterPasswordKey,
	}

	// Infrastructure Config
	region := basefun.GetRegion(r.Config.Viper)
	providerConfigReference := xpv1.Reference{
		Name: basefun.GetProviderConfig(r.Config.Viper),
	}
	dbInstance := &crossplanegcp.Instance{}

	params := &r.Input.HostParams
	multiAZ := basefun.GetMultiAZEnabled(r.Config.Viper)
	// trueVal := true

	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			validationError := params.CheckEngineVersion()
			if validationError != nil {
				return false, validationError
			}
			dbInstance = &crossplanegcp.Instance{
				ObjectMeta: metav1.ObjectMeta{
					Name: dbHostName,
					// TODO - Figure out the proper labels for resource
					//Labels: map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
				},
				Spec: crossplanegcp.InstanceSpec{
					ForProvider: crossplanegcp.InstanceParameters{
						// Annotations to allow client tools to store small amount of arbitrary data. This is distinct from labels.
						// Note: This field is non-authoritative, and will only manage the annotations present in your configuration.
						// Please refer to the field effective_annotations for all of the annotations present on the resource.
						// +kubebuilder:validation:Optional
						// +mapType=granular
						//Annotations map[string]*string `json:"annotations,omitempty" tf:"annotations,omitempty"`

						// 'Availability type of an Instance. Defaults to REGIONAL for both primary and read instances.
						// Note that primary and read instances can have different availability types.
						// Only READ_POOL instance supports ZONAL type. Users can't specify the zone for READ_POOL instance.
						// Zone is automatically chosen from the list of zones in the region specified.
						// Read pool of size 1 can only have zonal availability. Read pools with node count of 2 or more
						// can have regional availability (nodes are present in 2 or more zones in a region).'
						// Possible values are: AVAILABILITY_TYPE_UNSPECIFIED, ZONAL, REGIONAL.
						// +kubebuilder:validation:Optional
						AvailabilityType: ptr.To((map[bool]string{true: "ZONAL", false: "REGIONAL"})[multiAZ]), // *string `json:"availabilityType,omitempty" tf:"availability_type,omitempty"`

						// Client connection specific configurations.
						// Structure is documented below.
						// +kubebuilder:validation:Optional
						ClientConnectionConfig: &crossplanegcp.ClientConnectionConfigParameters{ //*ClientConnectionConfigParameters `json:"clientConnectionConfig,omitempty" tf:"client_connection_config,omitempty"`
							SSLConfig: &crossplanegcp.SSLConfigParameters{
								SSLMode: ptr.To("ENCRYPTED_ONLY"),
							},
						},

						// Identifies the alloydb cluster. Must be in the format
						// 'projects/{project}/locations/{location}/clusters/{cluster_id}'
						// +crossplane:generate:reference:type=github.com/upbound/provider-gcp/apis/alloydb/v1beta2.Cluster
						// +crossplane:generate:reference:extractor=github.com/crossplane/upjet/pkg/resource.ExtractParamPath("name",true)
						// +kubebuilder:validation:Optional
						//Cluster *string `json:"cluster,omitempty" tf:"cluster,omitempty"`

						// Reference to a Cluster in alloydb to populate cluster.
						// +kubebuilder:validation:Optional
						ClusterRef: &xpv1.Reference{ // *v1.Reference `json:"clusterRef,omitempty" tf:"-"`
							Name: dbHostName,
						},

						// Selector for a Cluster in alloydb to populate cluster.
						// +kubebuilder:validation:Optional
						//ClusterSelector *v1.Selector `json:"clusterSelector,omitempty" tf:"-"`

						// Database flags. Set at instance level. * They are copied from primary instance on read instance creation. * Read instances can set new or override existing flags that are relevant for reads, e.g. for enabling columnar cache on a read instance. Flags set on read instance may or may not be present on primary.
						// +kubebuilder:validation:Optional
						// +mapType=granular
						//DatabaseFlags map[string]*string `json:"databaseFlags,omitempty" tf:"database_flags,omitempty"`

						// User-settable and human-readable display name for the Instance.
						// +kubebuilder:validation:Optional
						DisplayName: ptr.To(dbHostName), // *string `json:"displayName,omitempty" tf:"display_name,omitempty"`

						// The Compute Engine zone that the instance should serve from, per https://cloud.google.com/compute/docs/regions-zones This can ONLY be specified for ZONAL instances. If present for a REGIONAL instance, an error will be thrown. If this is absent for a ZONAL instance, instance is created in a random zone with available capacity.
						// +kubebuilder:validation:Optional
						GceZone: ptr.To(region), // *string `json:"gceZone,omitempty" tf:"gce_zone,omitempty"`

						// The type of the instance.
						// If the instance type is READ_POOL, provide the associated PRIMARY/SECONDARY instance in the depends_on meta-data attribute.
						// If the instance type is SECONDARY, point to the cluster_type of the associated secondary cluster instead of mentioning SECONDARY.
						// Example: {instance_type = google_alloydb_cluster.<secondary_cluster_name>.
						// Use deletion_policy = "FORCE" in the associated secondary cluster and delete the cluster forcefully to delete the secondary cluster as well its associated secondary instance.
						// Possible values are: PRIMARY, READ_POOL, SECONDARY.
						// +crossplane:generate:reference:type=github.com/upbound/provider-gcp/apis/alloydb/v1beta2.Cluster
						// +crossplane:generate:reference:extractor=github.com/crossplane/upjet/pkg/resource.ExtractParamPath("cluster_type",false)
						// +kubebuilder:validation:Optional
						InstanceType: ptr.To("PRIMARY"), // *string `json:"instanceType,omitempty" tf:"instance_type,omitempty"`

						// Reference to a Cluster in alloydb to populate instanceType.
						// +kubebuilder:validation:Optional
						//InstanceTypeRef *v1.Reference `json:"instanceTypeRef,omitempty" tf:"-"`

						// Selector for a Cluster in alloydb to populate instanceType.
						// +kubebuilder:validation:Optional
						//InstanceTypeSelector *v1.Selector `json:"instanceTypeSelector,omitempty" tf:"-"`

						// User-defined labels for the alloydb instance.
						// Note: This field is non-authoritative, and will only manage the labels present in your configuration.
						// Please refer to the field effective_labels for all of the labels present on the resource.
						// +kubebuilder:validation:Optional
						// +mapType=granular
						//Labels: map[string]*string{"app.kubernetes.io/managed-by": ptr.To("db-controller")}, // map[string]*string `json:"labels,omitempty" tf:"labels,omitempty"`

						// Configurations for the machines that host the underlying database engine.
						// Structure is documented below.
						// +kubebuilder:validation:Optional
						//MachineConfig *MachineConfigParameters `json:"machineConfig,omitempty" tf:"machine_config,omitempty"`

						// Instance level network configuration.
						// Structure is documented below.
						// +kubebuilder:validation:Optional
						NetworkConfig: &crossplanegcp.InstanceNetworkConfigParameters{ // *InstanceNetworkConfigParameters `json:"networkConfig,omitempty" tf:"network_config,omitempty"`
							EnablePublicIP: ptr.To(false),
						},

						// Configuration for Private Service Connect (PSC) for the instance.
						// Structure is documented below.
						// +kubebuilder:validation:Optional
						//PscInstanceConfig *PscInstanceConfigParameters `json:"pscInstanceConfig,omitempty" tf:"psc_instance_config,omitempty"`

						// Configuration for query insights.
						// Structure is documented below.
						// +kubebuilder:validation:Optional
						//QueryInsightsConfig *QueryInsightsConfigParameters `json:"queryInsightsConfig,omitempty" tf:"query_insights_config,omitempty"`

						// Read pool specific config. If the instance type is READ_POOL, this configuration must be provided.
						// Structure is documented below.
						// +kubebuilder:validation:Optional
						//ReadPoolConfig *ReadPoolConfigParameters `json:"readPoolConfig,omitempty" tf:"read_pool_config,omitempty"`
					},
					ResourceSpec: xpv1.ResourceSpec{
						//WriteConnectionSecretToReference: &dbSecretInstance,
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}

			//create master password secret, before calling create on DBInstance
			err := r.manageMasterPassword(ctx, &dbMasterSecretInstance)
			if err != nil {
				return false, err
			}
			//create DBInstance
			logr.Info("creating crossplane DBInstance resource", "DBInstance", dbInstance.Name)
			if err := r.Client.Create(ctx, dbInstance); err != nil {
				return false, err
			}
		} else {
			return false, err
		}
	}
	// Deletion is long running task check that is not being deleted.
	if !dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud DB instance %s it is being deleted", dbHostName)
		logr.Error(err, "DBInstance", "dbHostIdentifier", dbHostName)
		return false, err
	}

	return r.isResourceReady(dbInstance.Status.ResourceStatus)
}

func (r *DatabaseClaimReconciler) updateDBClusterGCP(ctx context.Context, dbClaim *v1.DatabaseClaim, dbCluster *crossplanegcp.Cluster) (bool, error) {

	logr := log.FromContext(ctx)

	// Create a patch snapshot from current DBCluster
	patchDBCluster := client.MergeFrom(dbCluster.DeepCopy())

	// Update DBCluster
	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)
	if r.Input.BackupRetentionDays != 0 {
		dbCluster.Spec.ForProvider.AutomatedBackupPolicy = &crossplanegcp.AutomatedBackupPolicyParameters{
			Enabled: ptr.To(true),
			QuantityBasedRetention: &crossplanegcp.QuantityBasedRetentionParameters{
				Count: basefun.GetNumBackupsToRetain(r.Config.Viper),
			},
		}
	}
	dbCluster.Spec.DeletionPolicy = r.Input.HostParams.DeletionPolicy

	logr.Info("updating crossplane DBCluster resource", "DBCluster", dbCluster.Name)
	err := r.Client.Patch(ctx, dbCluster, patchDBCluster)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *DatabaseClaimReconciler) deleteExternalResourcesGCP(ctx context.Context, dbClaim *v1.DatabaseClaim) error {
	// delete any external resources associated with the dbClaim
	// Only RDS Instance are managed for now
	reclaimPolicy := basefun.GetDefaultReclaimPolicy(r.Config.Viper)

	if reclaimPolicy == "delete" {
		dbHostName := r.getDynamicHostName(dbClaim)

		// Delete
		if err := r.deleteCloudDatabaseGCP(dbHostName, ctx); err != nil {
			return err
		}
	}
	// else reclaimPolicy == "retain" nothing to do!

	return nil
}

func (r *DatabaseClaimReconciler) deleteCloudDatabaseGCP(dbHostName string, ctx context.Context) error {

	logr := log.FromContext(ctx)
	dbInstance := &crossplanegcp.Instance{}
	dbCluster := &crossplanegcp.Cluster{}

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
				logr.Info("unable delete crossplane Instance resource", "Instance", dbHostName+"-2")
				return err
			} else {
				logr.Info("deleted crossplane Instance resource", "Instance", dbHostName+"-2")
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
			logr.Info("unable delete crossplane Instance resource", "Instance", dbHostName)
			return err
		} else {
			logr.Info("deleted crossplane Instance resource", "Instance", dbHostName)
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
			logr.Info("unable delete crossplane Cluster resource", "Cluster", dbHostName)
			return err
		} else {
			logr.Info("deleted crossplane Cluster resource", "Cluster", dbHostName)
		}
	}

	return nil
}
