package databaseclaim

import (
	"context"
	"fmt"
	"strings"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	_ "github.com/lib/pq"
	crossplanegcp "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	persistanceinfobloxcomv1alpha1 "github.com/infobloxopen/db-controller/api/persistance.infoblox.com/v1alpha1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"

	"k8s.io/apimachinery/pkg/api/errors"
)

func (r *DatabaseClaimReconciler) manageCloudHostGCP(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) (bool, error) {
	dbHostIdentifier := r.getDynamicHostName(reqInfo.HostParams.Hash(), dbClaim)

	if dbClaim.Spec.Type != v1.Postgres {
		return false, fmt.Errorf("%w: %q must be one of %s", v1.ErrInvalidDBType, dbClaim.Spec.Type, []v1.DatabaseType{v1.Postgres})
	}

	_, err := r.manageDBClusterGCP(ctx, reqInfo, dbHostIdentifier, dbClaim)
	if err != nil {
		return false, err
	}

	log.FromContext(ctx).Info("dbcluster is ready. proceeding to manage dbinstance")
	insReady, err := r.managePostgresDBInstanceGCP(ctx, reqInfo, dbHostIdentifier, dbClaim)
	if err != nil {
		return false, err
	}

	if insReady {
		err = r.manageNetworkRecord(ctx, dbHostIdentifier)
		if err != nil {
			return false, err
		}

		err = r.createSecretWithConnInfo(ctx, reqInfo, dbHostIdentifier, dbClaim)
		if err != nil {
			log.FromContext(ctx).Error(err, "error writing secret with conn info")
			return false, err
		}
	}

	return insReady, nil
}

func (r *DatabaseClaimReconciler) createSecretWithConnInfo(ctx context.Context, reqInfo *requestInfo, dbHostIdentifier string, dbclaim *v1.DatabaseClaim) error {

	var instance crossplanegcp.Instance
	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostIdentifier,
	}, &instance)
	if err != nil {
		return err
	}

	serviceNS, err := r.getServiceNamespace()
	if err != nil {
		return err
	}

	var secret = &corev1.Secret{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Name:      dbHostIdentifier,
		Namespace: serviceNS,
	}, secret)
	if err != nil {
		return err
	}

	pass := string(secret.Data["attribute.initial_user.0.password"])

	secret.Data["username"] = []byte(reqInfo.HostParams.MasterUsername)
	secret.Data["password"] = []byte(pass)
	secret.Data["endpoint"] = []byte(*instance.Status.AtProvider.PscInstanceConfig.PscDNSName)
	secret.Data["port"] = []byte("5432")

	log.FromContext(ctx).Info("updating conninfo secret", "name", secret.Name, "namespace", secret.Namespace)
	return r.Client.Update(ctx, secret)
}

func (r *DatabaseClaimReconciler) manageNetworkRecord(ctx context.Context, dbHostIdentifier string) error {
	var netRec persistanceinfobloxcomv1alpha1.XNetworkRecord
	logr := log.FromContext(ctx)

	var instance crossplanegcp.Instance
	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostIdentifier,
	}, &instance)
	if err != nil {
		return err
	}
	pscDnsName := instance.Status.AtProvider.PscInstanceConfig.PscDNSName
	serviceAttachmentLink := instance.Status.AtProvider.PscInstanceConfig.ServiceAttachmentLink

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostIdentifier + "-psc-network",
	}, &netRec)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			logr.Error(err, "error != notfound retrieving XNetworkRecord")
			return err
		}

		serviceNS, err := r.getServiceNamespace()
		if err != nil {
			return err
		}

		netRec := &persistanceinfobloxcomv1alpha1.XNetworkRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbHostIdentifier + "-psc-network",
				Namespace: serviceNS,
			},
			Spec: persistanceinfobloxcomv1alpha1.XNetworkRecordSpec{
				Parameters: persistanceinfobloxcomv1alpha1.XNetworkRecordParameters{
					PSCDNSName:            *pscDnsName,
					ServiceAttachmentLink: *serviceAttachmentLink,
					Region:                basefun.GetRegion(r.Config.Viper),
					Subnetwork:            basefun.GetSubNetwork(r.Config.Viper),
					Network:               basefun.GetNetwork(r.Config.Viper),
				},
			},
		}

		logr.Info("creating XNetworkRecord resource", "XNetworkRecord", netRec.Name)
		if err := r.Client.Create(ctx, netRec); err != nil {
			logr.Error(err, "crossplane_xnetwork_create")
			return err
		}
	}

	return nil
}

func (r *DatabaseClaimReconciler) manageDBClusterGCP(ctx context.Context, reqInfo *requestInfo, dbHostName string,
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

	params := &reqInfo.HostParams

	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)

	labels := propagateLabels(dbClaim.Labels)

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbCluster)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			logr.Error(err, "error != notfound retrieving cluster")
			return false, err
		}

		logr.Info("creating_crossplane_dbcluster", "name", dbHostName)
		dbCluster = &crossplanegcp.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   dbHostName,
				Labels: labels,
			},
			Spec: crossplanegcp.ClusterSpec{
				ForProvider: crossplanegcp.ClusterParameters{

					AutomatedBackupPolicy: &crossplanegcp.AutomatedBackupPolicyParameters{
						Enabled: ptr.To(true),
					},

					DatabaseVersion: getAlloyDBVersion(&params.DBVersion),

					DeletionPolicy: ptr.To(string(params.DeletionPolicy)),

					DisplayName: &dbHostName,

					InitialUser: &crossplanegcp.InitialUserParameters{
						PasswordSecretRef: dbMasterSecretCluster,
						User:              &params.MasterUsername,
					},

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

					//Network: (map[bool]*string{true: ptr.To(basefun.GetNetwork(r.Config.Viper)), false: nil})[basefun.GetNetwork(r.Config.Viper) != ""],

					// NetworkConfig: &crossplanegcp.NetworkConfigParameters{
					// 	Network:          (map[bool]*string{true: ptr.To(basefun.GetNetwork(r.Config.Viper)), false: nil})[basefun.GetNetwork(r.Config.Viper) != ""],
					// 	AllocatedIPRange: ptr.To(basefun.GetAllocatedIpRange(r.Config.Viper)),
					// },

					PscConfig: &crossplanegcp.PscConfigParameters{
						PscEnabled: ptr.To(true),
					},
				},
				ResourceSpec: xpv1.ResourceSpec{
					WriteConnectionSecretToReference: &dbSecretCluster,
					ProviderConfigReference:          &providerConfigReference,
					DeletionPolicy:                   params.DeletionPolicy,
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
	_, err = r.updateDBClusterGCP(ctx, reqInfo, dbClaim, dbCluster)
	if err != nil {
		return false, err
	}

	return r.isResourceReady("alloydb.cluster", dbHostName, dbCluster.Status.ResourceStatus)
}

// https://cloud.google.com/alloydb/docs/reference/rest/v1beta/DatabaseVersion
func getAlloyDBVersion(engineVersion *string) *string {
	if strings.HasPrefix(*engineVersion, "14") {
		return ptr.To("POSTGRES_14")
	}
	return ptr.To("POSTGRES_15")
}

func (r *DatabaseClaimReconciler) managePostgresDBInstanceGCP(ctx context.Context, reqInfo *requestInfo, dbHostName string, dbClaim *v1.DatabaseClaim) (bool, error) {
	logr := log.FromContext(ctx)
	serviceNS, err := r.getServiceNamespace()
	if err != nil {
		return false, err
	}

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

	params := &reqInfo.HostParams
	multiAZ := basefun.GetMultiAZEnabled(r.Config.Viper)

	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)

	labels := propagateLabels(dbClaim.Labels)

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			dbInstance = &crossplanegcp.Instance{
				ObjectMeta: metav1.ObjectMeta{
					Name:   dbHostName,
					Labels: labels,
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

						PscInstanceConfig: &crossplanegcp.PscInstanceConfigParameters{
							AllowedConsumerProjects: []*string{ptr.To(basefun.GetProject(r.Config.Viper))},
						},

						DatabaseFlags: map[string]*string{"alloydb.iam_authentication": ptr.To("on")},
					},
					ResourceSpec: xpv1.ResourceSpec{
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
			logr.Info("creating crossplane alloydb.instance", "instance", dbInstance.Name)
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

	return r.isResourceReady("alloydb.instance", dbHostName, dbInstance.Status.ResourceStatus)
}

func (r *DatabaseClaimReconciler) updateDBClusterGCP(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim, dbCluster *crossplanegcp.Cluster) (bool, error) {

	logr := log.FromContext(ctx)

	// Create a patch snapshot from current DBCluster
	patchDBCluster := client.MergeFrom(dbCluster.DeepCopy())

	// Update DBCluster
	dbClaim.Spec.Tags = r.configureBackupPolicy(dbClaim.Spec.BackupPolicy, dbClaim.Spec.Tags)
	if reqInfo.BackupRetentionDays != 0 {
		dbCluster.Spec.ForProvider.AutomatedBackupPolicy = &crossplanegcp.AutomatedBackupPolicyParameters{
			Enabled: ptr.To(true),
			QuantityBasedRetention: &crossplanegcp.QuantityBasedRetentionParameters{
				Count: basefun.GetNumBackupsToRetain(r.Config.Viper),
			},
		}
	}
	dbCluster.Spec.DeletionPolicy = reqInfo.HostParams.DeletionPolicy

	logr.Info("updating crossplane DBCluster resource", "DBCluster", dbCluster.Name)
	err := r.Client.Patch(ctx, dbCluster, patchDBCluster)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *DatabaseClaimReconciler) deleteExternalResourcesGCP(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) error {
	// delete any external resources associated with the dbClaim
	// Only RDS Instance are managed for now
	reclaimPolicy := basefun.GetDefaultReclaimPolicy(r.Config.Viper)

	if reclaimPolicy == "delete" {
		dbHostName := r.getDynamicHostName(reqInfo.HostParams.Hash(), dbClaim)

		// Delete
		if err := r.deleteCloudDatabaseGCP(dbHostName, ctx); err != nil {
			return err
		}
	}
	// else reclaimPolicy == "retain" nothing to do!

	return nil
}

func (r *DatabaseClaimReconciler) cloudDatabaseExistsGCP(ctx context.Context, dbHostName string) bool {
	dbInstance := &crossplanegcp.Instance{}
	dbCluster := &crossplanegcp.Cluster{}

	var instanceExists, clusterExists bool
	var err error

	err = r.Client.Get(ctx, client.ObjectKey{Name: dbHostName}, dbCluster)
	if err == nil {
		clusterExists = true
	} else if !errors.IsNotFound(err) {
		return false // Unexpected error, assume failure
	}

	err = r.Client.Get(ctx, client.ObjectKey{Name: dbHostName}, dbInstance)
	if err == nil {
		instanceExists = true
	} else if !errors.IsNotFound(err) {
		return false
	}

	return instanceExists && clusterExists
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
