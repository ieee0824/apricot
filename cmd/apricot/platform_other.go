//go:build !darwin

package main

func macOSProductVersion() string {
	return ""
}
