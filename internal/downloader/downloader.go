package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const mastDownloadFileEndpoint = "https://mast.stsci.edu/api/v0.1/Download/file"

func DownloadByDataURI(client *http.Client, dataURI, savePath, label string) error {
	if client == nil {
		client = &http.Client{}
	}

	dataURI = strings.TrimSpace(dataURI)
	if dataURI == "" {
		return fmt.Errorf("empty dataURI")
	}

	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for download target: %w", err)
	}

	if strings.HasPrefix(dataURI, "http://") || strings.HasPrefix(dataURI, "https://") {
		return downloadViaGET(client, dataURI, savePath)
	}

	if strings.HasPrefix(dataURI, "mast:") {
		return downloadViaMASTGet(client, dataURI, savePath)
	}

	return fmt.Errorf("unsupported dataURI format: %q", dataURI)
}

func downloadViaGET(client *http.Client, rawURL, savePath string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build GET download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("perform GET download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("download status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return writeResponseBodyToFile(resp.Body, savePath)
}

func downloadViaMASTGet(client *http.Client, dataURI, savePath string) error {
	q := url.Values{}
	q.Set("uri", dataURI)

	downloadURL := mastDownloadFileEndpoint + "?" + q.Encode()

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("build MAST GET download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("perform MAST GET download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("download status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return writeResponseBodyToFile(resp.Body, savePath)
}

func writeResponseBodyToFile(body io.Reader, savePath string) error {
	tmpPath := savePath + ".part"

	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp output file: %w", err)
	}

	_, copyErr := io.Copy(out, body)
	closeErr := out.Close()

	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write download body: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp output file: %w", closeErr)
	}

	if err := os.Rename(tmpPath, savePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
