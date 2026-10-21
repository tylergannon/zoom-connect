// Command zoom-connect serves the SvelteKit application from Go.
package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"
	"net/url"

	app "github.com/tylergannon/zoom-connect"
	"github.com/tylergannon/zoom-connect/web"
)

var origin = "http://127.0.0.1:8080"

func main() {
	listen := flag.String("listen", hostPort(origin), "address to listen on")
	trusted := flag.String("origin", origin, "public browser origin")
	proxy := flag.String("proxy", "", "URL of a running VitePlus development server")
	flag.Parse()

	dist, err := fs.Sub(web.Build, "build")
	if err != nil {
		log.Fatal(err)
	}
	handler, mode, err := app.NewHandler(dist, *proxy, *trusted)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("zoom-connect: listening on http://%s (%s)", *listen, mode)
	if err := http.ListenAndServe(*listen, handler); err != nil {
		log.Fatal(err)
	}
}

func hostPort(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return "127.0.0.1:8080"
	}
	return u.Host
}
