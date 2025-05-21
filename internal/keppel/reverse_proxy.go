// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"io"
	"net/http"
	"net/url"

	"github.com/sapcc/go-bits/logg"
)

// When reverse-proxying, these headers from the client request will be
// forwarded. All other client headers will be discarded.
var reverseProxyHeaders = []string{
	"Accept",
	"Authorization",
}

// ReverseProxyAnycastRequestToPeer takes a http.Request for the anycast API and
// reverse-proxies it to a different keppel-api in this Keppel's peer group.
//
// If an error is returned, no response has been written and the caller is
// responsible for producing the error response.
func (cfg Configuration) ReverseProxyAnycastRequestToPeer(w http.ResponseWriter, r *http.Request, peerHostName string) error {
	// build request URL
	reqURL := url.URL{
		Scheme: "https",
		Host:   peerHostName,
		Path:   r.URL.Path,
	}

	// make the forwarding visible in the other Keppel's log file
	query := r.URL.Query()
	query.Set("forwarded-by", cfg.APIPublicHostname)
	reqURL.RawQuery = query.Encode()

	// when sending proxy request, do not follow redirects (we want to pass on 3xx
	// redirects to the user verbatim)
	client := *http.DefaultClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// send proxy request
	req, err := http.NewRequestWithContext(r.Context(), r.Method, reqURL.String(), http.NoBody)
	if err != nil {
		return err
	}
	for _, headerName := range reverseProxyHeaders {
		req.Header[headerName] = r.Header[headerName]
	}
	req.Header.Set("X-Keppel-Forwarded-By", cfg.APIPublicHostname)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	// forward response to caller
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)

	// forward response body to caller, if any
	if resp.Body != nil {
		_, err := io.Copy(w, resp.Body)
		if err == nil {
			err = resp.Body.Close()
		} else {
			resp.Body.Close()
		}
		if err != nil {
			logg.Error("while forwarding reverse-proxy response to caller: " + err.Error())
		}
	}

	return nil
}
