package middleware

import "github.com/nexusgate/nexusgate/internal/gateway"

type Chain struct {
	middlewares []gateway.Middleware
}

func NewChain(middlewares ...gateway.Middleware) *Chain {
	return &Chain{
		middlewares: append([]gateway.Middleware{}, middlewares...),
	}
}

func (c *Chain) Use(mw gateway.Middleware) *Chain {
	newChain := &Chain{
		middlewares: make([]gateway.Middleware, len(c.middlewares)+1),
	}
	copy(newChain.middlewares, c.middlewares)
	newChain.middlewares[len(c.middlewares)] = mw
	return newChain
}

func (c *Chain) Then(handler gateway.Handler) gateway.Handler {
	if len(c.middlewares) == 0 {
		return handler
	}

	result := handler
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		result = c.middlewares[i](result)
	}
	return result
}

func (c *Chain) Middlewares() []gateway.Middleware {
	result := make([]gateway.Middleware, len(c.middlewares))
	copy(result, c.middlewares)
	return result
}
