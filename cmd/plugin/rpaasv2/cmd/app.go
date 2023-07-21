// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	tsuruclient "github.com/tsuru/go-tsuruclient/pkg/client"
	"github.com/urfave/cli/v2"

	rpaasclient "github.com/tsuru/rpaas-operator/pkg/rpaas/client"
	"github.com/tsuru/rpaas-operator/pkg/rpaas/client/autogenerated"
	"github.com/tsuru/rpaas-operator/version"
)

func NewDefaultApp() *cli.App {
	return NewApp(os.Stdout, os.Stderr, nil)
}

func NewApp(o, e io.Writer, client rpaasclient.Client) (app *cli.App) {
	app = cli.NewApp()
	app.Usage = "Manipulates reverse proxy instances running on Reverse Proxy as a Service."
	app.Version = version.Version
	app.ErrWriter = e
	app.Writer = o
	app.Commands = []*cli.Command{
		NewCmdScale(),
		NewCmdAccessControlList(),
		NewCmdCertificates(),
		NewCmdBlocks(),
		NewCmdRoutes(),
		NewCmdInfo(),
		NewCmdAutoscale(),
		NewCmdExec(),
		NewCmdShell(),
		NewCmdLogs(),
		NewCmdExtraFiles(),
	}
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "rpaas-url",
			Usage: "URL to RPaaS server",
		},
		&cli.StringFlag{
			Name:  "rpaas-user",
			Usage: "user name to authenticate on RPaaS server directly",
		},
		&cli.StringFlag{
			Name:  "rpaas-password",
			Usage: "password of user to authenticate on RPaaS server directly",
		},
		&cli.StringFlag{
			Name:    "tsuru-target",
			Usage:   "address of Tsuru server",
			EnvVars: []string{"TSURU_TARGET"},
		},
		&cli.StringFlag{
			Name:        "tsuru-token",
			Usage:       "authentication credential to Tsuru server",
			EnvVars:     []string{"TSURU_TOKEN"},
			DefaultText: "-",
		},
		&cli.DurationFlag{
			Name:  "timeout",
			Usage: "time limit that a remote operation (HTTP request) can take",
			Value: 60 * time.Second,
		},
		&cli.BoolFlag{
			Name:  "insecure",
			Usage: "whether should allow to perform requests under insecure connection",
		},
	}
	app.Before = func(c *cli.Context) error {
		setClient(c, client)
		return nil
	}
	return
}

type contextKey string

const rpaasClientKey = contextKey("rpaas.client")

var errClientNotFoundAtContext = fmt.Errorf("rpaas client not found at context")

func setClient(c *cli.Context, client rpaasclient.Client) {
	c.Context = context.WithValue(c.Context, rpaasClientKey, client)
}

func getClient(c *cli.Context) (rpaasclient.Client, error) {
	client, ok := c.Context.Value(rpaasClientKey).(rpaasclient.Client)
	if !ok {
		return nil, errClientNotFoundAtContext
	}

	return client, nil
}

func setupClient(c *cli.Context) error {
	client, err := getClient(c)
	if err != nil && err != errClientNotFoundAtContext {
		return err
	}

	if client != nil {
		return nil
	}

	client, err = newClient(c)
	if err != nil {
		return err
	}

	setClient(c, client)
	return nil
}

func newClient(c *cli.Context) (rpaasclient.Client, error) {
	opts := rpaasclient.ClientOptions{Timeout: c.Duration("timeout")}
	if rpaasURL := c.String("rpaas-url"); rpaasURL != "" {
		return rpaasclient.NewClientWithOptions(rpaasURL, c.String("rpaas-user"), c.String("rpaas-password"), opts)
	}

	return rpaasclient.NewClientThroughTsuruWithOptions(c.String("tsuru-target"), c.String("tsuru-token"), c.String("tsuru-service"), opts)
}

func NewAutogeneratedClient(c *cli.Context) *autogenerated.APIClient {

	cfg := &autogenerated.Configuration{
		UserAgent: fmt.Sprintf("rpaasv2-cli/%s", c.App.Version),
		HTTPClient: &http.Client{
			Timeout: c.Duration("timeout"),
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: c.Bool("insecure"),
				},
			},
		},
	}

	if rpaasBaseURL := c.String("rpaas-url"); rpaasBaseURL != "" {
		cfg.Servers = autogenerated.ServerConfigurations{
			autogenerated.ServerConfiguration{URL: rpaasBaseURL},
		}

		if u, p := c.String("rpaas-user"), c.String("rpaas-password"); u != "" && p != "" {
			c.Context = context.WithValue(c.Context, autogenerated.ContextBasicAuth, autogenerated.BasicAuth{UserName: u, Password: p})
		}

		return autogenerated.NewAPIClient(cfg)
	}

	cfg.Servers = autogenerated.ServerConfigurations{
		autogenerated.ServerConfiguration{URL: c.String("tsuru-target")},
	}

	cfg.HTTPClient.Transport = &tsuruclient.TsuruProxyTransport{
		Target:   c.String("tsuru-target"),
		Token:    c.String("tsuru-token"),
		Service:  c.String("service"),
		Instance: c.String("instance"),
		Base:     cfg.HTTPClient.Transport,
	}

	return autogenerated.NewAPIClient(cfg)
}
