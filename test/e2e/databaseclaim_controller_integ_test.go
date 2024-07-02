package e2e

import (
	"context"
	"log"
	"testing"

	crossplanerds "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/controller"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
)

var hasOperationalTag = databaseclaim.HasOperationalTag

var _ = Describe("db-controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		BDClaimName      = "test-dbclaim"
		BDClaimNamespace = "default"
	)

	Context("When updating DB Claim Status", func() {
		It("Should update DB Claim status", func() {
			By("By creating a new DB Claim")
			ctx := context.Background()
			dbClaim := &persistancev1.DatabaseClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      BDClaimName,
					Namespace: BDClaimNamespace,
				},
				Spec: persistancev1.DatabaseClaimSpec{
					Class:                 ptr.To(""),
					AppID:                 "sample-app",
					DatabaseName:          "sample_app",
					InstanceLabel:         "sample-connection",
					SecretName:            "sample-secret",
					Username:              "sample_user",
					EnableSuperUser:       ptr.To(false),
					EnableReplicationRole: ptr.To(false),
					UseExistingSource:     ptr.To(false),
				},
			}
			Expect(e2e_k8sClient.Create(ctx, dbClaim)).Should(Succeed())
		})
	})
})

var _ = Describe("manageOperationalTagging", Ordered, func() {

	// define and create objects in the test cluster

	dbCluster := &crossplanerds.DBCluster{}
	dbClusterParam := &crossplanerds.DBClusterParameterGroup{}
	dbParam := &crossplanerds.DBParameterGroup{}
	dnInstance1 := &crossplanerds.DBInstance{}
	dnInstance2 := &crossplanerds.DBInstance{}
	dnInstance3 := &crossplanerds.DBInstance{}

	BeforeAll(func() {
		if testing.Short() {
			Skip("skipping k8s based tests")
		}
		log.Println("FIXME: move integration tests to a separate package kubebuilder uses testing/e2e")

		By("Creating objects beforehand of DBClsuerParameterGroup, DBCluser, DBParameterGroup and DBInstance")
		testString := "test"
		ctx := context.Background()
		dbCluster = &crossplanerds.DBCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rds.aws.crossplane.io/v1alpha1",
				Kind:       "DBCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "db",
				Namespace: "default",
			},
			Spec: crossplanerds.DBClusterSpec{
				ForProvider: crossplanerds.DBClusterParameters{
					Engine: &testString,
				},
			},
		}
		Expect(e2e_k8sClient.Create(ctx, dbCluster)).Should(Succeed())
		ctx = context.Background()
		dbClusterParam = &crossplanerds.DBClusterParameterGroup{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rds.aws.crossplane.io/v1alpha1",
				Kind:       "DBClusterParameterGroup",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dbparam",
				Namespace: "default",
			},
			Spec: crossplanerds.DBClusterParameterGroupSpec{
				ForProvider: crossplanerds.DBClusterParameterGroupParameters{
					Description: &testString,
				},
			},
		}
		Expect(e2e_k8sClient.Create(ctx, dbClusterParam)).Should(Succeed())

		dbParam = &crossplanerds.DBParameterGroup{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rds.aws.crossplane.io/v1alpha1",
				Kind:       "DBParameterGroup",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dbparam",
				Namespace: "default",
			},
			Spec: crossplanerds.DBParameterGroupSpec{
				ForProvider: crossplanerds.DBParameterGroupParameters{
					Description: &testString,
				},
			},
		}
		Expect(e2e_k8sClient.Create(ctx, dbParam)).Should(Succeed())

		ctx = context.Background()
		dnInstance1 = &crossplanerds.DBInstance{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rds.aws.crossplane.io/v1alpha1",
				Kind:       "DBInstance",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "db",
				Namespace: "default",
			},
			Spec: crossplanerds.DBInstanceSpec{
				ForProvider: crossplanerds.DBInstanceParameters{
					Engine:          &testString,
					DBInstanceClass: &testString,
				},
			},
		}
		Expect(e2e_k8sClient.Create(ctx, dnInstance1)).Should(Succeed())

		ctx = context.Background()
		dnInstance2 = &crossplanerds.DBInstance{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rds.aws.crossplane.io/v1alpha1",
				Kind:       "DBInstance",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "db-2",
				Namespace: "default",
			},
			Spec: crossplanerds.DBInstanceSpec{
				ForProvider: crossplanerds.DBInstanceParameters{
					Engine:          &testString,
					DBInstanceClass: &testString,
				},
			},
		}
		Expect(e2e_k8sClient.Create(ctx, dnInstance2)).Should(Succeed())

		ctx = context.Background()
		dnInstance3 = &crossplanerds.DBInstance{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rds.aws.crossplane.io/v1alpha1",
				Kind:       "DBInstance",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "db3",
				Namespace: "default",
			},
			Spec: crossplanerds.DBInstanceSpec{
				ForProvider: crossplanerds.DBInstanceParameters{
					Engine:          &testString,
					DBInstanceClass: &testString,
				},
			},
		}
		Expect(e2e_k8sClient.Create(ctx, dnInstance3)).Should(Succeed())
	})

	Context("Now, try adding tags to resources which does not exists, while multiAZ is enabled", func() {
		It("Should not add tags to any other already existing resources", func() {
			mockReconciler := &controller.DatabaseClaimReconciler{}
			mockReconciler.Client = e2e_k8sClient
			mockReconciler.Config.Viper = viper.New()
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", true)
			// providing names of non-existing resources below
			check, err := mockReconciler.Reconciler().ManageOperationalTagging(context.Background(), logr.Discard(), "dbb", "dbparamm")
			Expect(err).Should(HaveOccurred()) // This should create error
			Expect(check).To(BeFalse())

			By("Lets get all objects again to check whether tags have not been added to any resource, as we provied wrong names above")

			dbCluster := &crossplanerds.DBCluster{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db",
			}, dbCluster)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbCluster.Spec.ForProvider.Tags)).To(Equal(false))

			dbClusterParam := &crossplanerds.DBClusterParameterGroup{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "dbparam",
			}, dbClusterParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbClusterParam.Spec.ForProvider.Tags)).To(Equal(false))

			dbParam := &crossplanerds.DBParameterGroup{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "dbparam",
			}, dbParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbParam.Spec.ForProvider.Tags)).To(Equal(false))

			dnInstance1 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db",
			}, dnInstance1)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance1.Spec.ForProvider.Tags)).To(Equal(false))

			dnInstance2 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db-2",
			}, dnInstance2)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance2.Spec.ForProvider.Tags)).To(Equal(false))

			dnInstance3 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db3",
			}, dnInstance3)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance3.Spec.ForProvider.Tags)).To(Equal(false))
		})
	})

	Context("Now, try Adding tags to resources, with multiAZ  disabled", func() {
		It("Should add tags to all valid resources. Should skip instance-2 as multiAZ is disabled", func() {
			mockReconciler := &controller.DatabaseClaimReconciler{}
			mockReconciler.Client = e2e_k8sClient
			mockReconciler.Config.Viper = viper.New()
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", false)
			check, err := mockReconciler.Reconciler().ManageOperationalTagging(context.Background(), logr.Logger{}, "db", "dbparam")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(check).To(BeFalse())

			By("Lets get all objects again to check whether tags can be found at .spec.ForProvider")

			dbCluster = &crossplanerds.DBCluster{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db",
			}, dbCluster)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbCluster.Spec.ForProvider.Tags)).To(Equal(true))

			dbClusterParam = &crossplanerds.DBClusterParameterGroup{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "dbparam",
			}, dbClusterParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbClusterParam.Spec.ForProvider.Tags)).To(Equal(true))

			dbParam = &crossplanerds.DBParameterGroup{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "dbparam",
			}, dbParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbParam.Spec.ForProvider.Tags)).To(Equal(true))

			dnInstance1 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db",
			}, dnInstance1)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance1.Spec.ForProvider.Tags)).To(Equal(true))

			// tag should not be found at spec for dbInstance2 as multiAZ is disabled
			dnInstance2 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db-2",
			}, dnInstance2)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance2.Spec.ForProvider.Tags)).To(Equal(false))

			// tag should not be found at spec for dbInstance3 as we had not requested this resource to be tagged
			dnInstance3 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db3",
			}, dnInstance3)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance3.Spec.ForProvider.Tags)).To(Equal(false))
		})
	})

	Context("Adding tags to resources, while multiAZ is enabled", func() {
		It("Should add tags to all valid resources if exists. Should NOT skip instance-2 as multiAZ is enabled", func() {
			mockReconciler := &controller.DatabaseClaimReconciler{}
			mockReconciler.Client = e2e_k8sClient
			mockReconciler.Config.Viper = viper.New()
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", true)
			check, err := mockReconciler.Reconciler().ManageOperationalTagging(context.Background(), ctrl.Log.WithName("controllers"), "db", "dbparam")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(check).To(BeFalse())

			By("Lets get all DBinstance objects again to check whether tags can be found at .spec.ForProvider for all instances in multiAZ")

			dnInstance1 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db",
			}, dnInstance1)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance1.Spec.ForProvider.Tags)).To(Equal(true))

			// tag should  be found at spec for dbInstancw2 as multiAZ is enabled now
			dnInstance2 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db-2",
			}, dnInstance2)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance2.Spec.ForProvider.Tags)).To(Equal(true))

			// tag should not be found at spec for dbInstancr3 as we had not requested this resource to be tagged
			dnInstance3 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db3",
			}, dnInstance3)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance3.Spec.ForProvider.Tags)).To(Equal(false))
		})
	})

	Context("When tags get successfully updated, They are reflected at .status.AtProvider for DBInstance", func() {
		It("manageOperationalTagging() Should return true without any error", func() {
			mockReconciler := &controller.DatabaseClaimReconciler{}
			mockReconciler.Client = e2e_k8sClient
			mockReconciler.Config.Viper = viper.New()
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", true)

			By("adding tags beforehand to .status.AtProvier.TagList. As in reality, if tags gets successfully added. It will reflect at the said path")

			operationalStatusTagKeyPtr := databaseclaim.OperationalStatusTagKey
			operationalStatusInactiveValuePtr := databaseclaim.OperationalStatusInactiveValue
			ctx := context.Background()

			dnInstance1.Status.AtProvider.TagList = []*crossplanerds.Tag{
				{
					Key:   &operationalStatusTagKeyPtr,
					Value: &operationalStatusInactiveValuePtr,
				},
			}
			dnInstance2.Status.AtProvider.TagList = []*crossplanerds.Tag{
				{
					Key:   &operationalStatusTagKeyPtr,
					Value: &operationalStatusInactiveValuePtr,
				},
			}

			Expect(e2e_k8sClient.Status().Update(ctx, dnInstance1)).Should(Succeed())
			Expect(e2e_k8sClient.Status().Update(ctx, dnInstance2)).Should(Succeed())

			check, err := mockReconciler.Reconciler().ManageOperationalTagging(context.Background(), ctrl.Log.WithName("controllers"), "db", "dbparam")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(check).To(BeTrue())

			// Lets also check the tags at status
			dnInstance1 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db",
			}, dnInstance1)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance1.Status.AtProvider.TagList)).To(Equal(true))

			dnInstance2 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: "db-2",
			}, dnInstance2)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance2.Status.AtProvider.TagList)).To(Equal(true))

		})
	})

})

var _ = Describe("canTagResources", Ordered, func() {

	// Creating resources required to do tests beforehand
	BeforeAll(func() {
		if testing.Short() {
			Skip("skipping k8s based tests")
		}
		ctx := context.Background()
		dbClaim := &persistancev1.DatabaseClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "persistance.atlas.infoblox.com/v1",
				Kind:       "DatabaseClaim",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dbclaim",
				Namespace: "default",
			},
			Spec: persistancev1.DatabaseClaimSpec{
				AppID:         "sample-app",
				DatabaseName:  "sample_app",
				InstanceLabel: "sample-connection-3",
				SecretName:    "sample-secret",
				Username:      "sample_user",
			},
		}
		Expect(e2e_k8sClient.Create(ctx, dbClaim)).Should(Succeed())
		ctx2 := context.Background()
		dbClaim2 := &persistancev1.DatabaseClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "persistance.atlas.infoblox.com/v1",
				Kind:       "DatabaseClaim",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dbclaim-2",
				Namespace: "default",
			},
			Spec: persistancev1.DatabaseClaimSpec{
				AppID:         "sample-app",
				DatabaseName:  "sample_app",
				InstanceLabel: "sample-connection-3",
				SecretName:    "sample-secret",
				Username:      "sample_user",
			},
		}
		Expect(e2e_k8sClient.Create(ctx2, dbClaim2)).Should(Succeed())
	})

	Context("Adding tags to DBClaim with empty InstanceLabel", func() {
		It("Should  permite adding tags", func() {
			ctx2 := context.Background()
			dbClaim2 := &persistancev1.DatabaseClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dbclaim-2",
					Namespace: "default",
				},
				Spec: persistancev1.DatabaseClaimSpec{
					AppID:         "sample-app",
					DatabaseName:  "sample_app",
					InstanceLabel: "",
					SecretName:    "sample-secret",
					Username:      "sample_user",
				},
			}
			mockReconciler := &controller.DatabaseClaimReconciler{}
			mockReconciler.Client = e2e_k8sClient
			check, err2 := databaseclaim.CanTagResources(ctx2, e2e_k8sClient, dbClaim2)
			Expect(err2).ShouldNot(HaveOccurred())
			Expect(check).To(BeTrue())
		})
	})

	Context("Adding tags to DBClaim, When There are already more than one DBClaim exists with similar InstanceLabel", func() {
		It("Should not permite adding tags", func() {
			ctx2 := context.Background()
			dbClaim2 := &persistancev1.DatabaseClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dbclaim-2",
					Namespace: "default",
				},
				Spec: persistancev1.DatabaseClaimSpec{
					AppID:         "sample-app",
					DatabaseName:  "sample_app",
					InstanceLabel: "sample-connection-3",
					SecretName:    "sample-secret",
					Username:      "sample_user",
				},
			}
			mockReconciler := &controller.DatabaseClaimReconciler{}
			mockReconciler.Client = e2e_k8sClient
			check, err2 := databaseclaim.CanTagResources(ctx2, e2e_k8sClient, dbClaim2)
			Expect(err2).Should(HaveOccurred())
			Expect(check).To(BeFalse())
		})
	})

})
