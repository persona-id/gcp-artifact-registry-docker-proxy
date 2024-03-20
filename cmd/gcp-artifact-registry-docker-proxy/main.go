package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"cloud.google.com/go/compute/metadata"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	auth "golang.org/x/oauth2/google"
)

type Config struct {
	Listen       string `mapstructure:"listen"`
	OnlyMetadata bool   `mapstructure:"only-metadata"`
	Registry     string `mapstructure:"registry"`
}

func main() {
	config, err := parseConfiguration()
	if err != nil {
		slog.Error("Error reading configuration", slog.Any("err", err))
		os.Exit(1)
	}

	remote, err := url.Parse(config.Registry)
	if err != nil {
		slog.Error("Unable to parse registry address", slog.Any("err", err))
		os.Exit(1)
	}

	if !remote.IsAbs() {
		slog.Error("Expected absolute registry URL", slog.Any("registry", config.Registry))
		os.Exit(1)
	}

	// Figure out the prefix we'll need to transform in the URL which is generally project/repository.
	proxyPath := strings.Join([]string{remote.EscapedPath(), ""}, "/")
	remote.Path = ""

	proxy := httputil.NewSingleHostReverseProxy(remote)

	// Configure our GCP authentication.
	gcpScopes := []string{"https://www.googleapis.com/auth/cloud-platform"}

	var gcpCredentials *auth.Credentials

	if config.OnlyMetadata {
		if !metadata.OnGCE() {
			slog.Error("Not running on GCE instance to use metadata server")
			os.Exit(1)
		}

		id, _ := metadata.ProjectID()

		gcpCredentials = &auth.Credentials{
			ProjectID:   id,
			TokenSource: auth.ComputeTokenSource("", gcpScopes...),
		}
	} else {
		gcpCredentials, err = auth.FindDefaultCredentials(context.Background(), gcpScopes...)
		if err != nil {
			slog.Error("Unable to setup GCP credentials", slog.Any("err", err))
			os.Exit(1)
		}
	}

	if _, err = gcpCredentials.TokenSource.Token(); err != nil {
		slog.Error("Unable to fetch initial GCP token", slog.Any("err", err))
		os.Exit(1)
	}

	http.HandleFunc(proxyPath+"v2/", func(w http.ResponseWriter, r *http.Request) {
		token, err := gcpCredentials.TokenSource.Token()
		if err != nil {
			slog.Error("Unable to fetch GCP token", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		r.Host = remote.Host
		r.URL.Path = "/v2" + proxyPath + strings.TrimPrefix(r.URL.Path, proxyPath+"v2/")
		token.SetAuthHeader(r)

		proxy.ServeHTTP(w, r)
	})

	// GCP can use any number of paths to support the registry so just directly proxy
	// anything that didn't match above here without adding authentication.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		r.Host = remote.Host

		proxy.ServeHTTP(w, r)
	})

	slog.Info("Starting server...", slog.Any("addr", config.Listen))

	err = http.ListenAndServe(config.Listen, nil)
	if err != nil {
		slog.Error("Unable to start HTTP server", slog.Any("err", err))
		os.Exit(1)
	}
}

func parseConfiguration() (*Config, error) {
	replacer := strings.NewReplacer(".", "_")
	viper.GetViper().SetEnvKeyReplacer(replacer)
	viper.GetViper().SetEnvPrefix("PROXY")
	viper.GetViper().AutomaticEnv()

	viper.GetViper().SetDefault("listen", "localhost:8000")

	pflag.String("listen", "", "Address for the mirror to listen on.")
	pflag.Bool("only-metadata", false, "Only rely upon the GCE metadata server for authentication.")
	pflag.String("registry", "", "URL of the registry to proxy requests to.")

	pflag.Parse()

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("unable to bing pflags to viper: %w", err)
	}

	settings := &Config{}

	err := viper.Unmarshal(settings)
	if err != nil {
		return nil, fmt.Errorf("unable to parse settings: %w", err)
	}

	if viper.GetString("registry") == "" {
		return nil, errors.New("registry must be set")
	}

	return settings, nil
}
