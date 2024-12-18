package pgctl

import (
	"errors"
	"testing"
)

func TestParseLastLine(t *testing.T) {
	filePath := "/tmp/pub_1731362625.sql"
	for _, tt := range []struct {
		line string
		want error
	}{
		{
			line: `psql:/tmp/pub_1731362625.sql:36: ERROR:  function "calculate_user_total" already exists with same argument types`,
			want: errors.New(`36: ERROR:  function "calculate_user_total" already exists with same argument types`),
		},
	} {

		got := parseLastLine(filePath, tt.line)
		if got.Error() != tt.want.Error() {
			t.Errorf("got %s, want %s", got.Error(), tt.want.Error())
		}

	}
}
