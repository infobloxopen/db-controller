package dbuser

import (
	"flag"
	"testing"
)

// The following gingo struct and associted init() is required to run go test with ginkgo related flags
// Since this test is not using ginkgo, this is a hack to get around the issue of go test complaining about
// unknown flags.
var ginkgo struct {
	dry_run      string
	label_filter string
}

func init() {
	flag.StringVar(&ginkgo.dry_run, "ginkgo.dry-run", "", "Ignore this flag")
	flag.StringVar(&ginkgo.label_filter, "ginkgo.label-filter", "", "Ignore this flag")
}

func TestDBUser_IsUserChanged(t *testing.T) {
	tests := []struct {
		name            string
		rolename        string
		currentUserName string
		want            bool
	}{
		{
			name:            "User unchanged",
			rolename:        "oldUser",
			currentUserName: "oldUser",
			want:            false,
		},
		{
			name:            "User never set",
			rolename:        "oldUser",
			currentUserName: "",
			want:            false,
		},
		{
			name:            "User changed",
			rolename:        "newUser",
			currentUserName: "oldUser",
			want:            true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DBUser{
				rolename: tt.rolename,
			}
			if got := d.IsUserChanged(tt.currentUserName); got != tt.want {
				t.Errorf("isUserChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextUs(t *testing.T) {
	dbu := NewDBUser("test")

	tests := []struct {
		name    string
		current string
		want    string
	}{
		{
			name:    "User A",
			current: dbu.GetUserA(),
			want:    dbu.GetUserB(),
		},
		{
			name:    "User B",
			current: dbu.GetUserB(),
			want:    dbu.GetUserA(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dbu.NextUser(tt.current); got != tt.want {
				t.Errorf("NextUser() = %v, want %v", got, tt.want)
			}
		})
	}
}
