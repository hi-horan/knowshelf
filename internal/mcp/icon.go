package mcp

import (
	_ "embed"
	"encoding/base64"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed icon.svg
var iconData []byte

func serverIcons() []mcpsdk.Icon {
	return []mcpsdk.Icon{{
		Source:   "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(iconData),
		MIMEType: "image/svg+xml",
		Sizes:    []string{"any"},
		Theme:    mcpsdk.IconThemeLight,
	}}
}
