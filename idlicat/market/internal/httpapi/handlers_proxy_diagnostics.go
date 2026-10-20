package httpapi

import (
	"net/http"

	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

func (s *Server) checkProxy(sellerOnly, quality bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathID(r, "id")
		if err != nil {
			return err
		}
		var sellerID *int64
		if sellerOnly {
			uid := httpx.MustUser(r).UserID
			sellerID = &uid
		}
		p, err := s.Proxies.CheckProxy(r.Context(), sellerID, id, quality)
		if err != nil {
			return err
		}
		return ok(w, p)
	}
}
