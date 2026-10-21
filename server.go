// Package app composes the generated Go handlers with SvelteKit pages.
package app

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/tylergannon/skgo"
	"github.com/tylergannon/zoom-connect/internal/httpapi"
	generated "github.com/tylergannon/zoom-connect/internal/skgo"
)

// NewHandler builds the same Go-owned application stack in development and
// production. VitePlus supplies modules and assets in development; Go still
// owns the socket, endpoints, remote functions, loads, and page rendering.
func NewHandler(dist fs.FS, proxy, origin string) (http.Handler, string, error) {
	if proxy != "" {
		return nil, "", fmt.Errorf("the proxy development mode was removed; use `go tool skgo dev`")
	}
	handler, err := composeHandler(dist, origin, nil)
	if err != nil {
		return nil, "", err
	}
	return handler, "production", nil
}

// NewDevelopmentHandler is the entry point used by `go tool skgo dev`. It
// shares the production dispatch stack and lets Kit own development rendering.
func NewDevelopmentHandler(dist fs.FS, origin string, development *skgo.NativeDevelopment) (http.Handler, error) {
	return composeHandler(dist, origin, development)
}

func composeHandler(dist fs.FS, origin string, development *skgo.NativeDevelopment) (http.Handler, error) {
	manifest, err := skgo.ReadManifest(dist)
	if err != nil {
		return nil, err
	}

	remoteConfig := manifest.RemoteConfig(origin)
	loadConfig := manifest.LoadConfig(origin)
	endpointConfig := manifest.EndpointConfig(origin)
	loadConfig.Matchers = generated.Matchers()
	endpointConfig.Matchers = generated.Matchers()
	handleConfig := manifest.HandleConfig()
	handleConfig.Origin = origin
	handleConfig.Matchers = generated.Matchers()
	var bound http.Handler
	internal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { bound.ServeHTTP(w, r) })
	var endpoints *skgo.Endpoints
	actions, err := skgo.NewActions(generated.Actions()...)
	if err != nil {
		return nil, err
	}
	var pages func(*skgo.Loads, *skgo.Remotes) (http.Handler, error)
	pages = func(loads *skgo.Loads, remotes *skgo.Remotes) (http.Handler, error) {
		renderer, err := development.NewSSR(dist, manifest, loads, remotes, skgo.SSROptions{
			Fetch:   internal,
			Actions: actions,
		})
		if err != nil {
			return nil, err
		}
		endpoints.SetErrorTemplate(renderer.ErrorTemplate())
		handleConfig.ErrorTemplate = renderer.ErrorTemplate()
		return development.NewStaticHandler(dist, skgo.WithSSR(renderer))
	}

	remotes, err := skgo.NewRemotes(remoteConfig, generated.Remotes()...)
	if err != nil {
		return nil, err
	}
	loads, err := skgo.NewLoads(loadConfig, generated.Loads()...)
	if err != nil {
		return nil, err
	}
	endpoints, err = skgo.NewEndpoints(endpointConfig, generated.Endpoints()...)
	if err != nil {
		return nil, err
	}
	pageHandler, err := pages(loads, remotes)
	if err != nil {
		return nil, err
	}
	handleConfig.Loads = loads
	handleConfig.Static = skgo.ServedAsFile(pageHandler)
	bound = generated.RequestBoundary(handleConfig, loads.Intercept(remotes.Intercept(endpoints.Intercept(pageHandler))))
	api, err := httpapi.New(skgo.FetchConfig{Origin: origin, Base: manifest.Base, Handler: internal, Prerendered: manifest.Prerendered}.Intercept(bound))
	if err != nil {
		return nil, err
	}
	return api, nil
}
