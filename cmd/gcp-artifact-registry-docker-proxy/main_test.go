package main

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

const proxyListen = "127.0.0.1:8000"

type logSink struct{}

func (ls logSink) Write(b []byte) (int, error) {
	log.Printf("[MIRROR] %s", b)
	return len(b), nil
}

type requestDetails struct {
	Headers map[string][]string `json:"headers"`
	URL     string              `json:"url"`
}

func TestProxy(t *testing.T) {
	if os.Getenv("RUN_PROXY") == "1" {
		main()
		return
	}

	if gcpCredentials := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_CONTENTS"); gcpCredentials != "" {
		gcpSaKey, err := os.CreateTemp("", "gcp.*.json")
		if err != nil {
			t.Fatalf("Unable to create temporary file for GCP SA key: %+v", err)
		}
		defer os.Remove(gcpSaKey.Name())

		if _, err := gcpSaKey.WriteString(gcpCredentials); err != nil {
			t.Fatalf("Unable to write GCP SA key to temporary file: %+v", err)
		}

		if err := os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", gcpSaKey.Name()); err != nil {
			t.Fatalf("Unable to set GCP credentials env key: %+v", err)
		}

		defer func() {
			os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS")
		}()
	}

	// Setup the fake backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(requestDetails{ // nolint:errcheck
			Headers: r.Header,
			URL:     r.URL.String(),
		})
	}))
	defer backend.Close()

	cmd := exec.Command(os.Args[0], "-test.run=TestProxy", "-test.v")
	cmd.Env = append(
		os.Environ(),
		"PROXY_LISTEN="+proxyListen,
		"PROXY_REGISTRY="+backend.URL+"/example-project/example-repo",
		"RUN_PROXY=1",
	)

	cmd.Stdout = logSink{}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		t.Fatalf("Unable to run command: %+v", err)
	}

	if err := waitForMirrorBoot(); err != nil {
		cmdStatusErr := syscall.Kill(cmd.Process.Pid, syscall.Signal(0))

		if cmdStatusErr == nil {
			cmd.Process.Kill() // nolint:errcheck
		}

		cmd.Wait() // nolint:errcheck

		t.Fatal(err)
	}

	tests := []struct {
		CallerURL        string
		HasAuthorization bool
		MirrorURL        string
		StatusCode       int
	}{
		{
			CallerURL:        "/example-project/example-repo/v2/library/hello-world/manifests/latest",
			HasAuthorization: true,
			MirrorURL:        "/v2/example-project/example-repo/library/hello-world/manifests/latest",
		},
		{
			CallerURL:  "/example-project/example-repo/v2library/hello-world/manifests/latest",
			StatusCode: 404,
		},
		{
			CallerURL:  "/example-project/example-repo/abc",
			StatusCode: 404,
		},
		{
			CallerURL:  "/v2/example-project/example-repo/library/hello-world/manifests/latest",
			StatusCode: 404,
		},
	}

	for _, test := range tests {
		t.Run("path:"+test.CallerURL, func(t *testing.T) {
			resp, err := http.Get("http://" + proxyListen + test.CallerURL)
			if err != nil {
				t.Fatalf("Unable to call mirror: %+v", err)
			}

			if test.StatusCode > 0 {
				if resp.StatusCode != test.StatusCode {
					t.Fatalf("Got wrong status calling mirror, expected %d but was %d", test.StatusCode, resp.StatusCode)
				}

				return
			} else if resp.StatusCode != http.StatusOK {
				t.Fatalf("Got non-200 calling mirror: %d", resp.StatusCode)
			}

			rd := requestDetails{}

			if err := json.NewDecoder(resp.Body).Decode(&rd); err != nil {
				t.Fatalf("Unable to parse response body: %+v", err)
			}

			resp.Body.Close()

			if rd.URL != test.MirrorURL {
				t.Fatalf("Incorrect proxied URL, expected %q but was %q", test.MirrorURL, rd.URL)
			}

			if test.HasAuthorization {
				if values, ok := rd.Headers["Authorization"]; ok {
					if len(values) == 1 {
						if !strings.HasPrefix(values[0], "Bearer ya29.") {
							t.Fatalf("Invalid Authorization on response: %q", values[0])
						}
					} else {
						t.Fatalf("Expected 1 Authorization header on response, got %d", len(values))
					}
				} else {
					t.Fatal("Expected Authorization on response")
				}
			} else {
				if _, ok := rd.Headers["Authorization"]; ok {
					t.Fatal("Didn't expect Authorization on response")
				}
			}
		})
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("failed to kill process: %+v", err)
	}

	cmd.Wait() // nolint:errcheck
}

func waitForMirrorBoot() error {
	startTimeout := time.After(5 * time.Second)

	for {
		select {
		case <-startTimeout:
			return errors.New("Timed out waiting for proxy to boot")
		default:
			_, err := net.DialTimeout("tcp", proxyListen, 1*time.Second)
			if err == nil {
				log.Printf("Proxy process booted")
				time.Sleep(1 * time.Second)

				return nil
			}
		}
	}
}
