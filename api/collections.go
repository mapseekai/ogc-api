package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
	"github.com/mapseekai/ogc-api/response"
)

// CollectionQuery is a bounded, normalized directory page request. Offset is a
// provider position, not a database/keyset token or an authorization credential.
type CollectionQuery struct {
	Limit  uint64
	Offset uint64
}

// CollectionPage contains only authorized public metadata. NextOffset explicitly
// continues the scan, including after an empty permission-filtered page. It must
// advance; nil means terminal. PreviousOffset, when supplied, must go backwards.
// NumberMatched is optional and counts visible collections, never hidden rows.
// Returned metadata/position pointers transfer ownership; retain no mutable aliases.
type CollectionPage struct {
	Collections    []common.Collection
	NextOffset     *uint64
	PreviousOffset *uint64
	NumberMatched  *uint64
}

// CollectionMetadataProvider reads a page or one description without listing the
// whole directory. Scope is trusted host context and must never be put in links.
// IDs/capabilities remain registered in the authorized Snapshot independently of
// page contents. The provider must belong to that same immutable visibility view.
// Rich descriptions can be loaded lazily: registration requires only IDs and
// capability bindings. New IDs/capabilities require publishing a new Snapshot.
// Implementations honor cancellation and protect shared state. Across requests,
// ordering/data may change; the protocol does not promise a database snapshot.
type CollectionMetadataProvider interface {
	Collections(context.Context, string, CollectionQuery) (CollectionPage, error)
	Collection(context.Context, string, string) (common.Collection, error)
}

// CollectionPaging opts into bounded limit/offset directory responses. Zero
// limits select 10/1000. Unlike feature limits, overlarge directory limits are
// rejected. Nil Provider pages the already-published static metadata without I/O.
// A non-nil Provider reads only the requested page/description; it cannot expand
// the Snapshot's route/authorization set or change its bound capabilities.
type CollectionPaging struct {
	DefaultLimit uint64
	MaxLimit     uint64
	Provider     CollectionMetadataProvider
}

func (p CollectionPaging) prepare() (CollectionPaging, error) {
	if p.DefaultLimit == 0 {
		p.DefaultLimit = 10
	}
	if p.MaxLimit == 0 {
		p.MaxLimit = 1000
	}
	if p.DefaultLimit > p.MaxLimit {
		return CollectionPaging{}, errors.New("directory default limit exceeds maximum")
	}
	return p, nil
}

func (e *Engine) collectionList(ctx context.Context, input Input, snapshot *Snapshot, values common.Query) (*response.Plan, string, error) {
	const operation = "collections.list"
	if input.Method != "GET" && input.Method != "HEAD" {
		return methodPlan("GET, HEAD"), operation, nil
	}
	if err := values.Only("f", "limit", "offset"); err != nil {
		return nil, operation, err
	}
	if _, err := common.Negotiate(input.Accept, values["f"], []string{common.JSON}); err != nil {
		return nil, operation, err
	}
	policy := snapshot.collectionPaging
	limit, err := values.Integer("limit", policy.DefaultLimit, 1, policy.MaxLimit)
	if err != nil {
		return nil, operation, err
	}
	offset, err := values.Integer("offset", 0, 0, math.MaxUint64)
	if err != nil {
		return nil, operation, err
	}
	query := CollectionQuery{Limit: limit, Offset: offset}
	var page CollectionPage
	if policy.Provider != nil {
		page, err = policy.Provider.Collections(ctx, input.Scope, query)
		if err != nil {
			return nil, operation, err
		}
	} else {
		page = snapshot.staticCollectionPage(query)
	}
	if uint64(len(page.Collections)) > limit || page.NextOffset != nil && *page.NextOffset <= offset || page.PreviousOffset != nil && *page.PreviousOffset >= offset || page.NumberMatched != nil && (*page.NumberMatched < uint64(len(page.Collections)) || *page.NumberMatched > uint64(len(snapshot.collections))) {
		return nil, operation, errors.New("directory provider returned invalid page metadata")
	}
	if err := jsonutil.CheckSize(page.Collections, e.config.Limits.MaxDocumentBytes); err != nil {
		return nil, operation, err
	}
	items := make([]common.Collection, 0, len(page.Collections))
	seen := make(map[string]bool, len(page.Collections))
	for _, metadata := range page.Collections {
		if seen[metadata.ID] {
			return nil, operation, errors.New("directory provider returned a duplicate collection")
		}
		seen[metadata.ID] = true
		if policy.Provider != nil {
			metadata, err = snapshot.pageMetadata(metadata, e.config.Limits.MaxDocumentBytes)
			if err != nil {
				return nil, operation, err
			}
		}
		items = append(items, metadata)
	}
	parameters := url.Values{}
	if value, ok := values["f"]; ok {
		parameters.Set("f", value)
	}
	parameters.Set("limit", strconv.FormatUint(limit, 10))
	parameters.Set("offset", strconv.FormatUint(offset, 10))
	encoded := parameters.Encode()
	if len(encoded) > e.config.Limits.MaxQueryBytes {
		return nil, operation, errors.New("directory self link exceeds query limit")
	}
	links := []common.Link{{Href: snapshot.baseURL + "/collections?" + encoded, Rel: "self", Type: common.JSON}}
	for _, continuation := range []struct {
		rel    string
		offset *uint64
	}{{"next", page.NextOffset}, {"prev", page.PreviousOffset}} {
		if continuation.offset != nil {
			parameters.Set("offset", strconv.FormatUint(*continuation.offset, 10))
			encoded := parameters.Encode()
			if len(encoded) > e.config.Limits.MaxQueryBytes {
				return nil, operation, errors.New("directory continuation link exceeds query limit")
			}
			links = append(links, common.Link{Href: snapshot.baseURL + "/collections?" + encoded, Rel: continuation.rel, Type: common.JSON})
		}
	}
	body := struct {
		Collections    []common.Collection `json:"collections"`
		Links          []common.Link       `json:"links"`
		NumberMatched  *uint64             `json:"numberMatched,omitempty"`
		NumberReturned uint64              `json:"numberReturned"`
	}{items, links, page.NumberMatched, uint64(len(items))}
	plan, err := e.scopedMetadata(input, values, body)
	return plan, operation, err
}

func (e *Engine) collectionDescription(ctx context.Context, input Input, snapshot *Snapshot, id string, values common.Query) (*response.Plan, string, error) {
	const operation = "collections.description"
	if _, ok := snapshot.collections[id]; !ok {
		return nil, operation, common.ErrNotFound
	}
	if input.Method != "GET" && input.Method != "HEAD" {
		return methodPlan("GET, HEAD"), operation, nil
	}
	if err := values.Only("f"); err != nil {
		return nil, operation, err
	}
	if _, err := common.Negotiate(input.Accept, values["f"], []string{common.JSON}); err != nil {
		return nil, operation, err
	}
	metadata, err := snapshot.collectionPaging.Provider.Collection(ctx, input.Scope, id)
	if err != nil {
		return nil, operation, err
	}
	if metadata.ID != id {
		return nil, operation, errors.New("directory provider returned a different collection")
	}
	metadata, err = snapshot.pageMetadata(metadata, e.config.Limits.MaxDocumentBytes)
	if err != nil {
		return nil, operation, err
	}
	plan, err := e.scopedMetadata(input, values, metadata)
	return plan, operation, err
}

func (s *Snapshot) staticCollectionPage(query CollectionQuery) CollectionPage {
	total := uint64(len(s.collectionDescriptions))
	page := CollectionPage{Collections: []common.Collection{}, NumberMatched: &total}
	if query.Offset < total {
		end := query.Offset + min(query.Limit, total-query.Offset)
		page.Collections = s.collectionDescriptions[query.Offset:end]
		if end < total {
			page.NextOffset = &end
		}
	}
	if query.Offset > 0 {
		previous := query.Offset - min(query.Offset, query.Limit)
		page.PreviousOffset = &previous
	}
	return page
}

func (s *Snapshot) pageMetadata(metadata common.Collection, budget int) (common.Collection, error) {
	binding, ok := s.collections[metadata.ID]
	if !ok {
		return common.Collection{}, errors.New("directory provider returned an unpublished collection")
	}
	if err := checkCollection(metadata); err != nil {
		return common.Collection{}, err
	}
	metadata, err := copyMetadata(metadata, budget)
	if err != nil {
		return common.Collection{}, err
	}
	if binding.features != nil {
		if err := prepareFeatureCRS(&metadata, binding.features.Options); err != nil {
			return common.Collection{}, err
		}
		metadata.ItemType = "feature"
	}
	// Routing/capability links come from the same authorized snapshot, not the
	// current list page. Public non-routing metadata links can supplement them.
	generated := make(map[string]bool)
	for _, link := range binding.metadata.Links {
		generated[link.Rel] = true
	}
	links := make([]common.Link, 0, len(metadata.Links)+len(binding.metadata.Links))
	for _, link := range metadata.Links {
		if !generated[link.Rel] && link.Rel != "items" && link.Rel != "self" && link.Rel != "parent" && !strings.HasPrefix(link.Rel, "http://www.opengis.net/def/rel/ogc/1.0/") {
			links = append(links, link)
		}
	}
	metadata.Links = append(links, binding.metadata.Links...)
	return metadata, nil
}

func (e *Engine) scopedMetadata(input Input, values common.Query, value any) (*response.Plan, error) {
	data, err := jsonutil.Marshal(value, e.config.Limits.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	parameters := url.Values{}
	for key, value := range values {
		parameters.Set(key, value)
	}
	hash := sha256.New()
	for _, part := range [][]byte{[]byte(input.Scope), []byte(input.EscapedPath), []byte(parameters.Encode()), data} {
		_, _ = hash.Write([]byte(strconv.Itoa(len(part)) + ":"))
		_, _ = hash.Write(part)
	}
	payload := response.Bytes(common.JSON, data)
	payload.Cache.ETag = `"` + hex.EncodeToString(hash.Sum(nil)) + `"`
	payload.Cache.CacheControl = "private, no-cache"
	return response.New(200, payload), nil
}
