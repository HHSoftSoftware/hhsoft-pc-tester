//go:build !linux

package reporttarget

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
)

func discoverPlatform(context.Context, Options) Catalog {
	return Catalog{Schema: CatalogSchema, Status: StatusInconclusive, Detail: "report targets require Linux block topology"}
}

func authorizePlatform(context.Context, Options, AuthorizationRequest) (Authorization, error) {
	return Authorization{}, errors.New("report-target authorization is unavailable on this platform")
}

func persistPlatform(context.Context, Options, Authorization, *os.File, int64, string, [sha256.Size]byte) (Receipt, error) {
	return Receipt{}, errors.New("report persistence is unavailable on this platform")
}
