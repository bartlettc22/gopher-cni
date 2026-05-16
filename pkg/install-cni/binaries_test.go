package install

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyPluginBinary tests copying a single CNI plugin binary file to a destination directory.
func TestCopyPluginBinary(t *testing.T) {
	cases := []struct {
		name            string
		srcFilename     string
		srcContent      string
		existingContent string
		expectedContent string
	}{
		{
			name:            "copy new binary",
			srcFilename:     "test-cni",
			srcContent:      "binary-v1",
			expectedContent: "binary-v1",
		},
		{
			name:            "overwrite existing binary",
			srcFilename:     "gopher-cni",
			srcContent:      "binary-v2",
			existingContent: "binary-v1",
			expectedContent: "binary-v2",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create source directory and file
			srcDir := t.TempDir()
			srcFile := filepath.Join(srcDir, c.srcFilename)
			if err := os.WriteFile(srcFile, []byte(c.srcContent), 0644); err != nil {
				t.Fatalf("failed to create source file: %v", err)
			}

			// Create destination directory
			dstDir := t.TempDir()
			dstFile := filepath.Join(dstDir, c.srcFilename)

			// Create existing file if specified
			if c.existingContent != "" {
				if err := os.WriteFile(dstFile, []byte(c.existingContent), 0644); err != nil {
					t.Fatalf("failed to create existing file: %v", err)
				}
			}

			// Copy the binary
			if err := copyPluginBinary(srcFile, dstDir); err != nil {
				t.Fatalf("copyPluginBinary failed: %v", err)
			}

			// Verify file contents
			contents, err := os.ReadFile(dstFile)
			if err != nil {
				t.Fatalf("failed to read destination file: %v", err)
			}

			if string(contents) != c.expectedContent {
				t.Fatalf("unexpected contents: got %q, want %q", string(contents), c.expectedContent)
			}

			// Verify file is executable
			info, err := os.Stat(dstFile)
			if err != nil {
				t.Fatalf("failed to stat file: %v", err)
			}
			if info.Mode().Perm()&0111 == 0 {
				t.Errorf("binary is not executable: permissions are %v", info.Mode().Perm())
			}
		})
	}
}

// TestCopyFile tests the lower-level file copying function.
func TestCopyFile(t *testing.T) {
	cases := []struct {
		name        string
		srcContent  string
		expectError bool
	}{
		{
			name:       "copy normal file",
			srcContent: "test content",
		},
		{
			name:       "copy empty file",
			srcContent: "",
		},
		{
			name:       "copy binary content",
			srcContent: "\x00\x01\x02\x03\xff\xfe",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create source file
			srcDir := t.TempDir()
			srcFile := filepath.Join(srcDir, "source")
			if err := os.WriteFile(srcFile, []byte(c.srcContent), 0644); err != nil {
				t.Fatalf("failed to create source file: %v", err)
			}

			// Create destination path
			dstDir := t.TempDir()
			dstFile := filepath.Join(dstDir, "destination")

			// Copy the file
			err := copyFile(srcFile, dstFile)
			if c.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("copyFile failed: %v", err)
			}

			// Verify contents match
			contents, err := os.ReadFile(dstFile)
			if err != nil {
				t.Fatalf("failed to read destination file: %v", err)
			}
			if string(contents) != c.srcContent {
				t.Fatalf("contents mismatch: got %q, want %q", string(contents), c.srcContent)
			}

			// Verify permissions are 0755
			info, err := os.Stat(dstFile)
			if err != nil {
				t.Fatalf("failed to stat file: %v", err)
			}
			expectedPerms := os.FileMode(0755)
			if info.Mode().Perm() != expectedPerms {
				t.Errorf("incorrect permissions: got %v, want %v", info.Mode().Perm(), expectedPerms)
			}
		})
	}
}

// TestCopyFileErrors tests error conditions for copyFile.
func TestCopyFileErrors(t *testing.T) {
	t.Run("source file does not exist", func(t *testing.T) {
		dstDir := t.TempDir()
		dstFile := filepath.Join(dstDir, "destination")

		err := copyFile("/nonexistent/source", dstFile)
		if err == nil {
			t.Fatal("expected error when source does not exist")
		}
	})

	t.Run("destination directory does not exist", func(t *testing.T) {
		srcDir := t.TempDir()
		srcFile := filepath.Join(srcDir, "source")
		if err := os.WriteFile(srcFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		err := copyFile(srcFile, "/nonexistent/dir/destination")
		if err == nil {
			t.Fatal("expected error when destination directory does not exist")
		}
	})
}
