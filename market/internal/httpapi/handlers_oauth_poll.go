package httpapi

import (
	"net/http"
	"strings"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

type oauthCompleteInput struct {
	Code        string `json:"code"`
	CallbackURL string `json:"callback_url"`
}

func decodeOAuth(r *http.Request, value any) error {
	if err := httpx.Decode(r, value); err != nil {
		return httpx.BadRequest("INVALID_JSON", "invalid authorization request body")
	}
	return nil
}

func (in oauthCompleteInput) value() (string, error) {
	code, callback := strings.TrimSpace(in.Code), strings.TrimSpace(in.CallbackURL)
	if code != "" && callback != "" {
		return "", httpx.BadRequest("INVALID_CALLBACK", "provide either code or callback_url, not both")
	}
	if callback != "" {
		return callback, nil
	}
	return code, nil
}

func (s *Server) pollOAuth(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if r.ContentLength != 0 {
		if err := decodeOAuth(r, &struct{}{}); err != nil {
			return err
		}
	}
	result, err := s.Listings.PollAuthorization(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, result)
}

func (s *Server) adminPollUpstreamOAuth(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if r.ContentLength != 0 {
		if err := decodeOAuth(r, &struct{}{}); err != nil {
			return err
		}
	}
	result, err := s.Listings.AdminPollAuthorization(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, result)
}
