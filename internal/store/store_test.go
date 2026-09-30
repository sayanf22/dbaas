package store

import (
	"context"
	"errors"
	"testing"
)

func TestNewPoolRejectsUnsafeConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PoolConfig
		wantErr error // nil = any error
	}{
		{"remote host without verify-full", PoolConfig{URL: "postgres://app@db.example.com:6543/postgres?sslmode=require", Mode: TransactionPooler, MaxConns: 4}, ErrInsecureConnection},
		{"remote host, local exemption doesn't apply", PoolConfig{URL: "postgres://app@db.example.com:6543/postgres?sslmode=disable", Mode: TransactionPooler, MaxConns: 4, AllowInsecureLocal: true}, ErrInsecureConnection},
		{"loopback without the local exemption", PoolConfig{URL: "postgres://app@127.0.0.1:54329/postgres?sslmode=disable", Mode: TransactionPooler, MaxConns: 4}, ErrInsecureConnection},
		{"MaxConns zero", PoolConfig{URL: "postgres://app@127.0.0.1/postgres?sslmode=verify-full", Mode: SessionPooler}, nil},
		{"MaxConns over budget", PoolConfig{URL: "postgres://app@127.0.0.1/postgres?sslmode=verify-full", Mode: SessionPooler, MaxConns: 50}, nil},
		{"unknown mode", PoolConfig{URL: "postgres://app@127.0.0.1/postgres?sslmode=disable", MaxConns: 2, AllowInsecureLocal: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := NewPool(context.Background(), tt.cfg)
			if pool != nil {
				pool.Close()
			}
			if err == nil {
				t.Fatalf("NewPool(%s) = nil error; want rejection", tt.name)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("NewPool() error = %v; want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckTLSAllowsSafeConfig(t *testing.T) {
	for _, cfg := range []PoolConfig{
		{URL: "postgres://app@aws-0-ap-south-1.pooler.supabase.com:6543/postgres?sslmode=verify-full"},
		{URL: "postgres://app@127.0.0.1:54329/postgres?sslmode=disable", AllowInsecureLocal: true},
		{URL: "postgres://app@localhost:54322/postgres", AllowInsecureLocal: true},
	} {
		if err := checkTLS(cfg); err != nil {
			t.Errorf("checkTLS(%q) = %v; want nil", cfg.URL, err)
		}
	}
}
