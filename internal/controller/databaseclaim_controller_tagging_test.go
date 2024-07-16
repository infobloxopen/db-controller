package controller

import (
	"context"
	"fmt"

	crossplanerds "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
)

var hasOperationalTag = databaseclaim.HasOperationalTag

var _ = Describe("Tagging", Ordered, func() {

	// define and create objects in the test cluster

	name := fmt.Sprintf("test-%s-%s", namespace, rand.String(5))

	dbCluster := &crossplanerds.DBCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rds.aws.crossplane.io/v1alpha1",
			Kind:       "DBCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: crossplanerds.DBClusterSpec{
			ForProvider: crossplanerds.DBClusterParameters{
				Engine: ptr.To("test"),
			},
		},
	}
	dbClusterParam := &crossplanerds.DBClusterParameterGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rds.aws.crossplane.io/v1alpha1",
			Kind:       "DBClusterParameterGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: crossplanerds.DBClusterParameterGroupSpec{
			ForProvider: crossplanerds.DBClusterParameterGroupParameters{
				Description: ptr.To("test"),
			},
		},
	}

	dbParam := &crossplanerds.DBParameterGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rds.aws.crossplane.io/v1alpha1",
			Kind:       "DBParameterGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: crossplanerds.DBParameterGroupSpec{
			ForProvider: crossplanerds.DBParameterGroupParameters{
				Description: ptr.To("test"),
			},
		},
	}

	dnInstance1 := &crossplanerds.DBInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rds.aws.crossplane.io/v1alpha1",
			Kind:       "DBInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: crossplanerds.DBInstanceSpec{
			ForProvider: crossplanerds.DBInstanceParameters{
				Engine:          ptr.To("test"),
				DBInstanceClass: ptr.To("test"),
			},
		},
	}
	name2 := fmt.Sprintf("%s-2", name)
	dnInstance2 := &crossplanerds.DBInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rds.aws.crossplane.io/v1alpha1",
			Kind:       "DBInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name2,
			Namespace: namespace,
		},
		Spec: crossplanerds.DBInstanceSpec{
			ForProvider: crossplanerds.DBInstanceParameters{
				Engine:          ptr.To("test"),
				DBInstanceClass: ptr.To("test"),
			},
		},
	}
	name3 := fmt.Sprintf("%s-3", name)
	dnInstance3 := &crossplanerds.DBInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rds.aws.crossplane.io/v1alpha1",
			Kind:       "DBInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name3,
			Namespace: namespace,
		},
		Spec: crossplanerds.DBInstanceSpec{
			ForProvider: crossplanerds.DBInstanceParameters{
				Engine:          ptr.To("test"),
				DBInstanceClass: ptr.To("test"),
			},
		},
	}

	BeforeAll(func() {

		By("Creating objects beforehand of DBClusterParameterGroup, DBCluster, DBParameterGroup and DBInstance")
		ctx := context.Background()

		Expect(k8sClient.Create(ctx, dbCluster)).Should(Succeed())
		Expect(k8sClient.Create(ctx, dbClusterParam)).Should(Succeed())
		Expect(k8sClient.Create(ctx, dbParam)).Should(Succeed())

		Expect(k8sClient.Create(ctx, dnInstance1)).Should(Succeed())

		Expect(k8sClient.Create(ctx, dnInstance2)).Should(Succeed())
		Expect(k8sClient.Create(ctx, dnInstance3)).Should(Succeed())
	})

	AfterAll(func() {

		ctx := context.Background()
		By("Deleting objects of DBClsuerParameterGroup, DBCluster, DBParameterGroup and DBInstance")
		Expect(k8sClient.Delete(ctx, dbCluster)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dbClusterParam)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dbParam)).Should(Succeed())

		Expect(k8sClient.Delete(ctx, dnInstance1)).Should(Succeed())

		Expect(k8sClient.Delete(ctx, dnInstance2)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dnInstance3)).Should(Succeed())

	})

	Context("Now, try adding tags to resources which does not exists, while multiAZ is enabled", func() {
		It("Should not add tags to any other already existing resources", func() {

			mockReconciler := &DatabaseClaimReconciler{
				Client: k8sClient,
				Config: &databaseclaim.DatabaseClaimConfig{
					Viper: viper.New(),
				},
			}
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", true)
			mockReconciler.Setup()

			// providing names of non-existing resources below
			check, err := mockReconciler.Reconciler().ManageOperationalTagging(context.Background(), logr.Discard(), "dbb", "dbparamm")
			Expect(err).Should(HaveOccurred()) // This should create error
			Expect(check).To(BeFalse())

			By("Lets get all objects again to check whether tags have not been added to any resource, as we provied wrong names above")

			dbCluster := &crossplanerds.DBCluster{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dbCluster)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbCluster.Spec.ForProvider.Tags)).To(Equal(false))

			dbClusterParam := &crossplanerds.DBClusterParameterGroup{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dbClusterParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbClusterParam.Spec.ForProvider.Tags)).To(Equal(false))

			dbParam := &crossplanerds.DBParameterGroup{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dbParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbParam.Spec.ForProvider.Tags)).To(Equal(false))

			dnInstance1 = &crossplanerds.DBInstance{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dnInstance1)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance1.Spec.ForProvider.Tags)).To(Equal(false))

			dnInstance2 = &crossplanerds.DBInstance{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name: name2,
			}, dnInstance2)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance2.Spec.ForProvider.Tags)).To(Equal(false))

			dnInstance3 = &crossplanerds.DBInstance{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name: name3,
			}, dnInstance3)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance3.Spec.ForProvider.Tags)).To(Equal(false))
		})
	})

	Context("Now, try Adding tags to resources, with multiAZ  disabled", func() {
		It("Should add tags to all valid resources. Should skip instance-2 as multiAZ is disabled", func() {

			mockReconciler := &DatabaseClaimReconciler{
				Client: k8sClient,
				Config: &databaseclaim.DatabaseClaimConfig{
					Viper: viper.New(),
				},
			}
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", false)
			mockReconciler.Setup()

			check, err := mockReconciler.Reconciler().ManageOperationalTagging(context.Background(), logger, name, name)
			Expect(err).To(BeNil())
			Expect(check).To(BeFalse())

			By("Lets get all objects again to check whether tags can be found at .spec.ForProvider")

			dbCluster = &crossplanerds.DBCluster{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{Name: name}, dbCluster)).ShouldNot(HaveOccurred())

			Expect(hasOperationalTag(dbCluster.Spec.ForProvider.Tags)).To(Equal(true))

			dbClusterParam = &crossplanerds.DBClusterParameterGroup{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dbClusterParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbClusterParam.Spec.ForProvider.Tags)).To(Equal(true))

			dbParam = &crossplanerds.DBParameterGroup{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dbParam)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dbParam.Spec.ForProvider.Tags)).To(Equal(true))

			dnInstance1 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dnInstance1)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance1.Spec.ForProvider.Tags)).To(Equal(true))

			// tag should not be found at spec for dbInstance2 as multiAZ is disabled
			dnInstance2 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name2,
			}, dnInstance2)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance2.Spec.ForProvider.Tags)).To(Equal(false))

			// tag should not be found at spec for dbInstance3 as we had not requested this resource to be tagged
			dnInstance3 = &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name3,
			}, dnInstance3)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance3.Spec.ForProvider.Tags)).To(Equal(false))
		})
	})

	Context("Adding tags to resources, while multiAZ is enabled", func() {
		It("Should add tags to all valid resources if exists. Should NOT skip instance-2 as multiAZ is enabled", func() {

			mockReconciler := &DatabaseClaimReconciler{
				Client: k8sClient,
				Config: &databaseclaim.DatabaseClaimConfig{
					Viper: viper.New(),
				},
			}
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", true)
			mockReconciler.Setup()

			check, err := mockReconciler.Reconciler().ManageOperationalTagging(context.Background(), logger, name, name)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(check).To(BeFalse())

			By("Lets get all DBinstance objects again to check whether tags can be found at .spec.ForProvider for all instances in multiAZ")

			dnInstance1 := &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name,
			}, dnInstance1)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance1.Spec.ForProvider.Tags)).To(Equal(true))

			// tag should  be found at spec for dbInstancw2 as multiAZ is enabled now
			dnInstance2 := &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name2,
			}, dnInstance2)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance2.Spec.ForProvider.Tags)).To(Equal(true))

			// tag should not be found at spec for dbInstancr3 as we had not requested this resource to be tagged
			dnInstance3 := &crossplanerds.DBInstance{}
			Expect(mockReconciler.Client.Get(context.Background(), client.ObjectKey{
				Name: name3,
			}, dnInstance3)).ShouldNot(HaveOccurred())
			Expect(hasOperationalTag(dnInstance3.Spec.ForProvider.Tags)).To(Equal(false))
		})
	})

	Context("When tags get successfully updated, They are reflected at .status.AtProvider for DBInstance", func() {
		It("manageOperationalTagging() Should return true without any error", func() {

			mockReconciler := &DatabaseClaimReconciler{
				Client: k8sClient,
				Config: &databaseclaim.DatabaseClaimConfig{
					Viper: viper.New(),
				},
			}
			mockReconciler.Config.Viper.Set("dbMultiAZEnabled", true)
			mockReconciler.Setup()

			By("adding tags beforehand to .status.AtProvier.TagList. As in reality, if tags gets successfully added. It will reflect at the said path")

			operationalStatusTagKeyPtr := databaseclaim.OperationalStatusTagKey
			operationalStatusInactiveValuePtr := databaseclaim.OperationalStatusInactiveValue
			ctx := context.Background()

			var updateStatus = func(dbName string) {
				dnInstance := crossplanerds.DBInstance{}
				Expect(k8sClient.Get(context.Background(), client.ObjectKey{
					Name: dbName,
				}, &dnInstance)).ShouldNot(HaveOccurred())

				dnInstance.Status.AtProvider.TagList = []*crossplanerds.Tag{
					{
						Key:   &operationalStatusTagKeyPtr,
						Value: &operationalStatusInactiveValuePtr,
					},
				}
				Expect(k8sClient.Status().Update(ctx, &dnInstance)).Should(Succeed())
			}

			updateStatus(name)
			updateStatus(name2)

			check, err := mockReconciler.Reconciler().ManageOperationalTagging(ctx, logger, name, name)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(check).To(BeTrue())

			// Lets also check the tags at status
			checkInstanceStatus(k8sClient, name, true)
			checkInstanceStatus(k8sClient, name2, true)

		})
	})

})

func checkInstanceStatus(k8sClient client.Client, name string, expected bool) {

	dnInstance1 := &crossplanerds.DBInstance{}
	Expect(k8sClient.Get(context.Background(), client.ObjectKey{
		Name: name,
	}, dnInstance1)).ShouldNot(HaveOccurred())
	Expect(hasOperationalTag(dnInstance1.Status.AtProvider.TagList)).To(Equal(expected))

}
