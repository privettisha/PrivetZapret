package main

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

const controlLeaseTTL = 12 * time.Second

func controlLeasePath() string {
	return filepath.Join(filepath.Dir(platformConfigPath()), "control.lease")
}

// A running desktop window owns the service. A forced UI termination cannot
// execute a close callback, so the service also watches this short lease.
func startControlLease() (func(), error) {
	path := controlLeasePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	write := func() error { return os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0600) }
	if err := write(); err != nil {
		return nil, err
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = write()
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(stop)
			<-done
			_ = os.Remove(path)
		})
	}, nil
}

func controlLeaseAlive() bool {
	info, err := os.Stat(controlLeasePath())
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	age := time.Since(info.ModTime())
	return age >= 0 && age < controlLeaseTTL
}
