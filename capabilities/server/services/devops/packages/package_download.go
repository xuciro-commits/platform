package packages

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	runtimeprotocol "platformserver/kernel/protocol"
)

const maxRedirects = 5

var ErrUnreachable = errors.New("unreachable")

func Download(ctx context.Context, address *url.URL, allowPrivate bool, send func(*http.Request, bool) (*http.Response, error)) (*http.Response, error) {
	initialLoopback := allowPrivate
	for hop := 0; ; hop++ {
		reqCtx := ctx
		if allowPrivate {
			reqCtx = runtimeprotocol.WithLoopbackPackage(ctx)
		}
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, address.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		response, err := send(req, allowPrivate)
		if err != nil {
			return nil, ErrUnreachable
		}
		switch response.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		default:
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		if hop == maxRedirects || location == "" {
			return nil, fmt.Errorf("The address redirected too many times")
		}
		next, err := address.Parse(location)
		if err != nil {
			return nil, fmt.Errorf("The address redirected to an invalid address")
		}
		nextAddr, nextLoopback, err := ValidatePackageURL(next.String())
		if err != nil || (nextLoopback && !initialLoopback) {
			return nil, fmt.Errorf("The address redirected to an address that is not HTTPS")
		}
		address, allowPrivate = nextAddr, nextLoopback
	}
}
