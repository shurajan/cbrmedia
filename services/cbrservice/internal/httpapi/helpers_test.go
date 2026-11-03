package httpapi

import (
	"io"
	"log/slog"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))
