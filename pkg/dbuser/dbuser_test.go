package dbuser

import (
	"testing"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

func TestDBUser_IsUserChanged(t *testing.T) {
	type mockDBUser struct {
		role  string
		userA string
		userB string
	}
	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name   string
		dbuser mockDBUser
		args   args
		want   bool
	}{
		{
			"User unchanged",
			mockDBUser{
				role: "oldUser",
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Status: persistancev1.DatabaseClaimStatus{
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
							Username: "oldUser",
						},
					},
				},
			},
			false,
		},
		{
			"User unchanged",
			mockDBUser{
				role: "oldUser",
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Status: persistancev1.DatabaseClaimStatus{
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
							Username: "",
						},
					},
				},
			},
			false,
		},
		{
			"User changed",
			mockDBUser{
				role: "newUser",
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						Username: "newUser",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
							Username: "oldUser",
						},
					},
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DBUser{
				rolename: tt.dbuser.role,
			}
			if got := d.IsUserChanged(tt.args.dbClaim); got != tt.want {
				t.Errorf("isUserChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}
