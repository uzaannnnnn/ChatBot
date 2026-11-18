package domain

const (
	// DefaultPageLimit is applied by services when no limit is requested.
	DefaultPageLimit = 20
	// MaxPageLimit caps the number of rows returned by a single list query.
	MaxPageLimit = 100
)

// Page describes offset-based pagination.
type Page struct {
	Limit  int
	Offset int
}

// Validate ensures the page is within accepted bounds.
// Limit may beMaxPageLimit+1 internally when services probe one extra row to compute has_more.
func (p Page) Validate() error {
	if p.Limit < 1 || p.Limit > MaxPageLimit || p.Offset < 0 {
		return ErrInvalidPagination
	}
	return nil
}

// ValidateQuery validates a repository query page where up to MaxPageLimit+1 rows
// may be fetched to determine whether an additional page exists.
func (p Page) ValidateQuery() error {
	if p.Limit < 1 || p.Limit > MaxPageLimit+1 || p.Offset < 0 {
		return ErrInvalidPagination
	}
	return nil
}
