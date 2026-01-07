package utils

import (
	"os"
	"path/filepath"
)

// CopyFile copies a single file from src to dst, preserving permissions.
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	if err != nil {
		return err
	}

	// Copy file permissions
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, sourceInfo.Mode())
}

// CopyDir recursively copies a directory from src to dst.
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate destination path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return CopyFile(path, dstPath)
	})
}

// RunningAsRoot checks if the program is running with root privileges.
// If real is true, checks the real user ID (who ran the command).
// If real is false, checks the effective user ID (current privileges, affected by setuid).
func RunningAsRoot(real bool) bool {
	var uid int
	if real {
		uid = os.Getuid() // Real user ID - who actually ran the command
	} else {
		uid = os.Geteuid() // Effective user ID - current privileges (affected by setuid)
	}
	return uid == 0
}
