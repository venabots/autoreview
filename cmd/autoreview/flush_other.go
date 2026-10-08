//go:build !darwin && !linux

package main

func flushInput(int) error {
	return nil
}
