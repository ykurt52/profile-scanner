//go:build !windows

package config

import "errors"

// No encryption outside Windows, so secrets are never saved.

func protect(in []byte) ([]byte, error)   { return nil, errors.New("not supported") }
func unprotect(in []byte) ([]byte, error) { return nil, errors.New("not supported") }
