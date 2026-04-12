package clients

import (
	"net/http"
	"time"
)

type Set struct {
	Meta     *http.Client
	Download *http.Client
}

func New() Set {
	return Set{
		Meta:     &http.Client{Timeout: 180 * time.Second},
		Download: &http.Client{},
	}
}
