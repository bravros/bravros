package updater

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadFile(t *testing.T) {
	content := "#!/bin/sh\necho hello"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "test-binary")
	err := downloadFile(srv.URL, dst)
	if err != nil {
		t.Fatalf("downloadFile() error = %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != content {
		t.Errorf("downloaded content = %q, want %q", string(data), content)
	}
}

func TestDownloadFile_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "test-binary")
	err := downloadFile(srv.URL, dst)
	if err == nil {
		t.Fatal("expected error on server 500")
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	content := []byte("binary content here")
	if err := os.WriteFile(src, content, 0755); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("copied content = %q, want %q", got, content)
	}

	info, _ := os.Stat(dst)
	if info.Mode().Perm()&0100 == 0 {
		t.Error("expected executable permission on dst")
	}
}

func TestInstall_NoURL(t *testing.T) {
	err := Install("")
	if err == nil {
		t.Fatal("expected error when URL is empty")
	}
}
