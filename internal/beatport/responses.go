package beatport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrInvalidResponse = errors.New("invalid API response")

type catalogResource interface {
	Track | Release | Playlist | Chart | Label | Artist | PlaylistItem
}

func catalogResourceID(value any) int64 {
	switch value := value.(type) {
	case Track:
		return value.ID
	case Release:
		return value.ID
	case Playlist:
		return value.ID
	case Chart:
		return value.ID
	case Label:
		return value.ID
	case Artist:
		return value.ID
	case PlaylistItem:
		return value.Track.ID
	default:
		return 0
	}
}

func decodeCatalogJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode JSON: %w", ErrInvalidResponse, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return fmt.Errorf("%w: trailing JSON: %w", ErrInvalidResponse, err)
		}
		return fmt.Errorf("%w: expected one JSON document", ErrInvalidResponse)
	}
	return nil
}

func decodeCatalogResource[T catalogResource](reader io.Reader) (*T, error) {
	var response T
	if err := decodeCatalogJSON(reader, &response); err != nil {
		return nil, err
	}
	if catalogResourceID(response) <= 0 {
		return nil, fmt.Errorf("%w: %T requires a positive id", ErrInvalidResponse, response)
	}
	return &response, nil
}

func decodePaginationField[T int | string](data json.RawMessage, field string, target *T) error {
	if data == nil {
		return nil
	}
	var value *T
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%w: pagination %s: %w", ErrInvalidResponse, field, err)
	}
	if value == nil {
		return fmt.Errorf("%w: pagination %s must not be null", ErrInvalidResponse, field)
	}
	*target = *value
	return nil
}

func decodeCatalogPage[T catalogResource](reader io.Reader) (*Paginated[T], error) {
	var response Paginated[T]
	// Raw scalar fields distinguish omission from explicit null without decoding results twice.
	envelope := struct {
		*Paginated[T]
		Count   json.RawMessage `json:"count"`
		Page    json.RawMessage `json:"page"`
		PerPage json.RawMessage `json:"per_page"`
	}{Paginated: &response}
	if err := decodeCatalogJSON(reader, &envelope); err != nil {
		return nil, err
	}
	if err := decodePaginationField(envelope.Count, "count", &response.Count); err != nil {
		return nil, err
	}
	if err := decodePaginationField(envelope.Page, "page", &response.Page); err != nil {
		return nil, err
	}
	if err := decodePaginationField(envelope.PerPage, "per_page", &response.PerPage); err != nil {
		return nil, err
	}
	// An explicit empty array is valid; missing or null results are not a page.
	if response.Results == nil {
		return nil, fmt.Errorf("%w: results must be an array", ErrInvalidResponse)
	}
	if response.Count < 0 || response.PerPage < 0 {
		return nil, fmt.Errorf("%w: pagination counts must not be negative", ErrInvalidResponse)
	}
	for _, link := range []*string{response.Next, response.Previous} {
		if link != nil && strings.TrimSpace(*link) == "" {
			return nil, fmt.Errorf("%w: pagination links must be non-empty strings or null", ErrInvalidResponse)
		}
	}
	for i, value := range response.Results {
		if catalogResourceID(value) <= 0 {
			return nil, fmt.Errorf("%w: results[%d] requires a positive resource id", ErrInvalidResponse, i)
		}
	}
	return &response, nil
}
