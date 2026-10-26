//go:build windows

package config

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows DPAPI: data can only be decrypted by the same Windows user.

func protect(in []byte) ([]byte, error) {
	var out windows.DataBlob
	blob := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	err := windows.CryptProtectData(&blob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func unprotect(in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, nil
	}
	var out windows.DataBlob
	blob := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	err := windows.CryptUnprotectData(&blob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
