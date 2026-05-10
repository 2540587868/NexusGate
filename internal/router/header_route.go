package router

type HeaderRoute struct {
	headerName string
}

func NewHeaderRoute() *HeaderRoute {
	return &HeaderRoute{
		headerName: "X-Service-Version",
	}
}

func (hr *HeaderRoute) WithHeader(name string) *HeaderRoute {
	hr.headerName = name
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

	return backends[0], nil
}

func (hr *HeaderRoute) Name() StrategyType {
	return StrategyHeaderRoute
}

func (hr *HeaderRoute) HeaderName() string {
	return hr.headerName
}
