package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ulikunitz/xz/lzma"
)

func decompressBI5(srcBI5, dstBIN string) error {
	in, err := os.Open(srcBI5)
	if err != nil {
		return err
	}
	defer in.Close()

	r, err := lzma.NewReader(in)
	if err != nil {
		return err
	}

	tmp := dstBIN + ".part"
	if err := os.MkdirAll(filepath.Dir(dstBIN), 0o755); err != nil {
		return err
	}
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, r)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dstBIN)
}

// optional helper: sha256 of a file
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// (Unused) example if you later want gzip output rather than raw .bin
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	gw := gzip.NewWriter(out)

	if _, err := io.Copy(gw, in); err != nil {
		_ = gw.Close()
		_ = out.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
