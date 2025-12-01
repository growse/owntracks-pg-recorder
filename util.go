package main

import "slices"

func stringSliceContains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
