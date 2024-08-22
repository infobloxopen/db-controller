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

	// dbSecretCluster := xpv1.SecretReference{
	// 	Name:      dbHostName,
	// 	Namespace: serviceNS,
	// }

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

					AutomatedBackupPolicy: &crossplanegcp.AutomatedBackupPolicyParameters{
						Enabled: ptr.To(true),
					},

					DatabaseVersion: getAlloyDBVersion(&params.EngineVersion),

					DeletionPolicy: ptr.To(string(params.DeletionPolicy)),

					DisplayName: &dbHostName,

					InitialUser: &crossplanegcp.InitialUserParameters{
						PasswordSecretRef: dbMasterSecretCluster,
						User:              &params.MasterUsername,
					},

					//Labels: map[string]*string{"app.kubernetes.io/managed-by": ptr.To("db-controller")},

					Location: ptr.To(basefun.GetRegion(r.Config.Viper)), // *string `json:"location" tf:"location,omitempty"`

					// MaintenanceUpdatePolicy: &crossplanegcp.MaintenanceUpdatePolicyParameters{

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

					Network: (map[bool]*string{true: ptr.To(basefun.GetNetwork(r.Config.Viper)), false: nil})[basefun.GetNetwork(r.Config.Viper) != ""],

					NetworkConfig: &crossplanegcp.NetworkConfigParameters{
						Network:          (map[bool]*string{true: ptr.To(basefun.GetNetwork(r.Config.Viper)), false: nil})[basefun.GetNetwork(r.Config.Viper) != ""],
						AllocatedIPRange: ptr.To(basefun.GetAllocatedIpRange(r.Config.Viper)),
					},

					PscConfig: &crossplanegcp.PscConfigParameters{
						PscEnabled: ptr.To(true),
					},
				},
				ResourceSpec: xpv1.ResourceSpec{
					//WriteConnectionSecretToReference: &dbSecretCluster,
					ProviderConfigReference: &providerConfigReference,
					DeletionPolicy:          params.DeletionPolicy,
					PublishConnectionDetailsTo: &xpv1.PublishConnectionDetailsTo{
						Name: dbHostName,
					},
				},
			},
		}

		//create master password secret, before calling create on DBInstance
		err := r.manageMasterPassword(ctx, &dbMasterSecretCluster)
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
	// 	Name:      dbHostName + "-i",
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
						AvailabilityType: ptr.To((map[bool]string{true: "ZONAL", false: "REGIONAL"})[multiAZ]),

						ClientConnectionConfig: &crossplanegcp.ClientConnectionConfigParameters{
							SSLConfig: &crossplanegcp.SSLConfigParameters{
								SSLMode: ptr.To("ENCRYPTED_ONLY"),
							},
						},

						ClusterRef: &xpv1.Reference{
							Name: dbHostName,
						},

						DisplayName: ptr.To(dbHostName),

						GceZone: ptr.To(region),

						InstanceType: ptr.To("PRIMARY"),

						NetworkConfig: &crossplanegcp.InstanceNetworkConfigParameters{
							EnablePublicIP: ptr.To(false),
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						//WriteConnectionSecretToReference: &dbSecretInstance,
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
						PublishConnectionDetailsTo: &xpv1.PublishConnectionDetailsTo{
							Name: dbHostName + "-i",
						},
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
