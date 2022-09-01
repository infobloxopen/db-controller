package pgutil

import (
	"database/sql"
	"testing"
)

func TestPgReplicate(t *testing.T) {
	testCreatePublication(t)
}
func testCreatePublication(t *testing.T) {
	type args struct {
		db      *sql.DB
		pubName string
	}
	type testcase struct {
		name    string
		args    args
		want    string
		wantErr bool
	}

	db_ok, _ := sql.Open("postgres", "postgresql://bjeevan:example@localhost:5433/pub?sslmode=disable")

	defer db_ok.Close()

	tests := []testcase{
		{name: "ok", want: "blah", wantErr: false, args: args{db: db_ok, pubName: "blah"}},
		{name: "ok default name ", want: defaultPubName, wantErr: false, args: args{db: db_ok}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CreatePublication(tt.args.db, tt.args.pubName)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreatePublication() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("CreatePublication() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeletePublication(t *testing.T) {

	db_ok, _ := sql.Open("postgres", "postgresql://bjeevan:example@localhost:5433/pub?sslmode=disable")
	defer db_ok.Close()

	type args struct {
		db      *sql.DB
		pubName string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{name: "ok", want: "blah", wantErr: false, args: args{db: db_ok, pubName: "blah"}},
		{name: "ok default name ", want: defaultPubName, wantErr: false, args: args{db: db_ok}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeletePublication(tt.args.db, tt.args.pubName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeletePublication() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DeletePublication() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateSubscription(t *testing.T) {

	db_ok, _ := sql.Open("postgres", "postgresql://bjeevan:example@localhost:5434/sub?sslmode=disable")
	pubDbDsnURI := "postgresql://bjeevan:example@localhost:5433/pub?sslmode=disable"
	defer db_ok.Close()

	type args struct {
		db          *sql.DB
		pubDbDsnURI string
		pubName     string
		subName     string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{name: "ok", want: "subblah", wantErr: false, args: args{db: db_ok, pubDbDsnURI: pubDbDsnURI, pubName: "blah", subName: "subblah"}},
		{name: "ok default name ", want: defaultSubName, wantErr: false, args: args{db: db_ok, pubDbDsnURI: pubDbDsnURI}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CreateSubscription(tt.args.db, tt.args.pubDbDsnURI, tt.args.pubName, tt.args.subName)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateSubscription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("CreateSubscription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnableSubscription(t *testing.T) {

	db_ok, _ := sql.Open("postgres", "postgresql://bjeevan:example@localhost:5434/sub?sslmode=disable")

	type args struct {
		db      *sql.DB
		subName string
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{name: "ok", want: true, wantErr: false, args: args{db: db_ok, subName: defaultSubName}},
		{name: "not found", want: false, wantErr: true, args: args{db: db_ok, subName: "unknownsub"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EnableSubscription(tt.args.db, tt.args.subName)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnableSubscription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EnableSubscription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeleteSubscription(t *testing.T) {

	db_ok, _ := sql.Open("postgres", "postgresql://bjeevan:example@localhost:5434/sub?sslmode=disable")

	type args struct {
		db      *sql.DB
		subName string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{name: "ok", want: "subblah", wantErr: false, args: args{db: db_ok, subName: "subblah"}},
		{name: "ok default name ", want: defaultSubName, wantErr: false, args: args{db: db_ok}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeleteSubscription(tt.args.db, tt.args.subName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteSubscription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DeleteSubscription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDisableSubscription(t *testing.T) {
	db_ok, _ := sql.Open("postgres", "postgresql://bjeevan:example@localhost:5434/sub?sslmode=disable")

	type args struct {
		db      *sql.DB
		subName string
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{name: "ok", want: true, wantErr: false, args: args{db: db_ok, subName: defaultSubName}},
		{name: "not found", want: false, wantErr: true, args: args{db: db_ok, subName: "unknownsub"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DisableSubscription(tt.args.db, tt.args.subName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DisableSubscription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DisableSubscription() = %v, want %v", got, tt.want)
			}
		})
	}
}
