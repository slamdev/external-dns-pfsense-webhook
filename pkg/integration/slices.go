package integration

import "fmt"

func UniqueSlice[T comparable](inputSlice []T) []T {
	uniqueSlice := make([]T, 0, len(inputSlice))
	seen := make(map[T]bool, len(inputSlice))
	for _, element := range inputSlice {
		if !seen[element] {
			uniqueSlice = append(uniqueSlice, element)
			seen[element] = true
		}
	}
	return uniqueSlice
}

func FilterSlice[T any](inputSlice []T, filterFunc func(T) bool) []T {
	filteredSlice := make([]T, 0)
	for _, element := range inputSlice {
		if filterFunc(element) {
			filteredSlice = append(filteredSlice, element)
		}
	}
	return filteredSlice
}

func MapSlice[T, V any](ts []T, fn func(T) V) []V {
	result := make([]V, len(ts))
	for i, t := range ts {
		result[i] = fn(t)
	}
	return result
}

func MapSliceErr[T, V any](ts []T, fn func(T) (V, error)) ([]V, error) {
	result := make([]V, len(ts))
	for i, t := range ts {
		var err error
		result[i], err = fn(t)
		if err != nil {
			return nil, fmt.Errorf("failed to map slice: %w", err)
		}
	}
	return result, nil
}
