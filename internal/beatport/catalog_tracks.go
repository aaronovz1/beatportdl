package beatport

import (
	"fmt"
	"net/url"
	"strconv"
)

func (b *Beatport) getCatalogTracks(scope string, id int64, page int, params string) (*Paginated[Track], error) {
	query, err := url.ParseQuery(params)
	if err != nil {
		return nil, fmt.Errorf("parse catalog filters: %w", err)
	}
	query.Set(scope, strconv.FormatInt(id, 10))
	query.Set("page", strconv.Itoa(page))
	res, err := b.fetch("GET", "/catalog/tracks/?"+query.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return decodeCatalogPage[Track](res.Body)
}
