package zhipu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const DerivedKeyName = "zcode-api-key"

var (
	ErrNoProject         = errors.New("zhipu: no eligible organization/project")
	ErrSecretUnavailable = errors.New("zhipu: derived API key secret unavailable")
)

type remoteID string

func (s *remoteID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*s = ""
		return nil
	}
	var value string
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &value); err != nil {
			return err
		}
	} else {
		var n json.Number
		if err := json.Unmarshal(b, &n); err != nil {
			return err
		}
		value = n.String()
	}
	*s = remoteID(strings.TrimSpace(value))
	return nil
}

type project struct {
	ID   remoteID `json:"projectId"`
	Name string   `json:"projectName"`
	Type remoteID `json:"projectType"`
}

type organization struct {
	ID       remoteID  `json:"organizationId"`
	Name     string    `json:"organizationName"`
	Projects []project `json:"projects"`
}

type DerivedKey struct {
	Credential     string
	APIKeyID       string
	OrganizationID string
	ProjectID      string
}

func pickProject(organizations []organization) (string, string, error) {
	var selected *organization
	var projects []project
	for i := range organizations {
		org := &organizations[i]
		if org.ID == "" {
			continue
		}
		var eligible []project
		for _, p := range org.Projects {
			if p.ID != "" && p.Type != "2" {
				eligible = append(eligible, p)
			}
		}
		if len(eligible) == 0 {
			continue
		}
		if selected == nil || strings.Contains(org.Name, "默认机构") {
			selected, projects = org, eligible
			if strings.Contains(org.Name, "默认机构") {
				break
			}
		}
	}
	if selected == nil {
		return "", "", ErrNoProject
	}
	p := projects[0]
	for _, candidate := range projects {
		if strings.Contains(candidate.Name, "默认项目") {
			p = candidate
			break
		}
	}
	return string(selected.ID), string(p.ID), nil
}

// DeriveKey always lists before creating, and requires a copyable id.secret on both providers.
func (c *Client) DeriveKey(ctx context.Context, provider Provider, accessToken string) (*DerivedKey, error) {
	origin, auth, err := c.business(provider, accessToken)
	if err != nil {
		return nil, err
	}
	var customer struct {
		Organizations []organization `json:"organizations"`
	}
	if err := c.do(ctx, http.MethodGet, origin+"/api/biz/customer/getCustomerInfo", auth, nil, "biz", &customer); err != nil {
		return nil, err
	}
	org, proj, err := pickProject(customer.Organizations)
	if err != nil {
		return nil, err
	}
	endpoint := origin + "/api/biz/v1/organization/" + url.PathEscape(org) + "/projects/" + url.PathEscape(proj) + "/api_keys"
	type key struct {
		Name   string `json:"name"`
		APIKey string `json:"apiKey"`
	}
	var keys []key
	if err := c.do(ctx, http.MethodGet, endpoint, auth, nil, "biz", &keys); err != nil {
		return nil, err
	}
	var selected key
	found := false
	for _, k := range keys {
		if k.Name == DerivedKeyName {
			selected = k
			found = true
			break
		}
	}
	if !found {
		if err := c.do(ctx, http.MethodPost, endpoint, auth, map[string]string{"name": DerivedKeyName}, "biz", &selected); err != nil {
			return nil, err
		}
	}
	id := strings.TrimSpace(selected.APIKey)
	if id == "" {
		return nil, ErrSecretUnavailable
	}
	var copied struct {
		SecretKey string `json:"secretKey"`
	}
	if err := c.do(ctx, http.MethodGet, endpoint+"/copy/"+url.PathEscape(id), auth, nil, "biz", &copied); err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(copied.SecretKey)
	if secret == "" {
		return nil, ErrSecretUnavailable
	}
	credential := id + "." + secret
	if _, _, err := SplitCredential(credential); err != nil {
		return nil, ErrSecretUnavailable
	}
	return &DerivedKey{credential, id, org, proj}, nil
}

func SplitCredential(credential string) (id, secret string, err error) {
	parts := strings.Split(credential, ".")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.ContainsAny(credential, "\r\n") {
		return "", "", errors.New("zhipu: signing credential must be id.secret")
	}
	return parts[0], parts[1], nil
}
