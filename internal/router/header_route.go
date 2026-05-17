package router

import (
	"fmt"
)

type HeaderRoute struct {
	headerName string
	fallback   bool
}

func NewHeaderRoute(headerName string) *HeaderRoute {
	if headerName == "" {
		headerName = "X-Service-Version"
	}
	return &HeaderRoute{
		headerName: headerName,
		fallback:   true,
	}
}

func (hr *HeaderRoute) WithHeader(name string) *HeaderRoute {
	hr.headerName = name
	return hr
}

func (hr *HeaderRoute) WithFallback(enabled bool) *HeaderRoute {
	hr.fallback = enabled
	return hr
}

func (hr *HeaderRoute) Select(key string, backends []*Backend) (*Backend, error) {
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	if len(backends) == 1 {
		return backends[0], nil
	}

	for _, b := range backends {
		if b.Meta != nil {
			if version, ok := b.Meta["version"]; ok {
				if version == key {
					return b, nil
				}
			}
		}
	}

	if hr.fallback {
		return backends[0], nil
	}

	return nil, fmt.Errorf("no backend matches header value %q for %s", key, hr.headerName)
}

func (hr *HeaderRoute) Name() StrategyType {
	return StrategyHeaderRoute
}

func (hr *HeaderRoute) HeaderName() string {
	return hr.headerName
}
