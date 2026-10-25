package service

import (
	"errors"
	"net/http"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if account.isKimiOAuth() {
		return s.kimiTokenProvider.Do(request, account, func(req *http.Request) (*http.Response, error) {
			return s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
		})
	}
	if account.IsZhipuOAuth() {
		if s.zhipuGateway == nil {
			return nil, errors.New("zhipu gateway is not configured")
		}
		return s.zhipuGateway.Do(request, proxyURL, account)
	}
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if account.IsZhipuOAuth() || account.isKimiOAuth() {
		return s.doAccountTestUpstream(request, proxyURL, account)
	}
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}

func (s *AccountTestService) doAccountTestUpstream(req *http.Request, proxy string, a *Account) (*http.Response, error) {
	if a.isKimiOAuth() {
		return s.kimiTokenProvider.Do(req, a, func(r *http.Request) (*http.Response, error) { return s.httpUpstream.Do(r, proxy, a.ID, a.Concurrency) })
	}
	if a.IsZhipuOAuth() {
		if s.openaiGatewayService == nil || s.openaiGatewayService.zhipuGateway == nil {
			return nil, errors.New("zhipu gateway is not configured")
		}
		return s.openaiGatewayService.zhipuGateway.Do(req, proxy, a)
	}
	return s.httpUpstream.DoWithTLS(req, proxy, a.ID, a.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(a))
}
