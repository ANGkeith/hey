// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	gourl "net/url"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/rakyll/boom/boomer"
)

var (
	method      = flag.String("m", "GET", "")
	headers     = flag.String("h", "", "")
	body        = flag.String("d", "", "")
	accept      = flag.String("A", "", "")
	contentType = flag.String("T", "text/html", "")
	authHeader  = flag.String("a", "", "")

	output = flag.String("o", "", "")

	c    = flag.Int("c", 50, "")
	n    = flag.Int("n", 200, "")
	q    = flag.Int("q", 0, "")
	t    = flag.Int("t", 0, "")
	cpus = flag.Int("cpus", runtime.GOMAXPROCS(-1), "")

	insecure           = flag.Bool("allow-insecure", false, "")
	disableCompression = flag.Bool("disable-compression", false, "")
	disableKeepAlives  = flag.Bool("disable-keepalive", false, "")
	proxyAddr          = flag.String("x", "", "")
)

var usage = `Usage: boom [options...] <url>

Options:
  -n  Number of requests to run.
  -c  Number of requests to run concurrently. Total number of requests cannot
      be smaller than the concurency level.
  -q  Rate limit, in seconds (QPS).
  -o  Output type. If none provided, a summary is printed.
      "csv" is the only supported alternative. Dumps the response
      metrics in comma-seperated values format.

  -m  HTTP method, one of GET, POST, PUT, DELETE, HEAD, OPTIONS.
  -h  Custom HTTP headers, name1:value1;name2:value2.
  -A  HTTP Accept header.
  -d  HTTP request body.
  -T  Content-type, defaults to "text/html".
  -a  Basic authentication, username:password.
  -x  HTTP Proxy address as host:port.

  -allow-insecure       Allow bad/expired TLS/SSL certificates.
  -disable-compression  Disable compression.
  -disable-keepalive    Disable keep-alive, prevents re-use of TCP
                        connections between different HTTP requests.
  -cpus                 Number of used cpu cores.
                        (default for current machine is %d cores)
`

var defaultDNSResolver dnsResolver = &netDNSResolver{}

// DNS resolver interface.
type dnsResolver interface {
	Lookup(domain string) (addr []string, err error)
}

// A DNS resolver based on net.LookupHost.
type netDNSResolver struct{}

// Looks up for the resolved IP addresses of
// the provided domain.
func (*netDNSResolver) Lookup(domain string) (addr []string, err error) {
	return net.LookupHost(domain)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, usage, runtime.GOMAXPROCS(-1))
	}

	flag.Parse()
	if flag.NArg() < 1 {
		usageAndExit("")
	}

	num := *n
	conc := *c
	qps := *q
	timout := *t

	if num <= 0 || conc <= 0 {
		usageAndExit("n and c cannot be smaller than 1.")
	}

	if cpus != nil {
		runtime.GOMAXPROCS(*cpus)
	}

	var (
		url, m, originalHost string
		// Username and password for basic auth
		username, password string
		// request headers
		header http.Header = make(http.Header)
	)

	m = strings.ToUpper(*method)
	url, originalHost = resolveUrl(flag.Args()[0])

	// set content-type
	header.Set("Content-Type", *contentType)
	// set any other additional headers
	if *headers != "" {
		headers := strings.Split(*headers, ";")
		for _, h := range headers {
			re := regexp.MustCompile("([\\w|-]+):(.+)")
			matches := re.FindAllStringSubmatch(h, -1)
			if len(matches) < 1 {
				usageAndExit("")
			}
			header.Set(matches[0][1], matches[0][2])
		}
	}

	if *accept != "" {
		header.Set("Accept", *accept)
	}

	// set basic auth if set
	if *authHeader != "" {
		re := regexp.MustCompile("([\\w|\\-|_|\\.]+):(\\w+)")
		matches := re.FindAllStringSubmatch(*authHeader, -1)
		if len(matches) < 1 {
			usageAndExit("")
		}
		username = matches[0][1]
		password = matches[0][2]
	}

	if *output != "csv" && *output != "" {
		usageAndExit("Invalid output type.")
	}

	(&boomer.Boomer{
		Req: &boomer.ReqOpts{
			Method:       m,
			Url:          url,
			Body:         *body,
			Header:       header,
			Username:     username,
			Password:     password,
			OriginalHost: originalHost,
		},
		N:                  num,
		C:                  conc,
		Qps:                qps,
		Timeout:            timout,
		AllowInsecure:      *insecure,
		DisableCompression: *disableCompression,
		DisableKeepAlives:  *disableKeepAlives,
		Output:             *output,
		ProxyAddr:          *proxyAddr}).Run()
}

// Replaces host with an IP and returns the provided
// string URL as a *url.URL.
//
// DNS lookups are not cached in the package level in Go,
// and it's a huge overhead to resolve a host
// before each request in our case. Instead we resolve
// the domain and replace it with the resolved IP to avoid
// lookups during request time. Supported url strings:
//
// <schema>://google.com[:port]
// <schema>://173.194.116.73[:port]
// <schema>://\[2a00:1450:400a:806::1007\][:port]
func resolveUrl(url string) (string, string) {
	uri, err := gourl.ParseRequestURI(url)
	if err != nil {
		usageAndExit(err.Error())
	}
	originalHost := uri.Host

	serverName, port, err := net.SplitHostPort(uri.Host)
	if err != nil {
		serverName = uri.Host
	}

	addrs, err := defaultDNSResolver.Lookup(serverName)
	if err != nil {
		usageAndExit(err.Error())
	}
	ip := addrs[0]
	if port != "" {
		// join automatically puts square brackets around the
		// ipv6 IPs.
		uri.Host = net.JoinHostPort(ip, port)
	} else {
		uri.Host = ip
		// square brackets are required for ipv6 IPs.
		// otherwise, net.Dial fails with a parsing error.
		if strings.Contains(ip, ":") {
			uri.Host = fmt.Sprintf("[%s]", ip)
		}
	}
	return uri.String(), originalHost
}

func usageAndExit(message string) {
	if message != "" {
		fmt.Fprintf(os.Stderr, message)
		fmt.Fprintf(os.Stderr, "\n\n")
	}
	flag.Usage()
	fmt.Fprintf(os.Stderr, "\n")
	os.Exit(1)
}