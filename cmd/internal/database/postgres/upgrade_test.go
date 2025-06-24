package postgres

import (
	"testing"
)

func TestPostgres_extractVersion(t *testing.T) {

	tests := []struct {
		name          string
		commandOutput string
		want          uint64
		wantErr       bool
	}{
		{
			name:          "postgres alpine",
			commandOutput: "PostgreSQL 12.16",
			want:          12,
			wantErr:       false,
		},
		{
			name:          "postgres debian",
			commandOutput: "PostgreSQL 12.22 (Debian 12.22-1.pgdg120+1)",
			want:          12,
			wantErr:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &Postgres{}
			got, err := db.extractVersion(tt.commandOutput)
			if (err != nil) != tt.wantErr {
				t.Errorf("Postgres.extractVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Postgres.extractVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}
