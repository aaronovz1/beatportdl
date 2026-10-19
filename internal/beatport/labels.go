package beatport

import (
	"fmt"
	"strconv"
	"time"
)

type Label struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Slug    string    `json:"slug"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

func (l *Label) DirectoryName(n NamingPreferences) string {
	templateValues := map[string]string{
		"id":           strconv.Itoa(int(l.ID)),
		"name":         SanitizeForPath(l.Name),
		"slug":         l.Slug,
		"created_date": l.Created.Format("2006-01-02"),
		"updated_date": l.Updated.Format("2006-01-02"),
	}
	directoryName := ParseTemplate(n.Template, templateValues)
	return SanitizeDirectoryPath(directoryName, n.Whitespace)
}

func (l *Label) StoreUrl() string {
	return storeUrl(l.ID, "label", l.Slug)
}

func (b *Beatport) GetLabel(id int64) (*Label, error) {
	res, err := b.fetch(
		"GET",
		fmt.Sprintf("/catalog/labels/%d/", id),
		nil,
		"",
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return decodeCatalogResource[Label](res.Body)
}

func (b *Beatport) GetLabelReleases(id int64, page int, params string) (*Paginated[Release], error) {
	res, err := b.fetch(
		"GET",
		fmt.Sprintf("/catalog/labels/%d/releases/?page=%d&%s", id, page, params),
		nil,
		"",
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return decodeCatalogPage[Release](res.Body)
}

func (b *Beatport) GetLabelTracks(id int64, page int, params string) (*Paginated[Track], error) {
	return b.getCatalogTracks("label_id", id, page, params)
}
