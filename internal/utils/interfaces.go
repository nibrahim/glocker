package utils

import (
	"net"
	"os"
	"time"
)

// FileSystem abstracts file operations for testing.
// This allows us to mock filesystem operations in unit tests.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Stat(path string) (os.FileInfo, error)
	Remove(path string) error
	Chmod(path string, mode os.FileMode) error
	Chown(path string, uid, gid int) error
	MkdirAll(path string, perm os.FileMode) error
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
}

// CommandRunner abstracts command execution for testing.
// This allows us to mock exec.Command calls in unit tests.
type CommandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
	CombinedOutput(name string, args ...string) ([]byte, error)
}

// NetworkResolver abstracts DNS operations for testing.
// This allows us to mock network operations in unit tests.
type NetworkResolver interface {
	ResolveIPs(domain, recordType string) ([]string, error)
	ParseIP(s string) net.IP
	LookupHost(host string) ([]string, error)
}

// TimeProvider abstracts time operations for testing.
// This allows us to mock time.Now() in unit tests for time-dependent logic.
type TimeProvider interface {
	Now() time.Time
}

// DefaultFileSystem implements FileSystem using actual os package calls.
type DefaultFileSystem struct{}

func (DefaultFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (DefaultFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (DefaultFileSystem) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (DefaultFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (DefaultFileSystem) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

func (DefaultFileSystem) Chown(path string, uid, gid int) error {
	return os.Chown(path, uid, gid)
}

func (DefaultFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (DefaultFileSystem) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

// DefaultTimeProvider implements TimeProvider using actual time package calls.
type DefaultTimeProvider struct{}

func (DefaultTimeProvider) Now() time.Time {
	return time.Now()
}
