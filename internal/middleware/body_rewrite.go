package middleware

import (
	"net/http"
	"regexp"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/util"
)

type RewriteRule struct {
	Pattern     string
	Replacement string
}

type HeaderRewrite struct {
	Set   map[string]string
	Add   map[string]string
	Remove []string
}

type BodyRewriteConfig struct {
	RequestHeader  HeaderRewrite
	ResponseHeader HeaderRewrite
	RequestBody    []RewriteRule
	ResponseBody   []RewriteRule
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	return util.CompileRegex(pattern)
}

func BodyRewrite(cfg BodyRewriteConfig) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			applyHeaderRewrite(req.Headers, cfg.RequestHeader)

			if len(cfg.RequestBody) > 0 && len(req.Body) > 0 {
				body := string(req.Body)
				for _, rule := range cfg.RequestBody {
					re, err := compilePattern(rule.Pattern)
					if err != nil {
						continue
					}
					body = re.ReplaceAllString(body, rule.Replacement)
				}
				req.Body = []byte(body)
			}

			resp, err := next(req)
			if err != nil {
				return resp, err
			}

			if resp != nil {
				applyHeaderRewrite(resp.Headers, cfg.ResponseHeader)

				if len(cfg.ResponseBody) > 0 && len(resp.Body) > 0 {
					body := string(resp.Body)
					for _, rule := range cfg.ResponseBody {
						re, err := compilePattern(rule.Pattern)
						if err != nil {
							continue
						}
						body = re.ReplaceAllString(body, rule.Replacement)
					}
					resp.Body = []byte(body)
				}
			}

			return resp, nil
		}
	}
}

func applyHeaderRewrite(headers http.Header, rw HeaderRewrite) {
	if headers == nil {
		return
	}

	for _, key := range rw.Remove {
		headers.Del(key)
	}

	for key, value := range rw.Set {
		headers.Set(key, value)
	}

	for key, value := range rw.Add {
		headers.Add(key, value)
	}
}
