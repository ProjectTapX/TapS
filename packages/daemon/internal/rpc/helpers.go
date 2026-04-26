package rpc

import (
	"io"
	"os"
	"path/filepath"
)

func streamToFile(absPath string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	tmp := absPath + ".upload"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, absPath)
}

func filepathBase(p string) string { return filepath.Base(p) }
