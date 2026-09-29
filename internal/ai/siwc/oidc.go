package siwc

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type discovery struct {
	Issuer    string `json:"issuer"`
	Authorize string `json:"authorization_endpoint"`
	Token     string `json:"token_endpoint"`
	Revoke    string `json:"revocation_endpoint"`
	JWKS      string `json:"jwks_uri"`
}
type identity struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Nonce string `json:"nonce"`
}

// Endpoints must remain on the trusted issuer; redirects are never followed.
func (m *Manager) discovery(ctx context.Context) (discovery, error) {
	var d discovery
	if err := m.getJSON(ctx, m.issuer+"/.well-known/openid-configuration", "", &d); err != nil {
		return d, err
	}
	if d.Issuer != m.issuer {
		return d, fmt.Errorf("ChatGPT issuer mismatch")
	}
	for _, endpoint := range []string{d.Authorize, d.Token, d.Revoke, d.JWKS} {
		u, err := url.Parse(endpoint)
		base, _ := url.Parse(m.issuer)
		if err != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
			return d, fmt.Errorf("untrusted ChatGPT OAuth endpoint")
		}
	}
	return d, nil
}
func (m *Manager) verifyID(ctx context.Context, raw, client, nonce string, allowExpired bool) (identity, error) {
	var claims identity
	d, err := m.discovery(ctx)
	if err != nil {
		return claims, err
	}
	// Fetching keys for each exchange also handles key rotation without stale keys.
	var keys struct {
		Keys []struct{ Kty, Use, Alg, Kid, N, E string }
	}
	if err = m.getJSON(ctx, d.JWKS, "", &keys); err != nil {
		return claims, err
	}
	options := []jwt.ParserOption{jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(m.issuer), jwt.WithAudience(client), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(5 * time.Second)}
	if allowExpired {
		options = append(options, jwt.WithoutClaimsValidation())
	}
	_, err = jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		for _, key := range keys.Keys {
			if kid == "" || key.Kid != kid || key.Kty != "RSA" || key.Use != "" && key.Use != "sig" || key.Alg != "" && key.Alg != "RS256" {
				continue
			}
			n, e1 := base64.RawURLEncoding.DecodeString(key.N)
			eb, e2 := base64.RawURLEncoding.DecodeString(key.E)
			if e1 != nil || e2 != nil || len(n) < 256 || len(eb) > 4 || len(eb) == 0 {
				break
			}
			e := 0
			for _, b := range eb {
				e = e<<8 | int(b)
			}
			if e < 3 || e%2 == 0 {
				break
			}
			return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}, nil
		}
		return nil, fmt.Errorf("unknown signing key")
	}, options...)
	if err != nil || claims.Subject == "" || claims.Issuer != m.issuer || !contains(claims.Audience, client) || claims.ExpiresAt == nil || claims.IssuedAt == nil || nonce != "" && claims.Nonce != nonce {
		return identity{}, fmt.Errorf("ChatGPT identity validation failed")
	}
	return claims, nil
}
func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}

// ProviderError keeps admission shape, status, code, parameter and request ID.
// Raw diagnostic bodies are private and never included in Error() or logs.
type ProviderError struct {
	Status     int             `json:"status"`
	Code       string          `json:"code,omitempty"`
	Param      string          `json:"param,omitempty"`
	RequestID  string          `json:"request_id,omitempty"`
	BodyShape  string          `json:"body_shape,omitempty"`
	Body       json.RawMessage `json:"-"`
	RetryAfter string          `json:"retry_after,omitempty"`
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("ChatGPT request failed (HTTP %d, code %s)", e.Status, e.Code)
}
func (e *ProviderError) Message() string {
	switch e.Code {
	case "subscription_sharing_usage_limit_exceeded":
		return "ChatGPT 套餐使用达到限制，已暂停此通道。请到 ChatGPT 设置 → 用量管理额度，调整后恢复。"
	case "subscription_sharing_user_not_eligible":
		return "此 ChatGPT 账户或工作区暂不允许套餐授权。"
	case "subscription_sharing_unsupported_capability":
		return "ChatGPT 套餐通道不支持本次请求的参数或模型。"
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return "ChatGPT 授权已失效，请重新登录。"
	case "invalid_client":
		return "ChatGPT 客户端注册无效，请检查注册配置。"
	case "stream_incomplete":
		return "ChatGPT 响应没有完成，部分输出未保存。"
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		return "暂时无法核验 ChatGPT 套餐用量，请稍后重试。"
	}
	if e.Status == 401 {
		return "ChatGPT 授权未被接受，请检查连接账户和套餐权限。"
	}
	if e.Status == 403 {
		return "ChatGPT 套餐权限或服务地区限制了此次请求。"
	}
	return "ChatGPT 服务暂不可用，请稍后重试。"
}
func responseError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	e := &ProviderError{Status: resp.StatusCode, RequestID: requestID(resp), Body: json.RawMessage(b), RetryAfter: resp.Header.Get("Retry-After")}
	var value struct {
		Error  json.RawMessage `json:"error"`
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(b, &value) == nil {
		if len(value.Error) > 0 {
			e.BodyShape = "error"
			if json.Unmarshal(value.Error, &e.Code) != nil {
				var detail struct{ Code, Param string }
				_ = json.Unmarshal(value.Error, &detail)
				e.Code, e.Param = detail.Code, detail.Param
			}
		} else if len(value.Detail) > 0 {
			e.BodyShape = "detail"
		}
	}
	return e
}
func requestID(resp *http.Response) string {
	if id := resp.Header.Get("x-request-id"); id != "" {
		return id
	}
	return resp.Header.Get("openai-request-id")
}
func (m *Manager) getJSON(ctx context.Context, endpoint, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("ChatGPT service unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return responseError(resp)
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out) != nil {
		return fmt.Errorf("invalid ChatGPT service response")
	}
	return nil
}

type tokenResponse struct {
	Access   string `json:"access_token"`
	Refresh  string `json:"refresh_token"`
	ID       string `json:"id_token"`
	Type     string `json:"token_type"`
	Scope    string `json:"scope"`
	Expires  int64  `json:"expires_in"`
	Earliest int64  `json:"earliest_refresh_at"`
}

func (m *Manager) exchange(ctx context.Context, endpoint string, form url.Values) (tokenResponse, error) {
	var t tokenResponse
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.http.Do(req)
	if err != nil {
		return t, fmt.Errorf("ChatGPT token service unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return t, responseError(resp)
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&t) != nil {
		return t, fmt.Errorf("invalid ChatGPT token response")
	}
	if t.Access != "" && (!strings.EqualFold(t.Type, "Bearer") || t.Expires <= 0 || t.Expires > 86400) {
		return t, fmt.Errorf("invalid ChatGPT token lifetime or type")
	}
	return t, nil
}
