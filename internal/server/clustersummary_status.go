package server

import (
	"errors"
	"fmt"
	"sort"
)

// getMapInRange extracts a subset of key-value pairs from the given map, skipping the first 'skip' pairs and then taking up to 'limit' pairs.
func getMapInRange[K comparable, V any](m map[K]V, limit, skip int) (map[K]V, error) {
	if skip < 0 {
		return nil, errors.New("skip cannot be negative")
	}
	if limit < 0 {
		return nil, errors.New("limit cannot be negative")
	}
	if skip >= len(m) {
		return nil, errors.New("skip cannot be greater than or equal to the length of the map")
	}

	// Extract keys and sort them
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprintf("%v", keys[i]) < fmt.Sprintf("%v", keys[j])
	})

	// Create a new map for the result
	result := make(map[K]V)

	// Iterate over the sorted keys and collect the desired key-value pairs
	for i := skip; i < skip+limit && i < len(keys); i++ {
		k := keys[i]
		result[k] = m[k]
	}

	return result, nil
}

func getProfileStatusesInRange(profileStatuses map[string][]ClusterFeatureSummary, limit, skip int) (map[string][]ClusterFeatureSummary, error) {
	return getMapInRange(profileStatuses, limit, skip)
}
