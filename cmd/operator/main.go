// Copyright 2016 The prometheus-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/coreos/prometheus-operator/pkg/alertmanager"
	"github.com/coreos/prometheus-operator/pkg/analytics"
	"github.com/coreos/prometheus-operator/pkg/api"
	"github.com/coreos/prometheus-operator/pkg/prometheus"
	"github.com/go-kit/kit/log"
)

var (
	cfg              prometheus.Config
	analyticsEnabled bool
)

func init() {
	flagset := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	flagset.StringVar(&cfg.Host, "apiserver", "", "API Server addr, e.g. ' - NOT RECOMMENDED FOR PRODUCTION - http://127.0.0.1:8080'. Omit parameter to run in on-cluster mode and utilize the service account token.")
	flagset.StringVar(&cfg.TLSConfig.CertFile, "cert-file", "", " - NOT RECOMMENDED FOR PRODUCTION - Path to public TLS certificate file.")
	flagset.StringVar(&cfg.TLSConfig.KeyFile, "key-file", "", "- NOT RECOMMENDED FOR PRODUCTION - Path to private TLS certificate file.")
	flagset.StringVar(&cfg.TLSConfig.CAFile, "ca-file", "", "- NOT RECOMMENDED FOR PRODUCTION - Path to TLS CA file.")
	flagset.StringVar(&cfg.KubeletObject, "kubelet-service", "", "Service/Endpoints object to write kubelets into in format \"namespace/name\"")
	flagset.BoolVar(&cfg.TLSInsecure, "tls-insecure", false, "- NOT RECOMMENDED FOR PRODUCTION - Don't verify API server's CA certificate.")
	flagset.BoolVar(&analyticsEnabled, "analytics", true, "Send analytical event (Cluster Created/Deleted etc.) to Google Analytics")
	flagset.StringVar(&cfg.PrometheusConfigReloader, "prometheus-config-reloader", "quay.io/coreos/prometheus-config-reloader:v0.0.1", "Config and rule reload image")
	flagset.StringVar(&cfg.ConfigReloaderImage, "config-reloader-image", "quay.io/coreos/configmap-reload:v0.0.1", "Reload Image")

	flagset.Parse(os.Args[1:])
}

func Main() int {
	logger := log.NewContext(log.NewLogfmtLogger(os.Stdout)).
		With("ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	if analyticsEnabled {
		analytics.Enable()
	}

	po, err := prometheus.New(cfg, logger.With("component", "prometheusoperator"))
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return 1
	}

	ao, err := alertmanager.New(cfg, logger.With("component", "alertmanageroperator"))
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return 1
	}

	web, err := api.New(cfg, logger.With("component", "api"))
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return 1
	}

	web.Register(http.DefaultServeMux)
	l, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error { return po.Run(ctx.Done()) })
	wg.Go(func() error { return ao.Run(ctx.Done()) })

	go http.Serve(l, nil)

	term := make(chan os.Signal)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)

	select {
	case <-term:
		logger.Log("msg", "Received SIGTERM, exiting gracefully...")
	case <-ctx.Done():
	}

	cancel()
	if err := wg.Wait(); err != nil {
		logger.Log("msg", "Unhandled error received. Exiting...", "err", err)
		return 1
	}

	return 0
}

func main() {
	os.Exit(Main())
}
