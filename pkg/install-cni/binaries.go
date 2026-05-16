package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// copyPluginBinary copies a CNI plugin binary file to the specified destination directory.
// The source file is copied to dstDir, preserving the original filename.
// The destination file is created with 0755 permissions (executable).
func copyPluginBinary(srcFilePath string, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	filename := filepath.Base(srcFilePath)
	dstPath := filepath.Join(dstDir, filename)

	log.Debug("copying CNI binary", "source", srcFilePath, "destination", dstPath)
	if err := copyFile(srcFilePath, dstPath); err != nil {
		return fmt.Errorf("failed to copy %s: %w", filename, err)
	}
	log.Debug("copied binary", "source", srcFilePath, "destination", dstPath)

	return nil
}

// copyFile copies a single file from src to dst with executable permissions (0755).
// The file contents are synced to disk before returning to ensure durability.
func copyFile(src, dst string) error {
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer srcFile.Close()

	// Create destination file
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer dstFile.Close()

	// Copy contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Make the binary executable
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("failed to chmod file: %w", err)
	}

	return nil
}
