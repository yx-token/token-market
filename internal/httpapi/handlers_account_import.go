package httpapi

import (
	"fmt"
	"net/http"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/httpx"
)

func (s *Server) importUpstreamAccount(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.ImportAccountInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.ImportAccount(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	// 审计只记资源与厂商，不记录导入文件及令牌。
	s.audit(r, "upstream.import", fmt.Sprintf("upstream:%d", id), map[string]any{"platform": u.Platform})
	return ok(w, u)
}
