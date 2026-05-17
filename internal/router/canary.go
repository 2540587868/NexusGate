package router

import (
	"fmt"
	"hash/fnv"
	"strings"
)

type CanaryStrategy struct {
	rule        CanaryRule
	headerName  string
	cookieName  string
}

type CanaryRule struct {
	Header   *CanaryHeaderRule   `yaml:"header,omitempty"`
	Cookie   *CanaryCookieRule   `yaml:"cookie,omitempty"`
	Weight   *CanaryWeightRule   `yaml:"weight,omitempty"`
}

type CanaryHeaderRule struct {
	Name   string `yaml:"name"`
	Value  string `yaml:"value"`
}

type CanaryCookieRule struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type CanaryWeightRule struct {
	CanaryWeight int `yaml:"canary_weight"`
	TotalWeight  int `yaml:"total_weight"`
}

func NewCanaryStrategy(rule CanaryRule) *CanaryStrategy {
	cs := &CanaryStrategy{rule: rule}
	if rule.Header != nil {
		cs.headerName = rule.Header.Name
		if cs.headerName == "" {
			cs.headerName = "X-Canary"
		}
	}
	if rule.Cookie != nil {
		cs.cookieName = rule.Cookie.Name
		if cs.cookieName == "" {
			cs.cookieName = "canary"
		}
	}
	return cs
}

func (cs *CanaryStrategy) Select(key string, backends []*Backend) (*Backend, error) {
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}
	if len(backends) == 1 {
		return backends[0], nil
	}

	canaryBackend := cs.findCanaryBackend(backends)
	stableBackend := cs.findStableBackend(backends)

	if canaryBackend == nil || stableBackend == nil {
		return backends[0], nil
	}

	if cs.rule.Header != nil {
		return cs.selectByHeader(key, canaryBackend, stableBackend)
	}
	if cs.rule.Cookie != nil {
		return cs.selectByCookie(key, canaryBackend, stableBackend)
	}
	if cs.rule.Weight != nil {
		return cs.selectByWeight(key, canaryBackend, stableBackend)
	}

	return stableBackend, nil
}

func (cs *CanaryStrategy) selectByHeader(key string, canary, stable *Backend) (*Backend, error) {
	if key == cs.rule.Header.Value {
		return canary, nil
	}
	return stable, nil
}

func (cs *CanaryStrategy) selectByCookie(key string, canary, stable *Backend) (*Backend, error) {
	cookieValue := extractCookieValue(key, cs.cookieName)
	if cookieValue == cs.rule.Cookie.Value {
		return canary, nil
	}
	return stable, nil
}

func (cs *CanaryStrategy) selectByWeight(key string, canary, stable *Backend) (*Backend, error) {
	h := fnv.New32a()
	h.Write([]byte(key))
	hashVal := h.Sum32()

	totalWeight := cs.rule.Weight.TotalWeight
	if totalWeight <= 0 {
		totalWeight = 100
	}
	canaryWeight := cs.rule.Weight.CanaryWeight
	if canaryWeight < 0 {
		canaryWeight = 0
	}

	threshold := uint32(uint64(canaryWeight) * uint64(^uint32(0)) / uint64(totalWeight))
	if hashVal < threshold {
		return canary, nil
	}
	return stable, nil
}

func (cs *CanaryStrategy) findCanaryBackend(backends []*Backend) *Backend {
	for _, b := range backends {
		if b.Meta != nil {
			if v, ok := b.Meta["canary"]; ok && (v == "true" || v == "1") {
				return b
			}
		}
	}
	if len(backends) > 1 {
		return backends[len(backends)-1]
	}
	return nil
}

func (cs *CanaryStrategy) findStableBackend(backends []*Backend) *Backend {
	for _, b := range backends {
		if b.Meta != nil {
			if v, ok := b.Meta["canary"]; ok && (v == "true" || v == "1") {
				continue
			}
		}
		return b
	}
	if len(backends) > 0 {
		return backends[0]
	}
	return nil
}

func (cs *CanaryStrategy) Name() StrategyType {
	return StrategyCanary
}

func extractCookieValue(cookieHeader, name string) string {
	cookies := strings.Split(cookieHeader, ";")
	for _, c := range cookies {
		c = strings.TrimSpace(c)
		if strings.HasPrefix(c, name+"=") {
			return strings.TrimPrefix(c, name+"=")
		}
	}
	return ""
}

func ParseCanaryRuleFromRoute(rc interface{}) (CanaryRule, error) {
	return CanaryRule{}, fmt.Errorf("not implemented")
}
