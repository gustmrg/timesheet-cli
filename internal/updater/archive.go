package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
)

const maxBinarySize = 50 << 20

func extractBinary(archive []byte, archiveFormat, binaryName string) ([]byte, error) {
	switch archiveFormat {
	case "tar.gz":
		compressed, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, fmt.Errorf("open release archive: %w", err)
		}
		defer compressed.Close()
		reader := tar.NewReader(compressed)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("read release archive: %w", err)
			}
			if path.Clean(header.Name) != binaryName || header.Typeflag != tar.TypeReg {
				continue
			}
			return readBinary(reader, header.Size)
		}
	case "zip":
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("open release archive: %w", err)
		}
		for _, file := range reader.File {
			if path.Clean(file.Name) != binaryName || !file.Mode().IsRegular() {
				continue
			}
			opened, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s from release archive: %w", binaryName, err)
			}
			binary, readErr := readBinary(opened, int64(file.UncompressedSize64))
			closeErr := opened.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, fmt.Errorf("close %s from release archive: %w", binaryName, closeErr)
			}
			return binary, nil
		}
	default:
		return nil, fmt.Errorf("unsupported release archive format: %s", archiveFormat)
	}
	return nil, fmt.Errorf("release archive did not contain %s", binaryName)
}

func readBinary(reader io.Reader, size int64) ([]byte, error) {
	if size < 0 || size > maxBinarySize {
		return nil, fmt.Errorf("release binary exceeded the %d byte limit", maxBinarySize)
	}
	binary, err := io.ReadAll(io.LimitReader(reader, maxBinarySize+1))
	if err != nil {
		return nil, fmt.Errorf("read release binary: %w", err)
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("release binary was empty")
	}
	if len(binary) > maxBinarySize {
		return nil, fmt.Errorf("release binary exceeded the %d byte limit", maxBinarySize)
	}
	return binary, nil
}
