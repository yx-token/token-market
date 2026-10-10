package listing

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CategoryPack 系统标签：额度包挂单自动带上，卖家不可单独勾选/去掉。
const CategoryPack = "pack"

type Category struct {
	ID             string     `json:"id"`
	ParentID       *string    `json:"parent_id,omitempty"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Sort           int        `json:"sort"`
	Enabled        bool       `json:"enabled"`
	SellerPickable bool       `json:"seller_pickable"`
	Icon           string     `json:"icon,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Children       []Category `json:"children,omitempty"`
}

// CategoryOption 卖家勾选用的扁平选项（仅 seller_pickable 且启用）。
type CategoryOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	ParentID string `json:"parent_id,omitempty"`
	Path     string `json:"path,omitempty"`
}

const categoryColumns = `id, parent_id, name, description, sort, enabled, seller_pickable, icon, created_at, updated_at`

type scannable interface {
	Scan(dest ...any) error
}

func scanCategory(row scannable) (Category, error) {
	var c Category
	err := row.Scan(&c.ID, &c.ParentID, &c.Name, &c.Description, &c.Sort, &c.Enabled, &c.SellerPickable, &c.Icon, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func ListAllCategories(ctx context.Context, db DB) ([]Category, error) {
	rows, err := db.Query(ctx, `SELECT `+categoryColumns+` FROM market_categories ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CategoryTree 返回分类树；includeDisabled 时含停用节点（控制台）。
func CategoryTree(ctx context.Context, db DB, includeDisabled bool) ([]Category, error) {
	all, err := ListAllCategories(ctx, db)
	if err != nil {
		return nil, err
	}
	byParent := map[string][]Category{}
	var roots []Category
	for _, c := range all {
		if !includeDisabled && !c.Enabled {
			continue
		}
		if c.ParentID == nil || *c.ParentID == "" {
			roots = append(roots, c)
			continue
		}
		byParent[*c.ParentID] = append(byParent[*c.ParentID], c)
	}
	var attach func(Category) Category
	attach = func(n Category) Category {
		kids := byParent[n.ID]
		n.Children = make([]Category, 0, len(kids))
		for _, k := range kids {
			n.Children = append(n.Children, attach(k))
		}
		return n
	}
	out := make([]Category, 0, len(roots))
	for _, r := range roots {
		out = append(out, attach(r))
	}
	return out, nil
}

func GetCategory(ctx context.Context, db DB, id string) (*Category, error) {
	c, err := scanCategory(db.QueryRow(ctx, `SELECT `+categoryColumns+` FROM market_categories WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("CATEGORY_NOT_FOUND", "category not found")
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SellerCategoryOptions 卖家可勾选标签（扁平，带路径名）。
func SellerCategoryOptions(ctx context.Context, db DB) ([]CategoryOption, error) {
	all, err := ListAllCategories(ctx, db)
	if err != nil {
		return nil, err
	}
	byID := map[string]Category{}
	for _, c := range all {
		byID[c.ID] = c
	}
	var pathOf func(string) string
	pathOf = func(id string) string {
		c, ok := byID[id]
		if !ok {
			return id
		}
		if c.ParentID == nil || *c.ParentID == "" {
			return c.Name
		}
		return pathOf(*c.ParentID) + " / " + c.Name
	}
	var out []CategoryOption
	for _, c := range all {
		if !c.Enabled || !c.SellerPickable {
			continue
		}
		parent := ""
		if c.ParentID != nil {
			parent = *c.ParentID
		}
		out = append(out, CategoryOption{
			ID: c.ID, Name: c.Name, Desc: c.Description, ParentID: parent, Path: pathOf(c.ID),
		})
	}
	return out, nil
}

func sellerPickableSet(ctx context.Context, db DB) (map[string]bool, error) {
	opts, err := SellerCategoryOptions(ctx, db)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(opts))
	for _, o := range opts {
		m[o.ID] = true
	}
	return m, nil
}

// NormalizeCategories 校验卖家勾选（须为启用且可勾选），并按 mode 自动补 pack。
func NormalizeCategories(mode string, in []string, allowed map[string]bool) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in)+1)
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" || c == CategoryPack {
			continue
		}
		if !allowed[c] {
			return nil, httpx.BadRequest("INVALID_CATEGORY", "unknown or non-pickable market category: "+c)
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	if mode == ModePack {
		out = append(out, CategoryPack)
	}
	slices.Sort(out)
	return out, nil
}

func HasCategory(cats []string, id string) bool {
	return slices.Contains(cats, id)
}

func requireSellerCategory(cats []string) error {
	for _, c := range cats {
		if c != CategoryPack {
			return nil
		}
	}
	return httpx.BadRequest("CATEGORY_REQUIRED", "请至少选择一个市场分类标签")
}

type CategoryInput struct {
	ID             string  `json:"id"`
	ParentID       *string `json:"parent_id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Sort           *int    `json:"sort"`
	Enabled        *bool   `json:"enabled"`
	SellerPickable *bool   `json:"seller_pickable"`
	Icon           *string `json:"icon"`
}

func CreateCategory(ctx context.Context, db DB, in CategoryInput) (*Category, error) {
	id := strings.TrimSpace(in.ID)
	name := strings.TrimSpace(in.Name)
	if id == "" || name == "" {
		return nil, httpx.BadRequest("INVALID_CATEGORY", "id and name are required")
	}
	if id == CategoryPack {
		return nil, httpx.BadRequest("RESERVED_ID", "id 'pack' is reserved for system use")
	}
	if !validCategoryID(id) {
		return nil, httpx.BadRequest("INVALID_ID", "id must be lowercase letters, digits, underscore")
	}
	var parent *string
	if in.ParentID != nil {
		p := strings.TrimSpace(*in.ParentID)
		if p != "" {
			if _, err := GetCategory(ctx, db, p); err != nil {
				return nil, err
			}
			parent = &p
		}
	}
	sort := 0
	if in.Sort != nil {
		sort = *in.Sort
	}
	enabled, pickable := true, false
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.SellerPickable != nil {
		pickable = *in.SellerPickable
	}
	icon := ""
	if in.Icon != nil {
		icon = strings.TrimSpace(*in.Icon)
	}
	c, err := scanCategory(db.QueryRow(ctx, `INSERT INTO market_categories
		(id, parent_id, name, description, sort, enabled, seller_pickable, icon)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+categoryColumns, id, parent, name, strings.TrimSpace(in.Description), sort, enabled, pickable, icon))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, httpx.Conflict("CATEGORY_EXISTS", "category id already exists")
		}
		return nil, err
	}
	return &c, nil
}

func UpdateCategory(ctx context.Context, db DB, id string, in CategoryInput) (*Category, error) {
	cur, err := GetCategory(ctx, db, id)
	if err != nil {
		return nil, err
	}
	name := cur.Name
	if strings.TrimSpace(in.Name) != "" {
		name = strings.TrimSpace(in.Name)
	}
	desc := strings.TrimSpace(in.Description)
	sort := cur.Sort
	if in.Sort != nil {
		sort = *in.Sort
	}
	enabled := cur.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	pickable := cur.SellerPickable
	if in.SellerPickable != nil {
		pickable = *in.SellerPickable
	}
	icon := cur.Icon
	if in.Icon != nil {
		icon = strings.TrimSpace(*in.Icon)
	}
	parent := cur.ParentID
	if in.ParentID != nil {
		p := strings.TrimSpace(*in.ParentID)
		if p == "" {
			parent = nil
		} else {
			if p == id {
				return nil, httpx.BadRequest("INVALID_PARENT", "category cannot be its own parent")
			}
			if _, err := GetCategory(ctx, db, p); err != nil {
				return nil, err
			}
			if err := assertNotDescendant(ctx, db, id, p); err != nil {
				return nil, err
			}
			parent = &p
		}
	}
	c, err := scanCategory(db.QueryRow(ctx, `UPDATE market_categories SET
		parent_id = $2, name = $3, description = $4, sort = $5, enabled = $6, seller_pickable = $7,
		icon = $8, updated_at = now()
		WHERE id = $1 RETURNING `+categoryColumns,
		id, parent, name, desc, sort, enabled, pickable, icon))
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func DeleteCategory(ctx context.Context, db DB, id string) error {
	if id == CategoryPack {
		return httpx.BadRequest("RESERVED_ID", "cannot delete system pack tag")
	}
	var kids int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM market_categories WHERE parent_id = $1`, id).Scan(&kids); err != nil {
		return err
	}
	if kids > 0 {
		return httpx.Conflict("HAS_CHILDREN", "请先删除或移走子分类")
	}
	var used int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM listings WHERE $1 = ANY (COALESCE(categories, '{}')) AND closed_at IS NULL`, id).Scan(&used); err != nil {
		return err
	}
	if used > 0 {
		return httpx.Conflict("IN_USE", "仍有挂单使用该分类，请先停用或改挂单标签")
	}
	tag, err := db.Exec(ctx, `DELETE FROM market_categories WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("CATEGORY_NOT_FOUND", "category not found")
	}
	return nil
}

func assertNotDescendant(ctx context.Context, db DB, ancestor, node string) error {
	cur := node
	for i := 0; i < 64; i++ {
		if cur == ancestor {
			return httpx.BadRequest("INVALID_PARENT", "cannot move category under its descendant")
		}
		var parent *string
		err := db.QueryRow(ctx, `SELECT parent_id FROM market_categories WHERE id = $1`, cur).Scan(&parent)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if parent == nil || *parent == "" {
			return nil
		}
		cur = *parent
	}
	return httpx.BadRequest("INVALID_PARENT", "category tree too deep")
}

func validCategoryID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
