//go:build !embedweb

package server

import "net/http"

// webHandler is nil in normal builds: nginx serves web/dist. Build with -tags embedweb to bundle it.
func webHandler() http.Handler { return nil }
