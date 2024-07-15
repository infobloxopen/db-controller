package databaseclaim

import (
	"context"
	"testing"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCanTagResources(t *testing.T) {

	claims := v1.DatabaseClaimList{
		Items: []v1.DatabaseClaim{
			v1.DatabaseClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dbclaim",
					Namespace: "default",
				},
				Spec: v1.DatabaseClaimSpec{
					InstanceLabel: "sample-connection-3",
				},
			},
			v1.DatabaseClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dbclaim-2",
					Namespace: "default",
				},
				Spec: v1.DatabaseClaimSpec{
					InstanceLabel: "sample-connection-3",
				},
			},
		},
	}

	for _, tt := range []struct {
		name  string
		claim v1.DatabaseClaim
		ok    bool
	}{
		{
			name: "test1",
			claim: v1.DatabaseClaim{
				Spec: v1.DatabaseClaimSpec{
					InstanceLabel: "",
				},
			},
			ok: true,
		},
		{
			name: "test2",
			claim: v1.DatabaseClaim{
				Spec: v1.DatabaseClaimSpec{
					InstanceLabel: "sample-connection-3",
				},
			},
		},
	} {
		ctx := context.Background()
		t.Run(tt.name, func(t *testing.T) {
			ok, _ := CanTagResources(ctx, claims, tt.claim)
			if ok != tt.ok {
				t.Errorf("got %v, want %v", ok, tt.ok)
			}

		})
	}

}
