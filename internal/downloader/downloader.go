package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const downloadFileURL = "https://mast.stsci.edu/api/v0.1/Download/file"

var progressPrintMutex sync.Mutex
var lastProgressLineLen int

func DownloadByDataURI(client *http.Client, dataURI, savePath, label string) error {
	u, err := url.Parse(downloadFileURL)
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("uri", dataURI)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-object-rgb-cli/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download status %s: %s", resp.Status, string(body))
	}

	f, err := os.Create(savePath)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	startedAt := time.Now()

	buf := make([]byte, 1024*1024)
	var downloaded int64
	var lastPrinted time.Time
	var lastPercentBucket int64 = -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return writeErr
			}

			downloaded += int64(n)

			shouldPrint := false
			if total > 0 {
				percentBucket := (downloaded * 100) / total
				if percentBucket != lastPercentBucket {
					lastPercentBucket = percentBucket
					shouldPrint = true
				}
			}

			if time.Since(lastPrinted) >= 700*time.Millisecond {
				shouldPrint = true
			}

			if shouldPrint {
				printDownloadProgress(label, downloaded, total, startedAt)
				lastPrinted = time.Now()
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	finishDownloadProgress(label, downloaded, total, startedAt)
	return nil
}

func formatBytes(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case n >= GB:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func printDownloadProgress(label string, downloaded, total int64, startedAt time.Time) {
	progressPrintMutex.Lock()
	defer progressPrintMutex.Unlock()

	elapsed := time.Since(startedAt).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	speed := int64(float64(downloaded) / elapsed)

	var line string
	if total > 0 {
		percent := float64(downloaded) * 100.0 / float64(total)
		line = fmt.Sprintf(
			"Downloading %-20s %6.2f%%  %s / %s  (%s/s)",
			label,
			percent,
			formatBytes(downloaded),
			formatBytes(total),
			formatBytes(speed),
		)
	} else {
		line = fmt.Sprintf(
			"Downloading %-20s %s  (%s/s)",
			label,
			formatBytes(downloaded),
			formatBytes(speed),
		)
	}

	padding := ""
	if len(line) < lastProgressLineLen {
		padding = strings.Repeat(" ", lastProgressLineLen-len(line))
	}

	fmt.Printf("\r%s%s", line, padding)
	lastProgressLineLen = len(line)
}

func finishDownloadProgress(label string, downloaded, total int64, startedAt time.Time) {
	printDownloadProgress(label, downloaded, total, startedAt)

	progressPrintMutex.Lock()
	fmt.Print("\n")
	lastProgressLineLen = 0
	progressPrintMutex.Unlock()
}
